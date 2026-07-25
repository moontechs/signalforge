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
	if SignalIDPrefix != "reddit" {
		t.Fatalf("SignalIDPrefix = %q, want %q", SignalIDPrefix, "reddit")
	}
}

func TestSupportedSortValues(t *testing.T) {
	t.Parallel()

	expected := []string{"hot", "new", "top", "rising"}
	if len(SupportedSortValues) != len(expected) {
		t.Fatalf("SupportedSortValues = %v, want %v", SupportedSortValues, expected)
	}
	for i, v := range expected {
		if SupportedSortValues[i] != v {
			t.Fatalf("SupportedSortValues[%d] = %q, want %q", i, SupportedSortValues[i], v)
		}
	}
}

func TestSupportedTimeValues(t *testing.T) {
	t.Parallel()

	expected := []string{"hour", "day", "week", "month", "year", "all"}
	if len(SupportedTimeValues) != len(expected) {
		t.Fatalf("SupportedTimeValues = %v, want %v", SupportedTimeValues, expected)
	}
	for i, v := range expected {
		if SupportedTimeValues[i] != v {
			t.Fatalf("SupportedTimeValues[%d] = %q, want %q", i, SupportedTimeValues[i], v)
		}
	}
}

func TestDefaultSort(t *testing.T) {
	t.Parallel()
	if DefaultSort != "new" {
		t.Fatalf("DefaultSort = %q, want %q", DefaultSort, "new")
	}
}

func TestDefaultTime(t *testing.T) {
	t.Parallel()
	if DefaultTime != "week" {
		t.Fatalf("DefaultTime = %q, want %q", DefaultTime, "week")
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
	if MetaKeyListingSort != "listing_sort" {
		t.Fatalf("MetaKeyListingSort = %q, want %q", MetaKeyListingSort, "listing_sort")
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
	if scope.timeFilter != "week" {
		t.Fatalf("scope.timeFilter = %q, want %q", scope.timeFilter, "week")
	}
}

func TestDeriveScopeExplicitValues(t *testing.T) {
	t.Parallel()

	cfg := &ConfigValues{
		Enabled:            true,
		Subreddits:         []string{"golang"},
		Sort:               "top",
		Time:               "month",
		MaxPostsPerRun:     100,
		MaxCommentsPerPost: 10,
		MaxRequests:        300,
	}
	since := time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC)

	scope := deriveScope(cfg, since)

	if scope.sort != "top" {
		t.Fatalf("scope.sort = %q, want %q", scope.sort, "top")
	}
	if scope.timeFilter != "month" {
		t.Fatalf("scope.timeFilter = %q, want %q", scope.timeFilter, "month")
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
		Time:               "all",
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
