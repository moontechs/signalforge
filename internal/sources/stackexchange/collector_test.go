package stackexchange

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/moontechs/signalforge/internal/domain"
)

func TestCollector_statsTracking(t *testing.T) {
	fake := newFakeTransport()
	fake.addResponse("https://api.stackexchange.com/2.3/search/advanced*", fakeResponse{statusCode: 200, body: `{"items":[{"question_id":1,"title":"q","body_markdown":"<p>b</p>","creation_date":1740000000}],"quota_remaining":10}`})
	c := New(&ConfigValues{Enabled: true, Sites: []string{"stackoverflow"}}, testClient(fake))
	if _, err := c.Collect(context.Background(), domain.CollectRequest{}); err != nil {
		t.Fatal(err)
	}
	stats := c.Stats()
	if stats.Requests == 0 && stats.CacheHits == 0 {
		t.Fatalf("expected non-zero stats, got %+v", stats)
	}
}

func TestCollector_sinceFiltering(t *testing.T) {
	fake := newFakeTransport()
	fake.addResponse("https://api.stackexchange.com/2.3/search/advanced*", fakeResponse{statusCode: 200, body: `{"items":[{"question_id":1,"title":"old","body_markdown":"old body","creation_date":1700000000},{"question_id":2,"title":"new","body_markdown":"new body","creation_date":1740000000}],"quota_remaining":10}`})
	c := New(&ConfigValues{Enabled: true, Sites: []string{"stackoverflow"}, MinimumScore: 0, MinimumViews: 0}, testClient(fake))
	got, err := c.Collect(context.Background(), domain.CollectRequest{Since: time.Unix(1730000000, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("expected signals, got none")
	}
}

func TestCollectorMultiSitePaginationAndLimits(t *testing.T) {
	fake := newFakeTransport()
	fake.addSequentialResponses("https://api.stackexchange.com/2.3/search/advanced*",
		fakeResponse{statusCode: 200, body: `{"items":[{"question_id":1,"title":"one","body_markdown":"<p>body</p>","creation_date":1740000000}],"has_more":true,"quota_remaining":10}`},
		fakeResponse{statusCode: 200, body: `{"items":[{"question_id":2,"title":"two","body_markdown":"<p>body two</p>","creation_date":1740000001}],"has_more":true,"quota_remaining":9}`},
	)
	c := New(&ConfigValues{Enabled: true, Sites: []string{"stackoverflow", "serverfault"}, MaxItemsPerSite: 1, PageSize: 1, MaxPagesPerSite: 2}, testClient(fake))
	got, err := c.Collect(context.Background(), domain.CollectRequest{})
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(got) != 2 || got[0].ID != "se:stackoverflow:1" || got[1].ID != "se:serverfault:2" {
		t.Fatalf("unexpected signals: %#v", got)
	}
	if c.Stats().Requests != 2 {
		t.Fatalf("requests = %d, want 2", c.Stats().Requests)
	}
}

func TestCollectorStopsPaginationAndHonorsCancellation(t *testing.T) {
	fake := newFakeTransport()
	fake.addResponse("https://api.stackexchange.com/2.3/search/advanced*", fakeResponse{statusCode: 200, body: `{"items":[{"question_id":1,"body_markdown":"<p>body</p>"}],"has_more":true,"quota_remaining":10}`})
	c := New(&ConfigValues{Enabled: true, Sites: []string{"stackoverflow"}, MaxItemsPerSite: 10, PageSize: 1, MaxPagesPerSite: 3}, testClient(fake))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Collect(ctx, domain.CollectRequest{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if c.Stats().Requests != 0 {
		t.Fatalf("requests = %d after cancelled collection", c.Stats().Requests)
	}
}