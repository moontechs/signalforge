package reddit

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moontechs/signalforge/internal/cache"
	"github.com/moontechs/signalforge/internal/domain"
	"github.com/moontechs/signalforge/internal/storage"
)

func marshalListing(t *testing.T, after string, posts ...postResponse) string {
	t.Helper()
	children := make([]listingChild, 0, len(posts))
	for index := range posts {
		children = append(children, listingChild{Kind: "t3", Data: posts[index]})
	}
	body, err := json.Marshal(listingResponse{Data: listingData{After: after, Children: children}})
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func newCollectorForTest(t *testing.T, cfg *ConfigValues, transport transport) *Collector {
	t.Helper()
	collector, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	collector.WithTransport(transport).WithNow(func() time.Time { return time.Unix(1_000, 0).UTC() })
	return collector
}

func tokenResponseForTest() *http.Response {
	return response(http.StatusOK, `{"access_token":"tok","expires_in":3600}`)
}

func TestNewRejectsDisabledAndInvalidSubreddit(t *testing.T) {
	if _, err := New(nil); !errors.Is(err, ErrDisabled) {
		t.Fatalf("nil config error = %v", err)
	}
	if _, err := New(&ConfigValues{}); !errors.Is(err, ErrDisabled) {
		t.Fatalf("disabled error = %v", err)
	}
	if _, err := New(&ConfigValues{Enabled: true, Subreddits: []string{"bad/name"}}); !errors.Is(err, ErrInvalidSubreddit) {
		t.Fatalf("invalid subreddit error = %v", err)
	}
}

func TestCollectUsesConfiguredCommentsForZeroValueRequest(t *testing.T) {
	commentBody, err := os.ReadFile("../../../testdata/reddit/comments.json")
	if err != nil {
		t.Fatal(err)
	}
	listing := marshalListing(t, "", postResponse{
		ID:          "abc123",
		Title:       "problem",
		Selftext:    "body",
		Subreddit:   "smallbusiness",
		CreatedUTC:  100,
		NumComments: 3,
	})
	var commentsRequested atomic.Bool
	collector := newCollectorForTest(t, &ConfigValues{
		Enabled:            true,
		Subreddits:         []string{"smallbusiness"},
		MaxPostsPerRun:     1,
		MaxCommentsPerPost: 20,
		MaxRequests:        10,
	}, transportFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case req.URL.Host == "www.reddit.com":
			return tokenResponseForTest(), nil
		case strings.Contains(req.URL.Path, "/comments/"):
			commentsRequested.Store(true)
			return response(http.StatusOK, string(commentBody)), nil
		default:
			return response(http.StatusOK, listing), nil
		}
	}))

	signals, err := collector.Collect(context.Background(), domain.CollectRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !commentsRequested.Load() || len(signals) != 1 || len(signals[0].Comments) != 3 {
		t.Fatalf("comments requested=%v signals=%+v", commentsRequested.Load(), signals)
	}
	if collector.Stats().Requests != 3 {
		t.Fatalf("stats = %+v", collector.Stats())
	}
}

func TestCollectPaginatesFiltersAndDeduplicatesAcrossSubreddits(t *testing.T) {
	firstPage := marshalListing(t, "next",
		postResponse{ID: "old", Title: "old", Subreddit: "one", CreatedUTC: 98},
		postResponse{ID: "blank", Subreddit: "one", CreatedUTC: 500},
		postResponse{ID: "duplicate", Title: "duplicate", Subreddit: "one", CreatedUTC: 100},
	)
	secondPage := marshalListing(t, "",
		postResponse{ID: "duplicate", Title: "duplicate", Subreddit: "one", CreatedUTC: 100},
		postResponse{ID: "page-two", Title: "page two", Subreddit: "one", CreatedUTC: 300},
	)
	secondSubreddit := marshalListing(t, "",
		postResponse{ID: "page-two", Title: "page two", Subreddit: "two", CreatedUTC: 300},
		postResponse{ID: "sub-two", Title: "sub two", Subreddit: "two", CreatedUTC: 200},
	)
	collector := newCollectorForTest(t, &ConfigValues{
		Enabled:        true,
		Subreddits:     []string{"one", "two"},
		MaxPostsPerRun: 3,
		MaxRequests:    10,
	}, transportFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "www.reddit.com" {
			return tokenResponseForTest(), nil
		}
		switch {
		case strings.Contains(req.URL.Path, "/r/one/") && req.URL.Query().Get("after") == "":
			return response(http.StatusOK, firstPage), nil
		case strings.Contains(req.URL.Path, "/r/one/") && req.URL.Query().Get("after") == "next":
			return response(http.StatusOK, secondPage), nil
		case strings.Contains(req.URL.Path, "/r/two/"):
			return response(http.StatusOK, secondSubreddit), nil
		default:
			return nil, errors.New("unexpected request: " + req.URL.String())
		}
	}))

	signals, err := collector.Collect(context.Background(), domain.CollectRequest{Since: time.Unix(99, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if len(signals) != 3 {
		t.Fatalf("signals = %+v", signals)
	}
	want := []string{"page-two", "sub-two", "duplicate"}
	for index, sourceID := range want {
		if signals[index].SourceID != sourceID {
			t.Fatalf("signal %d = %q, want %q", index, signals[index].SourceID, sourceID)
		}
	}
	if collector.Stats().Requests != 4 {
		t.Fatalf("stats = %+v", collector.Stats())
	}
}

func TestCollectPreservesPostWhenCommentsFail(t *testing.T) {
	listing := marshalListing(t, "",
		postResponse{ID: "failed", Title: "failed comments", Subreddit: "go", CreatedUTC: 100},
		postResponse{ID: "success", Title: "successful comments", Subreddit: "go", CreatedUTC: 200},
	)
	successComments := `[
		{"data":{"children":[]}},
		{"data":{"children":[{"kind":"t1","data":{"id":"comment","body":"useful","created_utc":201,"replies":""}}]}}
	]`
	collector := newCollectorForTest(t, &ConfigValues{
		Enabled:            true,
		Subreddits:         []string{"go"},
		MaxPostsPerRun:     2,
		MaxCommentsPerPost: 2,
		MaxRequests:        20,
	}, transportFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case req.URL.Host == "www.reddit.com":
			return tokenResponseForTest(), nil
		case strings.Contains(req.URL.Path, "/comments/failed"):
			return response(http.StatusInternalServerError, `{}`), nil
		case strings.Contains(req.URL.Path, "/comments/success"):
			return response(http.StatusOK, successComments), nil
		default:
			return response(http.StatusOK, listing), nil
		}
	}))
	collector.client.backoff = func(int) time.Duration { return 0 }

	signals, err := collector.Collect(context.Background(), domain.CollectRequest{})
	if !errors.Is(err, ErrRetriesExhausted) {
		t.Fatalf("error = %v, want retry exhaustion", err)
	}
	if len(signals) != 2 {
		t.Fatalf("partial signals = %+v", signals)
	}
	commentsByID := make(map[string]int, len(signals))
	for _, signal := range signals {
		commentsByID[signal.SourceID] = len(signal.Comments)
	}
	if commentsByID["failed"] != 0 || commentsByID["success"] != 1 {
		t.Fatalf("comments by post = %v", commentsByID)
	}
}

func TestCollectHonorsRequestCapWithConcurrentComments(t *testing.T) {
	listing := marshalListing(t, "",
		postResponse{ID: "one", Title: "one", Subreddit: "go", CreatedUTC: 100},
		postResponse{ID: "two", Title: "two", Subreddit: "go", CreatedUTC: 200},
		postResponse{ID: "three", Title: "three", Subreddit: "go", CreatedUTC: 300},
	)
	var calls atomic.Int32
	collector := newCollectorForTest(t, &ConfigValues{
		Enabled:            true,
		Subreddits:         []string{"go"},
		MaxPostsPerRun:     3,
		MaxCommentsPerPost: 1,
		MaxRequests:        3,
	}, transportFunc(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		if req.URL.Host == "www.reddit.com" {
			return tokenResponseForTest(), nil
		}
		if strings.Contains(req.URL.Path, "/comments/") {
			return response(http.StatusOK, `[]`), nil
		}
		return response(http.StatusOK, listing), nil
	}))

	signals, err := collector.Collect(context.Background(), domain.CollectRequest{})
	if !errors.Is(err, ErrRequestCap) {
		t.Fatalf("error = %v, want request cap", err)
	}
	if len(signals) != 3 {
		t.Fatalf("signals = %+v", signals)
	}
	if calls.Load() != 3 || collector.Stats().Requests != 3 {
		t.Fatalf("calls=%d stats=%+v", calls.Load(), collector.Stats())
	}
}

func TestCollectBoundsCommentConcurrency(t *testing.T) {
	posts := make([]postResponse, 10)
	for index := range posts {
		posts[index] = postResponse{
			ID:         string(rune('a' + index)),
			Title:      "post",
			Subreddit:  "go",
			CreatedUTC: float64(100 + index),
		}
	}
	listing := marshalListing(t, "", posts...)
	started := make(chan struct{}, len(posts))
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseAll := func() {
		releaseOnce.Do(func() {
			close(release)
		})
	}
	defer releaseAll()
	var active, maximum atomic.Int32
	collector := newCollectorForTest(t, &ConfigValues{
		Enabled:            true,
		Subreddits:         []string{"go"},
		MaxPostsPerRun:     len(posts),
		MaxCommentsPerPost: 1,
		MaxRequests:        20,
	}, transportFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "www.reddit.com" {
			return tokenResponseForTest(), nil
		}
		if !strings.Contains(req.URL.Path, "/comments/") {
			return response(http.StatusOK, listing), nil
		}
		current := active.Add(1)
		for {
			observed := maximum.Load()
			if current <= observed || maximum.CompareAndSwap(observed, current) {
				break
			}
		}
		started <- struct{}{}
		<-release
		active.Add(-1)
		return response(http.StatusOK, `[]`), nil
	}))

	type result struct {
		signals []domain.RawSignal
		err     error
	}
	done := make(chan result, 1)
	go func() {
		signals, err := collector.Collect(context.Background(), domain.CollectRequest{})
		done <- result{signals: signals, err: err}
	}()
	for range 5 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("five comment requests did not start")
		}
	}
	releaseAll()
	out := <-done
	if out.err != nil {
		t.Fatal(out.err)
	}
	if len(out.signals) != len(posts) || maximum.Load() != 5 {
		t.Fatalf("signals=%d maximum concurrency=%d", len(out.signals), maximum.Load())
	}
}

func TestCollectReportsPerRunCacheStats(t *testing.T) {
	listing := marshalListing(t, "", postResponse{ID: "post", Title: "post", Subreddit: "go", CreatedUTC: 100})
	var calls atomic.Int32
	collector := newCollectorForTest(t, &ConfigValues{
		Enabled:        true,
		Subreddits:     []string{"go"},
		MaxPostsPerRun: 1,
		MaxRequests:    2,
	}, transportFunc(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		if req.URL.Host == "www.reddit.com" {
			return tokenResponseForTest(), nil
		}
		return response(http.StatusOK, listing), nil
	}))
	collector.WithCache(cache.NewCache(storage.New(t.TempDir()), "reddit"))

	if _, err := collector.Collect(context.Background(), domain.CollectRequest{}); err != nil {
		t.Fatal(err)
	}
	if collector.Stats() != (Stats{Requests: 2}) {
		t.Fatalf("first stats = %+v", collector.Stats())
	}
	if _, err := collector.Collect(context.Background(), domain.CollectRequest{}); err != nil {
		t.Fatal(err)
	}
	if collector.Stats() != (Stats{CacheHits: 1}) || calls.Load() != 2 {
		t.Fatalf("second stats=%+v transport calls=%d", collector.Stats(), calls.Load())
	}
}

func TestCollectorImplementsSourceCollector(t *testing.T) {
	var collector domain.SourceCollector = newCollectorForTest(t, &ConfigValues{
		Enabled:        true,
		Subreddits:     []string{"go"},
		MaxPostsPerRun: 1,
	}, transportFunc(func(*http.Request) (*http.Response, error) {
		return nil, io.EOF
	}))
	if collector.Name() != SourceName {
		t.Fatalf("collector name = %q", collector.Name())
	}
}

func TestCollectStopsOnRepeatedPaginationCursor(t *testing.T) {
	listing := marshalListing(t, "same", postResponse{ID: "blank", Subreddit: "go"})
	var listingCalls atomic.Int32
	collector := newCollectorForTest(t, &ConfigValues{
		Enabled:        true,
		Subreddits:     []string{"go"},
		MaxPostsPerRun: 1,
		MaxRequests:    10,
	}, transportFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "www.reddit.com" {
			return tokenResponseForTest(), nil
		}
		listingCalls.Add(1)
		return response(http.StatusOK, listing), nil
	}))

	signals, err := collector.Collect(context.Background(), domain.CollectRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(signals) != 0 || listingCalls.Load() != 2 {
		t.Fatalf("signals=%v listing calls=%d", signals, listingCalls.Load())
	}
}
