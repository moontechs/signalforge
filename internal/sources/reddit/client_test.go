package reddit

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/moontechs/signalforge/internal/cache"
	"github.com/moontechs/signalforge/internal/storage"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// testClient creates a client backed by a fake transport with default
// credentials and a pre-registered token response. If fake is nil, a new
// one is created.
func testClient(fake *fakeTransport, maxRequests int) *client {
	if fake == nil {
		fake = newFakeTransport()
	}
	// Pre-register a default successful token response so most tests don't
	// need to think about OAuth. Tests that verify token behaviour can
	// override this response.
	fake.addResponse("https://www.reddit.com/api/v1/access_token",
		fakeResponse{statusCode: 200, body: `{"access_token":"test_token","token_type":"bearer","expires_in":3600,"scope":"*"}`})

	return newClient(fake, "test_client_id", "test_client_secret", maxRequests)
}

// testClientNoCreds creates a client with empty credentials.
func testClientNoCreds(fake *fakeTransport, maxRequests int) *client {
	if fake == nil {
		fake = newFakeTransport()
	}
	return newClient(fake, "", "", maxRequests)
}

// tokenURL is the full token endpoint URL used in tests.
const tokenURL = "https://www.reddit.com/api/v1/access_token"

// apiURLFor returns the full oauth.reddit.com URL for a given path and params.
func apiURLFor(path string, params url.Values) string {
	u := "https://oauth.reddit.com" + path
	if params != nil {
		u += "?" + params.Encode()
	}
	return u
}

// ---------------------------------------------------------------------------
// Token acquisition
// ---------------------------------------------------------------------------

func TestClient_tokenRequest_methodAndHeaders(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()
	// Override the default token response with one that lets us inspect the request.
	fake.addResponse(tokenURL,
		fakeResponse{statusCode: 200, body: `{"access_token":"tok","token_type":"bearer","expires_in":3600,"scope":"*"}`})

	c := testClient(fake, 100)

	// Force token acquisition (ensureToken triggers the full flow since no
	// token exists yet).
	err := c.ensureToken(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	reqs := fake.requests()
	if len(reqs) == 0 {
		t.Fatal("expected at least one request")
	}

	req := reqs[0]
	if req.Method != http.MethodPost {
		t.Fatalf("expected POST, got %s", req.Method)
	}
	if req.URL.String() != tokenURL {
		t.Fatalf("expected URL %s, got %s", tokenURL, req.URL.String())
	}

	// Content-Type
	ct := req.Header.Get("Content-Type")
	if ct != "application/x-www-form-urlencoded" {
		t.Fatalf("expected Content-Type application/x-www-form-urlencoded, got %s", ct)
	}

	// User-Agent
	ua := req.Header.Get("User-Agent")
	if ua == "" {
		t.Fatal("expected User-Agent header")
	}
	if !strings.Contains(ua, "SignalForge") {
		t.Fatalf("expected User-Agent to contain SignalForge, got %s", ua)
	}

	// Basic auth
	user, pass, ok := req.BasicAuth()
	if !ok {
		t.Fatal("expected Basic auth header")
	}
	if user != "test_client_id" || pass != "test_client_secret" {
		t.Fatalf("expected credentials test_client_id:test_client_secret, got %s:%s", user, pass)
	}

	// Body
	err = req.ParseForm()
	if err != nil {
		t.Fatalf("parse form: %v", err)
	}
	if req.Form.Get("grant_type") != "client_credentials" {
		t.Fatalf("expected grant_type=client_credentials, got %s", req.Form.Get("grant_type"))
	}
}

func TestClient_tokenRequest_missingCredentials(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()
	c := testClientNoCreds(fake, 100)

	err := c.ensureToken(t.Context())
	if err == nil {
		t.Fatal("expected error for missing credentials")
	}
	if !errors.Is(err, ErrMissingCredentials) {
		t.Fatalf("expected ErrMissingCredentials, got %v", err)
	}
}

func TestClient_tokenRequest_emptyAccessToken(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()
	fake.addResponse(tokenURL,
		fakeResponse{statusCode: 200, body: `{"access_token":"","token_type":"bearer","expires_in":3600}`})

	c := testClient(fake, 100)

	err := c.ensureToken(t.Context())
	if err == nil {
		t.Fatal("expected error for empty access token")
	}
	if !errors.Is(err, ErrTokenAuth) {
		t.Fatalf("expected ErrTokenAuth, got %v", err)
	}
}

func TestClient_tokenRequest_nonRetryableStatus(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()
	// 403 is non-retryable.
	fake.addResponse(tokenURL,
		fakeResponse{statusCode: 403, body: `{"error":"forbidden"}`})

	c := testClient(fake, 100)

	err := c.ensureToken(t.Context())
	if err == nil {
		t.Fatal("expected error for 403")
	}
	if !errors.Is(err, ErrTokenAuth) {
		t.Fatalf("expected ErrTokenAuth, got %v", err)
	}
	if errors.Is(err, ErrRetriesExhausted) {
		t.Fatal("4xx should not be wrapped in ErrRetriesExhausted")
	}
	if fake.callCountFor(tokenURL) != 1 {
		t.Fatalf("expected only 1 call for 403, got %d", fake.callCountFor(tokenURL))
	}
}

func TestClient_tokenRequest_retry429(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()
	fake.addSequentialResponses(tokenURL,
		fakeResponse{statusCode: 429, body: `{}`},
		fakeResponse{statusCode: 200, body: `{"access_token":"tok","token_type":"bearer","expires_in":3600,"scope":"*"}`},
	)

	// Use a raw client to avoid testClient's default success response.
	c := newClient(fake, "test_client_id", "test_client_secret", 100)
	c.retryMax = 2
	c.retryBackoff = func(_ int) time.Duration { return time.Millisecond }

	err := c.ensureToken(t.Context())
	if err != nil {
		t.Fatalf("expected recovery after 429 retry, got: %v", err)
	}
}

func TestClient_tokenRequest_retryExhaustion(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()
	fake.addResponse(tokenURL,
		fakeResponse{statusCode: 500, body: `{}`})

	// Use a raw client to avoid testClient's default success response.
	c := newClient(fake, "test_client_id", "test_client_secret", 100)
	c.retryMax = 1
	c.retryBackoff = func(_ int) time.Duration { return time.Millisecond }

	err := c.ensureToken(t.Context())
	if err == nil {
		t.Fatal("expected error after retry exhaustion")
	}
	if !errors.Is(err, ErrRetriesExhausted) {
		t.Fatalf("expected ErrRetriesExhausted, got %v", err)
	}
}

func TestClient_tokenRequest_requestCapExhausted(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()
	fake.addResponse(tokenURL,
		fakeResponse{statusCode: 200, body: `{"access_token":"tok","token_type":"bearer","expires_in":3600,"scope":"*"}`})

	c := testClient(fake, 1)
	// Pre-fill to reach the cap.
	c.mu.Lock()
	c.requests = 1
	c.mu.Unlock()

	err := c.ensureToken(t.Context())
	if err == nil {
		t.Fatal("expected error for request cap")
	}
	if !errors.Is(err, ErrRequestCap) {
		t.Fatalf("expected ErrRequestCap, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Token reuse and expiry
// ---------------------------------------------------------------------------

func TestClient_tokenReuse(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()
	c := testClient(fake, 100)

	// First call acquires a token.
	err := c.ensureToken(t.Context())
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if fake.callCountFor(tokenURL) != 1 {
		t.Fatalf("expected 1 token request, got %d", fake.callCountFor(tokenURL))
	}

	// Second call should reuse the existing token (no new request).
	err = c.ensureToken(t.Context())
	if err != nil {
		t.Fatalf("second acquire: %v", err)
	}
	if fake.callCountFor(tokenURL) != 1 {
		t.Fatalf("expected token reuse (still 1 request), got %d", fake.callCountFor(tokenURL))
	}
}

func TestClient_tokenExpiryRefresh(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()
	fake.addSequentialResponses(tokenURL,
		fakeResponse{statusCode: 200, body: `{"access_token":"first","token_type":"bearer","expires_in":1,"scope":"*"}`},
		fakeResponse{statusCode: 200, body: `{"access_token":"second","token_type":"bearer","expires_in":3600,"scope":"*"}`},
	)

	c := testClient(fake, 100)
	c.retryBackoff = func(_ int) time.Duration { return time.Millisecond }

	// Acquire first token.
	err := c.ensureToken(t.Context())
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if c.token.AccessToken != "first" {
		t.Fatalf("expected first token, got %s", c.token.AccessToken)
	}

	// Simulate token expiry by setting expiry in the past.
	c.mu.Lock()
	c.tokenExpiry = time.Now().Add(-1 * time.Second)
	c.mu.Unlock()

	// Second acquire should refresh.
	err = c.ensureToken(t.Context())
	if err != nil {
		t.Fatalf("second acquire: %v", err)
	}
	if c.token.AccessToken != "second" {
		t.Fatalf("expected second token after refresh, got %s", c.token.AccessToken)
	}
	if fake.callCountFor(tokenURL) != 2 {
		t.Fatalf("expected 2 token requests, got %d", fake.callCountFor(tokenURL))
	}
}

// ---------------------------------------------------------------------------
// Authenticated API calls
// ---------------------------------------------------------------------------

func TestClient_listing_bearerHeader(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()
	listingBody := `{"kind":"Listing","data":{"after":null,"dist":1,"children":[{"kind":"t3","data":{"id":"abc123","title":"Test Post"}}]}}`
	listingURL := apiURLFor("/r/golang/new.json", url.Values{"limit": {"10"}})
	fake.addResponse(listingURL,
		fakeResponse{statusCode: 200, body: listingBody})

	c := testClient(fake, 100)

	_, err := c.listing(t.Context(), "golang", "new", "", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check the API request had a Bearer token.
	reqs := fake.requests()
	var apiReq *http.Request
	for _, r := range reqs {
		if r.URL.Host == "oauth.reddit.com" {
			apiReq = r
			break
		}
	}
	if apiReq == nil {
		t.Fatal("no API request found")
	}
	auth := apiReq.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		t.Fatalf("expected Bearer token, got Authorization: %s", auth)
	}

	ua := apiReq.Header.Get("User-Agent")
	if ua == "" || !strings.Contains(ua, "SignalForge") {
		t.Fatalf("expected User-Agent with SignalForge, got %s", ua)
	}
}

func TestClient_listing_success(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()
	listingBody := `{"kind":"Listing","data":{"after":null,"dist":1,"children":[{"kind":"t3","data":{"id":"abc123","title":"Test Post"}}]}}`
	listingURL := apiURLFor("/r/golang/new.json", url.Values{"limit": {"10"}})
	fake.addResponse(listingURL,
		fakeResponse{statusCode: 200, body: listingBody})

	c := testClient(fake, 100)

	resp, err := c.listing(t.Context(), "golang", "new", "", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Data.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(resp.Data.Children))
	}
	if resp.Data.Children[0].Data.ID != "abc123" {
		t.Fatalf("expected ID abc123, got %s", resp.Data.Children[0].Data.ID)
	}
}

func TestClient_listing_withTimeFilter(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()
	listingBody := `{"kind":"Listing","data":{"after":null,"dist":1,"children":[{"kind":"t3","data":{"id":"top1","title":"Top Weekly"}}]}}`
	listingURL := apiURLFor("/r/golang/top.json", url.Values{"limit": {"10"}, "t": {"week"}})
	fake.addResponse(listingURL,
		fakeResponse{statusCode: 200, body: listingBody})

	c := testClient(fake, 100)

	resp, err := c.listing(t.Context(), "golang", "top", "week", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Data.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(resp.Data.Children))
	}
}

func TestClient_listing_timeFilterIgnoredForNew(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()
	// For "new" sort, the t parameter should NOT be included.
	listingURL := apiURLFor("/r/golang/new.json", url.Values{"limit": {"10"}})
	fake.addResponse(listingURL,
		fakeResponse{statusCode: 200, body: `{"kind":"Listing","data":{"children":[]}}`})

	c := testClient(fake, 100)

	_, err := c.listing(t.Context(), "golang", "new", "week", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify no t param in the request.
	reqs := fake.requests()
	for _, r := range reqs {
		if r.URL.Host == "oauth.reddit.com" {
			if r.URL.Query().Has("t") {
				t.Fatal("t parameter should not be present for new sort")
			}
		}
	}
}

func TestClient_comments_success(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()
	commentsBody := `[
		{"kind":"Listing","data":{"children":[{"kind":"t3","data":{"id":"post1"}}]}},
		{"kind":"Listing","data":{"children":[{"kind":"t1","data":{"id":"c1","body":"A comment"}}]}}
	]`
	commentsURL := apiURLFor("/comments/post1.json", nil)
	fake.addResponse(commentsURL,
		fakeResponse{statusCode: 200, body: commentsBody})

	c := testClient(fake, 100)

	resp, err := c.comments(t.Context(), "post1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp) != 2 {
		t.Fatalf("expected 2 elements in comment tree, got %d", len(resp))
	}
}

func TestClient_comments_malformed(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()
	commentsURL := apiURLFor("/comments/post1.json", nil)
	fake.addResponse(commentsURL,
		fakeResponse{statusCode: 200, body: `not json`})

	c := testClient(fake, 100)

	_, err := c.comments(t.Context(), "post1")
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if !errors.Is(err, ErrMalformedResponse) {
		t.Fatalf("expected ErrMalformedResponse, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Request cap enforcement
// ---------------------------------------------------------------------------

func TestClient_requestCapReached(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()
	listingURL := apiURLFor("/r/golang/new.json", url.Values{"limit": {"10"}})
	fake.addResponse(listingURL,
		fakeResponse{statusCode: 200, body: `{"kind":"Listing","data":{"children":[]}}`})

	c := testClient(fake, 5)
	// Pre-fill to reach cap.
	c.mu.Lock()
	c.requests = 5
	c.mu.Unlock()

	_, err := c.listing(t.Context(), "golang", "new", "", 10)
	if err == nil {
		t.Fatal("expected error for request cap")
	}
	if !errors.Is(err, ErrRequestCap) {
		t.Fatalf("expected ErrRequestCap, got %v", err)
	}
}

func TestClient_requestCapNotReached(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()
	listingURL := apiURLFor("/r/golang/new.json", url.Values{"limit": {"10"}})
	fake.addResponse(listingURL,
		fakeResponse{statusCode: 200, body: `{"kind":"Listing","data":{"children":[]}}`})

	c := testClient(fake, 5)
	c.mu.Lock()
	c.requests = 4 // One below cap.
	c.mu.Unlock()

	_, err := c.listing(t.Context(), "golang", "new", "", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Retries
// ---------------------------------------------------------------------------

func TestClient_retryTransientRecovers(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()
	listingURL := apiURLFor("/r/golang/new.json", url.Values{"limit": {"10"}})
	fake.addSequentialResponses(listingURL,
		fakeResponse{statusCode: 500, body: `{}`},
		fakeResponse{statusCode: 200, body: `{"kind":"Listing","data":{"children":[]}}`},
	)

	c := testClient(fake, 100)
	c.retryMax = 2
	c.retryBackoff = func(_ int) time.Duration { return time.Millisecond }

	_, err := c.listing(t.Context(), "golang", "new", "", 10)
	if err != nil {
		t.Fatalf("expected recovery after retry, got: %v", err)
	}
}

func TestClient_retryExhaustion(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()
	listingURL := apiURLFor("/r/golang/new.json", url.Values{"limit": {"10"}})
	fake.addResponse(listingURL,
		fakeResponse{statusCode: 500, body: `{}`})

	c := testClient(fake, 100)
	c.retryMax = 1
	c.retryBackoff = func(_ int) time.Duration { return time.Millisecond }

	_, err := c.listing(t.Context(), "golang", "new", "", 10)
	if err == nil {
		t.Fatal("expected error after retry exhaustion")
	}
	if !errors.Is(err, ErrRetriesExhausted) {
		t.Fatalf("expected ErrRetriesExhausted, got %v", err)
	}
	if fake.callCountFor(listingURL) != 2 {
		t.Fatalf("expected 2 calls (1 initial + 1 retry), got %d", fake.callCountFor(listingURL))
	}
}

func TestClient_nonRetryableError(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()
	listingURL := apiURLFor("/r/golang/new.json", url.Values{"limit": {"10"}})
	fake.addResponse(listingURL,
		fakeResponse{statusCode: 403, body: `{"error":"forbidden"}`})

	c := testClient(fake, 100)
	c.retryMax = 3

	_, err := c.listing(t.Context(), "golang", "new", "", 10)
	if err == nil {
		t.Fatal("expected error for 403")
	}
	if errors.Is(err, ErrRetriesExhausted) {
		t.Fatal("4xx should not be retried, got ErrRetriesExhausted")
	}
	if fake.callCountFor(listingURL) != 1 {
		t.Fatalf("expected only 1 call for 403, got %d", fake.callCountFor(listingURL))
	}
}

func TestClient_429retryThenRecover(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()
	listingURL := apiURLFor("/r/golang/new.json", url.Values{"limit": {"10"}})
	fake.addSequentialResponses(listingURL,
		fakeResponse{statusCode: 429, body: `{}`},
		fakeResponse{statusCode: 200, body: `{"kind":"Listing","data":{"children":[]}}`},
	)

	c := testClient(fake, 100)
	c.retryMax = 2
	c.retryBackoff = func(_ int) time.Duration { return time.Millisecond }

	_, err := c.listing(t.Context(), "golang", "new", "", 10)
	if err != nil {
		t.Fatalf("expected recovery after 429 retry, got: %v", err)
	}
}

func TestClient_401TriggersTokenRefresh(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()
	listingURL := apiURLFor("/r/golang/new.json", url.Values{"limit": {"10"}})

	// First API call returns 401 (expired token), second returns success.
	fake.addSequentialResponses(listingURL,
		fakeResponse{statusCode: 401, body: `{}`},
		fakeResponse{statusCode: 200, body: `{"kind":"Listing","data":{"children":[{"kind":"t3","data":{"id":"abc"}}]}}`},
	)

	// Token endpoint serves two tokens.
	fake.addSequentialResponses(tokenURL,
		fakeResponse{statusCode: 200, body: `{"access_token":"first","token_type":"bearer","expires_in":3600,"scope":"*"}`},
		fakeResponse{statusCode: 200, body: `{"access_token":"second","token_type":"bearer","expires_in":3600,"scope":"*"}`},
	)

	c := testClient(fake, 100)
	c.retryMax = 2
	c.retryBackoff = func(_ int) time.Duration { return time.Millisecond }

	resp, err := c.listing(t.Context(), "golang", "new", "", 10)
	if err != nil {
		t.Fatalf("expected recovery after 401 + token refresh, got: %v", err)
	}
	if len(resp.Data.Children) != 1 {
		t.Fatalf("expected 1 child after recovery, got %d", len(resp.Data.Children))
	}

	// Should have acquired a second token after the 401.
	c.mu.Lock()
	token := c.token.AccessToken
	c.mu.Unlock()
	if token != "second" {
		t.Fatalf("expected refreshed token 'second', got %s", token)
	}
}

// ---------------------------------------------------------------------------
// Context cancellation
// ---------------------------------------------------------------------------

func TestClient_contextCancellation(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()
	listingURL := apiURLFor("/r/golang/new.json", url.Values{"limit": {"10"}})
	fake.addResponse(listingURL,
		fakeResponse{statusCode: 200, body: `{"kind":"Listing","data":{"children":[]}}`})

	c := testClient(fake, 100)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := c.listing(ctx, "golang", "new", "", 10)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestClient_contextCancellationDuringRetry(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()
	listingURL := apiURLFor("/r/golang/new.json", url.Values{"limit": {"10"}})
	fake.addResponse(listingURL,
		fakeResponse{statusCode: 500, body: `{}`})

	c := testClient(fake, 100)
	c.retryMax = 3

	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := c.listing(ctx, "golang", "new", "", 10)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Malformed / oversized responses
// ---------------------------------------------------------------------------

func TestClient_malformedJSON(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()
	listingURL := apiURLFor("/r/golang/new.json", url.Values{"limit": {"10"}})
	fake.addResponse(listingURL,
		fakeResponse{statusCode: 200, body: `not json at all`})

	c := testClient(fake, 100)
	_, err := c.listing(t.Context(), "golang", "new", "", 10)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if !errors.Is(err, ErrMalformedResponse) {
		t.Fatalf("expected ErrMalformedResponse, got %v", err)
	}
}

func TestClient_responseSizeLimitExceeded(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()
	listingURL := apiURLFor("/r/golang/new.json", url.Values{"limit": {"10"}})
	// Generate a body larger than maxBodySize.
	largeBody := make([]byte, 100*1024+1) // 100KB + 1 byte
	for i := range largeBody {
		largeBody[i] = 'x'
	}
	fake.addResponse(listingURL,
		fakeResponse{statusCode: 200, body: string(largeBody)})

	c := testClient(fake, 100)
	c.maxBodySize = 100 * 1024 // 100KB limit
	c.retryMax = 0

	_, err := c.listing(t.Context(), "golang", "new", "", 10)
	if err == nil {
		t.Fatal("expected error for oversized response")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected size limit error, got: %v", err)
	}
}

func TestClient_defaultBodySizeLimit(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()
	c := testClient(fake, 100)
	if c.maxBodySize != 10*1024*1024 {
		t.Fatalf("expected default 10MB limit, got %d", c.maxBodySize)
	}
}

// ---------------------------------------------------------------------------
// Cache behaviour
// ---------------------------------------------------------------------------

func TestClient_listing_cacheHit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := storage.New(dir)
	fake := newFakeTransport()

	listingBody := `{"kind":"Listing","data":{"children":[{"kind":"t3","data":{"id":"abc"}}]}}`
	listingURL := apiURLFor("/r/golang/new.json", url.Values{"limit": {"10"}})
	fake.addResponse(listingURL,
		fakeResponse{statusCode: 200, body: listingBody})

	c := testClient(fake, 100)
	c.WithCache(cache.NewCache(store, "reddit"))

	// First call: cache miss, HTTP request.
	_, err := c.listing(t.Context(), "golang", "new", "", 10)
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	if fake.callCountFor(listingURL) != 1 {
		t.Fatalf("expected 1 HTTP call, got %d", fake.callCountFor(listingURL))
	}

	// Second call: cache hit, no HTTP request.
	fake.resetCallCount()
	_, err = c.listing(t.Context(), "golang", "new", "", 10)
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}
	if fake.callCountFor(listingURL) != 0 {
		t.Fatalf("expected 0 HTTP calls (cache hit), got %d", fake.callCountFor(listingURL))
	}
}

func TestClient_cacheExpiration(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := storage.New(dir)
	fake := newFakeTransport()

	// Pre-write an expired cache entry.
	oldCache := cache.NewCache(store, "reddit")
	cacheKey := "/r/golang/new.json?limit=10"
	err := oldCache.Set(cacheKey, cache.CacheEntry{
		Body:     []byte(`{"kind":"Listing","data":{"children":[]}}`),
		TTL:      5 * time.Minute,
		StoredAt: time.Now().Add(-10 * time.Minute),
	})
	if err != nil {
		t.Fatalf("pre-write cache: %v", err)
	}

	freshBody := `{"kind":"Listing","data":{"children":[{"kind":"t3","data":{"id":"fresh"}}]}}`
	listingURL := apiURLFor("/r/golang/new.json", url.Values{"limit": {"10"}})
	fake.addResponse(listingURL,
		fakeResponse{statusCode: 200, body: freshBody})

	c := testClient(fake, 100)
	c.WithCache(oldCache)

	resp, err := c.listing(t.Context(), "golang", "new", "", 10)
	if err != nil {
		t.Fatalf("call failed: %v", err)
	}
	if fake.callCountFor(listingURL) != 1 {
		t.Fatalf("expected 1 HTTP call (cache expired), got %d", fake.callCountFor(listingURL))
	}
	if len(resp.Data.Children) != 1 || resp.Data.Children[0].Data.ID != "fresh" {
		t.Fatalf("expected fresh data, got %+v", resp)
	}
}

func TestClient_noStore_neverCaches(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()
	listingURL := apiURLFor("/r/golang/new.json", url.Values{"limit": {"10"}})
	fake.addResponse(listingURL,
		fakeResponse{statusCode: 200, body: `{"kind":"Listing","data":{"children":[]}}`})

	c := testClient(fake, 100)
	// No cache attached.

	_, err := c.listing(t.Context(), "golang", "new", "", 10)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	// Without cache, second call also makes HTTP request.
	fake.resetCallCount()
	_, err = c.listing(t.Context(), "golang", "new", "", 10)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if fake.callCountFor(listingURL) != 1 {
		t.Fatalf("expected 1 HTTP call (no cache), got %d", fake.callCountFor(listingURL))
	}
}

// ---------------------------------------------------------------------------
// Request count tracking
// ---------------------------------------------------------------------------

func TestClient_requestCountTracking(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()
	listingURL := apiURLFor("/r/golang/new.json", url.Values{"limit": {"10"}})
	fake.addResponse(listingURL,
		fakeResponse{statusCode: 200, body: `{"kind":"Listing","data":{"children":[]}}`})

	c := testClient(fake, 100)

	stats := c.Stats()
	if stats.Requests != 0 || stats.CacheHits != 0 {
		t.Fatalf("expected zero stats, got %+v", stats)
	}

	_, err := c.listing(t.Context(), "golang", "new", "", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stats = c.Stats()
	// Should count: 1 token request + 1 API request = 2.
	if stats.Requests != 2 {
		t.Fatalf("expected 2 requests (1 token + 1 API), got %d", stats.Requests)
	}
	if stats.CacheHits != 0 {
		t.Fatalf("expected 0 cache hits, got %d", stats.CacheHits)
	}
}

func TestClient_requestCountTrackingWithCache(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := storage.New(dir)
	fake := newFakeTransport()
	listingURL := apiURLFor("/r/golang/new.json", url.Values{"limit": {"10"}})
	fake.addResponse(listingURL,
		fakeResponse{statusCode: 200, body: `{"kind":"Listing","data":{"children":[]}}`})

	c := testClient(fake, 100)
	c.WithCache(cache.NewCache(store, "reddit"))

	// First call: 1 token + 1 API = 2 requests.
	_, _ = c.listing(t.Context(), "golang", "new", "", 10)
	stats := c.Stats()
	if stats.Requests != 2 {
		t.Fatalf("expected 2 requests, got %d", stats.Requests)
	}

	// Second call: cache hit — no new requests, 1 cache hit.
	_, _ = c.listing(t.Context(), "golang", "new", "", 10)
	stats = c.Stats()
	if stats.Requests != 2 {
		t.Fatalf("expected still 2 requests, got %d", stats.Requests)
	}
	if stats.CacheHits != 1 {
		t.Fatalf("expected 1 cache hit, got %d", stats.CacheHits)
	}
}

func TestClient_statsAfterCacheHit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := storage.New(dir)
	fake := newFakeTransport()
	commentsURL := apiURLFor("/comments/abc123.json", nil)
	fake.addResponse(commentsURL,
		fakeResponse{statusCode: 200, body: `[{},{"data":{"children":[]}}]`})

	c := testClient(fake, 100)
	c.WithCache(cache.NewCache(store, "reddit"))

	_, _ = c.comments(t.Context(), "abc123")
	_, _ = c.comments(t.Context(), "abc123")

	stats := c.Stats()
	if stats.Requests != 2 {
		t.Fatalf("expected 2 requests (1 token + 1 API), got %d", stats.Requests)
	}
	if stats.CacheHits != 1 {
		t.Fatalf("expected 1 cache hit, got %d", stats.CacheHits)
	}
}

// ---------------------------------------------------------------------------
// Concurrent access safety
// ---------------------------------------------------------------------------

func TestClient_concurrentAccess(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	// Register responses for multiple listing calls.
	for i := 0; i < 5; i++ {
		sub := fmt.Sprintf("sub%d", i)
		listingURL := apiURLFor(fmt.Sprintf("/r/%s/new.json", sub), url.Values{"limit": {"10"}})
		body := fmt.Sprintf(`{"kind":"Listing","data":{"children":[{"kind":"t3","data":{"id":"id%d"}}]}}`, i)
		fake.addResponse(listingURL,
			fakeResponse{statusCode: 200, body: body})
	}

	c := testClient(fake, 100)

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sub := fmt.Sprintf("sub%d", idx)
			_, err := c.listing(t.Context(), sub, "new", "", 10)
			if err != nil {
				t.Errorf("listing %s: %v", sub, err)
			}
		}(i)
	}
	wg.Wait()

	stats := c.Stats()
	// Under concurrent access at least 1 token request is made; due to
	// scheduling some goroutines may race and acquire the token multiple
	// times before the first completes. Accept a range.
	minRequests := 6 // 1 token + 5 API
	maxRequests := 9 // up to 4 redundant token acquisitions
	if stats.Requests < minRequests || stats.Requests > maxRequests {
		t.Fatalf("expected requests between %d and %d, got %d", minRequests, maxRequests, stats.Requests)
	}
	if stats.CacheHits != 0 {
		t.Fatalf("expected 0 cache hits, got %d", stats.CacheHits)
	}
}

// ---------------------------------------------------------------------------
// Empty listing
// ---------------------------------------------------------------------------

func TestClient_listing_empty(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()
	listingURL := apiURLFor("/r/golang/new.json", url.Values{"limit": {"10"}})
	fake.addResponse(listingURL,
		fakeResponse{statusCode: 200, body: `{"kind":"Listing","data":{"children":[]}}`})

	c := testClient(fake, 100)

	resp, err := c.listing(t.Context(), "golang", "new", "", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Data.Children) != 0 {
		t.Fatalf("expected empty listing, got %d children", len(resp.Data.Children))
	}
}

// ---------------------------------------------------------------------------
// Transport error
// ---------------------------------------------------------------------------

func TestClient_transportError(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()
	// No response registered for the listing URL — fakeTransport returns 404.
	c := testClient(fake, 100)

	_, err := c.listing(t.Context(), "unknown", "new", "", 10)
	if err == nil {
		t.Fatal("expected error for unregistered URL")
	}
}

// ---------------------------------------------------------------------------
// Credentials not exposed in cache keys or stats
// ---------------------------------------------------------------------------

func TestClient_credentialsNotInCacheKey(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := storage.New(dir)
	fake := newFakeTransport()

	listingBody := `{"kind":"Listing","data":{"children":[]}}`
	listingURL := apiURLFor("/r/golang/new.json", url.Values{"limit": {"10"}})
	fake.addResponse(listingURL,
		fakeResponse{statusCode: 200, body: listingBody})

	c := testClient(fake, 100)
	cacheInstance := cache.NewCache(store, "reddit")
	c.WithCache(cacheInstance)

	_, err := c.listing(t.Context(), "golang", "new", "", 10)
	if err != nil {
		t.Fatalf("listing: %v", err)
	}

	// List the cache files — none should contain client_id or client_secret.
	files, err := store.ListFiles("cache/reddit", ".json")
	if err != nil {
		t.Fatalf("list cache: %v", err)
	}
	for _, f := range files {
		data, err := store.ReadFile(f)
		if err != nil {
			continue
		}
		content := string(data)
		if strings.Contains(content, "test_client_id") || strings.Contains(content, "test_client_secret") {
			t.Fatalf("credentials leaked in cache file %s", f)
		}
	}
}
