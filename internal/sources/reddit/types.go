package reddit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	SourceName          = "reddit"
	SourceType          = "discussion"
	SignalIDPrefix      = "rd"
	MetaKeyAuthor       = "author"
	MetaKeySubreddit    = "subreddit"
	MetaKeyPostScore    = "post_score"
	MetaKeyCommentCount = "comment_count"
)

// ConfigValues contains the Reddit settings needed by the collector.
type ConfigValues struct {
	Enabled            bool
	ClientID           string
	ClientSecret       string
	Subreddits         []string
	MaxPostsPerRun     int
	MaxCommentsPerPost int
	MaxRequests        int
	Sort               string
	TimeRange          string
}

type collectionScope struct {
	subreddits                         []string
	maxPosts, maxComments, maxRequests int
	sort, timeRange                    string
	since                              time.Time
}

func deriveScope(cfg *ConfigValues, since time.Time) collectionScope {
	sort := strings.ToLower(strings.TrimSpace(cfg.Sort))
	timeRange := strings.ToLower(strings.TrimSpace(cfg.TimeRange))
	if sort == "" {
		sort = "new"
	}
	if timeRange == "" {
		timeRange = "all"
	}
	return collectionScope{subreddits: cfg.Subreddits, maxPosts: cfg.MaxPostsPerRun, maxComments: cfg.MaxCommentsPerPost, maxRequests: cfg.MaxRequests, sort: sort, timeRange: timeRange, since: since}
}

type Stats struct {
	Requests  int
	CacheHits int
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	Scope       string `json:"scope"`
}

type listingResponse struct {
	Data listingData `json:"data"`
}
type listingData struct {
	Children []listingChild `json:"children"`
	After    string         `json:"after"`
}
type listingChild struct {
	Kind string       `json:"kind"`
	Data postResponse `json:"data"`
}

// listingReplies models Reddit's polymorphic replies field, which is either a
// listing object or an empty string/null for leaf comments.
type listingReplies struct {
	Listing *listingResponse `json:"-"`
}

func (r *listingReplies) UnmarshalJSON(data []byte) error {
	value := bytes.TrimSpace(data)
	if bytes.Equal(value, []byte("null")) || bytes.Equal(value, []byte(`""`)) {
		r.Listing = nil
		return nil
	}
	var listing listingResponse
	if err := json.Unmarshal(value, &listing); err != nil {
		return fmt.Errorf("decode reddit replies: %w", err)
	}
	r.Listing = &listing
	return nil
}

type postResponse struct {
	ID                string         `json:"id"`
	Name              string         `json:"name"`
	Title             string         `json:"title"`
	Selftext          string         `json:"selftext"`
	Author            string         `json:"author"`
	Subreddit         string         `json:"subreddit"`
	Permalink         string         `json:"permalink"`
	URL               string         `json:"url"`
	Score             int            `json:"score"`
	NumComments       int            `json:"num_comments"`
	CreatedUTC        float64        `json:"created_utc"`
	Over18            bool           `json:"over_18"`
	Removed           bool           `json:"removed"`
	RemovedByCategory string         `json:"removed_by_category"`
	IsSelf            bool           `json:"is_self"`
	Body              string         `json:"body"`
	Replies           listingReplies `json:"replies"`
	ParentID          string         `json:"parent_id"`
	LinkID            string         `json:"link_id"`
}
