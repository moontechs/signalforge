package stackexchange

import "net/http"

const (
	SourceName     = "stackexchange"
	SourceType     = "discussion"
	SignalIDPrefix = "se"
	APIBaseURL     = "https://api.stackexchange.com/2.3"
	APIFieldFilter = "!m(fMuA8s6W7k*5U0oVWnI*C)lS"
)

var DefaultSites = []string{"stackoverflow", "serverfault", "superuser"}

// searchResponse is the response envelope returned by the Stack Exchange API.
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

type Stats struct {
	Requests  int
	CacheHits int
}
