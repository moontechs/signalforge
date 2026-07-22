package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewClientRequiresToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")

	_, err := NewClient(ClientConfig{})
	if err == nil {
		t.Fatal("expected missing token error")
	}
}

func TestSearchIssuesSuccessAndPagination(t *testing.T) {
	client := newTestClient(t, sequenceDoer(
		jsonResponse(t, http.StatusOK, fixture(t, "search_issues.json"), map[string]string{
			"Link": `<https://api.github.com/search/issues?page=2>; rel="next"`,
		}),
	))

	page, err := client.SearchIssues(context.Background(), SearchIssuesParams{
		Query:   "label:bug repo:openai/codex",
		Page:    1,
		PerPage: 25,
	})
	if err != nil {
		t.Fatalf("search issues: %v", err)
	}

	if !page.HasNext {
		t.Fatal("expected next page")
	}
	if len(page.Response.Items) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(page.Response.Items))
	}
	stats := client.Stats()
	if stats.Requests != 1 || stats.RESTRequests != 1 || stats.GraphQLRequests != 0 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

func TestListIssueCommentsRetriesTransientFailure(t *testing.T) {
	var sleeps []time.Duration
	client := newTestClient(t, sequenceDoer(
		errorDoer(errors.New("temporary network failure")),
		jsonResponse(t, http.StatusOK, fixture(t, "issue_comments.json"), map[string]string{
			"Link": `<https://api.github.com/repos/openai/codex/issues/7/comments?page=2>; rel="next"`,
		}),
	), func(cfg *ClientConfig) {
		cfg.MaxRetries = 1
		cfg.Sleep = func(d time.Duration) {
			sleeps = append(sleeps, d)
		}
	})

	page, err := client.ListIssueComments(context.Background(), IssueCommentsParams{
		Owner:    "openai",
		Repo:     "codex",
		IssueNum: 7,
		Page:     1,
		PerPage:  10,
	})
	if err != nil {
		t.Fatalf("list issue comments: %v", err)
	}

	if !page.HasNext {
		t.Fatal("expected next page")
	}
	if len(page.Comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(page.Comments))
	}
	if len(sleeps) != 1 || sleeps[0] != defaultBackoffBase {
		t.Fatalf("unexpected backoff sequence: %v", sleeps)
	}
	stats := client.Stats()
	if stats.Requests != 2 || stats.Retries != 1 {
		t.Fatalf("unexpected stats after retry: %+v", stats)
	}
}

func TestSearchDiscussionsGraphQLSuccess(t *testing.T) {
	client := newTestClient(t, sequenceDoer(
		jsonResponse(t, http.StatusOK, fixture(t, "search_discussions.json"), nil),
	))

	page, err := client.SearchDiscussions(context.Background(), DiscussionSearchParams{
		Query: "repo:openai/codex label:feedback",
		First: 10,
	})
	if err != nil {
		t.Fatalf("search discussions: %v", err)
	}

	if len(page.Response.Data.Search.Nodes) != 1 {
		t.Fatalf("expected 1 discussion, got %d", len(page.Response.Data.Search.Nodes))
	}
	if !page.Response.Data.Search.PageInfo.HasNextPage {
		t.Fatal("expected graphql next page")
	}
	stats := client.Stats()
	if stats.Requests != 1 || stats.GraphQLRequests != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

func TestListDiscussionCommentsPagination(t *testing.T) {
	client := newTestClient(t, sequenceDoer(
		jsonResponse(t, http.StatusOK, fixture(t, "discussion_comments.json"), nil),
	))

	page, err := client.ListDiscussionComments(context.Background(), DiscussionCommentsParams{
		DiscussionID: "D_kwDOAA",
		First:        10,
		After:        "cursor-1",
	})
	if err != nil {
		t.Fatalf("list discussion comments: %v", err)
	}

	if len(page.Response.Data.Node.Comments.Nodes) != 1 {
		t.Fatalf("expected 1 discussion comment, got %d", len(page.Response.Data.Node.Comments.Nodes))
	}
	if page.Response.Data.Node.Comments.PageInfo.HasNextPage {
		t.Fatal("expected final page")
	}
}

func TestAuthenticationErrorIsTyped(t *testing.T) {
	client := newTestClient(t, sequenceDoer(
		jsonResponse(t, http.StatusUnauthorized, `{"message":"Bad credentials"}`, nil),
	))

	_, err := client.SearchIssues(context.Background(), SearchIssuesParams{Query: "repo:openai/codex"})
	var authErr *AuthenticationError
	if !errors.As(err, &authErr) {
		t.Fatalf("expected authentication error, got %T", err)
	}
}

func TestRateLimitErrorIsTyped(t *testing.T) {
	client := newTestClient(t, sequenceDoer(
		jsonResponse(t, http.StatusForbidden, `{"message":"API rate limit exceeded"}`, map[string]string{
			"X-RateLimit-Remaining": "0",
			"X-RateLimit-Reset":     "1784736000",
		}),
	))

	_, err := client.SearchIssues(context.Background(), SearchIssuesParams{Query: "repo:openai/codex"})
	var rateErr *RateLimitError
	if !errors.As(err, &rateErr) {
		t.Fatalf("expected rate limit error, got %T", err)
	}
	if rateErr.ResetAt.IsZero() {
		t.Fatal("expected rate limit reset time")
	}
}

func TestMalformedResponseErrorIsTyped(t *testing.T) {
	client := newTestClient(t, sequenceDoer(
		jsonResponse(t, http.StatusOK, `{"total_count":`, nil),
	))

	_, err := client.SearchIssues(context.Background(), SearchIssuesParams{Query: "repo:openai/codex"})
	var malformedErr *MalformedResponseError
	if !errors.As(err, &malformedErr) {
		t.Fatalf("expected malformed response error, got %T", err)
	}
}

func TestRequestLimitExceeded(t *testing.T) {
	client := newTestClient(t, sequenceDoer(
		jsonResponse(t, http.StatusOK, fixture(t, "search_issues.json"), nil),
	), func(cfg *ClientConfig) {
		cfg.MaxRequests = 1
	})

	if _, err := client.SearchIssues(context.Background(), SearchIssuesParams{Query: "repo:openai/codex"}); err != nil {
		t.Fatalf("first request failed: %v", err)
	}

	_, err := client.SearchIssues(context.Background(), SearchIssuesParams{Query: "repo:openai/codex"})
	if !errors.Is(err, ErrRequestLimitExceeded) {
		t.Fatalf("expected request limit error, got %v", err)
	}
}

func TestGraphQLErrorsBecomeAPIErrors(t *testing.T) {
	client := newTestClient(t, sequenceDoer(
		jsonResponse(t, http.StatusOK, `{"errors":[{"message":"Something broke"}]}`, nil),
	))

	_, err := client.SearchDiscussions(context.Background(), DiscussionSearchParams{
		Query: "repo:openai/codex",
		First: 10,
	})
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected api error, got %T", err)
	}
	if apiErr.Retryable {
		t.Fatal("graphql errors should not be retryable")
	}
}

func TestRequestHookReceivesAttempts(t *testing.T) {
	var events []RequestEvent
	client := newTestClient(t, sequenceDoer(
		errorDoer(errors.New("temporary network failure")),
		jsonResponse(t, http.StatusOK, fixture(t, "search_issues.json"), nil),
	), func(cfg *ClientConfig) {
		cfg.MaxRetries = 1
		cfg.Sleep = func(time.Duration) {}
		cfg.OnRequest = func(event RequestEvent) {
			events = append(events, event)
		}
	})

	if _, err := client.SearchIssues(context.Background(), SearchIssuesParams{Query: "repo:openai/codex"}); err != nil {
		t.Fatalf("search issues: %v", err)
	}

	if len(events) != 2 {
		t.Fatalf("expected 2 request events, got %d", len(events))
	}
	if events[0].Attempt != 1 || events[1].Attempt != 2 {
		t.Fatalf("unexpected attempts: %+v", events)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

func sequenceDoer(steps ...roundTripFunc) roundTripFunc {
	index := 0
	return func(req *http.Request) (*http.Response, error) {
		if index >= len(steps) {
			return nil, fmt.Errorf("unexpected extra request: %s", req.URL.String())
		}
		step := steps[index]
		index++
		return step(req)
	}
}

func jsonResponse(t *testing.T, status int, body string, headers map[string]string) roundTripFunc {
	t.Helper()
	return func(req *http.Request) (*http.Response, error) {
		if auth := req.Header.Get("Authorization"); !strings.HasPrefix(auth, "Bearer ") || auth == "Bearer " {
			t.Fatalf("missing auth header")
		}
		resp := &http.Response{
			StatusCode: status,
			Header:     make(http.Header),
			Body:       ioNopCloser(strings.NewReader(body)),
			Request:    req,
		}
		for k, v := range headers {
			resp.Header.Set(k, v)
		}
		return resp, nil
	}
}

func errorDoer(err error) roundTripFunc {
	return func(*http.Request) (*http.Response, error) {
		return nil, err
	}
}

func newTestClient(t *testing.T, doer roundTripFunc, opts ...func(*ClientConfig)) *Client {
	t.Helper()
	cfg := ClientConfig{
		Token:      "test-token",
		HTTPClient: doer,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	return client
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("..", "..", "..", "testdata", "github", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(data)
}

type nopCloser struct {
	*strings.Reader
}

func (n nopCloser) Close() error {
	return nil
}

func ioNopCloser(r *strings.Reader) nopCloser {
	return nopCloser{Reader: r}
}
