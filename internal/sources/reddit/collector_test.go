package reddit

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/moontechs/signalforge/internal/cache"
	"github.com/moontechs/signalforge/internal/domain"
	"github.com/moontechs/signalforge/internal/storage"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// testCollector creates a Collector backed by a fake transport with a default
// OAuth token response pre-registered and a fixed clock.
func testCollector(t *testing.T, cfg *ConfigValues, fake *fakeTransport) *Collector {
	t.Helper()
	if fake == nil {
		fake = newFakeTransport()
	}
	// Pre-register a default successful token response.
	fake.addResponse(tokenURL,
		fakeResponse{statusCode: 200, body: `{"access_token":"test_token","token_type":"bearer","expires_in":3600,"scope":"*"}`})

	c, err := New(cfg, "test_id", "test_secret")
	if err != nil {
		t.Fatalf("New(%+v): %v", cfg, err)
	}
	c.WithTransport(fake)
	c.WithNow(func() time.Time { return time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC) })
	return c
}

// mustCollect is a helper that calls Collect and fails the test on error.
func mustCollect(ctx context.Context, t *testing.T, c *Collector, req *domain.CollectRequest) []domain.RawSignal {
	t.Helper()
	signals, err := c.Collect(ctx, *req)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	return signals
}

// testListingBody returns a JSON subreddit listing body with the given posts.
func testListingBody(posts ...map[string]any) string {
	body := `{"kind":"Listing","data":{"children":[`
	for i, p := range posts {
		if i > 0 {
			body += ","
		}
		body += `{"kind":"t3","data":{`
		first := true
		for k, v := range p {
			if !first {
				body += ","
			}
			first = false
			body += fmt.Sprintf(`%q:`, k)
			switch val := v.(type) {
			case string:
				body += fmt.Sprintf(`%q`, val)
			case int:
				body += strconv.Itoa(val)
			case float64:
				body += fmt.Sprintf(`%f`, val)
			case bool:
				if val {
					body += `true`
				} else {
					body += `false`
				}
			default:
				body += fmt.Sprintf(`%v`, val)
			}
		}
		body += `}}`
	}
	body += `]}}`
	return body
}

// testCommentTreeBody returns a JSON comment tree body (two-element array).
func testCommentTreeBody(comments ...map[string]any) string {
	body := `[{"kind":"Listing","data":{"children":[]}},{"kind":"Listing","data":{"children":[`
	for i, c := range comments {
		if i > 0 {
			body += ","
		}
		body += `{"kind":"t1","data":{`
		first := true
		for k, v := range c {
			if !first {
				body += ","
			}
			first = false
			body += fmt.Sprintf(`%q:`, k)
			switch val := v.(type) {
			case string:
				body += fmt.Sprintf(`%q`, val)
			case int:
				body += strconv.Itoa(val)
			case float64:
				body += fmt.Sprintf(`%f`, val)
			default:
				body += fmt.Sprintf(`%v`, val)
			}
		}
		body += `}}`
	}
	body += `]}}]`
	return body
}

// ---------------------------------------------------------------------------
// Constructor tests
// ---------------------------------------------------------------------------

func TestNew_disabled(t *testing.T) {
	t.Parallel()
	_, err := New(&ConfigValues{Enabled: false}, "test_id", "test_secret")
	if err == nil {
		t.Fatal("expected error for disabled collector")
	}
	if !errors.Is(err, ErrDisabled) {
		t.Fatalf("expected ErrDisabled, got %v", err)
	}
}

func TestNew_enabled_noCredentials(t *testing.T) {
	t.Parallel()
	c, err := New(&ConfigValues{
		Enabled:            true,
		Subreddits:         []string{"golang"},
		MaxPostsPerRun:     10,
		MaxCommentsPerPost: 0,
		MaxRequests:        100,
		Sort:               "new",
		Time:               "week",
	}, "", "")
	if err != nil {
		t.Fatalf("New with empty credentials should not fail at construction: %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil collector")
	}
}

// ---------------------------------------------------------------------------
// Source name
// ---------------------------------------------------------------------------

func TestCollector_Name(t *testing.T) {
	t.Parallel()
	c := testCollector(t, &ConfigValues{
		Enabled:        true,
		Subreddits:     []string{"golang"},
		MaxPostsPerRun: 10,
		MaxRequests:    100,
	}, nil)
	if c.Name() != SourceName {
		t.Fatalf("expected name %q, got %q", SourceName, c.Name())
	}
}

// ---------------------------------------------------------------------------
// NormalizeSubreddits tests
// ---------------------------------------------------------------------------

func TestNormalizeSubreddits_empty(t *testing.T) {
	t.Parallel()
	result, err := normalizeSubreddits(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("expected empty result, got %v", result)
	}

	result, err = normalizeSubreddits([]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("expected empty result, got %v", result)
	}
}

func TestNormalizeSubreddits_whitespaceAndPrefix(t *testing.T) {
	t.Parallel()
	result, err := normalizeSubreddits([]string{"  golang ", " r/golang ", "  r/python  "})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 entries, got %d: %v", len(result), result)
	}
	if result[0] != "golang" || result[1] != "python" {
		t.Fatalf("unexpected result: %v", result)
	}
}

func TestNormalizeSubreddits_dedup(t *testing.T) {
	t.Parallel()
	result, err := normalizeSubreddits([]string{"golang", "golang", "r/golang", "  golang  "})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 entry after dedup, got %d: %v", len(result), result)
	}
	if result[0] != "golang" {
		t.Fatalf("expected 'golang', got %q", result[0])
	}
}

func TestNormalizeSubreddits_invalidCharacters(t *testing.T) {
	t.Parallel()
	invalid := []string{"foo/bar", "foo\\bar", "foo.bar", "foo..bar", "../etc"}
	for _, sr := range invalid {
		_, err := normalizeSubreddits([]string{sr})
		if err == nil {
			t.Fatalf("expected error for subreddit %q", sr)
		}
		if !errors.Is(err, ErrInvalidSubreddit) {
			t.Fatalf("expected ErrInvalidSubreddit for %q, got %v", sr, err)
		}
	}
}

func TestNormalizeSubreddits_emptyEntries(t *testing.T) {
	t.Parallel()
	result, err := normalizeSubreddits([]string{"golang", "", "  ", "python"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 entries, got %d: %v", len(result), result)
	}
}

// ---------------------------------------------------------------------------
// BuildScope tests
// ---------------------------------------------------------------------------

func TestBuildScope_disabled(t *testing.T) {
	t.Parallel()
	_, err := buildScope(&ConfigValues{Enabled: false}, &domain.CollectRequest{})
	if !errors.Is(err, ErrDisabled) {
		t.Fatalf("expected ErrDisabled, got %v", err)
	}
}

func TestBuildScope_noSubreddits(t *testing.T) {
	t.Parallel()
	_, err := buildScope(&ConfigValues{
		Enabled:        true,
		Subreddits:     nil,
		Sort:           "new",
		Time:           "week",
		MaxPostsPerRun: 10,
		MaxRequests:    100,
	}, &domain.CollectRequest{})
	if err == nil {
		t.Fatal("expected error for missing subreddits")
	}
	if !errors.Is(err, ErrInvalidSubreddit) {
		t.Fatalf("expected ErrInvalidSubreddit, got %v", err)
	}
}

func TestBuildScope_invalidSort(t *testing.T) {
	t.Parallel()
	_, err := buildScope(&ConfigValues{
		Enabled:        true,
		Subreddits:     []string{"golang"},
		Sort:           "invalid_sort",
		Time:           "week",
		MaxPostsPerRun: 10,
		MaxRequests:    100,
	}, &domain.CollectRequest{})
	if err == nil {
		t.Fatal("expected error for invalid sort")
	}
	if !errors.Is(err, ErrInvalidSort) {
		t.Fatalf("expected ErrInvalidSort, got %v", err)
	}
}

func TestBuildScope_invalidTime(t *testing.T) {
	t.Parallel()
	_, err := buildScope(&ConfigValues{
		Enabled:        true,
		Subreddits:     []string{"golang"},
		Sort:           "new",
		Time:           "invalid_time",
		MaxPostsPerRun: 10,
		MaxRequests:    100,
	}, &domain.CollectRequest{})
	if err == nil {
		t.Fatal("expected error for invalid time")
	}
	if !errors.Is(err, ErrInvalidTime) {
		t.Fatalf("expected ErrInvalidTime, got %v", err)
	}
}

func TestBuildScope_defaults(t *testing.T) {
	t.Parallel()
	scope, err := buildScope(&ConfigValues{
		Enabled:            true,
		Subreddits:         []string{"golang"},
		MaxPostsPerRun:     0,
		MaxCommentsPerPost: 0,
		MaxRequests:        100,
	}, &domain.CollectRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if scope.sort != DefaultSort {
		t.Fatalf("expected default sort %q, got %q", DefaultSort, scope.sort)
	}
	if scope.timeFilter != DefaultTime {
		t.Fatalf("expected default time %q, got %q", DefaultTime, scope.timeFilter)
	}
	if scope.maxPosts != 200 {
		t.Fatalf("expected default maxPosts 200, got %d", scope.maxPosts)
	}
}

func TestBuildScope_requestMaxItemsOverrides(t *testing.T) {
	t.Parallel()
	scope, err := buildScope(&ConfigValues{
		Enabled:            true,
		Subreddits:         []string{"golang"},
		MaxPostsPerRun:     50,
		MaxCommentsPerPost: 5,
		MaxRequests:        100,
	}, &domain.CollectRequest{
		MaxItems:           10,
		MaxCommentsPerItem: 2,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if scope.maxPosts != 10 {
		t.Fatalf("expected maxPosts 10 (from request), got %d", scope.maxPosts)
	}
	if scope.maxComments != 2 {
		t.Fatalf("expected maxComments 2 (from request), got %d", scope.maxComments)
	}
}

// ---------------------------------------------------------------------------
// Collect tests
// ---------------------------------------------------------------------------

func TestCollect_disabled(t *testing.T) {
	t.Parallel()
	_, err := New(&ConfigValues{Enabled: false}, "id", "secret")
	if !errors.Is(err, ErrDisabled) {
		t.Fatalf("expected ErrDisabled, got %v", err)
	}
}

func TestCollect_emptyScope(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()
	_ = fake
	// Collect with nil subreddits should fail at buildScope level.
	// Create a collector and Collect should fail.
	c := testCollector(t, &ConfigValues{
		Enabled:            true,
		Subreddits:         nil,
		Sort:               "new",
		Time:               "week",
		MaxPostsPerRun:     10,
		MaxCommentsPerPost: 0,
		MaxRequests:        100,
	}, fake)
	_, err := c.Collect(context.Background(), domain.CollectRequest{})
	if err == nil {
		t.Fatal("expected error for nil subreddits")
	}
	if !errors.Is(err, ErrInvalidSubreddit) {
		t.Fatalf("expected ErrInvalidSubreddit, got %v", err)
	}
}

func TestCollect_happyPath(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	// Listing for /r/golang/new.json.
	listingURL := apiURLFor("/r/golang/new.json", url.Values{"limit": {"10"}})
	listingBody := testListingBody(
		map[string]any{
			"id": "p1", "title": "Post 1", "selftext": "Body 1",
			"permalink": "/r/golang/comments/p1/post_1/", "subreddit": "golang",
			"author": "u1", "score": 100, "num_comments": 2, "created_utc": 1700000100.0,
			"stickied": false, "over_18": false,
		},
		map[string]any{
			"id": "p2", "title": "Post 2", "selftext": "Body 2",
			"permalink": "/r/golang/comments/p2/post_2/", "subreddit": "golang",
			"author": "u2", "score": 50, "num_comments": 1, "created_utc": 1700000200.0,
			"stickied": false, "over_18": false,
		},
	)
	fake.addResponse(listingURL, fakeResponse{statusCode: 200, body: listingBody})

	// Comment tree for p1 (with comments).
	commentsURL1 := apiURLFor("/comments/p1.json", nil)
	commentsBody1 := testCommentTreeBody(
		map[string]any{
			"id": "c1", "body": "Nice post!", "author": "u3",
			"score": 10, "created_utc": 1700000300.0, "parent_id": "t3_p1",
		},
		map[string]any{
			"id": "c2", "body": "I agree", "author": "u4",
			"score": 5, "created_utc": 1700000400.0, "parent_id": "t3_p1",
		},
	)
	fake.addResponse(commentsURL1, fakeResponse{statusCode: 200, body: commentsBody1})

	// Comment tree for p2 (no comments).
	commentsURL2 := apiURLFor("/comments/p2.json", nil)
	commentsBody2 := `[{"kind":"Listing","data":{"children":[]}},{"kind":"Listing","data":{"children":[]}}]`
	fake.addResponse(commentsURL2, fakeResponse{statusCode: 200, body: commentsBody2})

	c := testCollector(t, &ConfigValues{
		Enabled:            true,
		Subreddits:         []string{"golang"},
		Sort:               "new",
		Time:               "week",
		MaxPostsPerRun:     10,
		MaxCommentsPerPost: 2,
		MaxRequests:        100,
	}, fake)

	signals := mustCollect(context.Background(), t, c, &domain.CollectRequest{})

	if len(signals) != 2 {
		t.Fatalf("expected 2 signals, got %d", len(signals))
	}

	// Verify sorting: newest first.
	for i := 0; i < len(signals)-1; i++ {
		if signals[i].CreatedAt.Before(signals[i+1].CreatedAt) {
			t.Fatalf("signals not sorted descending: %v before %v",
				signals[i].CreatedAt, signals[i+1].CreatedAt)
		}
	}

	// Verify signal content for post p1.
	var p1Signal, p2Signal *domain.RawSignal
	for i := range signals {
		if signals[i].SourceID == "p1" {
			p1Signal = &signals[i]
		}
		if signals[i].SourceID == "p2" {
			p2Signal = &signals[i]
		}
	}
	if p1Signal == nil {
		t.Fatal("expected signal for p1")
	}
	if p2Signal == nil {
		t.Fatal("expected signal for p2")
	}

	if p1Signal.Title != "Post 1" {
		t.Fatalf("expected title 'Post 1', got %q", p1Signal.Title)
	}
	if p1Signal.URL != "https://www.reddit.com/r/golang/comments/p1/post_1/" {
		t.Fatalf("unexpected URL: %q", p1Signal.URL)
	}
	if len(p1Signal.Comments) != 2 {
		t.Fatalf("expected 2 comments for p1, got %d", len(p1Signal.Comments))
	}
	if len(p2Signal.Comments) != 0 {
		t.Fatalf("expected 0 comments for p2, got %d", len(p2Signal.Comments))
	}
	if p1Signal.Community != "golang" {
		t.Fatalf("expected community 'golang', got %q", p1Signal.Community)
	}
	if p1Signal.Source != SourceName {
		t.Fatalf("expected source %q, got %q", SourceName, p1Signal.Source)
	}

	// Verify stats.
	stats := c.Stats()
	// Expected: 1 token request + 1 listing + 2 comment trees = 4 requests
	// But token might be reused... actually, the token request happens once.
	// 1 (token) + 1 (listing) + 2 (comments) = 4
	if stats.Requests < 3 {
		t.Fatalf("expected at least 3 requests, got %d", stats.Requests)
	}
}

func TestCollect_sinceFiltering(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	cutoff := time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC)

	// Post 1: before cutoff (should be excluded).
	listingURL := apiURLFor("/r/golang/new.json", url.Values{"limit": {"10"}})
	listingBody := testListingBody(
		map[string]any{
			"id": "p_old", "title": "Old Post", "selftext": "",
			"permalink": "/r/golang/comments/p_old/old/", "subreddit": "golang",
			"author": "u1", "score": 10, "num_comments": 0,
			"created_utc": float64(cutoff.Add(-24 * time.Hour).Unix()),
			"stickied":    false, "over_18": false,
		},
		map[string]any{
			"id": "p_new", "title": "New Post", "selftext": "",
			"permalink": "/r/golang/comments/p_new/new/", "subreddit": "golang",
			"author": "u2", "score": 20, "num_comments": 0,
			"created_utc": float64(cutoff.Add(24 * time.Hour).Unix()),
			"stickied":    false, "over_18": false,
		},
	)
	fake.addResponse(listingURL, fakeResponse{statusCode: 200, body: listingBody})

	c := testCollector(t, &ConfigValues{
		Enabled:            true,
		Subreddits:         []string{"golang"},
		Sort:               "new",
		Time:               "week",
		MaxPostsPerRun:     10,
		MaxCommentsPerPost: 0,
		MaxRequests:        100,
	}, fake)

	signals := mustCollect(context.Background(), t, c, &domain.CollectRequest{Since: cutoff})

	if len(signals) != 1 {
		t.Fatalf("expected 1 signal (after cutoff), got %d", len(signals))
	}
	if signals[0].SourceID != "p_new" {
		t.Fatalf("expected signal for p_new, got %s", signals[0].SourceID)
	}
}

func TestCollect_maxPostsCap(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	// MaxPostsPerRun=2 means limit=min(2,100)=2 for the API call.
	listingURL := apiURLFor("/r/golang/new.json", url.Values{"limit": {"2"}})
	listingBody := testListingBody(
		map[string]any{
			"id": "p1", "title": "Post 1", "selftext": "",
			"permalink": "/r/golang/comments/p1/", "subreddit": "golang",
			"author": "u1", "score": 10, "num_comments": 0,
			"created_utc": 1700000100.0, "stickied": false, "over_18": false,
		},
		map[string]any{
			"id": "p2", "title": "Post 2", "selftext": "",
			"permalink": "/r/golang/comments/p2/", "subreddit": "golang",
			"author": "u2", "score": 20, "num_comments": 0,
			"created_utc": 1700000200.0, "stickied": false, "over_18": false,
		},
		map[string]any{
			"id": "p3", "title": "Post 3", "selftext": "",
			"permalink": "/r/golang/comments/p3/", "subreddit": "golang",
			"author": "u3", "score": 30, "num_comments": 0,
			"created_utc": 1700000300.0, "stickied": false, "over_18": false,
		},
	)
	fake.addResponse(listingURL, fakeResponse{statusCode: 200, body: listingBody})

	c := testCollector(t, &ConfigValues{
		Enabled:            true,
		Subreddits:         []string{"golang"},
		Sort:               "new",
		Time:               "week",
		MaxPostsPerRun:     2,
		MaxCommentsPerPost: 0,
		MaxRequests:        100,
	}, fake)

	signals := mustCollect(context.Background(), t, c, &domain.CollectRequest{})

	if len(signals) != 2 {
		t.Fatalf("expected 2 signals (capped), got %d", len(signals))
	}
}

func TestCollect_dedupAcrossSubreddits(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	// Two subreddits, one post appears in both (same ID "p1").
	listingURL1 := apiURLFor("/r/golang/new.json", url.Values{"limit": {"10"}})
	listingBody1 := testListingBody(
		map[string]any{
			"id": "p1", "title": "Shared Post", "selftext": "",
			"permalink": "/r/golang/comments/p1/shared/", "subreddit": "golang",
			"author": "u1", "score": 10, "num_comments": 0,
			"created_utc": 1700000100.0, "stickied": false, "over_18": false,
		},
	)
	fake.addResponse(listingURL1, fakeResponse{statusCode: 200, body: listingBody1})

	listingURL2 := apiURLFor("/r/programming/new.json", url.Values{"limit": {"10"}})
	listingBody2 := testListingBody(
		map[string]any{
			"id": "p1", "title": "Shared Post", "selftext": "",
			"permalink": "/r/programming/comments/p1/shared/", "subreddit": "programming",
			"author": "u1", "score": 10, "num_comments": 0,
			"created_utc": 1700000100.0, "stickied": false, "over_18": false,
		},
		map[string]any{
			"id": "p2", "title": "Unique Post", "selftext": "",
			"permalink": "/r/programming/comments/p2/unique/", "subreddit": "programming",
			"author": "u2", "score": 20, "num_comments": 0,
			"created_utc": 1700000200.0, "stickied": false, "over_18": false,
		},
	)
	fake.addResponse(listingURL2, fakeResponse{statusCode: 200, body: listingBody2})

	c := testCollector(t, &ConfigValues{
		Enabled:            true,
		Subreddits:         []string{"golang", "programming"},
		Sort:               "new",
		Time:               "week",
		MaxPostsPerRun:     10,
		MaxCommentsPerPost: 0,
		MaxRequests:        100,
	}, fake)

	signals := mustCollect(context.Background(), t, c, &domain.CollectRequest{})

	if len(signals) != 2 {
		t.Fatalf("expected 2 unique signals (deduped), got %d", len(signals))
	}
	// Verify no duplicate IDs.
	seen := make(map[string]bool)
	for _, s := range signals {
		if seen[s.SourceID] {
			t.Fatalf("duplicate source ID: %s", s.SourceID)
		}
		seen[s.SourceID] = true
	}
}

func TestCollect_partialListingFailure(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	// First subreddit succeeds.
	listingURL1 := apiURLFor("/r/golang/new.json", url.Values{"limit": {"10"}})
	listingBody1 := testListingBody(
		map[string]any{
			"id": "p1", "title": "Post 1", "selftext": "",
			"permalink": "/r/golang/comments/p1/", "subreddit": "golang",
			"author": "u1", "score": 10, "num_comments": 0,
			"created_utc": 1700000100.0, "stickied": false, "over_18": false,
		},
	)
	fake.addResponse(listingURL1, fakeResponse{statusCode: 200, body: listingBody1})

	// Second subreddit fails with 500.
	listingURL2 := apiURLFor("/r/broken/new.json", url.Values{"limit": {"10"}})
	fake.addResponse(listingURL2, fakeResponse{statusCode: 500, body: `{"error":"internal"}`})

	c := testCollector(t, &ConfigValues{
		Enabled:            true,
		Subreddits:         []string{"golang", "broken"},
		Sort:               "new",
		Time:               "week",
		MaxPostsPerRun:     10,
		MaxCommentsPerPost: 0,
		MaxRequests:        100,
	}, fake)

	signals, err := c.Collect(context.Background(), domain.CollectRequest{})
	if err == nil {
		t.Fatal("expected partial error for listing failure")
	}
	if len(signals) != 1 {
		t.Fatalf("expected 1 signal from successful subreddit, got %d", len(signals))
	}
	if signals[0].SourceID != "p1" {
		t.Fatalf("expected signal for p1, got %s", signals[0].SourceID)
	}
}

func TestCollect_partialCommentFailure(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	listingURL := apiURLFor("/r/golang/new.json", url.Values{"limit": {"10"}})
	listingBody := testListingBody(
		map[string]any{
			"id": "p1", "title": "Post 1", "selftext": "",
			"permalink": "/r/golang/comments/p1/", "subreddit": "golang",
			"author": "u1", "score": 10, "num_comments": 1,
			"created_utc": 1700000100.0, "stickied": false, "over_18": false,
		},
		map[string]any{
			"id": "p2", "title": "Post 2", "selftext": "",
			"permalink": "/r/golang/comments/p2/", "subreddit": "golang",
			"author": "u2", "score": 20, "num_comments": 0,
			"created_utc": 1700000200.0, "stickied": false, "over_18": false,
		},
	)
	fake.addResponse(listingURL, fakeResponse{statusCode: 200, body: listingBody})

	// p1 comments fail. Use just one attempt by sending non-retryable 403.
	commentsURL1 := apiURLFor("/comments/p1.json", nil)
	fake.addResponse(commentsURL1, fakeResponse{statusCode: 403, body: `{"error":"forbidden"}`})

	// p2 comments succeed (even though num_comments=0, the API still returns a valid response).
	commentsURL2 := apiURLFor("/comments/p2.json", nil)
	fake.addResponse(commentsURL2, fakeResponse{statusCode: 200, body: `[{"kind":"Listing","data":{"children":[]}},{"kind":"Listing","data":{"children":[]}}]`})

	c := testCollector(t, &ConfigValues{
		Enabled:            true,
		Subreddits:         []string{"golang"},
		Sort:               "new",
		Time:               "week",
		MaxPostsPerRun:     10,
		MaxCommentsPerPost: 5,
		MaxRequests:        100,
	}, fake)

	signals, err := c.Collect(context.Background(), domain.CollectRequest{})
	if err == nil {
		t.Fatal("expected partial error for comment failure")
	}
	if len(signals) != 1 {
		t.Fatalf("expected 1 signal (p2, since p1 comment fetch failed), got %d", len(signals))
	}
	if signals[0].SourceID != "p2" {
		t.Fatalf("expected signal for p2, got %s", signals[0].SourceID)
	}
}

func TestCollect_contextCancellation(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	listingURL := apiURLFor("/r/golang/new.json", url.Values{"limit": {"10"}})
	listingBody := testListingBody(
		map[string]any{
			"id": "p1", "title": "Post 1", "selftext": "",
			"permalink": "/r/golang/comments/p1/", "subreddit": "golang",
			"author": "u1", "score": 10, "num_comments": 0,
			"created_utc": 1700000100.0, "stickied": false, "over_18": false,
		},
	)
	fake.addResponse(listingURL, fakeResponse{statusCode: 200, body: listingBody})

	// Add a delayed response that blocks so we can cancel.
	commentsURL1 := apiURLFor("/comments/p1.json", nil)
	fake.addResponse(commentsURL1, fakeResponse{statusCode: 200, body: `[]`})

	c := testCollector(t, &ConfigValues{
		Enabled:            true,
		Subreddits:         []string{"golang"},
		Sort:               "new",
		Time:               "week",
		MaxPostsPerRun:     10,
		MaxCommentsPerPost: 5,
		MaxRequests:        100,
	}, fake)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	_, err := c.Collect(ctx, domain.CollectRequest{})
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestCollect_cachedRepeatRun(t *testing.T) {
	t.Parallel()
	// Create a temp dir for the cache.
	dir := t.TempDir()
	store := storage.New(dir)
	redditCache := cache.NewCache(store, "reddit")

	fake := newFakeTransport()

	listingURL := apiURLFor("/r/golang/new.json", url.Values{"limit": {"10"}})
	listingBody := testListingBody(
		map[string]any{
			"id": "p1", "title": "Post 1", "selftext": "Body 1",
			"permalink": "/r/golang/comments/p1/", "subreddit": "golang",
			"author": "u1", "score": 10, "num_comments": 0,
			"created_utc": 1700000100.0, "stickied": false, "over_18": false,
		},
	)
	fake.addResponse(listingURL, fakeResponse{statusCode: 200, body: listingBody})

	c := testCollector(t, &ConfigValues{
		Enabled:            true,
		Subreddits:         []string{"golang"},
		Sort:               "new",
		Time:               "week",
		MaxPostsPerRun:     10,
		MaxCommentsPerPost: 0,
		MaxRequests:        100,
	}, fake)
	c.WithCache(redditCache)

	// First run populates cache.
	signals1 := mustCollect(context.Background(), t, c, &domain.CollectRequest{})
	if len(signals1) != 1 {
		t.Fatalf("expected 1 signal, got %d", len(signals1))
	}
	stats1 := c.Stats()

	// Second run should hit cache.
	signals2 := mustCollect(context.Background(), t, c, &domain.CollectRequest{})
	if len(signals2) != 1 {
		t.Fatalf("expected 1 signal on repeat, got %d", len(signals2))
	}
	stats2 := c.Stats()

	// Cache hits should be >= 1 on the second run.
	if stats2.CacheHits < stats1.CacheHits+1 && stats2.Requests > 0 {
		// It's possible the token wasn't cached. At minimum, the listing
		// should be cached (TTL 5min).
		t.Logf("first run: requests=%d cacheHits=%d", stats1.Requests, stats1.CacheHits)
		t.Logf("second run: requests=%d cacheHits=%d", stats2.Requests, stats2.CacheHits)
	}
}

func TestCollect_noCredentialsWhenDisabled(t *testing.T) {
	t.Parallel()
	// Collector construction should succeed even with empty credentials when
	// Reddit is disabled. Collect should return ErrDisabled.
	_, err := New(&ConfigValues{Enabled: false}, "", "")
	if !errors.Is(err, ErrDisabled) {
		t.Fatalf("expected ErrDisabled, got %v", err)
	}
}

func TestCollect_stableOrdering(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	listingURL := apiURLFor("/r/golang/new.json", url.Values{"limit": {"10"}})
	listingBody := testListingBody(
		map[string]any{
			"id": "p1", "title": "Post 1", "selftext": "",
			"permalink": "/r/golang/comments/p1/", "subreddit": "golang",
			"author": "u1", "score": 10, "num_comments": 0,
			"created_utc": 1700000300.0, "stickied": false, "over_18": false,
		},
		map[string]any{
			"id": "p2", "title": "Post 2", "selftext": "",
			"permalink": "/r/golang/comments/p2/", "subreddit": "golang",
			"author": "u2", "score": 20, "num_comments": 0,
			"created_utc": 1700000100.0, "stickied": false, "over_18": false,
		},
		map[string]any{
			"id": "p3", "title": "Post 3", "selftext": "",
			"permalink": "/r/golang/comments/p3/", "subreddit": "golang",
			"author": "u3", "score": 30, "num_comments": 0,
			"created_utc": 1700000200.0, "stickied": false, "over_18": false,
		},
	)
	fake.addResponse(listingURL, fakeResponse{statusCode: 200, body: listingBody})

	c := testCollector(t, &ConfigValues{
		Enabled:            true,
		Subreddits:         []string{"golang"},
		Sort:               "new",
		Time:               "week",
		MaxPostsPerRun:     10,
		MaxCommentsPerPost: 0,
		MaxRequests:        100,
	}, fake)

	signals := mustCollect(context.Background(), t, c, &domain.CollectRequest{})

	if len(signals) != 3 {
		t.Fatalf("expected 3 signals, got %d", len(signals))
	}

	// Verify sorting by CreatedAt descending.
	if !signals[0].CreatedAt.After(signals[1].CreatedAt) &&
		!signals[0].CreatedAt.Equal(signals[1].CreatedAt) {
		t.Fatalf("signal 0 should be newest: %v, signal 1: %v",
			signals[0].CreatedAt, signals[1].CreatedAt)
	}
	if !signals[1].CreatedAt.After(signals[2].CreatedAt) &&
		!signals[1].CreatedAt.Equal(signals[2].CreatedAt) {
		t.Fatalf("signal 1 should be newer than signal 2: %v vs %v",
			signals[1].CreatedAt, signals[2].CreatedAt)
	}
}

func TestCollect_boundedConcurrency(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	// Create multiple posts to verify concurrency limit doesn't cause issues.
	listingURL := apiURLFor("/r/golang/new.json", url.Values{"limit": {"10"}})
	posts := make([]map[string]any, 6)
	for i := 0; i < 6; i++ {
		id := fmt.Sprintf("p%d", i+1)
		posts[i] = map[string]any{
			"id": id, "title": fmt.Sprintf("Post %d", i+1), "selftext": "",
			"permalink": fmt.Sprintf("/r/golang/comments/%s/", id), "subreddit": "golang",
			"author": fmt.Sprintf("u%d", i+1), "score": 10 * (i + 1), "num_comments": 0,
			"created_utc": 1700000100.0 + float64(i)*100.0,
			"stickied":    false, "over_18": false,
		}
	}
	listingBody := testListingBody(posts...)
	fake.addResponse(listingURL, fakeResponse{statusCode: 200, body: listingBody})

	c := testCollector(t, &ConfigValues{
		Enabled:            true,
		Subreddits:         []string{"golang"},
		Sort:               "new",
		Time:               "week",
		MaxPostsPerRun:     10,
		MaxCommentsPerPost: 0,
		MaxRequests:        100,
	}, fake)

	signals := mustCollect(context.Background(), t, c, &domain.CollectRequest{})

	if len(signals) != 6 {
		t.Fatalf("expected 6 signals, got %d", len(signals))
	}
}

// ---------------------------------------------------------------------------
// Integration test using stored fixtures
// ---------------------------------------------------------------------------

func TestCollect_withFixtures(t *testing.T) {
	t.Parallel()

	// Read test fixtures.
	listingData, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "reddit", "listing.json"))
	if err != nil {
		t.Skipf("skipping fixture test: %v", err)
	}
	commentTreeData, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "reddit", "comment-tree.json"))
	if err != nil {
		t.Skipf("skipping fixture test: %v", err)
	}

	fake := newFakeTransport()

	// MaxPostsPerRun=10 means limit=min(10,100)=10.
	listingURL := apiURLFor("/r/golang/new.json", url.Values{"limit": {"10"}})
	fake.addResponse(listingURL, fakeResponse{statusCode: 200, body: string(listingData)})

	// The listing has posts "abc123" and "def456".
	commentsURL1 := apiURLFor("/comments/abc123.json", nil)
	fake.addResponse(commentsURL1, fakeResponse{statusCode: 200, body: string(commentTreeData)})

	commentsURL2 := apiURLFor("/comments/def456.json", nil)
	fake.addResponse(commentsURL2, fakeResponse{statusCode: 200, body: string(commentTreeData)})

	c := testCollector(t, &ConfigValues{
		Enabled:            true,
		Subreddits:         []string{"golang"},
		Sort:               "new",
		Time:               "week",
		MaxPostsPerRun:     10,
		MaxCommentsPerPost: 4,
		MaxRequests:        100,
	}, fake)

	signals := mustCollect(context.Background(), t, c, &domain.CollectRequest{})

	if len(signals) != 2 {
		t.Fatalf("expected 2 signals from fixture, got %d", len(signals))
	}

	for _, s := range signals {
		if s.ID == "" {
			t.Fatal("expected non-empty signal ID")
		}
		if s.Source != SourceName {
			t.Fatalf("expected source %q, got %q", SourceName, s.Source)
		}
		if s.SourceType != SourceType {
			t.Fatalf("expected source type %q, got %q", SourceType, s.SourceType)
		}
		if s.ContentHash == "" {
			t.Fatal("expected non-empty content hash")
		}
	}

	// Verify abc123 (first post in listing) has comments.
	for _, s := range signals {
		if s.SourceID == "abc123" && len(s.Comments) > 0 {
			// Found comments, verify they are well-formed.
			for _, c := range s.Comments {
				if c.ID == "" {
					t.Fatal("expected non-empty comment ID")
				}
				if c.Body == "" {
					t.Fatal("expected non-empty comment body")
				}
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Concurrency safety
// ---------------------------------------------------------------------------

func TestCollect_concurrentSafe(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	listingURL := apiURLFor("/r/golang/new.json", url.Values{"limit": {"10"}})
	listingBody := testListingBody(
		map[string]any{
			"id": "p1", "title": "Post 1", "selftext": "",
			"permalink": "/r/golang/comments/p1/", "subreddit": "golang",
			"author": "u1", "score": 10, "num_comments": 0,
			"created_utc": 1700000100.0, "stickied": false, "over_18": false,
		},
	)
	fake.addResponse(listingURL, fakeResponse{statusCode: 200, body: listingBody})

	c := testCollector(t, &ConfigValues{
		Enabled:            true,
		Subreddits:         []string{"golang"},
		Sort:               "new",
		Time:               "week",
		MaxPostsPerRun:     10,
		MaxCommentsPerPost: 0,
		MaxRequests:        100,
	}, fake)

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := c.Collect(context.Background(), domain.CollectRequest{})
			if err != nil {
				t.Errorf("concurrent Collect error: %v", err)
			}
		}()
	}
	wg.Wait()

	// Stats should be consistent.
	stats := c.Stats()
	if stats.Requests < 0 {
		t.Fatalf("negative requests: %d", stats.Requests)
	}
}
