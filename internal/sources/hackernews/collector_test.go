package hackernews

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/moontechs/signalforge/internal/domain"
	"github.com/moontechs/signalforge/internal/storage"
)

// ---- Helper to create a collector with a fake transport ----.

func testCollector(t *testing.T, cfg *ConfigValues, fake *fakeTransport) *Collector {
	t.Helper()
	c, err := New(cfg)
	if err != nil {
		t.Fatalf("New(%+v): %v", cfg, err)
	}
	c.WithTransport(fake)
	c.WithNow(func() time.Time { return time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC) })
	return c
}

// ---- Not-enabled ----.

func TestCollector_New_disabled(t *testing.T) {
	t.Parallel()
	_, err := New(&ConfigValues{Enabled: false})
	if err == nil {
		t.Fatal("expected error for disabled collector")
	}
	if !errors.Is(err, ErrDisabled) {
		t.Fatalf("expected ErrDisabled, got %v", err)
	}
}

// ---- Invalid feed ----.

func TestCollector_New_invalidFeed(t *testing.T) {
	t.Parallel()
	_, err := New(&ConfigValues{
		Enabled: true,
		Feeds:   []string{"nonexistent"},
	})
	if err == nil {
		t.Fatal("expected error for invalid feed")
	}
	if !errors.Is(err, ErrInvalidFeed) {
		t.Fatalf("expected ErrInvalidFeed, got %v", err)
	}
}

// ---- Happy path: all feeds produce signals ----.

func TestCollector_happyPath(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	// Feed responses.
	fake.addResponse("https://hacker-news.firebaseio.com/v0/askstories.json",
		fakeResponse{statusCode: 200, body: `[40000101, 40000102]`})
	fake.addResponse("https://hacker-news.firebaseio.com/v0/showstories.json",
		fakeResponse{statusCode: 200, body: `[40000103]`})

	// Item responses (all stories with score >= 5).
	for id, info := range map[int]struct {
		title, url, by string
		time           int64
		score          int
	}{
		40000101: {"Ask HN: Best Go libraries?", "https://example.com/ask", "user1", 1700000000, 50},
		40000102: {"Ask HN: How to learn Rust?", "https://example.com/rust", "user2", 1700000100, 25},
		40000103: {"Show HN: My new project", "https://example.com/project", "user3", 1700000200, 100},
	} {
		body := fmt.Sprintf(
			`{"id":%d,"type":"story","by":%q,"time":%d,"title":%q,"url":%q,"score":%d,"descendants":0}`,
			id, info.by, info.time, info.title, info.url, info.score,
		)
		fake.addResponse(fmt.Sprintf("https://hacker-news.firebaseio.com/v0/item/%d.json", id),
			fakeResponse{statusCode: 200, body: body})
	}

	c := testCollector(t, &ConfigValues{
		Enabled:            true,
		Feeds:              []string{"askstories", "showstories"},
		MaxItemsPerRun:     0, // unlimited
		MaxCommentsPerItem: 0, // no comments
		MinimumScore:       5,
		MaxRequests:        100,
	}, fake)

	signals, err := c.Collect(context.Background(), domain.CollectRequest{})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(signals) != 3 {
		t.Fatalf("expected 3 signals, got %d", len(signals))
	}

	// Verify sorting: newest first.
	for i := 0; i < len(signals)-1; i++ {
		if signals[i].CreatedAt.Before(signals[i+1].CreatedAt) {
			t.Fatalf("signals not sorted descending: %v before %v",
				signals[i].CreatedAt, signals[i+1].CreatedAt)
		}
	}

	// Verify stats.
	stats := c.Stats()
	if stats.Requests != 5 { // 2 feed + 3 item
		t.Fatalf("expected 5 requests, got %d", stats.Requests)
	}
}

// ---- Feed ID de-duplication ----.

func TestCollector_dedupAcrossFeeds(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	// Two feeds with overlapping IDs.
	fake.addResponse("https://hacker-news.firebaseio.com/v0/newstories.json",
		fakeResponse{statusCode: 200, body: `[100, 200, 300]`})
	fake.addResponse("https://hacker-news.firebaseio.com/v0/topstories.json",
		fakeResponse{statusCode: 200, body: `[200, 300, 400]`}) // 200, 300 overlap

	// Item responses.
	for id, info := range map[int]struct {
		title, url, by string
		time           int64
		score          int
	}{
		100: {"Story A", "https://a.com", "u1", 1700000000, 10},
		200: {"Story B", "https://b.com", "u2", 1700000100, 10},
		300: {"Story C", "https://c.com", "u3", 1700000200, 10},
		400: {"Story D", "https://d.com", "u4", 1700000300, 10},
	} {
		body := fmt.Sprintf(
			`{"id":%d,"type":"story","by":%q,"time":%d,"title":%q,"url":%q,"score":%d,"descendants":0}`,
			id, info.by, info.time, info.title, info.url, info.score,
		)
		fake.addResponse(fmt.Sprintf("https://hacker-news.firebaseio.com/v0/item/%d.json", id),
			fakeResponse{statusCode: 200, body: body})
	}

	c := testCollector(t, &ConfigValues{
		Enabled:            true,
		Feeds:              []string{"newstories", "topstories"},
		MaxItemsPerRun:     0,
		MaxCommentsPerItem: 0,
		MinimumScore:       0,
		MaxRequests:        100,
	}, fake)

	signals, err := c.Collect(context.Background(), domain.CollectRequest{})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(signals) != 4 {
		t.Fatalf("expected 4 unique signals (deduped), got %d", len(signals))
	}

	// Verify no duplicates.
	seen := make(map[string]bool)
	for _, s := range signals {
		if seen[s.SourceID] {
			t.Fatalf("duplicate source ID: %s", s.SourceID)
		}
		seen[s.SourceID] = true
	}
}

// ---- Score filtering ----.

func TestCollector_scoreFiltering(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	fake.addResponse("https://hacker-news.firebaseio.com/v0/newstories.json",
		fakeResponse{statusCode: 200, body: `[1, 2, 3]`})

	for id, score := range map[int]int{1: 50, 2: 3, 3: 100} {
		body := fmt.Sprintf(
			`{"id":%d,"type":"story","by":"u","time":1700000000,"title":"Story %d","url":"https://x.com/%d","score":%d,"descendants":0}`,
			id, id, id, score,
		)
		fake.addResponse(fmt.Sprintf("https://hacker-news.firebaseio.com/v0/item/%d.json", id),
			fakeResponse{statusCode: 200, body: body})
	}

	c := testCollector(t, &ConfigValues{
		Enabled:            true,
		Feeds:              []string{"newstories"},
		MaxItemsPerRun:     10,
		MaxCommentsPerItem: 0,
		MinimumScore:       10, // Items with score < 10 excluded
		MaxRequests:        100,
	}, fake)

	signals, err := c.Collect(context.Background(), domain.CollectRequest{})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(signals) != 2 {
		t.Fatalf("expected 2 signals (score >= 10), got %d", len(signals))
	}
	for _, s := range signals {
		if s.Score < 10 {
			t.Fatalf("signal %s has score %d, expected >= 10", s.SourceID, s.Score)
		}
	}
}

// ---- Since filtering ----.

func TestCollector_sinceFiltering(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	cutoff := time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC)
	fake.addResponse("https://hacker-news.firebaseio.com/v0/newstories.json",
		fakeResponse{statusCode: 200, body: `[1, 2]`})

	// Item 1: before cutoff (should be excluded).
	body1 := fmt.Sprintf(
		`{"id":1,"type":"story","by":"u","time":%d,"title":"Old","url":"https://x.com/1","score":10,"descendants":0}`,
		cutoff.Add(-24*time.Hour).Unix(),
	)
	fake.addResponse("https://hacker-news.firebaseio.com/v0/item/1.json",
		fakeResponse{statusCode: 200, body: body1})

	// Item 2: after cutoff (should be included).
	body2 := fmt.Sprintf(
		`{"id":2,"type":"story","by":"u","time":%d,"title":"New","url":"https://x.com/2","score":10,"descendants":0}`,
		cutoff.Add(24*time.Hour).Unix(),
	)
	fake.addResponse("https://hacker-news.firebaseio.com/v0/item/2.json",
		fakeResponse{statusCode: 200, body: body2})

	c := testCollector(t, &ConfigValues{
		Enabled:            true,
		Feeds:              []string{"newstories"},
		MaxItemsPerRun:     10,
		MaxCommentsPerItem: 0,
		MinimumScore:       0,
		MaxRequests:        100,
	}, fake)

	signals, err := c.Collect(context.Background(), domain.CollectRequest{Since: cutoff})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(signals) != 1 {
		t.Fatalf("expected 1 signal (after cutoff), got %d", len(signals))
	}
	if signals[0].SourceID != "2" {
		t.Fatalf("expected signal 2, got %s", signals[0].SourceID)
	}
}

// ---- Max items cap ----.

func TestCollector_maxItemsCap(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	fake.addResponse("https://hacker-news.firebaseio.com/v0/newstories.json",
		fakeResponse{statusCode: 200, body: `[1, 2, 3, 4, 5]`})

	for id := 1; id <= 5; id++ {
		body := fmt.Sprintf(
			`{"id":%d,"type":"story","by":"u","time":%d,"title":"Story %d","url":"https://x.com/%d","score":10,"descendants":0}`,
			id, 1700000000+id*100, id, id,
		)
		fake.addResponse(fmt.Sprintf("https://hacker-news.firebaseio.com/v0/item/%d.json", id),
			fakeResponse{statusCode: 200, body: body})
	}

	c := testCollector(t, &ConfigValues{
		Enabled:            true,
		Feeds:              []string{"newstories"},
		MaxItemsPerRun:     2,
		MaxCommentsPerItem: 0,
		MinimumScore:       0,
		MaxRequests:        100,
	}, fake)

	signals, err := c.Collect(context.Background(), domain.CollectRequest{})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(signals) != 2 {
		t.Fatalf("expected 2 signals (capped), got %d", len(signals))
	}
}

// ---- Max comments cap ----.

func TestCollector_maxCommentsCap(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	// Story with kids.
	fake.addResponse("https://hacker-news.firebaseio.com/v0/newstories.json",
		fakeResponse{statusCode: 200, body: `[1]`})

	fake.addResponse("https://hacker-news.firebaseio.com/v0/item/1.json",
		fakeResponse{statusCode: 200, body: `{"id":1,"type":"story","by":"u","time":1700000000,"title":"Story","url":"https://x.com/1","score":10,"descendants":3,"kids":[10,11,12]}`})

	// Three comments.
	for id, text := range map[int]string{10: "Comment A", 11: "Comment B", 12: "Comment C"} {
		body := fmt.Sprintf(`{"id":%d,"type":"comment","by":"u","time":1700000100,"text":%q,"parent":1}`,
			id, text)
		fake.addResponse(fmt.Sprintf("https://hacker-news.firebaseio.com/v0/item/%d.json", id),
			fakeResponse{statusCode: 200, body: body})
	}

	c := testCollector(t, &ConfigValues{
		Enabled:            true,
		Feeds:              []string{"newstories"},
		MaxItemsPerRun:     10,
		MaxCommentsPerItem: 2, // Only 2 comments
		MinimumScore:       0,
		MaxRequests:        100,
	}, fake)

	signals, err := c.Collect(context.Background(), domain.CollectRequest{})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(signals) != 1 {
		t.Fatalf("expected 1 signal, got %d", len(signals))
	}
	if len(signals[0].Comments) != 2 {
		t.Fatalf("expected 2 comments (capped), got %d", len(signals[0].Comments))
	}
}

// ---- Request cap failure ----.

func TestCollector_requestCap(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	// maxRequests=1 means only 1 request allowed. The feed request succeeds
	// using that quota, but the item request will hit the cap.
	fake.addResponse("https://hacker-news.firebaseio.com/v0/newstories.json",
		fakeResponse{statusCode: 200, body: `[1]`})
	// Item fetch is not triggered because request cap is reached.

	c := testCollector(t, &ConfigValues{
		Enabled:            true,
		Feeds:              []string{"newstories"},
		MaxItemsPerRun:     10,
		MaxCommentsPerItem: 0,
		MinimumScore:       0,
		MaxRequests:        1, // only 1 request allowed (used by feed)
	}, fake)

	_, err := c.Collect(context.Background(), domain.CollectRequest{})
	if err == nil {
		t.Fatal("expected error for request cap")
	}
	// The feed request succeeds, but the item fetch hits the cap.
	// ErrRequestCap is returned via itemErrs and joined.
	if !errors.Is(err, ErrRequestCap) {
		t.Fatalf("expected ErrRequestCap, got %v", err)
	}
}

// ---- Partial failure (one feed fails, others succeed) ----.

func TestCollector_partialFeedFailure(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	// First feed succeeds.
	fake.addResponse("https://hacker-news.firebaseio.com/v0/newstories.json",
		fakeResponse{statusCode: 200, body: `[1]`})
	fake.addResponse("https://hacker-news.firebaseio.com/v0/item/1.json",
		fakeResponse{statusCode: 200, body: `{"id":1,"type":"story","by":"u","time":1700000000,"title":"Good","url":"https://x.com/1","score":10,"descendants":0}`})

	// Second feed fails with 404 (non-retryable, avoids slow retry backoff).
	fake.addResponse("https://hacker-news.firebaseio.com/v0/topstories.json",
		fakeResponse{statusCode: 404, body: `{}`})

	c := testCollector(t, &ConfigValues{
		Enabled:            true,
		Feeds:              []string{"newstories", "topstories"},
		MaxItemsPerRun:     10,
		MaxCommentsPerItem: 0,
		MinimumScore:       0,
		MaxRequests:        100,
	}, fake)

	signals, err := c.Collect(context.Background(), domain.CollectRequest{})
	if err == nil {
		t.Fatal("expected partial error for feed failure")
	}
	if len(signals) != 1 {
		t.Fatalf("expected 1 signal from successful feed, got %d", len(signals))
	}
	if signals[0].SourceID != "1" {
		t.Fatalf("expected signal 1, got %s", signals[0].SourceID)
	}
}

// ---- Empty feeds ----.

func TestCollector_emptyFeeds(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	fake.addResponse("https://hacker-news.firebaseio.com/v0/newstories.json",
		fakeResponse{statusCode: 200, body: `[]`})
	fake.addResponse("https://hacker-news.firebaseio.com/v0/topstories.json",
		fakeResponse{statusCode: 200, body: `[]`})

	c := testCollector(t, &ConfigValues{
		Enabled:            true,
		Feeds:              []string{"newstories", "topstories"},
		MaxItemsPerRun:     10,
		MaxCommentsPerItem: 0,
		MinimumScore:       0,
		MaxRequests:        100,
	}, fake)

	signals, err := c.Collect(context.Background(), domain.CollectRequest{})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(signals) != 0 {
		t.Fatalf("expected 0 signals, got %d", len(signals))
	}
}

// ---- Context cancellation ----.

func TestCollector_contextCancellation(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	fake.addResponse("https://hacker-news.firebaseio.com/v0/newstories.json",
		fakeResponse{statusCode: 200, body: `[1, 2, 3, 4, 5]`})

	// Register item responses but make them slow by requiring a retry.
	fake.addResponse("https://hacker-news.firebaseio.com/v0/item/1.json",
		fakeResponse{statusCode: 200, body: `{"id":1,"type":"story","by":"u","time":1700000000,"title":"S1","url":"https://x.com/1","score":10,"descendants":0}`})

	c := testCollector(t, &ConfigValues{
		Enabled:            true,
		Feeds:              []string{"newstories"},
		MaxItemsPerRun:     10,
		MaxCommentsPerItem: 0,
		MinimumScore:       0,
		MaxRequests:        100,
	}, fake)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	signals, err := c.Collect(ctx, domain.CollectRequest{})
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if len(signals) != 0 {
		t.Fatalf("expected 0 signals on cancellation, got %d", len(signals))
	}
}

// ---- Cached repeat collection ----.

func TestCollector_cachedRepeat(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := storage.New(dir)
	fake := newFakeTransport()

	fake.addResponse("https://hacker-news.firebaseio.com/v0/newstories.json",
		fakeResponse{statusCode: 200, body: `[1]`})
	fake.addResponse("https://hacker-news.firebaseio.com/v0/item/1.json",
		fakeResponse{statusCode: 200, body: `{"id":1,"type":"story","by":"u","time":1700000000,"title":"Cached","url":"https://x.com/1","score":10,"descendants":0}`})

	c := testCollector(t, &ConfigValues{
		Enabled:            true,
		Feeds:              []string{"newstories"},
		MaxItemsPerRun:     10,
		MaxCommentsPerItem: 0,
		MinimumScore:       0,
		MaxRequests:        100,
	}, fake)
	c.WithCache(store)

	// First call: 2 requests (1 feed + 1 item).
	signals1, err := c.Collect(context.Background(), domain.CollectRequest{})
	if err != nil {
		t.Fatalf("first collect: %v", err)
	}
	if len(signals1) != 1 {
		t.Fatalf("expected 1 signal, got %d", len(signals1))
	}
	firstHash := signals1[0].ContentHash

	stats1 := c.Stats()
	if stats1.Requests != 2 {
		t.Fatalf("expected 2 requests on first call, got %d", stats1.Requests)
	}
	if stats1.CacheHits != 0 {
		t.Fatalf("expected 0 cache hits on first call, got %d", stats1.CacheHits)
	}

	// Second call: cache hits, no HTTP requests.
	// Reset the fake call count to verify no new HTTP calls are made.
	fake.resetCallCount()
	signals2, err := c.Collect(context.Background(), domain.CollectRequest{})
	if err != nil {
		t.Fatalf("second collect: %v", err)
	}
	if len(signals2) != 1 {
		t.Fatalf("expected 1 signal, got %d", len(signals2))
	}

	// Should have 0 HTTP calls because everything was cached.
	if fake.callCountFor("https://hacker-news.firebaseio.com/v0/newstories.json") != 0 {
		t.Fatal("expected 0 HTTP calls for cached feed")
	}

	// Content hash should be identical.
	if signals2[0].ContentHash != firstHash {
		t.Fatal("content hash differs between cached and fresh collection")
	}

	// Stats should reflect cache hits (0 new requests, 2 cache hits).
	stats2 := c.Stats()
	if stats2.Requests != 0 {
		t.Fatalf("expected 0 new requests on second call, got %d", stats2.Requests)
	}
	if stats2.CacheHits != 2 {
		t.Fatalf("expected 2 cache hits on second call, got %d", stats2.CacheHits)
	}
}

// ---- BFS comment ordering ----.

func TestCollector_bfsCommentOrder(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	// Story with nested comments: 3-level tree.
	// Root: 40000010 -> kids [40000011, 40000012, 40000013]
	// 40000011 -> kids [40000014, 40000015]
	// 40000012 -> dead (skipped)
	// 40000013 -> kids [40000017]
	// 40000014 -> kids [40000018]
	// 40000015 -> leaf
	// 40000017 -> kids [40000019] (deleted)
	// 40000018 -> leaf
	// 40000019 -> deleted (skipped)
	fake.addResponse("https://hacker-news.firebaseio.com/v0/newstories.json",
		fakeResponse{statusCode: 200, body: `[40000010]`})

	// Story.
	fake.addResponse("https://hacker-news.firebaseio.com/v0/item/40000010.json",
		fakeResponse{statusCode: 200, body: `{"id":40000010,"type":"story","by":"bob","time":1700100000,"title":"BFS Test","url":"https://x.com/bfs","score":85,"descendants":9,"kids":[40000011,40000012,40000013]}`})

	// Level 1 comments.
	fake.addResponse("https://hacker-news.firebaseio.com/v0/item/40000011.json",
		fakeResponse{statusCode: 200, body: `{"id":40000011,"type":"comment","by":"charlie","time":1700100100,"text":"Level 1 A","parent":40000010,"kids":[40000014,40000015]}`})
	fake.addResponse("https://hacker-news.firebaseio.com/v0/item/40000012.json",
		fakeResponse{statusCode: 200, body: `{"id":40000012,"type":"comment","by":"dave","time":1700100200,"text":"Level 1 B dead","parent":40000010,"dead":true,"kids":[40000016]}`})
	fake.addResponse("https://hacker-news.firebaseio.com/v0/item/40000013.json",
		fakeResponse{statusCode: 200, body: `{"id":40000013,"type":"comment","by":"eve","time":1700100300,"text":"Level 1 C","parent":40000010,"kids":[40000017]}`})

	// Level 2 comments.
	fake.addResponse("https://hacker-news.firebaseio.com/v0/item/40000014.json",
		fakeResponse{statusCode: 200, body: `{"id":40000014,"type":"comment","by":"frank","time":1700100400,"text":"Level 2 from A","parent":40000011,"kids":[40000018]}`})
	fake.addResponse("https://hacker-news.firebaseio.com/v0/item/40000015.json",
		fakeResponse{statusCode: 200, body: `{"id":40000015,"type":"comment","by":"grace","time":1700100500,"text":"Level 2 from A no kids","parent":40000011}`})
	fake.addResponse("https://hacker-news.firebaseio.com/v0/item/40000016.json",
		fakeResponse{statusCode: 200, body: `{"id":40000016,"type":"comment","by":"heidi","time":1700100600,"text":"Child of dead","parent":40000012}`})
	fake.addResponse("https://hacker-news.firebaseio.com/v0/item/40000017.json",
		fakeResponse{statusCode: 200, body: `{"id":40000017,"type":"comment","by":"ivan","time":1700100700,"text":"Level 2 from C","parent":40000013,"kids":[40000019]}`})

	// Level 3 comments.
	fake.addResponse("https://hacker-news.firebaseio.com/v0/item/40000018.json",
		fakeResponse{statusCode: 200, body: `{"id":40000018,"type":"comment","by":"judy","time":1700100800,"text":"Level 3 deepest leaf","parent":40000014}`})
	fake.addResponse("https://hacker-news.firebaseio.com/v0/item/40000019.json",
		fakeResponse{statusCode: 200, body: `{"id":40000019,"type":"comment","by":"karl","time":1700100900,"text":"Level 3 deleted","parent":40000017,"deleted":true}`})

	c := testCollector(t, &ConfigValues{
		Enabled:            true,
		Feeds:              []string{"newstories"},
		MaxItemsPerRun:     10,
		MaxCommentsPerItem: 100, // enable comment flattening
		MinimumScore:       0,
		MaxRequests:        100,
	}, fake)

	signals, err := c.Collect(context.Background(), domain.CollectRequest{})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(signals) != 1 {
		t.Fatalf("expected 1 signal, got %d", len(signals))
	}

	comments := signals[0].Comments

	// Expected BFS order (skipping dead/deleted/non-comment):
	// Queue initially: [40000011, 40000012, 40000013] (depths: 1, 1, 1)
	// Dequeue 40000011: surviving comment, add kids [40000014, 40000015] (depth 2)
	// Dequeue 40000012: dead → skip, kids NOT added
	// Dequeue 40000013: surviving comment, add kids [40000017] (depth 2)
	// Dequeue 40000014: surviving comment, add kids [40000018] (depth 3)
	// Dequeue 40000015: surviving comment, no kids
	// Dequeue 40000017: surviving comment, add kids [40000019] (depth 3)
	// Dequeue 40000018: surviving comment, no kids
	// Dequeue 40000019: deleted → skip
	//
	// So expected order: 40000011, 40000013, 40000014, 40000015, 40000017, 40000018
	// That's 6 comments.
	expectedIDs := []string{"40000011", "40000013", "40000014", "40000015", "40000017", "40000018"}
	if len(comments) != len(expectedIDs) {
		t.Fatalf("expected %d comments, got %d: %v", len(expectedIDs), len(comments), commentIDs(comments))
	}
	for i, id := range expectedIDs {
		if comments[i].ID != id {
			t.Fatalf("comment[%d].ID = %s, want %s (full: %v)", i, comments[i].ID, id, commentIDs(comments))
		}
	}
}

func commentIDs(comments []domain.Comment) []string {
	ids := make([]string, len(comments))
	for i := range comments {
		ids[i] = comments[i].ID
	}
	return ids
}

// ---- Concurrent safety ----.

func TestCollector_concurrentAccess(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	feeds := []string{"newstories", "topstories", "beststories"}
	for _, feed := range feeds {
		fake.addResponse(fmt.Sprintf("https://hacker-news.firebaseio.com/v0/%s.json", feed),
			fakeResponse{statusCode: 200, body: `[1, 2]`})
	}
	// Item responses.
	for id := 1; id <= 2; id++ {
		body := fmt.Sprintf(
			`{"id":%d,"type":"story","by":"u","time":%d,"title":"S%d","url":"https://x.com/%d","score":10,"descendants":0}`,
			id, 1700000000+id*100, id, id,
		)
		fake.addResponse(fmt.Sprintf("https://hacker-news.firebaseio.com/v0/item/%d.json", id),
			fakeResponse{statusCode: 200, body: body})
	}

	c := testCollector(t, &ConfigValues{
		Enabled:            true,
		Feeds:              feeds,
		MaxItemsPerRun:     10,
		MaxCommentsPerItem: 0,
		MinimumScore:       0,
		MaxRequests:        100,
	}, fake)

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := c.Collect(context.Background(), domain.CollectRequest{})
			if err != nil {
				t.Errorf("concurrent collect: %v", err)
			}
		}()
	}
	wg.Wait()
}

// ---- Stats collector fields ----.

func TestCollector_stats(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	fake.addResponse("https://hacker-news.firebaseio.com/v0/newstories.json",
		fakeResponse{statusCode: 200, body: `[1]`})
	fake.addResponse("https://hacker-news.firebaseio.com/v0/item/1.json",
		fakeResponse{statusCode: 200, body: `{"id":1,"type":"story","by":"u","time":1700000000,"title":"S1","url":"https://x.com/1","score":10,"descendants":0}`})

	c := testCollector(t, &ConfigValues{
		Enabled:            true,
		Feeds:              []string{"newstories"},
		MaxItemsPerRun:     10,
		MaxCommentsPerItem: 0,
		MinimumScore:       0,
		MaxRequests:        100,
	}, fake)

	// Before collection, stats should be zero.
	stats := c.Stats()
	if stats.Requests != 0 || stats.CacheHits != 0 {
		t.Fatalf("expected zero stats before collect, got %+v", stats)
	}

	_, err := c.Collect(context.Background(), domain.CollectRequest{})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	// After collection, stats should reflect requests.
	stats = c.Stats()
	if stats.Requests != 2 { // 1 feed + 1 item
		t.Fatalf("expected 2 requests, got %d", stats.Requests)
	}
}
