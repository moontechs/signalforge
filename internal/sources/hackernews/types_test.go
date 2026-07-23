package hackernews

import (
	"testing"
	"time"
)

func TestSourceConstants(t *testing.T) {
	t.Parallel()

	if SourceName != "hackernews" {
		t.Fatalf("SourceName = %q, want %q", SourceName, "hackernews")
	}
	if SourceType != "discussion" {
		t.Fatalf("SourceType = %q, want %q", SourceType, "discussion")
	}
	if SignalIDPrefix != "hn" {
		t.Fatalf("SignalIDPrefix = %q, want %q", SignalIDPrefix, "hn")
	}
}

func TestSupportedFeeds(t *testing.T) {
	t.Parallel()

	expected := []string{"askstories", "showstories", "newstories", "topstories", "beststories"}
	if len(SupportedFeeds) != len(expected) {
		t.Fatalf("SupportedFeeds has %d entries, want %d", len(SupportedFeeds), len(expected))
	}
	for i, feed := range expected {
		if SupportedFeeds[i] != feed {
			t.Fatalf("SupportedFeeds[%d] = %q, want %q", i, SupportedFeeds[i], feed)
		}
	}
}

func TestDefaultFeeds(t *testing.T) {
	t.Parallel()

	expected := []string{"askstories", "showstories", "newstories"}
	if len(DefaultFeeds) != len(expected) {
		t.Fatalf("DefaultFeeds has %d entries, want %d", len(DefaultFeeds), len(expected))
	}
	for i, feed := range expected {
		if DefaultFeeds[i] != feed {
			t.Fatalf("DefaultFeeds[%d] = %q, want %q", i, DefaultFeeds[i], feed)
		}
	}
}

func TestMetadataKeyConstants(t *testing.T) {
	t.Parallel()

	if MetaKeyCommentParentIDs != "parent_ids" {
		t.Fatalf("MetaKeyCommentParentIDs = %q, want %q", MetaKeyCommentParentIDs, "parent_ids")
	}
	if MetaKeyCommentDepth != "depth" {
		t.Fatalf("MetaKeyCommentDepth = %q, want %q", MetaKeyCommentDepth, "depth")
	}
	if MetaKeyStoryScore != "story_score" {
		t.Fatalf("MetaKeyStoryScore = %q, want %q", MetaKeyStoryScore, "story_score")
	}
	if MetaKeyCommentCount != "comment_count" {
		t.Fatalf("MetaKeyCommentCount = %q, want %q", MetaKeyCommentCount, "comment_count")
	}
	if MetaKeyAuthor != "author" {
		t.Fatalf("MetaKeyAuthor = %q, want %q", MetaKeyAuthor, "author")
	}
}

func TestDeriveScope(t *testing.T) {
	t.Parallel()

	cfg := &ConfigValues{
		Enabled:            true,
		Feeds:              []string{"askstories", "newstories"},
		MaxItemsPerRun:     100,
		MaxCommentsPerItem: 20,
		MinimumScore:       5,
		MaxRequests:        500,
	}

	now := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	scope := deriveScope(cfg, now)

	if len(scope.feeds) != 2 {
		t.Fatalf("expected 2 feeds, got %d", len(scope.feeds))
	}
	if scope.feeds[0] != "askstories" || scope.feeds[1] != "newstories" {
		t.Fatalf("feeds = %v, want [askstories newstories]", scope.feeds)
	}
	if scope.maxItems != 100 {
		t.Fatalf("maxItems = %d, want 100", scope.maxItems)
	}
	if scope.maxComments != 20 {
		t.Fatalf("maxComments = %d, want 20", scope.maxComments)
	}
	if scope.minimumScore != 5 {
		t.Fatalf("minimumScore = %d, want 5", scope.minimumScore)
	}
	if !scope.since.Equal(now) {
		t.Fatalf("since = %v, want %v", scope.since, now)
	}
	if scope.maxRequests != 500 {
		t.Fatalf("maxRequests = %d, want 500", scope.maxRequests)
	}
}

func TestDeriveScopeDefault(t *testing.T) {
	t.Parallel()

	cfg := &ConfigValues{
		Enabled:            true,
		Feeds:              DefaultFeeds,
		MaxItemsPerRun:     300,
		MaxCommentsPerItem: 30,
		MinimumScore:       2,
		MaxRequests:        1000,
	}

	scope := deriveScope(cfg, time.Time{})

	if scope.maxItems != 300 {
		t.Fatalf("maxItems = %d, want 300", scope.maxItems)
	}
	if scope.maxComments != 30 {
		t.Fatalf("maxComments = %d, want 30", scope.maxComments)
	}
	if scope.minimumScore != 2 {
		t.Fatalf("minimumScore = %d, want 2", scope.minimumScore)
	}
	if !scope.since.IsZero() {
		t.Fatalf("since should be zero value")
	}
	if scope.maxRequests != 1000 {
		t.Fatalf("maxRequests = %d, want 1000", scope.maxRequests)
	}
}
