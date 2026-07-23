package stackexchange

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/moontechs/signalforge/internal/storage"
)

// ---- Questions endpoint ----.

func TestClient_questions_success(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()
	body := `{"items":[{"question_id":42,"title":"Test","score":10}],"has_more":false,"quota_remaining":98}`
	fake.addResponse("https://api.stackexchange.com/2.3/questions*",
		fakeResponse{statusCode: 200, body: body})

	c := testClient(fake)
	resp, err := c.questions(t.Context(), "stackoverflow", 0, 0, 1, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Questions) != 1 {
		t.Fatalf("expected 1 question, got %d", len(resp.Questions))
	}
	if resp.Questions[0].QuestionID != 42 {
		t.Fatalf("expected question_id 42, got %d", resp.Questions[0].QuestionID)
	}
	if resp.QuotaRemaining != 98 {
		t.Fatalf("expected quota_remaining 98, got %d", resp.QuotaRemaining)
	}
}

// ---- Answers endpoint ----.

func TestClient_answers_success(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()
	body := `{"items":[{"answer_id":100,"question_id":42,"body_markdown":"Answer body","score":5,"is_accepted":true}],"has_more":false,"quota_remaining":95}`
	fake.addResponse("https://api.stackexchange.com/2.3/questions/42/answers*",
		fakeResponse{statusCode: 200, body: body})

	c := testClient(fake)
	resp, err := c.answers(t.Context(), "stackoverflow", 42, 1, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Answers) != 1 {
		t.Fatalf("expected 1 answer, got %d", len(resp.Answers))
	}
	if resp.Answers[0].AnswerID != 100 {
		t.Fatalf("expected answer_id 100, got %d", resp.Answers[0].AnswerID)
	}
	if !resp.Answers[0].IsAccepted {
		t.Fatal("expected answer to be accepted")
	}
}

// ---- Comments endpoint ----.

func TestClient_comments_success(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()
	body := `{"items":[{"comment_id":200,"post_id":42,"body_markdown":"Comment body","score":3}],"has_more":false,"quota_remaining":94}`
	fake.addResponse("https://api.stackexchange.com/2.3/questions/42/comments*",
		fakeResponse{statusCode: 200, body: body})

	c := testClient(fake)
	resp, err := c.comments(t.Context(), "stackoverflow", 42, 1, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(resp.Comments))
	}
	if resp.Comments[0].CommentID != 200 {
		t.Fatalf("expected comment_id 200, got %d", resp.Comments[0].CommentID)
	}
}

// ---- Forced backoff ----.

func TestClient_backoffDelay(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	// First response returns backoff=2, second is normal.
	firstBody := `{"items":[{"question_id":1}],"has_more":false,"quota_remaining":90,"backoff":2}`
	secondBody := `{"items":[{"question_id":2}],"has_more":false,"quota_remaining":89}`
	url := "https://api.stackexchange.com/2.3/questions*"

	fake.addSequentialResponses(url,
		fakeResponse{statusCode: 200, body: firstBody},
		fakeResponse{statusCode: 200, body: secondBody},
	)

	c := testClient(fake)

	// First call should succeed with backoff=2.
	start := time.Now()
	resp1, err := c.questions(t.Context(), "stackoverflow", 0, 0, 1, 100)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if resp1.Backoff != 2 {
		t.Fatalf("expected backoff=2, got %d", resp1.Backoff)
	}

	// Second call should be delayed by the forced backoff.
	resp2, err := c.questions(t.Context(), "stackoverflow", 0, 0, 1, 100)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if resp2.Questions[0].QuestionID != 2 {
		t.Fatalf("expected question_id 2, got %d", resp2.Questions[0].QuestionID)
	}
	elapsed := time.Since(start)
	if elapsed < 2*time.Second {
		t.Fatalf("expected delay >= 2s due to forced backoff, got %v", elapsed)
	}
}

func TestClient_backoffContextCancellation(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	// Return backoff=10 to force a long wait.
	body := `{"items":[{"question_id":1}],"has_more":false,"quota_remaining":90,"backoff":10}`
	fake.addResponse("https://api.stackexchange.com/2.3/questions*",
		fakeResponse{statusCode: 200, body: body})

	c := testClient(fake)

	// First call consumes the backoff=10 response.
	_, err := c.questions(t.Context(), "stackoverflow", 0, 0, 1, 100)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	// Second call with short timeout should be cancelled during backoff.
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	_, err = c.questions(ctx, "stackoverflow", 0, 0, 1, 100)
	if err == nil {
		t.Fatal("expected error from backoff cancellation, got nil")
	}
	if !errors.Is(err, ErrBackoffCancelled) && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected ErrBackoffCancelled or DeadlineExceeded, got %v", err)
	}
}

// ---- Quota parsing ----.

func TestClient_quotaExhausted(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()
	// quota_remaining == 0 triggers ErrQuotaExhausted.
	body := `{"items":[{"question_id":1}],"has_more":false,"quota_remaining":0}`
	fake.addResponse("https://api.stackexchange.com/2.3/questions*",
		fakeResponse{statusCode: 200, body: body})

	c := testClient(fake)
	_, err := c.questions(t.Context(), "stackoverflow", 0, 0, 1, 100)
	if !errors.Is(err, ErrQuotaExhausted) {
		t.Fatalf("expected ErrQuotaExhausted, got %v", err)
	}
}

// ---- API key in URL ----.

func TestClient_apiKeyInURL(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	c := newClient(fake, &ConfigValues{
		APIKey:  "my-secret-key",
		BaseURL: "https://api.stackexchange.com/2.3",
	})

	// Register a wildcard so the fake catches the URL.
	fake.addResponse("https://api.stackexchange.com/2.3/questions*",
		fakeResponse{statusCode: 200, body: `{"items":[],"quota_remaining":99}`})

	_, err := c.questions(t.Context(), "stackoverflow", 0, 0, 1, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the URL contains the API key.
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.calls) == 0 {
		t.Fatal("no requests recorded")
	}
	query := fake.calls[0].URL.Query()
	if query.Get("key") != "my-secret-key" {
		t.Fatalf("expected key=my-secret-key, got %q", query.Get("key"))
	}
}

// ---- Cache hit ----.

func TestClient_cacheHit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := storage.New(dir)
	fake := newFakeTransport()

	body := `{"items":[{"question_id":99,"title":"Cached","score":5}],"has_more":false,"quota_remaining":98}`
	// Use a broad wildcard to match any questions URL.
	fake.addResponse("https://api.stackexchange.com/2.3/questions*",
		fakeResponse{statusCode: 200, body: body})

	c := testClient(fake)
	c.WithCache(store)

	// First call: HTTP, cache miss.
	resp1, err := c.questions(t.Context(), "stackoverflow", 0, 0, 1, 100)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	// Second call: cache hit, no HTTP.
	fake.resetCallCount()
	resp2, err := c.questions(t.Context(), "stackoverflow", 0, 0, 1, 100)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	// Verify it came from cache (no additional HTTP calls).
	fake.mu.Lock()
	totalCalls := 0
	for _, count := range fake.callCount {
		totalCalls += count
	}
	fake.mu.Unlock()
	if totalCalls != 0 {
		t.Fatalf("expected 0 HTTP calls (cache hit), got %d total", totalCalls)
	}
	if resp1.Questions[0].QuestionID != resp2.Questions[0].QuestionID {
		t.Fatalf("cached result mismatch: %d vs %d", resp1.Questions[0].QuestionID, resp2.Questions[0].QuestionID)
	}
}

// ---- Cache expiration ----.

func TestClient_cacheExpiration(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := storage.New(dir)
	fake := newFakeTransport()

	// Compute cache path for this questions query.
	path := "/questions?filter=withbody&order=desc&pagesize=100&site=stackoverflow&sort=creation"
	sum := sha256.Sum256([]byte(path))
	cacheFile := filepath.Join(dir, "cache", "stackexchange", hex.EncodeToString(sum[:])+".json")

	// Pre-write expired cache (TTL for questions is 5 min, use 10 min old).
	oldResp := cachedResponse{
		Body:        []byte(`{"items":[{"question_id":99,"title":"Stale"}],"has_more":false,"quota_remaining":98}`),
		CollectedAt: time.Now().Add(-10 * time.Minute),
	}
	if err := store.SaveJSON(cacheFile, oldResp); err != nil {
		t.Fatalf("pre-write cache: %v", err)
	}

	freshBody := `{"items":[{"question_id":100,"title":"Fresh"}],"has_more":false,"quota_remaining":97}`
	fake.addResponse("https://api.stackexchange.com/2.3/questions*",
		fakeResponse{statusCode: 200, body: freshBody})

	c := testClient(fake)
	c.WithCache(store)

	resp, err := c.questions(t.Context(), "stackoverflow", 0, 0, 1, 100)
	if err != nil {
		t.Fatalf("call failed: %v", err)
	}
	if resp.Questions[0].QuestionID != 100 {
		t.Fatalf("expected fresh question_id 100, got %d (cache was not expired)", resp.Questions[0].QuestionID)
	}
}

// ---- Retry transient + exhaustion ----.

func TestClient_retryTransientRecovers(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	url := "https://api.stackexchange.com/2.3/questions*"
	fake.addSequentialResponses(url,
		fakeResponse{statusCode: 500, body: `{}`},
		fakeResponse{statusCode: 200, body: `{"items":[{"question_id":1}],"has_more":false,"quota_remaining":99}`},
	)

	c := testClient(fake)
	c.retryMax = 2
	c.retryBackoff = func(_ int) time.Duration { return time.Millisecond }

	resp, err := c.questions(t.Context(), "stackoverflow", 0, 0, 1, 100)
	if err != nil {
		t.Fatalf("expected recovery after retry, got: %v", err)
	}
	if len(resp.Questions) != 1 || resp.Questions[0].QuestionID != 1 {
		t.Fatalf("unexpected response: %+v", resp.Questions)
	}
}

func TestClient_retryExhaustion(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	url := "https://api.stackexchange.com/2.3/questions*"
	fake.addResponse(url, fakeResponse{statusCode: 500, body: `{}`})

	c := testClient(fake)
	c.retryMax = 1
	c.retryBackoff = func(_ int) time.Duration { return time.Millisecond }

	_, err := c.questions(t.Context(), "stackoverflow", 0, 0, 1, 100)
	if err == nil {
		t.Fatal("expected error but got nil")
	}
	if !errors.Is(err, ErrRetriesExhausted) {
		t.Fatalf("expected ErrRetriesExhausted, got %v", err)
	}
}

// ---- Context cancellation ----.

func TestClient_contextCancellation(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()
	fake.addResponse("https://api.stackexchange.com/2.3/questions*",
		fakeResponse{statusCode: 200, body: `{"items":[],"quota_remaining":99}`})

	c := testClient(fake)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := c.questions(ctx, "stackoverflow", 0, 0, 1, 100)
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
	fake.addResponse("https://api.stackexchange.com/2.3/questions*",
		fakeResponse{statusCode: 500, body: `{}`})

	c := testClient(fake)
	c.retryMax = 3
	c.retryBackoff = func(_ int) time.Duration { return time.Second }

	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := c.questions(ctx, "stackoverflow", 0, 0, 1, 100)
	if err == nil {
		t.Fatal("expected error")
	}
}

// ---- Request cap ----.

func TestClient_requestCap(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()
	fake.addResponse("https://api.stackexchange.com/2.3/questions*",
		fakeResponse{statusCode: 200, body: `{"items":[{"question_id":1}],"has_more":false,"quota_remaining":99}`})

	c := testClient(fake)
	c.maxRequests = 1 // allow only 1 request

	// First request should succeed.
	_, err := c.questions(t.Context(), "stackoverflow", 0, 0, 1, 100)
	if err != nil {
		t.Fatalf("first request: %v", err)
	}

	// Second request should hit cap.
	_, err = c.questions(t.Context(), "stackoverflow", 0, 0, 1, 100)
	if !errors.Is(err, ErrRequestCap) {
		t.Fatalf("expected ErrRequestCap, got %v", err)
	}
}

// ---- Malformed JSON ----.

func TestClient_malformedJSON(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()
	fake.addResponse("https://api.stackexchange.com/2.3/questions*",
		fakeResponse{statusCode: 200, body: `{not valid json}`})

	c := testClient(fake)
	_, err := c.questions(t.Context(), "stackoverflow", 0, 0, 1, 100)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if !errors.Is(err, ErrMalformedResponse) {
		t.Fatalf("expected ErrMalformedResponse, got %v", err)
	}
}

// ---- Non-retryable 4xx ----.

func TestClient_nonRetryable4xx(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()
	fake.addResponse("https://api.stackexchange.com/2.3/questions*",
		fakeResponse{statusCode: 400, body: `{"error_id":400,"error_name":"bad_request","error_message":"invalid parameter"}`})

	c := testClient(fake)
	_, err := c.questions(t.Context(), "stackoverflow", 0, 0, 1, 100)
	if err == nil {
		t.Fatal("expected error for 400")
	}
	// Should NOT be ErrRetriesExhausted since 400 is non-retryable.
	if errors.Is(err, ErrRetriesExhausted) {
		t.Fatal("400 should not trigger retry exhaustion")
	}
}

// ---- 429 retry ----.

func TestClient_429retry(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	url := "https://api.stackexchange.com/2.3/questions*"
	fake.addSequentialResponses(url,
		fakeResponse{statusCode: 429, body: `{}`},
		fakeResponse{statusCode: 200, body: `{"items":[{"question_id":1}],"has_more":false,"quota_remaining":99}`},
	)

	c := testClient(fake)
	c.retryMax = 2
	c.retryBackoff = func(_ int) time.Duration { return time.Millisecond }

	resp, err := c.questions(t.Context(), "stackoverflow", 0, 0, 1, 100)
	if err != nil {
		t.Fatalf("expected recovery from 429, got: %v", err)
	}
	if len(resp.Questions) != 1 {
		t.Fatalf("expected 1 question, got %d", len(resp.Questions))
	}
}

// ---- Oversized body ----.

func TestClient_oversizedBody(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	// Create a body that exceeds the max body size (10 bytes).
	bigBody := strings.Repeat("x", 20)
	body := fmt.Sprintf(`{"items":[{"question_id":1,"body_markdown":%q}],"has_more":false,"quota_remaining":99}`, bigBody)
	fake.addResponse("https://api.stackexchange.com/2.3/questions*",
		fakeResponse{statusCode: 200, body: body})

	c := testClient(fake)
	c.maxBodySize = 10

	_, err := c.questions(t.Context(), "stackoverflow", 0, 0, 1, 100)
	if err == nil {
		t.Fatal("expected error for oversized body")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected body size error, got %v", err)
	}
}

// ---- Stats tracking ----.

func TestClient_statsTracking(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	body := `{"items":[{"question_id":1}],"has_more":false,"quota_remaining":99}`
	fake.addResponse("https://api.stackexchange.com/2.3/questions*",
		fakeResponse{statusCode: 200, body: body})

	c := testClient(fake)

	stats := c.Stats()
	if stats.Requests != 0 || stats.CacheHits != 0 {
		t.Fatalf("expected zero stats, got %+v", stats)
	}

	_, err := c.questions(t.Context(), "stackoverflow", 0, 0, 1, 100)
	if err != nil {
		t.Fatalf("call: %v", err)
	}

	stats = c.Stats()
	if stats.Requests != 1 {
		t.Fatalf("expected 1 request, got %d", stats.Requests)
	}
}

// ---- Concurrent access (-race) ----.

func TestClient_concurrentAccess(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	body := `{"items":[{"question_id":%d}],"has_more":false,"quota_remaining":99}`
	// Use wildcard for all question requests.
	fake.addResponse("https://api.stackexchange.com/2.3/questions*",
		fakeResponse{statusCode: 200, body: fmt.Sprintf(body, 1)})

	c := testClient(fake)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := c.questions(t.Context(), "stackoverflow", 0, 0, 1, 100)
			if err != nil {
				t.Logf("concurrent call error: %v", err)
			}
		}()
	}
	wg.Wait()
}

// ---- Pagination ----.

func TestClient_paginationMetadata(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	// First page has has_more=true, second page has has_more=false.
	page1 := `{"items":[{"question_id":1}],"has_more":true,"quota_remaining":90}`
	page2 := `{"items":[{"question_id":2}],"has_more":false,"quota_remaining":89}`

	// Use wildcard plus sequential responses for distinct pages.
	fake.addSequentialResponses("https://api.stackexchange.com/2.3/questions*",
		fakeResponse{statusCode: 200, body: page1},
		fakeResponse{statusCode: 200, body: page2})

	c := testClient(fake)

	resp1, err := c.questions(t.Context(), "stackoverflow", 0, 0, 1, 100)
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if !resp1.HasMore {
		t.Fatal("expected has_more=true on page 1")
	}
	if len(resp1.Questions) != 1 || resp1.Questions[0].QuestionID != 1 {
		t.Fatalf("unexpected page 1: %+v", resp1.Questions)
	}

	resp2, err := c.questions(t.Context(), "stackoverflow", 0, 0, 2, 100)
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if resp2.HasMore {
		t.Fatal("expected has_more=false on page 2")
	}
	if len(resp2.Questions) != 1 || resp2.Questions[0].QuestionID != 2 {
		t.Fatalf("unexpected page 2: %+v", resp2.Questions)
	}
}

// ---- API error in response body ----.

func TestClient_apiError(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()
	body := `{"error_id":502,"error_name":"throttle_violation","error_message":"too many requests from this IP"}`
	fake.addResponse("https://api.stackexchange.com/2.3/questions*",
		fakeResponse{statusCode: 200, body: body})

	c := testClient(fake)
	_, err := c.questions(t.Context(), "stackoverflow", 0, 0, 1, 100)
	if err == nil {
		t.Fatal("expected API error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.ID != 502 {
		t.Fatalf("expected error_id 502, got %d", apiErr.ID)
	}
}

// ---- retry on 408 ----.

func TestClient_408retry(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	url := "https://api.stackexchange.com/2.3/questions*"
	fake.addSequentialResponses(url,
		fakeResponse{statusCode: 408, body: `{}`},
		fakeResponse{statusCode: 200, body: `{"items":[{"question_id":1}],"has_more":false,"quota_remaining":99}`},
	)

	c := testClient(fake)
	c.retryMax = 2
	c.retryBackoff = func(_ int) time.Duration { return time.Millisecond }

	resp, err := c.questions(t.Context(), "stackoverflow", 0, 0, 1, 100)
	if err != nil {
		t.Fatalf("expected recovery from 408, got: %v", err)
	}
	if len(resp.Questions) != 1 {
		t.Fatalf("expected 1 question, got %d", len(resp.Questions))
	}
}

// ---- fromdate/todate parameters ----.

func TestClient_questionsFromDateToDate(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	c := newClient(fake, &ConfigValues{
		BaseURL: "https://api.stackexchange.com/2.3",
	})

	fake.addResponse("https://api.stackexchange.com/2.3/questions*",
		fakeResponse{statusCode: 200, body: `{"items":[],"quota_remaining":99}`})

	_, err := c.questions(t.Context(), "stackoverflow", 1000000, 2000000, 1, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify fromdate/todate in URL.
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.calls) == 0 {
		t.Fatal("no requests recorded")
	}
	q := fake.calls[0].URL.Query()
	if q.Get("fromdate") != "1000000" {
		t.Fatalf("expected fromdate=1000000, got %q", q.Get("fromdate"))
	}
	if q.Get("todate") != "2000000" {
		t.Fatalf("expected todate=2000000, got %q", q.Get("todate"))
	}
}

// ---- JSON serialization round-trip for cachedResponse ----.

func TestClient_cachedResponseJSON(t *testing.T) {
	t.Parallel()
	cr := cachedResponse{
		Body:        []byte(`{"items":[]}`),
		CollectedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	data, err := json.Marshal(cr)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var cr2 cachedResponse
	if err := json.Unmarshal(data, &cr2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !bytes.Equal(cr2.Body, cr.Body) {
		t.Fatalf("body mismatch: %q vs %q", string(cr2.Body), string(cr.Body))
	}
}

// ---- Site and filter query params ----.

func TestClient_questionsQueryParams(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	c := newClient(fake, &ConfigValues{
		BaseURL: "https://api.stackexchange.com/2.3",
	})

	fake.addResponse("https://api.stackexchange.com/2.3/questions*",
		fakeResponse{statusCode: 200, body: `{"items":[],"quota_remaining":99}`})

	_, err := c.questions(t.Context(), "superuser", 0, 0, 3, 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.calls) == 0 {
		t.Fatal("no requests recorded")
	}
	q := fake.calls[0].URL.Query()
	if q.Get("site") != "superuser" {
		t.Fatalf("expected site=superuser, got %q", q.Get("site"))
	}
	if q.Get("page") != "3" {
		t.Fatalf("expected page=3, got %q", q.Get("page"))
	}
	if q.Get("pagesize") != "50" {
		t.Fatalf("expected pagesize=50, got %q", q.Get("pagesize"))
	}
	if q.Get("sort") != "creation" {
		t.Fatalf("expected sort=creation, got %q", q.Get("sort"))
	}
	if q.Get("order") != "desc" {
		t.Fatalf("expected order=desc, got %q", q.Get("order"))
	}
	if q.Get("filter") != "withbody" {
		t.Fatalf("expected filter=withbody, got %q", q.Get("filter"))
	}
}
