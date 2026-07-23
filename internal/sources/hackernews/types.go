package hackernews

import "time"

// ---------------------------------------------------------------------------
// Source constants
// ---------------------------------------------------------------------------

const (
	// SourceName is the canonical source name for Hacker News.
	SourceName = "hackernews"

	// SourceType is the signal source type label used in domain.RawSignal.
	SourceType = "discussion"
)

// SupportedFeeds is the set of feed names the HN Firebase API provides.
var SupportedFeeds = []string{
	"askstories",
	"showstories",
	"newstories",
	"topstories",
	"beststories",
}

// DefaultFeeds are the feeds enabled by default.
var DefaultFeeds = []string{"askstories", "showstories", "newstories"}

// ---------------------------------------------------------------------------
// Metadata key conventions
// ---------------------------------------------------------------------------

const (
	// MetaKeyCommentParentIDs is the RawSignal.Metadata key whose value is a
	// comma-separated list of parent item IDs for a flattened comment, ordered
	// from immediate parent to root.
	MetaKeyCommentParentIDs = "parent_ids"

	// MetaKeyCommentDepth is the RawSignal.Metadata key for the depth of a
	// flattenend comment within the original tree (0 = direct reply to story).
	MetaKeyCommentDepth = "depth"

	// MetaKeyStoryScore stores the original story score.
	MetaKeyStoryScore = "story_score"

	// MetaKeyCommentCount stores the story's descendant count.
	MetaKeyCommentCount = "comment_count"

	// MetaKeyAuthor stores the item author.
	MetaKeyAuthor = "author"
)

// ---------------------------------------------------------------------------
// Signal identifier prefix
// ---------------------------------------------------------------------------

const (
	// SignalIDPrefix is prepended when building a domain.RawSignal.ID.
	SignalIDPrefix = "hn"
)

// ---------------------------------------------------------------------------
// Firebase API response types
// ---------------------------------------------------------------------------

// feedResponse is a list of item IDs returned by a feed endpoint
// (e.g., /v0/newstories.json).
type feedResponse []int

// itemResponse represents a single Hacker News item (story, comment, etc.)
// from the Firebase API.
type itemResponse struct {
	ID          int    `json:"id"`
	Deleted     bool   `json:"deleted,omitempty"`
	Type        string `json:"type,omitempty"`
	By          string `json:"by,omitempty"`
	Time        int64  `json:"time,omitempty"`
	Text        string `json:"text,omitempty"`
	Dead        bool   `json:"dead,omitempty"`
	Parent      int    `json:"parent,omitempty"`
	Kids        []int  `json:"kids,omitempty"`
	URL         string `json:"url,omitempty"`
	Score       int    `json:"score,omitempty"`
	Title       string `json:"title,omitempty"`
	Descendants int    `json:"descendants,omitempty"`
}

// ---------------------------------------------------------------------------
// Collector configuration and scope
// ---------------------------------------------------------------------------

// configValues holds the subset of configuration fields needed by the
// collector, extracted from config.HackerNewsConfig + Limits.MaxHNRequests.
type configValues struct {
	Enabled            bool
	Feeds              []string
	MaxItemsPerRun     int
	MaxCommentsPerItem int
	MinimumScore       int
	MaxRequests        int
}

// collectionScope is a concrete collection plan derived from configuration
// and a domain.CollectRequest.
type collectionScope struct {
	feeds              []string
	maxItems           int
	maxComments        int
	minimumScore       int
	since              time.Time
	maxRequests        int
}

// deriveScope maps configValues + request parameters into a collectionScope.
func deriveScope(cfg *configValues, since time.Time) collectionScope {
	return collectionScope{
		feeds:              cfg.Feeds,
		maxItems:           cfg.MaxItemsPerRun,
		maxComments:        cfg.MaxCommentsPerItem,
		minimumScore:       cfg.MinimumScore,
		since:              since,
		maxRequests:        cfg.MaxRequests,
	}
}

// ---------------------------------------------------------------------------
// Cache types
// ---------------------------------------------------------------------------

// cachedResponse holds a cached API response body and its collection time.
type cachedResponse struct {
	Body        []byte    `json:"body"`
	CollectedAt time.Time `json:"collected_at"`
}

// Stats holds per-run request and cache-hit counters exposed by the collector.
type Stats struct {
	Requests  int
	CacheHits int
}
