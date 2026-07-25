package reddit

import (
	"testing"
	"time"
)

func TestSourceConstants(t *testing.T) {
	t.Parallel()

	if SourceName != "reddit" {
		t.Fatalf("SourceName = %q, want %q", SourceName, "reddit")
	}
	if SourceType != "discussion" {
		t.Fatalf("SourceType = %q, want %q", SourceType, "discussion")
	}
	if SignalIDPrefix != "rd" {
		t.Fatalf("SignalIDPrefix = %q, want %q", SignalIDPrefix, "rd")
	}
}

func TestMetadataKeyConstants(t *testing.T) {
	t.Parallel()

	if MetaKeyPostScore != "post_score" {
		t.Fatalf("MetaKeyPostScore = %q, want %q", MetaKeyPostScore, "post_score")
	}
	if MetaKeyCommentCount != "comment_count" {
		t.Fatalf("MetaKeyCommentCount = %q, want %q", MetaKeyCommentCount, "comment_count")
	}
	if MetaKeyAuthor != "author" {
		t.Fatalf("MetaKeyAuthor = %q, want %q", MetaKeyAuthor, "author")
	}
	if MetaKeySubreddit != "subreddit" {
		t.Fatalf("MetaKeySubreddit = %q, want %q", MetaKeySubreddit, "subreddit")
	}
}

func TestDeriveScopeDefaultValues(t *testing.T) {
	t.Parallel()

	cfg := &ConfigValues{
		Enabled:            true,
		Subreddits:         []string{"golang"},
		MaxPostsPerRun:     100,
		MaxCommentsPerPost: 10,
		MaxRequests:        300,
	}
	since := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	scope := deriveScope(cfg, since)

	if scope.sort != "new" {
		t.Fatalf("scope.sort = %q, want %q", scope.sort, "new")
	}
	if scope.timeRange != "all" {
		t.Fatalf("scope.timeRange = %q, want %q", scope.timeRange, "all")
	}
}

func TestDeriveScopeExplicitValues(t *testing.T) {
	t.Parallel()

	cfg := &ConfigValues{
		Enabled:            true,
		Subreddits:         []string{"golang"},
		Sort:               "TOP",
		TimeRange:          "MONTH",
		MaxPostsPerRun:     100,
		MaxCommentsPerPost: 10,
		MaxRequests:        300,
	}
	since := time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC)

	scope := deriveScope(cfg, since)

	if scope.sort != "top" {
		t.Fatalf("scope.sort = %q, want %q", scope.sort, "top")
	}
	if scope.timeRange != "month" {
		t.Fatalf("scope.timeRange = %q, want %q", scope.timeRange, "month")
	}
	if len(scope.subreddits) != 1 || scope.subreddits[0] != "golang" {
		t.Fatalf("scope.subreddits = %v, want [golang]", scope.subreddits)
	}
	if scope.maxPosts != 100 {
		t.Fatalf("scope.maxPosts = %d, want %d", scope.maxPosts, 100)
	}
	if scope.maxComments != 10 {
		t.Fatalf("scope.maxComments = %d, want %d", scope.maxComments, 10)
	}
	if !scope.since.Equal(since) {
		t.Fatalf("scope.since = %v, want %v", scope.since, since)
	}
	if scope.maxRequests != 300 {
		t.Fatalf("scope.maxRequests = %d, want %d", scope.maxRequests, 300)
	}
}

func TestDeriveScopePreservesSince(t *testing.T) {
	t.Parallel()

	cfg := &ConfigValues{
		Enabled:            true,
		Subreddits:         []string{"golang"},
		Sort:               "new",
		TimeRange:          "all",
		MaxPostsPerRun:     50,
		MaxCommentsPerPost: 5,
		MaxRequests:        100,
	}
	since := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)

	scope := deriveScope(cfg, since)
	if !scope.since.Equal(since) {
		t.Fatalf("scope.since = %v, want %v", scope.since, since)
	}
}

func TestStatsZeroValues(t *testing.T) {
	t.Parallel()

	var s Stats
	if s.Requests != 0 {
		t.Fatalf("Stats.Requests = %d, want 0", s.Requests)
	}
	if s.CacheHits != 0 {
		t.Fatalf("Stats.CacheHits = %d, want 0", s.CacheHits)
	}
}

func TestConfigValuesDefaults(t *testing.T) {
	t.Parallel()

	cfg := &ConfigValues{
		Enabled: false,
	}
	if cfg.Enabled {
		t.Fatal("expected default ConfigValues.Enabled to be false")
	}
}
