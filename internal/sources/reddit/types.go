package reddit

import "time"

// ---------------------------------------------------------------------------
// Source constants
// ---------------------------------------------------------------------------

const (
	// SourceName is the canonical source name for Reddit.
	SourceName = "reddit"

	// SourceType is the signal source type label used in domain.RawSignal.
	SourceType = "discussion"
)

// SupportedSortValues is the set of valid Reddit listing sort parameters.
var SupportedSortValues = []string{
	"hot",
	"new",
	"top",
	"rising",
}

// SupportedTimeValues is the set of valid Reddit time filter parameters.
var SupportedTimeValues = []string{
	"hour",
	"day",
	"week",
	"month",
	"year",
	"all",
}

// DefaultSort is the default listing sort when not explicitly configured.
const DefaultSort = "new"

// DefaultTime is the default time filter when not explicitly configured.
const DefaultTime = "week"

// ---------------------------------------------------------------------------
// Metadata key conventions
// ---------------------------------------------------------------------------

const (
	// MetaKeyCommentParentIDs is the RawSignal.Metadata key whose value is a
	// comma-separated list of parent IDs for a flattened comment, ordered
	// from immediate parent to root.
	MetaKeyCommentParentIDs = "parent_ids"

	// MetaKeyCommentDepth is the RawSignal.Metadata key for the depth of a
	// flattened comment within the original tree (0 = direct reply to post).
	MetaKeyCommentDepth = "depth"

	// MetaKeyPostScore stores the original post score.
	MetaKeyPostScore = "post_score"

	// MetaKeyCommentCount stores the post's comment count.
	MetaKeyCommentCount = "comment_count"

	// MetaKeyAuthor stores the post or comment author.
	MetaKeyAuthor = "author"

	// MetaKeySubreddit stores the subreddit name.
	MetaKeySubreddit = "subreddit"

	// MetaKeyListingSort stores the sort used for the listing request.
	MetaKeyListingSort = "listing_sort"
)

// ---------------------------------------------------------------------------
// Signal identifier prefix
// ---------------------------------------------------------------------------

const (
	// SignalIDPrefix is prepended when building a domain.RawSignal.ID.
	SignalIDPrefix = "reddit"
)

// ---------------------------------------------------------------------------
// Reddit API response types
// ---------------------------------------------------------------------------

// oauthTokenResponse represents the Reddit OAuth token endpoint response.
type oauthTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	Scope       string `json:"scope"`
}

// listingResponse represents a Reddit API listing response (e.g., /r/{sub}/{sort}.json).
type listingResponse struct {
	Kind string      `json:"kind"`
	Data listingData `json:"data"`
}

// listingData holds the inner data of a listing response.
type listingData struct {
	After    string         `json:"after,omitempty"`
	Dist     int            `json:"dist"`
	Children []listingChild `json:"children"`
}

// listingChild is a wrapper for a listing child (post or comment).
type listingChild struct {
	Kind string   `json:"kind"`
	Data postData `json:"data"`
}

// postData represents a Reddit post (kind "t3") within a listing.
type postData struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Title       string  `json:"title"`
	Selftext    string  `json:"selftext"`
	Permalink   string  `json:"permalink"`
	Subreddit   string  `json:"subreddit"`
	Author      string  `json:"author"`
	Score       int     `json:"score"`
	NumComments int     `json:"num_comments"`
	CreatedUTC  float64 `json:"created_utc"`
	URL         string  `json:"url,omitempty"`
	Stickied    bool    `json:"stickied,omitempty"`
	Over18      bool    `json:"over_18,omitempty"`
}

// commentTreeResponse represents a Reddit comment tree response (/comments/{id}.json).
// It is a two-element array: the first is the post listing, the second is the comment listing.
type commentTreeResponse []listingResponse

// commentData represents a Reddit comment (kind "t1") within a comment tree.
type commentData struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Body       string          `json:"body"`
	Author     string          `json:"author"`
	Score      int             `json:"score"`
	CreatedUTC float64         `json:"created_utc"`
	ParentID   string          `json:"parent_id"`
	LinkID     string          `json:"link_id"`
	Replies    *repliesWrapper `json:"replies,omitempty"`
	Stickied   bool            `json:"stickied,omitempty"`
}

// repliesWrapper wraps nested comment replies in a listing structure.
type repliesWrapper struct {
	Kind string      `json:"kind"`
	Data repliesData `json:"data"`
}

// repliesData holds the inner data of a reply listing.
type repliesData struct {
	Children []replyChild `json:"children"`
}

// replyChild is a wrapper for a reply child (comment or more placeholder).
type replyChild struct {
	Kind string      `json:"kind"`
	Data commentData `json:"data"`
}

// ---------------------------------------------------------------------------
// Collector configuration and scope
// ---------------------------------------------------------------------------

// ConfigValues holds the subset of configuration fields needed by the
// collector, extracted from config.RedditConfig + Limits.MaxRedditRequests.
type ConfigValues struct {
	Enabled            bool
	Subreddits         []string
	Sort               string
	Time               string
	MaxPostsPerRun     int
	MaxCommentsPerPost int
	MaxRequests        int
}

// collectionScope is a concrete collection plan derived from configuration
// and a domain.CollectRequest.
type collectionScope struct {
	subreddits  []string
	sort        string
	timeFilter  string
	maxPosts    int
	maxComments int
	since       time.Time
	maxRequests int
}

// deriveScope maps ConfigValues + request parameters into a collectionScope.
func deriveScope(cfg *ConfigValues, since time.Time) collectionScope {
	sort := cfg.Sort
	if sort == "" {
		sort = DefaultSort
	}
	timeFilter := cfg.Time
	if timeFilter == "" {
		timeFilter = DefaultTime
	}
	return collectionScope{
		subreddits:  cfg.Subreddits,
		sort:        sort,
		timeFilter:  timeFilter,
		maxPosts:    cfg.MaxPostsPerRun,
		maxComments: cfg.MaxCommentsPerPost,
		since:       since,
		maxRequests: cfg.MaxRequests,
	}
}

// Stats holds per-run request and cache-hit counters exposed by the collector.
type Stats struct {
	Requests  int
	CacheHits int
}
