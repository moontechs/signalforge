package stackexchange

import (
	"encoding/json"
	"net/http"
	"time"
)

const (
	SourceName     = "stackexchange"
	SourceType     = "discussion"
	SignalIDPrefix = "se"
	APIBaseURL     = "https://api.stackexchange.com/2.3"
	APIFieldFilter = "!m(fMuA8s6W7k*5U0oVWnI*C)lS"
)

var DefaultSites = []string{"stackoverflow", "serverfault", "superuser"}

type searchResponse struct {
	Items          []questionDTO `json:"items"`
	HasMore        bool          `json:"has_more"`
	QuotaMax       int           `json:"quota_max"`
	QuotaRemaining int           `json:"quota_remaining"`
	Backoff        int           `json:"backoff,omitempty"`
	ErrorID        *int          `json:"error_id,omitempty"`
	ErrorName      string        `json:"error_name,omitempty"`
	ErrorMessage   string        `json:"error_message,omitempty"`
}

// apiResponse is the generic response envelope from the Stack Exchange API.
// Items is left as raw JSON so that each endpoint can decode its own item type.
type apiResponse struct {
	Items          json.RawMessage `json:"items"`
	HasMore        bool            `json:"has_more"`
	QuotaMax       int             `json:"quota_max"`
	QuotaRemaining int             `json:"quota_remaining"`
	Backoff        int             `json:"backoff,omitempty"`
	ErrorID        *int            `json:"error_id,omitempty"`
	ErrorName      string          `json:"error_name,omitempty"`
	ErrorMessage   string          `json:"error_message,omitempty"`
}

// questionsResponse wraps apiResponse for the /questions endpoint.
type questionsResponse struct {
	apiResponse
	Questions []questionDTO
}

// answersResponse wraps apiResponse for the /questions/{id}/answers endpoint.
type answersResponse struct {
	apiResponse
	Answers []answerDTO
}

// commentsResponse wraps apiResponse for the /questions/{id}/comments endpoint.
type commentsResponse struct {
	apiResponse
	Comments []commentDTO
}

type questionDTO struct {
	QuestionID       int      `json:"question_id"`
	Title            string   `json:"title"`
	BodyMarkdown     string   `json:"body_markdown"`
	Link             string   `json:"link"`
	Owner            ownerDTO `json:"owner"`
	CreationDate     int64    `json:"creation_date"`
	LastActivityDate int64    `json:"last_activity_date"`
	Score            int      `json:"score"`
	AnswerCount      int      `json:"answer_count"`
	ViewCount        int      `json:"view_count"`
	TagCount         int      `json:"tag_count"`
	Tags             []string `json:"tags"`
	IsAnswered       bool     `json:"is_answered"`
	AcceptedAnswerID *int     `json:"accepted_answer_id,omitempty"`
	ClosedDate       *int64   `json:"closed_date,omitempty"`
	ProtectedDate    *int64   `json:"protected_date,omitempty"`
}

type ownerDTO struct {
	DisplayName string `json:"display_name"`
	UserID      int    `json:"user_id"`
	Reputation  int    `json:"reputation"`
	UserType    string `json:"user_type"`
	AcceptRate  *int   `json:"accept_rate,omitempty"`
}

// answerDTO represents an answer from the Stack Exchange API.
type answerDTO struct {
	AnswerID     int      `json:"answer_id"`
	QuestionID   int      `json:"question_id"`
	BodyMarkdown string   `json:"body_markdown"`
	Owner        ownerDTO `json:"owner"`
	CreationDate int64    `json:"creation_date"`
	Score        int      `json:"score"`
	IsAccepted   bool     `json:"is_accepted"`
}

// commentDTO represents a comment from the Stack Exchange API.
type commentDTO struct {
	CommentID    int      `json:"comment_id"`
	PostID       int      `json:"post_id"`
	BodyMarkdown string   `json:"body_markdown"`
	Owner        ownerDTO `json:"owner"`
	CreationDate int64    `json:"creation_date"`
	Score        int      `json:"score"`
}

// cachedResponse stores a response body and its collection timestamp for caching.
type cachedResponse struct {
	Body        []byte    `json:"body"`
	CollectedAt time.Time `json:"collected_at"`
}

// SiteConfig describes collection settings for one Stack Exchange site.
type SiteConfig struct {
	Name         string
	MinimumScore int
	MinimumViews int
	PageSize     int
	MaxPages     int
}

// ConfigValues contains the collector settings extracted from application config.
// APIKey is used only for requests and must never be persisted or included in cache keys.
type ConfigValues struct {
	Enabled         bool
	APIKey          string
	Sites           []string
	MaxItemsPerSite int
	MinimumScore    int
	MinimumViews    int
	PageSize        int
	MaxPagesPerSite int
	MaxRequests     int
	BaseURL         string
	HTTPClient      *http.Client
}

// Stats holds per-run request and cache-hit counters.
type Stats struct {
	Requests  int
	CacheHits int
}

// QuestionScope defines eligibility criteria for a single SE question.
type QuestionScope struct {
	MinimumScore int
	MinimumViews int
	Since        time.Time
}

const (
	MetaKeyStoryScore     = "story_score"
	MetaKeyAuthor         = "author"
	MetaKeyViewCount      = "view_count"
	MetaKeyTags           = "tags"
	MetaKeySiteName       = "site_name"
	MetaKeyAnswerCount    = "answer_count"
	MetaKeyIsAnswered     = "is_answered"
	MetaKeyAcceptedAnswer = "accepted_answer_id"
)
