package hackernews

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/moontechs/signalforge/internal/domain"
	"github.com/moontechs/signalforge/internal/storage"
)

// integrationTestFixture builds a complete fake environment for integration tests.
// It creates 3 feeds with overlapping IDs, nested comments (3+ levels),
// deleted/dead/non-story items, and a mix of eligible and ineligible stories.
type integrationFixture struct {
	fake  *fakeTransport
	store *storage.Storage
	dir   string
}

func newIntegrationFixture(t *testing.T) *integrationFixture {
	t.Helper()
	dir := t.TempDir()
	fake := newFakeTransport()
	store := storage.New(dir)

	// ----- Feeds -----
	// newstories: IDs 100-103 (4 items, IDs 100, 101, 102, 103)
	// askstories: IDs 102, 103, 200-202 (overlaps with newstories on 102, 103)
	// showstories: IDs 201, 202, 300 (overlaps with askstories on 201, 202)
	fake.addResponse("https://hacker-news.firebaseio.com/v0/newstories.json",
		fakeResponse{statusCode: 200, body: `[100, 101, 102, 103]`})
	fake.addResponse("https://hacker-news.firebaseio.com/v0/askstories.json",
		fakeResponse{statusCode: 200, body: `[102, 103, 200, 201, 202]`})
	fake.addResponse("https://hacker-news.firebaseio.com/v0/showstories.json",
		fakeResponse{statusCode: 200, body: `[201, 202, 300]`})

	// ----- Items -----
	// Story 100: normal story with high score and 2 comments
	// Story 101: low score (1) — should be filtered out
	// Story 102: normal story with nested comments (3 levels)
	// Story 103: deleted story — should be filtered out
	// Story 200: normal story with a dead comment
	// Story 201: normal story with a deleted comment
	// Story 202: non-story type (poll) — should be filtered out
	// Story 300: normal story with no comments

	fake.addResponse("https://hacker-news.firebaseio.com/v0/item/100.json",
		fakeResponse{statusCode: 200, body: `{"id":100,"type":"story","by":"alice","time":1700000000,"title":"Story 100","url":"https://x.com/100","score":50,"descendants":2,"kids":[110,111]}`})
	fake.addResponse("https://hacker-news.firebaseio.com/v0/item/101.json",
		fakeResponse{statusCode: 200, body: `{"id":101,"type":"story","by":"bob","time":1700000100,"title":"Story 101 low score","url":"https://x.com/101","score":1,"descendants":0}`})
	fake.addResponse("https://hacker-news.firebaseio.com/v0/item/102.json",
		fakeResponse{statusCode: 200, body: `{"id":102,"type":"story","by":"carol","time":1700000200,"title":"Story 102 nested","url":"https://x.com/102","score":30,"descendants":3,"kids":[120,121]}`})
	fake.addResponse("https://hacker-news.firebaseio.com/v0/item/103.json",
		fakeResponse{statusCode: 200, body: `{"id":103,"type":"story","by":"dave","time":1700000300,"title":"Story 103 deleted","url":"https://x.com/103","score":40,"descendants":0,"deleted":true}`})
	fake.addResponse("https://hacker-news.firebaseio.com/v0/item/200.json",
		fakeResponse{statusCode: 200, body: `{"id":200,"type":"story","by":"eve","time":1700000400,"title":"Story 200 with dead comment","url":"https://x.com/200","score":60,"descendants":1,"kids":[130]}`})
	fake.addResponse("https://hacker-news.firebaseio.com/v0/item/201.json",
		fakeResponse{statusCode: 200, body: `{"id":201,"type":"story","by":"frank","time":1700000500,"title":"Story 201 with deleted comment","url":"https://x.com/201","score":70,"descendants":1,"kids":[140]}`})
	fake.addResponse("https://hacker-news.firebaseio.com/v0/item/202.json",
		fakeResponse{statusCode: 200, body: `{"id":202,"type":"poll","by":"grace","time":1700000600,"title":"Poll 202","url":"https://x.com/202","score":80,"descendants":0}`})
	fake.addResponse("https://hacker-news.firebaseio.com/v0/item/300.json",
		fakeResponse{statusCode: 200, body: `{"id":300,"type":"story","by":"heidi","time":1700000700,"title":"Story 300 alone","url":"https://x.com/300","score":90,"descendants":0}`})

	// ----- Comments for stories -----
	// Story 100 comments.
	fake.addResponse("https://hacker-news.firebaseio.com/v0/item/110.json",
		fakeResponse{statusCode: 200, body: `{"id":110,"type":"comment","by":"ivan","time":1700001000,"text":"Comment 110 on story 100","parent":100}`})
	fake.addResponse("https://hacker-news.firebaseio.com/v0/item/111.json",
		fakeResponse{statusCode: 200, body: `{"id":111,"type":"comment","by":"judy","time":1700001100,"text":"Comment 111 on story 100","parent":100}`})

	// Story 102 nested comments: level 1 → 120, 121; level 2 → 122 (child of 120), 123 (child of 121); level 3 → 124 (child of 122)
	fake.addResponse("https://hacker-news.firebaseio.com/v0/item/120.json",
		fakeResponse{statusCode: 200, body: `{"id":120,"type":"comment","by":"karl","time":1700002000,"text":"Level 1 comment for 102","parent":102,"kids":[122]}`})
	fake.addResponse("https://hacker-news.firebaseio.com/v0/item/121.json",
		fakeResponse{statusCode: 200, body: `{"id":121,"type":"comment","by":"leo","time":1700002100,"text":"Another level 1 comment for 102","parent":102,"kids":[123]}`})
	fake.addResponse("https://hacker-news.firebaseio.com/v0/item/122.json",
		fakeResponse{statusCode: 200, body: `{"id":122,"type":"comment","by":"mia","time":1700002200,"text":"Level 2 child of 120","parent":120,"kids":[124]}`})
	fake.addResponse("https://hacker-news.firebaseio.com/v0/item/123.json",
		fakeResponse{statusCode: 200, body: `{"id":123,"type":"comment","by":"nick","time":1700002300,"text":"Level 2 child of 121 — dead","parent":121,"dead":true,"kids":[125]}`})
	fake.addResponse("https://hacker-news.firebaseio.com/v0/item/124.json",
		fakeResponse{statusCode: 200, body: `{"id":124,"type":"comment","by":"olivia","time":1700002400,"text":"Level 3 deepest comment","parent":122}`})
	// 125 would be child of 123 (dead) — not registered, won't be fetched

	// Story 200: dead comment
	fake.addResponse("https://hacker-news.firebaseio.com/v0/item/130.json",
		fakeResponse{statusCode: 200, body: `{"id":130,"type":"comment","by":"paul","time":1700003000,"text":"Dead comment on 200","parent":200,"dead":true}`})

	// Story 201: deleted comment
	fake.addResponse("https://hacker-news.firebaseio.com/v0/item/140.json",
		fakeResponse{statusCode: 200, body: `{"id":140,"type":"comment","by":"quin","time":1700003100,"text":"Deleted comment on 201","parent":201,"deleted":true}`})

	return &integrationFixture{
		fake:  fake,
		store: store,
		dir:   dir,
	}
}

func TestIntegration_fullCollection(t *testing.T) {
	t.Parallel()
	fixture := newIntegrationFixture(t)

	c := testCollector(t, &ConfigValues{
		Enabled:            true,
		Feeds:              []string{"newstories", "askstories", "showstories"},
		MaxItemsPerRun:     10,
		MaxCommentsPerItem: 100, // enable comment flattening
		MinimumScore:       5,
		MaxRequests:        100,
	}, fixture.fake)

	signals, err := c.Collect(context.Background(), domain.CollectRequest{})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	// Expected signals after filtering:
	// - ID 100: score 50 >= 5, story type → included
	// - ID 101: score 1 < 5 → excluded
	// - ID 102: score 30 >= 5, story type → included
	// - ID 103: deleted → excluded
	// - ID 200: score 60 >= 5, story type → included (dead comment, but story is fine)
	// - ID 201: score 70 >= 5, story type → included (deleted comment, story is fine)
	// - ID 202: poll type → excluded
	// - ID 300: score 90 >= 5, story type → included
	// Total: 5 signals (100, 102, 200, 201, 300)
	if len(signals) != 5 {
		t.Fatalf("expected 5 signals, got %d", len(signals))
	}

	// Verify content hashes are deterministic.
	hashes := make(map[string]bool)
	for _, s := range signals {
		if s.ContentHash == "" {
			t.Fatalf("signal %s has empty content hash", s.SourceID)
		}
		if hashes[s.ContentHash] {
			t.Fatalf("duplicate content hash for signal %s", s.SourceID)
		}
		hashes[s.ContentHash] = true
	}

	// Verify sorting: descending by CreatedAt.
	for i := 0; i < len(signals)-1; i++ {
		if signals[i].CreatedAt.Before(signals[i+1].CreatedAt) {
			t.Fatalf("signals not sorted descending: %v before %v",
				signals[i].CreatedAt, signals[i+1].CreatedAt)
		}
	}

	// Verify specific signal metadata.
	signalMap := make(map[string]domain.RawSignal)
	for _, s := range signals {
		signalMap[s.SourceID] = s
	}

	// Story 100 should have 2 comments.
	s100, ok := signalMap["100"]
	if !ok {
		t.Fatal("expected signal 100")
	}
	if len(s100.Comments) != 2 {
		t.Fatalf("signal 100: expected 2 comments, got %d", len(s100.Comments))
	}
	if s100.Comments[0].Body != "Comment 110 on story 100" {
		t.Fatalf("signal 100 comment 0: expected 'Comment 110 on story 100', got %q", s100.Comments[0].Body)
	}

	// Story 200 should have 0 comments (only dead comment — filtered by BFS).
	s200, ok := signalMap["200"]
	if !ok {
		t.Fatal("expected signal 200")
	}
	if len(s200.Comments) != 0 {
		t.Fatalf("signal 200: expected 0 comments (dead only), got %d", len(s200.Comments))
	}

	// Story 201 should have 0 comments (only deleted comment — filtered by BFS).
	s201, ok := signalMap["201"]
	if !ok {
		t.Fatal("expected signal 201")
	}
	if len(s201.Comments) != 0 {
		t.Fatalf("signal 201: expected 0 comments (deleted only), got %d", len(s201.Comments))
	}

	// Story 102 should have BFS-ordered comments.
	// Kids: [120, 121]
	// 120 -> kid 122
	// 121 -> kid 123 (dead)
	// 122 -> kid 124
	// BFS order should be: 120 (depth 1), 121 (depth 1), 122 (depth 2), 124 (depth 3)
	// 123 is dead, so it and its kids are skipped (125 not registered anyway).
	s102, ok := signalMap["102"]
	if !ok {
		t.Fatal("expected signal 102")
	}
	expectedCommentIDs := []string{"120", "121", "122", "124"}
	if len(s102.Comments) != len(expectedCommentIDs) {
		t.Fatalf("signal 102 comments: expected %d, got %d (%v)",
			len(expectedCommentIDs), len(s102.Comments), commentIDs(s102.Comments))
	}
	for i, id := range expectedCommentIDs {
		if s102.Comments[i].ID != id {
			t.Fatalf("signal 102 comment[%d]: expected ID %s, got %s",
				i, id, s102.Comments[i].ID)
		}
	}

	// Verify ContentHash determinism: re-parse the same data should yield same hash.
	// We'll test this by running with cache and checking consistency.
}

func TestIntegration_withCache(t *testing.T) {
	t.Parallel()
	fixture := newIntegrationFixture(t)

	c := testCollector(t, &ConfigValues{
		Enabled:            true,
		Feeds:              []string{"newstories", "askstories", "showstories"},
		MaxItemsPerRun:     10,
		MaxCommentsPerItem: 100,
		MinimumScore:       5,
		MaxRequests:        100,
	}, fixture.fake)
	c.WithCache(fixture.store)

	// First collection: should use HTTP for everything.
	signals1, err := c.Collect(context.Background(), domain.CollectRequest{})
	if err != nil {
		t.Fatalf("first collect: %v", err)
	}
	if len(signals1) != 5 {
		t.Fatalf("expected 5 signals, got %d", len(signals1))
	}

	firstHashes := make([]string, len(signals1))
	for i, s := range signals1 {
		firstHashes[i] = s.ContentHash
	}

	stats1 := c.Stats()
	if stats1.Requests <= 0 {
		t.Fatalf("expected >0 requests on first collect, got %d", stats1.Requests)
	}

	// Second collection: should use cache, no HTTP calls.
	fixture.fake.resetCallCount()
	signals2, err := c.Collect(context.Background(), domain.CollectRequest{})
	if err != nil {
		t.Fatalf("second collect: %v", err)
	}
	if len(signals2) != 5 {
		t.Fatalf("expected 5 signals on second collect, got %d", len(signals2))
	}

	// No HTTP calls should be made.
	for url, count := range map[string]int{
		"https://hacker-news.firebaseio.com/v0/newstories.json": 0,
		"https://hacker-news.firebaseio.com/v0/askstories.json": 0,
		"https://hacker-news.firebaseio.com/v0/showstories.json": 0,
	} {
		if fixture.fake.callCountFor(url) != count {
			t.Fatalf("expected %d calls to %s on cache hit, got %d", count, url, fixture.fake.callCountFor(url))
		}
	}

	// Content hashes should be identical.
	for i := range signals2 {
		if signals2[i].ContentHash != firstHashes[i] {
			t.Fatalf("signal %s content hash mismatch: %s vs %s",
				signals2[i].SourceID, signals2[i].ContentHash, firstHashes[i])
		}
	}

	// Stats should show cache hits.
	stats2 := c.Stats()
	if stats2.CacheHits <= 0 {
		t.Fatalf("expected >0 cache hits on second collect, got %d", stats2.CacheHits)
	}
}

// TestIntegration_sinceFiltering tests that the since parameter correctly
// filters items based on creation time.
func TestIntegration_sinceFiltering(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	cutoff := time.Date(2025, 1, 10, 12, 0, 0, 0, time.UTC)

	// Feed with 3 items.
	fake.addResponse("https://hacker-news.firebaseio.com/v0/newstories.json",
		fakeResponse{statusCode: 200, body: `[1, 2, 3]`})

	// Item 1: before cutoff (should be excluded).
	fake.addResponse("https://hacker-news.firebaseio.com/v0/item/1.json",
		fakeResponse{statusCode: 200, body: fmt.Sprintf(
			`{"id":1,"type":"story","by":"u","time":%d,"title":"Old story","url":"https://x.com/1","score":10,"descendants":0}`,
			cutoff.Add(-48*time.Hour).Unix())})

	// Item 2: right at cutoff (should be included - eligibleStory uses !before(since)).
	fake.addResponse("https://hacker-news.firebaseio.com/v0/item/2.json",
		fakeResponse{statusCode: 200, body: fmt.Sprintf(
			`{"id":2,"type":"story","by":"u","time":%d,"title":"On cutoff","url":"https://x.com/2","score":10,"descendants":0}`,
			cutoff.Unix())})

	// Item 3: after cutoff (should be included).
	fake.addResponse("https://hacker-news.firebaseio.com/v0/item/3.json",
		fakeResponse{statusCode: 200, body: fmt.Sprintf(
			`{"id":3,"type":"story","by":"u","time":%d,"title":"New story","url":"https://x.com/3","score":10,"descendants":0}`,
			cutoff.Add(48*time.Hour).Unix())})

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
	if len(signals) != 2 {
		t.Fatalf("expected 2 signals (at or after cutoff), got %d", len(signals))
	}
}

// TestIntegration_concurrency runs multiple collector instances concurrently
// to ensure no race conditions.
func TestIntegration_concurrency(t *testing.T) {
	t.Parallel()
	// Each goroutine creates its own fixture to avoid shared state.
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			fixture := newIntegrationFixture(t)

			c := testCollector(t, &ConfigValues{
				Enabled:            true,
				Feeds:              []string{"newstories", "askstories", "showstories"},
				MaxItemsPerRun:     10,
				MaxCommentsPerItem: 0,
				MinimumScore:       5,
				MaxRequests:        100,
			}, fixture.fake)

			signals, err := c.Collect(context.Background(), domain.CollectRequest{})
			if err != nil {
				t.Errorf("goroutine %d: Collect: %v", idx, err)
				return
			}
			if len(signals) != 5 {
				t.Errorf("goroutine %d: expected 5 signals, got %d", idx, len(signals))
			}
		}(i)
	}
	wg.Wait()
}

// TestIntegration_invalidFeed ensures New returns error for unknown feeds.
func TestIntegration_invalidFeed(t *testing.T) {
	t.Parallel()
	_, err := New(&ConfigValues{
		Enabled: true,
		Feeds:   []string{"unknownfeed"},
	})
	if err == nil {
		t.Fatal("expected error for invalid feed")
	}
	if !strings.Contains(err.Error(), "unknownfeed") {
		t.Fatalf("expected error mentioning unknownfeed, got: %v", err)
	}
}
