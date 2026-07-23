package github

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---- Fake transport implementation ----

// fakeResponse describes a single response to return.
type fakeResponse struct {
	statusCode int
	headers    map[string]string
	body       string
}

// fakeTransport is a test HTTP transport that returns canned responses.
// It supports multiple calls per URL, recording all requests made.
type fakeTransport struct {
	mu        sync.Mutex
	responses map[string][]fakeResponse // URL -> ordered responses (consumed in order)
	callCount map[string]int
	calls     []*http.Request
	nextSeq   int // for auto-generating sequence keys
}

// newFakeTransport creates a fakeTransport ready for use.
func newFakeTransport() *fakeTransport {
	return &fakeTransport{
		responses: make(map[string][]fakeResponse),
		callCount: make(map[string]int),
	}
}

// addResponse registers a response for a given URL pattern.
// If the URL pattern ends with *, all URLs starting with that prefix match.
func (f *fakeTransport) addResponse(url string, resp fakeResponse) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.responses[url] = append(f.responses[url], resp)
}

// addSequentialResponses registers multiple responses for the same URL.
func (f *fakeTransport) addSequentialResponses(url string, resp ...fakeResponse) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.responses[url] = append(f.responses[url], resp...)
}

// findResponse finds a matching response for a URL.
func (f *fakeTransport) findResponse(urlStr string) (fakeResponse, bool) {
	// Exact match first
	if resp, ok := f.nextResponse(urlStr); ok {
		return resp, true
	}

	// Prefix match (for wildcard patterns)
	for pattern := range f.responses {
		if strings.HasSuffix(pattern, "*") {
			prefix := strings.TrimSuffix(pattern, "*")
			if strings.HasPrefix(urlStr, prefix) {
				if resp, ok := f.nextResponse(pattern); ok {
					return resp, true
				}
			}
		}
	}

	return fakeResponse{}, false
}

// nextResponse gets the next response for a key, cycling through sequential responses.
func (f *fakeTransport) nextResponse(key string) (fakeResponse, bool) {
	responses, ok := f.responses[key]
	if !ok || len(responses) == 0 {
		return fakeResponse{}, false
	}

	count := f.callCount[key]
	f.callCount[key]++

	// Return the last response if we've exhausted the list
	if count >= len(responses) {
		return responses[len(responses)-1], true
	}
	return responses[count], true
}

// Do implements the transport interface.
func (f *fakeTransport) Do(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	f.calls = append(f.calls, req)
	f.mu.Unlock()

	resp, ok := f.findResponse(req.URL.String())
	if !ok {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	}

	header := make(http.Header)
	for k, v := range resp.headers {
		header.Set(k, v)
	}

	// Add rate-limit headers by default if not specified
	if header.Get("X-RateLimit-Remaining") == "" {
		header.Set("X-RateLimit-Remaining", "4999")
		header.Set("X-RateLimit-Reset", "0")
	}

	return &http.Response{
		StatusCode: resp.statusCode,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(resp.body)),
	}, nil
}

// callCountFor returns how many times a specific URL was called.
func (f *fakeTransport) callCountFor(url string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.callCount[url]
}

// resetCallCount resets the call counter for all URLs.
func (f *fakeTransport) resetCallCount() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.callCount = make(map[string]int)
	f.calls = nil
}

// helper to create a test client with a fake transport.
func testClient(fake *fakeTransport) *githubClient {
	if fake == nil {
		fake = newFakeTransport()
	}
	return newClient(fake, 500)
}

// ---- Tests ----

// TestClient_RESTPagination verifies that paginated search results
// are collected across multiple pages.
func TestClient_RESTPagination(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	// Use maxItems >= 100 so per_page=100 (the default)
	perPage := 100

	// Build 100 items for page 1 — this fills the page completely,
	// so the code fetches page 2 to check for more items.
	page1Items := make([]ghIssue, 100)
	for i := range 100 {
		page1Items[i] = ghIssue{ID: int64(i + 1), Number: i + 1, Title: fmt.Sprintf("Issue %d", i+1)}
	}
	page1 := ghSearchResponse{
		TotalCount: 102,
		Items:      page1Items,
	}
	page1Body, _ := json.Marshal(page1)

	page1URL := fmt.Sprintf("/search/issues?q=is%%3Aissue+is%%3Aopen&sort=updated&direction=asc&per_page=%d&page=1", perPage)
	page1FullURL := "https://api.github.com" + page1URL
	fake.addResponse(page1FullURL, fakeResponse{
		statusCode: 200,
		headers: map[string]string{
			"Link": fmt.Sprintf(`</search/issues?q=is%%3Aissue+is%%3Aopen&sort=updated&direction=asc&per_page=%d&page=2>; rel="next"`, perPage),
		},
		body: string(page1Body),
	})

	// Page 2: 2 items (fewer than per_page=100, signals last page)
	page2Items := []ghIssue{
		{ID: 101, Number: 101, Title: "Issue 101"},
		{ID: 102, Number: 102, Title: "Issue 102"},
	}
	page2 := ghSearchResponse{
		TotalCount: 102,
		Items:      page2Items,
	}
	page2Body, _ := json.Marshal(page2)

	page2URL := fmt.Sprintf("/search/issues?q=is%%3Aissue+is%%3Aopen&sort=updated&direction=asc&per_page=%d&page=2", perPage)
	page2FullURL := "https://api.github.com" + page2URL
	fake.addResponse(page2FullURL, fakeResponse{
		statusCode: 200,
		body:       string(page2Body),
	})

	c := testClient(fake)
	scope := collectionScope{
		strategy:     strategySearch,
		maxItems:     200,
		maxComments:  5,
		searchIssues: true,
	}

	issues, err := fetchIssuesSearchStrategy(t.Context(), c, scope)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(issues) != 102 {
		t.Fatalf("expected 102 issues, got %d", len(issues))
	}

	if issues[0].ID != 1 || issues[100].ID != 101 || issues[101].ID != 102 {
		t.Fatalf("unexpected issue IDs: first=%d, last=%d, second_last=%d",
			issues[0].ID, issues[101].ID, issues[100].ID)
	}
}

// TestClient_GraphQLPagination verifies cursor-based pagination for discussions.
func TestClient_GraphQLPagination(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	// GraphQL responses need to match the POST to /graphql
	// Since the body changes between calls (different cursor), we use a
	// prefix match on the URL only and return sequential responses.

	// Page 1: returns 1 discussion + hasNextPage + cursor
	page1Resp := graphQLResponse{
		Data: json.RawMessage(`{
			"repository": {
				"discussions": {
					"pageInfo": {"hasNextPage": true, "endCursor": "cursor2"},
					"nodes": [
						{
							"id": "D_kw1", "number": 1, "title": "Discussion 1",
							"body": "Body 1", "url": "https://github.com/o/r/discussions/1",
							"createdAt": "2025-01-01T00:00:00Z",
							"updatedAt": "2025-01-02T00:00:00Z",
							"category": {"name": "Ideas", "slug": "ideas"},
							"comments": {"totalCount": 2, "nodes": []},
							"upvoteCount": 5
						}
					]
				}
			}
		}`),
	}
	page1Body, _ := json.Marshal(page1Resp)
	fake.addResponse("https://api.github.com/graphql", fakeResponse{
		statusCode: 200,
		body:       string(page1Body),
	})

	// Page 2: returns 1 discussion, no next page
	page2Resp := graphQLResponse{
		Data: json.RawMessage(`{
			"repository": {
				"discussions": {
					"pageInfo": {"hasNextPage": false, "endCursor": ""},
					"nodes": [
						{
							"id": "D_kw2", "number": 2, "title": "Discussion 2",
							"body": "Body 2", "url": "https://github.com/o/r/discussions/2",
							"createdAt": "2025-01-03T00:00:00Z",
							"updatedAt": "2025-01-04T00:00:00Z",
							"category": {"name": "Q&A", "slug": "qna"},
							"comments": {"totalCount": 0, "nodes": []},
							"upvoteCount": 3
						}
					]
				}
			}
		}`),
	}
	page2Body, _ := json.Marshal(page2Resp)
	fake.addResponse("https://api.github.com/graphql", fakeResponse{
		statusCode: 200,
		body:       string(page2Body),
	})

	c := testClient(fake)
	scope := collectionScope{
		maxItems:          10,
		searchDiscussions: true,
		repos:             []string{"owner/repo"},
	}

	discussions, err := fetchDiscussions(t.Context(), c, nil, scope)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(discussions) != 2 {
		t.Fatalf("expected 2 discussions, got %d", len(discussions))
	}

	if discussions[0].Number != 1 || discussions[1].Number != 2 {
		t.Fatalf("unexpected discussion order: got numbers %d,%d",
			discussions[0].Number, discussions[1].Number)
	}
}

// TestClient_TransientRetrySuccess verifies that a transient 500 error
// is retried and the client recovers.
func TestClient_TransientRetrySuccess(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	// per_page will be 10 since maxItems=10 and len(allIssues)=0
	perPage := 10
	searchURL := fmt.Sprintf("https://api.github.com/search/issues?q=is%%3Aissue+is%%3Aopen&sort=updated&direction=asc&per_page=%d&page=1", perPage)

	// First call returns 500, second returns 200
	searchResp := ghSearchResponse{
		TotalCount: 1,
		Items: []ghIssue{
			{ID: 42, Number: 1, Title: "Recovered", Body: "After retry"},
		},
	}
	searchBody, _ := json.Marshal(searchResp)

	fake.addSequentialResponses(searchURL,
		fakeResponse{statusCode: 500, body: `{"message":"Internal Server Error"}`},
		fakeResponse{statusCode: 200, body: string(searchBody)},
	)

	c := testClient(fake)
	scope := collectionScope{
		strategy:     strategySearch,
		maxItems:     10,
		searchIssues: true,
	}

	issues, err := fetchIssuesSearchStrategy(t.Context(), c, scope)
	if err != nil {
		t.Fatalf("unexpected error after retry: %v", err)
	}

	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}

	if issues[0].ID != 42 {
		t.Fatalf("expected issue ID 42, got %d", issues[0].ID)
	}
}

// TestClient_RetryExhaustion verifies that repeated errors eventually fail.
func TestClient_RetryExhaustion(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	// per_page will be 10 since maxItems=10 and len(allIssues)=0
	perPage := 10
	searchURL := fmt.Sprintf("https://api.github.com/search/issues?q=is%%3Aissue+is%%3Aopen&sort=updated&direction=asc&per_page=%d&page=1", perPage)
	fake.addResponse(searchURL, fakeResponse{
		statusCode: 500,
		body:       `{"message":"Server Error"}`,
	})

	c := testClient(fake)
	c.retryMax = 2 // 1 initial + 2 retries = 3 total attempts

	scope := collectionScope{
		strategy:     strategySearch,
		maxItems:     10,
		searchIssues: true,
	}

	_, err := fetchIssuesSearchStrategy(t.Context(), c, scope)
	if err == nil {
		t.Fatal("expected error but got nil")
	}

	// Should be a RetryExhaustionError
	var retryErr *RetryExhaustionError
	if !errors.As(err, &retryErr) {
		t.Fatalf("expected RetryExhaustionError, got %T: %v", err, err)
	}

	// Should have called 3 times (1 initial + 2 retries)
	if fake.callCountFor(searchURL) != 3 {
		t.Fatalf("expected 3 calls, got %d", fake.callCountFor(searchURL))
	}
}

// TestClient_PrimaryRateLimit verifies handling of 429 Too Many Requests.
func TestClient_PrimaryRateLimit(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	perPage := 10
	searchURL := fmt.Sprintf("https://api.github.com/search/issues?q=is%%3Aissue+is%%3Aopen&sort=updated&direction=asc&per_page=%d&page=1", perPage)

	searchResp := ghSearchResponse{
		TotalCount: 1,
		Items: []ghIssue{
			{ID: 99, Number: 1, Title: "After Rate Limit", Body: "Recovered"},
		},
	}
	searchBody, _ := json.Marshal(searchResp)

	// First call: 429 rate limited, second: success
	fake.addSequentialResponses(searchURL,
		fakeResponse{
			statusCode: 429,
			headers:    map[string]string{"Retry-After": "0"},
			body:       `{"message":"Rate limit exceeded"}`,
		},
		fakeResponse{statusCode: 200, body: string(searchBody)},
	)

	c := testClient(fake)

	scope := collectionScope{
		strategy:     strategySearch,
		maxItems:     10,
		searchIssues: true,
	}

	issues, err := fetchIssuesSearchStrategy(t.Context(), c, scope)
	if err != nil {
		t.Fatalf("unexpected error after rate limit recovery: %v", err)
	}

	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}

	if issues[0].ID != 99 {
		t.Fatalf("expected issue ID 99, got %d", issues[0].ID)
	}
}

// TestClient_SecondaryRateLimit verifies handling of 403 + Retry-After.
func TestClient_SecondaryRateLimit(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	perPage := 10
	searchURL := fmt.Sprintf("https://api.github.com/search/issues?q=is%%3Aissue+is%%3Aopen&sort=updated&direction=asc&per_page=%d&page=1", perPage)

	searchResp := ghSearchResponse{
		TotalCount: 1,
		Items: []ghIssue{
			{ID: 77, Number: 1, Title: "After Secondary Limit", Body: "Recovered"},
		},
	}
	searchBody, _ := json.Marshal(searchResp)

	// First: 403 with Retry-After, then success
	fake.addSequentialResponses(searchURL,
		fakeResponse{
			statusCode: 403,
			headers:    map[string]string{"Retry-After": "0"},
			body:       `{"message":"Secondary rate limit"}`,
		},
		fakeResponse{statusCode: 200, body: string(searchBody)},
	)

	c := testClient(fake)

	scope := collectionScope{
		strategy:     strategySearch,
		maxItems:     10,
		searchIssues: true,
	}

	issues, err := fetchIssuesSearchStrategy(t.Context(), c, scope)
	if err != nil {
		t.Fatalf("unexpected error after secondary rate limit recovery: %v", err)
	}

	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
}

// TestClient_304ConditionalResponse verifies ETag/If-None-Match handling.
func TestClient_304ConditionalResponse(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	// Simulate two requests to the same endpoint.
	// First: returns 200 with ETag, second: returns 304 with cached data.

	perPage := 10
	searchURL := fmt.Sprintf("https://api.github.com/search/issues?q=is%%3Aissue+is%%3Aopen&sort=updated&direction=asc&per_page=%d&page=1", perPage)

	searchResp := ghSearchResponse{
		TotalCount: 2,
		Items: []ghIssue{
			{ID: 1, Number: 1, Title: "Cached Issue", Body: "Body"},
		},
	}
	searchBody, _ := json.Marshal(searchResp)

	// First request: returns 200 with ETag
	fake.addResponse(searchURL, fakeResponse{
		statusCode: 200,
		headers:    map[string]string{"ETag": `W/"abc123"`},
		body:       string(searchBody),
	})

	c := testClient(fake)

	// Execute first request — will cache ETag and body
	scope := collectionScope{
		strategy:     strategySearch,
		maxItems:     10,
		searchIssues: true,
	}

	issues1, err := fetchIssuesSearchStrategy(t.Context(), c, scope)
	if err != nil {
		t.Fatalf("first request failed: %v", err)
	}

	if len(issues1) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues1))
	}

	// Verify the transport was called once
	firstCalls := fake.callCountFor(searchURL)
	if firstCalls != 1 {
		t.Fatalf("expected 1 call, got %d", firstCalls)
	}

	// Reset call count and add a 304 response for the second call
	fake.resetCallCount()

	// Second call: 304
	fake.addResponse(searchURL, fakeResponse{
		statusCode: 304,
		headers: map[string]string{
			"ETag":                  `W/"abc123"`,
			"X-RateLimit-Remaining": "4998",
			"X-RateLimit-Reset":     "0",
		},
		body: "",
	})

	// Execute second request — should use ETag and get 304
	issues2, err := fetchIssuesSearchStrategy(t.Context(), c, scope)
	if err != nil {
		t.Fatalf("second request failed: %v", err)
	}

	if len(issues2) != 1 {
		t.Fatalf("expected 1 issue from cache, got %d", len(issues2))
	}

	if issues2[0].ID != 1 {
		t.Fatalf("expected issue ID 1, got %d", issues2[0].ID)
	}
}

// TestClient_RequestLimitCutoff verifies that the request cap is enforced.
func TestClient_RequestLimitCutoff(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	// Create a client with only 1 request allowed
	c := testClient(fake)
	c.requestLimit = 1

	scope := collectionScope{
		strategy:     strategySearch,
		maxItems:     10,
		searchIssues: true,
	}

	// Pre-fill request count to reach limit
	c.requestCount = 1

	_, err := fetchIssuesSearchStrategy(t.Context(), c, scope)
	if err == nil {
		t.Fatal("expected request limit error, got nil")
	}

	var limitErr *RequestLimitError
	if !errors.As(err, &limitErr) {
		t.Fatalf("expected RequestLimitError, got %T: %v", err, err)
	}

	if limitErr.Limit != 1 {
		t.Fatalf("expected limit=1, got %d", limitErr.Limit)
	}
}

// TestClient_ParseLinkHeader verifies Link header parsing.
func TestClient_ParseLinkHeader(t *testing.T) {
	t.Parallel()
	header := `<https://api.github.com/search/issues?page=2>; rel="next", <https://api.github.com/search/issues?page=5>; rel="last"`
	links := parseLinkHeader(header)

	if links["next"] != "https://api.github.com/search/issues?page=2" {
		t.Fatalf("unexpected next link: %q", links["next"])
	}

	if links["last"] != "https://api.github.com/search/issues?page=5" {
		t.Fatalf("unexpected last link: %q", links["last"])
	}

	// Empty header
	empty := parseLinkHeader("")
	if len(empty) != 0 {
		t.Fatalf("expected empty map for empty header, got %v", empty)
	}
}

// TestClient_ParseRepo verifies repository string parsing.
func TestClient_ParseRepo(t *testing.T) {
	t.Parallel()
	owner, repo, err := parseRepo("owner/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if owner != "owner" || repo != "repo" {
		t.Fatalf("expected owner/repo, got %s/%s", owner, repo)
	}

	// Invalid format
	_, _, err = parseRepo("invalid")
	if err == nil {
		t.Fatal("expected error for invalid repo format")
	}

	// Empty parts
	_, _, err = parseRepo("/repo")
	if err == nil {
		t.Fatal("expected error for empty owner")
	}

	_, _, err = parseRepo("owner/")
	if err == nil {
		t.Fatal("expected error for empty repo")
	}
}

// TestClient_BuildSearchQuery verifies search query construction.
func TestClient_BuildSearchQuery(t *testing.T) {
	t.Parallel()
	scope := collectionScope{
		labels:    []string{"bug", "enhancement"},
		languages: []string{"go", "python"},
		repos:     []string{"owner/repo"},
		strategy:  strategySearch,
		since:     "2025-01-01T00:00:00Z",
	}

	query := buildSearchQuery(scope)

	if !strings.Contains(query, "is:issue") {
		t.Fatal("expected is:issue in query")
	}
	if !strings.Contains(query, "is:open") {
		t.Fatal("expected is:open in query")
	}
	if !strings.Contains(query, "language:go") {
		t.Fatal("expected language:go in query")
	}
	if !strings.Contains(query, "language:python") {
		t.Fatal("expected language:python in query")
	}
	if !strings.Contains(query, "label:bug") {
		t.Fatal("expected label:bug in query")
	}
	if !strings.Contains(query, "label:enhancement") {
		t.Fatal("expected label:enhancement in query")
	}
	if !strings.Contains(query, "repo:owner/repo") {
		t.Fatal("expected repo:owner/repo in query")
	}
	if !strings.Contains(query, "updated:>=2025-01-01") {
		t.Fatal("expected updated:>=2025-01-01 in query")
	}

	// Empty scope
	empty := collectionScope{}
	q := buildSearchQuery(empty)
	if q != "is:issue is:open" {
		t.Fatalf("unexpected empty query: %q", q)
	}
}

// TestClient_RateLimitCheck verifies rate-limit checking.
func TestClient_RateLimitCheck(t *testing.T) {
	t.Parallel()
	c := testClient(newFakeTransport())

	// Initially should be fine
	if err := c.checkRateLimit(false); err != nil {
		t.Fatalf("expected no rate limit error, got %v", err)
	}

	if err := c.checkRateLimit(true); err != nil {
		t.Fatalf("expected no rate limit error, got %v", err)
	}

	// Exhaust REST rate limit
	c.restRemaining = 0
	c.restReset = time.Now().Add(1 * time.Hour)

	err := c.checkRateLimit(false)
	if err == nil {
		t.Fatal("expected rate limit error")
	}
	var rle *RateLimitError
	if !errors.As(err, &rle) {
		t.Fatalf("expected RateLimitError, got %T: %v", err, err)
	}
	if !rle.IsPrimary {
		t.Fatal("expected primary rate limit error")
	}

	// GraphQL should still be fine
	if err := c.checkRateLimit(true); err != nil {
		t.Fatalf("expected no rate limit error for graphql, got %v", err)
	}

	// Exhaust GraphQL rate limit
	c.gqlRemaining = 0
	c.gqlReset = time.Now().Add(1 * time.Hour)

	err = c.checkRateLimit(true)
	if err == nil {
		t.Fatal("expected graphql rate limit error")
	}
}

// TestClient_RequestCountValue verifies that request counts are tracked.
func TestClient_RequestCountValue(t *testing.T) {
	t.Parallel()
	c := testClient(newFakeTransport())

	if c.requestCountValue() != 0 {
		t.Fatalf("expected 0, got %d", c.requestCountValue())
	}

	c.incrementRequestCount()

	if c.requestCountValue() != 1 {
		t.Fatalf("expected 1, got %d", c.requestCountValue())
	}
}

// TestClient_ParseRetryAfter verifies Retry-After header parsing.
func TestClient_ParseRetryAfter(t *testing.T) {
	t.Parallel()
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	d := parseRetryAfter("30", now)
	if d != 30*time.Second {
		t.Fatalf("expected 30s, got %v", d)
	}

	d = parseRetryAfter("invalid", now)
	if d <= 0 {
		t.Fatalf("expected fallback duration, got %v", d)
	}
}

// TestClient_doJSONRequest_non200 tests error handling for non-2xx responses.
func TestClient_doJSONRequest_non200(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()
	fake.addResponse("https://api.github.com/some/path", fakeResponse{
		statusCode: 404,
		body:       `{"message":"Not Found"}`,
	})

	c := testClient(fake)

	var target struct{}
	_, err := c.doJSONRequest(t.Context(), requestOptions{
		Method: "GET",
		Path:   "/some/path",
	}, &target)

	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}

	if !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected error to mention 404, got: %v", err)
	}
}

// TestClient_RateLimitUpdate verifies rate-limit header parsing.
func TestClient_RateLimitUpdate(t *testing.T) {
	t.Parallel()
	c := testClient(newFakeTransport())

	// Default values
	if c.restRemaining != 5000 {
		t.Fatalf("expected 5000, got %d", c.restRemaining)
	}

	// Simulate response with headers
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"X-Ratelimit-Remaining": []string{"4242"},
			"X-Ratelimit-Reset":     []string{"1735689600"},
		},
	}

	c.updateRateLimits(resp, false)

	if c.restRemaining != 4242 {
		t.Fatalf("expected 4242, got %d", c.restRemaining)
	}

	expectedReset := time.Unix(1735689600, 0)
	if !c.restReset.Equal(expectedReset) {
		t.Fatalf("expected reset %v, got %v", expectedReset, c.restReset)
	}
}
