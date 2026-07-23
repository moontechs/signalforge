# Plan: M2-T7 — Stack Exchange Collector

## Overview

Implement a Stack Exchange API source collector under `internal/sources/stackexchange/` that fetches questions and answers from configured SE sites, handles the API's `backoff` parameter and quota headers, maps qualifying items into `domain.RawSignal` with bounded flattened comments, caches public responses on disk, exposes per-run request stats, and wires into the existing `collect` CLI command. Follow the same package structure and patterns as `internal/sources/hackernews/`.

## Context

- **New files:**
  - `internal/sources/stackexchange/doc.go`
  - `internal/sources/stackexchange/types.go`
  - `internal/sources/stackexchange/errors.go`
  - `internal/sources/stackexchange/client.go`
  - `internal/sources/stackexchange/fake_transport.go`
  - `internal/sources/stackexchange/parser.go`
  - `internal/sources/stackexchange/collector.go`
  - `internal/sources/stackexchange/types_test.go`
  - `internal/sources/stackexchange/client_test.go`
  - `internal/sources/stackexchange/parser_test.go`
  - `internal/sources/stackexchange/collector_test.go`
  - `internal/sources/stackexchange/integration_test.go`
  - `testdata/stackexchange/questions.json`
  - `testdata/stackexchange/answers.json`
  - `testdata/stackexchange/comments.json`

- **Modified files:**
  - `internal/config/config.go` — add `Validate()` for `StackExchangeConfig`, wire into `Config.Validate()`
  - `internal/config/config_test.go` — add validation tests for SE config
  - `internal/cli/collect.go` — add SE case in `buildCollector`, add SE stats tracking in `executeCollect`, update `collectStatsDelta` and `statsDelta` and `reportCollectSummary`
  - `internal/cli/collect_test.go` — add CLI tests for SE path
  - `internal/memory/memory.go` — add `AddStackExchangeRequests` and `AddStackExchangeCacheHits` methods
  - `internal/memory/memory_test.go` — add memory tests for SE methods (create file if not exists)
  - `.golangci.yml` — add gosec G404, funlen, gocritic, unparam exclude rules for SE files

- **Already present (no changes needed):**
  - `domain/types.go` — `StackExchangeReqs int` and `StackExchangeCache int` already defined in `ResearchStats`
  - `config.go` — `StackExchangeConfig` struct and `MaxStackExchangeReqs` limits already defined
  - `config.go` — `sourceAliases` already has `se`, `stackexchange`, `stack-overflow` → `stackexchange`
  - `config.go` — `DefaultDirStructure` already has `sources/stackexchange` and `cache/stackexchange`

## Detailed Tasks

---

### Task 1: Define Stack Exchange configuration validation, source types, and errors

**Files:** `internal/config/config.go`, `internal/config/config_test.go`, `internal/sources/stackexchange/doc.go`, `internal/sources/stackexchange/types.go`, `internal/sources/stackexchange/errors.go`, `internal/sources/stackexchange/types_test.go`

#### 1a. Add `Validate()` and helper functions to `config.go`

Add `ValidSESites()` returning:
```go
func ValidSESites() []string {
    return []string{
        "stackoverflow", "superuser", "serverfault", "webapps",
        "askubuntu", "unix", "softwareengineering", "diy",
        "gaming", "workplace", "security", "webmasters",
        "dba", "salesforce", "wordpress", "apple", "meta",
    }
}
```

Add `IsValidSESite(site string) bool` — case-insensitive check against `ValidSESites()`.

Add `Validate()` method on `StackExchangeConfig`:
```go
func (c *StackExchangeConfig) Validate() error {
    if !c.Enabled {
        return nil
    }
    if c.MaxItemsPerSite <= 0 {
        return errors.New("max_items_per_site must be greater than zero")
    }
    if c.MinimumScore < 0 {
        return errors.New("minimum_score must be zero or greater")
    }
    if c.MinimumViews < 0 {
    		return errors.New("minimum_views must be zero or greater")
    	}
    	if c.MaxCommentsPerItem < 0 {
    		return errors.New("max_comments_per_item must be zero or greater")
    	}
    	if len(c.Sites) == 0 {
        return errors.New("at least one site must be specified")
    }
    for _, site := range c.Sites {
        if !IsValidSESite(site) {
            return fmt.Errorf("unsupported site %q: must be one of %v", site, ValidSESites())
        }
    }
    return nil
}
```

Wire into `Config.Validate()` — add after HN validation:
```go
if err := c.Sources.StackExchange.Validate(); err != nil {
    return fmt.Errorf("validate stackexchange config: %w", err)
}
```

Update default `StackExchangeConfig` block in `DefaultConfig()` to match expected values:
```go
StackExchange: StackExchangeConfig{
	Enabled:            true,
	Sites:              []string{"stackoverflow", "superuser", "webapps"},
	MaxItemsPerSite:    200,
	MaxCommentsPerItem: 30,
	MinimumScore:       0,
	MinimumViews:       0,
},
```

#### 1b. Create `internal/sources/stackexchange/doc.go`

Package-level docstring (see HN pattern):
```go
// Package stackexchange implements the Stack Exchange API source collector
// for SignalForge.
//
// It fetches questions, answers, and comments from api.stackexchange.com/2.3,
// maps them to domain.RawSignal, and caches public responses on disk.
// The package handles the API's backoff parameter (mandatory pause),
// quota tracking, and pagination. An optional API key can be provided
// via STACKEXCHANGE_API_KEY or the ConfigValues.APIKey field.
//
// Authentication: optional. Without an API key the rate limit is
// 300 requests/day per IP; with a key it is 10,000 requests/day.
//
// Supported sites are defined in SupportedSites.
package stackexchange
```

#### 1c. Create `internal/sources/stackexchange/types.go`

Define, following the HN `types.go` pattern:

**Source constants:**
```go
const (
    SourceName     = "stackexchange"
    SourceType     = "discussion"
    SignalIDPrefix = "se"
)
```

**Supported/Default sites:**
```go
var SupportedSites = []string{
    "stackoverflow", "superuser", "serverfault", "webapps",
    "askubuntu", "unix", "softwareengineering", "diy",
    "gaming", "workplace", "security", "webmasters",
    "dba", "salesforce", "wordpress", "apple", "meta",
}

var DefaultSites = []string{"stackoverflow", "superuser", "webapps"}
```

**Metadata key constants:**
```go
const (
    MetaKeySite              = "site"
    MetaKeyQuestionScore     = "question_score"
    MetaKeyAnswerCount       = "answer_count"
    MetaKeyViewCount         = "view_count"
    MetaKeyAcceptedAnswerID  = "accepted_answer_id"
    MetaKeyTags              = "tags"
    MetaKeyAuthor            = "author"
    MetaKeyIsAnswered        = "is_answered"
)
```

**SE API response wrapper (generic, used for all endpoints):**
```go
type seAPIResponse struct {
	Items          json.RawMessage `json:"items"`
	HasMore        bool            `json:"has_more"`
	Backoff        int             `json:"backoff,omitempty"`
	QuotaRemaining int             `json:"quota_remaining"`
	QuotaMax       int             `json:"quota_max"`
	Total          int             `json:"total"`
	Page           int             `json:"page"`
	PageSize       int             `json:"pagesize"`
	ErrorID        int             `json:"error_id,omitempty"`
	ErrorName      string          `json:"error_name,omitempty"`
	ErrorMessage   string          `json:"error_message,omitempty"`
}
```

**Question fields:**
```go
type questionResponse struct {
    QuestionID       int            `json:"question_id"`
    Title            string         `json:"title"`
    Body             string         `json:"body"`
    Score            int            `json:"score"`
    ViewCount        int            `json:"view_count"`
    AnswerCount      int            `json:"answer_count"`
    AcceptedAnswerID int            `json:"accepted_answer_id,omitempty"`
    Tags             []string       `json:"tags"`
    Owner            *ownerResponse `json:"owner,omitempty"`
    CreationDate     int64          `json:"creation_date"`
    LastActivityDate int64          `json:"last_activity_date"`
    Link             string         `json:"link"`
    IsAnswered       bool           `json:"is_answered"`
    ClosedDate       int64          `json:"closed_date,omitempty"`
    ProtectedDate    int64          `json:"protected_date,omitempty"`
}
```

**Answer fields:**
```go
type answerResponse struct {
    AnswerID     int            `json:"answer_id"`
    QuestionID   int            `json:"question_id"`
    Body         string         `json:"body"`
    Score        int            `json:"score"`
    IsAccepted   bool           `json:"is_accepted"`
    Owner        *ownerResponse `json:"owner,omitempty"`
    CreationDate int64          `json:"creation_date"`
}
```

**Comment fields:**
```go
type commentResponse struct {
    CommentID    int            `json:"comment_id"`
    Body         string         `json:"body"`
    Score        int            `json:"score"`
    Owner        *ownerResponse `json:"owner,omitempty"`
    CreationDate int64          `json:"creation_date"`
}
```

**Owner fields:**
```go
type ownerResponse struct {
    DisplayName string `json:"display_name"`
    UserID      int    `json:"user_id"`
    UserType    string `json:"user_type"`
}
```

**Wrapper types for endpoint-specific responses (used for type-safe JSON unmarshalling):**
```go
type questionsResponse struct {
    Items          []questionResponse `json:"items"`
    HasMore        bool               `json:"has_more"`
    Backoff        int                `json:"backoff,omitempty"`
    QuotaRemaining int                `json:"quota_remaining"`
    QuotaMax       int                `json:"quota_max"`
    Total          int                `json:"total"`
    Page           int                `json:"page"`
    PageSize       int                `json:"pagesize"`
}

type answersResponse struct {
    Items          []answerResponse `json:"items"`
    HasMore        bool             `json:"has_more"`
    Backoff        int              `json:"backoff,omitempty"`
    QuotaRemaining int              `json:"quota_remaining"`
    QuotaMax       int              `json:"quota_max"`
    Total          int              `json:"total"`
    Page           int              `json:"page"`
    PageSize       int              `json:"pagesize"`
}

type commentsResponse struct {
    Items          []commentResponse `json:"items"`
    HasMore        bool              `json:"has_more"`
    Backoff        int               `json:"backoff,omitempty"`
    QuotaRemaining int               `json:"quota_remaining"`
    QuotaMax       int               `json:"quota_max"`
    Total          int               `json:"total"`
    Page           int               `json:"page"`
    PageSize       int               `json:"pagesize"`
}
```

**ConfigValues (exported, mirrors HN pattern):**
```go
type ConfigValues struct {
    Enabled            bool
    Sites              []string
    MaxItemsPerSite    int
    MaxCommentsPerItem int    // max answers+comments per question (0 = unlimited)
    MinimumScore       int
    MinimumViews       int
    MaxRequests        int
    APIKey             string // optional
}
```

**collectionScope (internal):**
```go
type collectionScope struct {
    sites                []string
    maxItemsPerSite      int
    maxCommentsPerItem   int
    minimumScore         int
    minimumViews         int
    since                time.Time
    maxRequests          int
    apiKey               string
}
```

**deriveScope:**
```go
func deriveScope(cfg *ConfigValues, since time.Time) collectionScope {
    return collectionScope{
        sites:              cfg.Sites,
        maxItemsPerSite:    cfg.MaxItemsPerSite,
        maxCommentsPerItem: cfg.MaxCommentsPerItem,
        minimumScore:       cfg.MinimumScore,
        minimumViews:       cfg.MinimumViews,
        since:              since,
        maxRequests:        cfg.MaxRequests,
        apiKey:             cfg.APIKey,
    }
}
```

**Stats and cachedResponse (same pattern as HN):**
```go
type Stats struct {
    Requests  int
    CacheHits int
}

type cachedResponse struct {
    Body        []byte    `json:"body"`
    CollectedAt time.Time `json:"collected_at"`
}
```

#### 1d. Create `internal/sources/stackexchange/errors.go`

```go
package stackexchange

import "errors"

var (
    ErrDisabled          = errors.New("stackexchange collection is disabled")
    ErrInvalidSite       = errors.New("invalid stackexchange site")
    ErrRequestCap        = errors.New("stackexchange request cap exhausted")
    ErrMalformedResponse = errors.New("malformed stackexchange response")
    ErrRetriesExhausted  = errors.New("stackexchange retries exhausted")
    ErrBackoffRequired   = errors.New("stackexchange backoff required")
	ErrAPIError          = errors.New("stackexchange api error")
)
```

#### 1e. Add config tests in `config_test.go`

Following the exact pattern of `TestDefaultConfigHackerNewsDefaults`, `TestHackerNewsConfigValidate`, `TestLoadConfigValidatesHackerNewsConfig`, `TestValidHNFeeds`, `TestIsValidHNFeed`:

- `TestDefaultConfigStackExchangeDefaults` — verify `Enabled = true`, `Sites = ["stackoverflow", "superuser", "webapps"]`, `MaxItemsPerSite = 200`, `MinimumScore = 0`, `MinimumViews = 0`, `MaxStackExchangeReqs = 500`
- `TestStackExchangeConfigValidate` — table-driven with sub-tests: valid enabled, disabled with bad values passes, zero max items, negative min score, negative min views, empty sites, unsupported site
- `TestValidSESites` — verify returned list includes known sites (spot-check 5)
- `TestIsValidSESite` — case-insensitive match (e.g., "StackOverflow"), invalid returns false
- `TestLoadConfigValidatesStackExchangeConfig` — write config JSON with `"stackexchange":{"max_items_per_site":0}`, verify `LoadConfig` returns validation error

#### 1f. Create types tests in `types_test.go`

Following HN's `types_test.go` pattern:
- `TestSourceConstants` — verify `SourceName = "stackexchange"`, `SourceType = "discussion"`, `SignalIDPrefix = "se"`
- `TestSupportedSites` — verify `SupportedSites` slice content
- `TestDefaultSites` — verify `DefaultSites = ["stackoverflow", "superuser", "webapps"]`
- `TestMetadataKeyConstants` — verify all metadata key constants
- `TestDeriveScope` — full scope derivation from ConfigValues + since time
- `TestDeriveScopeDefault` — zero since time

---

### Task 2: Implement the Stack Exchange API client with backoff handling, retries, quota tracking, and caching

**Files:** `internal/sources/stackexchange/client.go`, `internal/sources/stackexchange/fake_transport.go`, `internal/sources/stackexchange/client_test.go`

#### 2a. Create `client.go`

Implement following the exact same structure as HN's `client.go` with these additions:

**Transport interface + httpTransport** — identical to HN pattern.

**Client struct:**
```go
type client struct {
    transport             transport
    baseURL               string
    timeout               time.Duration
    retryMax              int
    maxRequests           int
    maxBodySize           int64
    apiKey                string
    retryBackoff          func(attempt int) time.Duration
    mu                    sync.Mutex
    requests, cacheHits   int
    store                 *storage.Storage
    forcedBackoff         time.Time // when server-requested backoff expires
}
```

**Constructor `newClient(t transport, cfg ConfigValues) *client`:**
- `baseURL = "https://api.stackexchange.com/2.3"`
- `timeout = 30 * time.Second`
- `retryMax = 3`
- `retryBackoff = math.Pow(2, attempt) * time.Second + rand.Intn(1000) * time.Millisecond` (same as HN)
- `maxBodySize = 10 * 1024 * 1024` (10 MB)
- `forcedBackoff = time.Time{}` (zero = no backoff active)
- `apiKey = cfg.APIKey`

**WithCache, Stats, cachePath, cached, save, requestCapReached, incrementRequests** — same pattern as HN, using `"cache/stackexchange"` in cache path.

**`get(ctx, path, ttl, out)` method** — the core HTTP method, expanded for SE:

1. Check cache first (same as HN)
2. Check request cap (same as HN)
3. **Check forcedBackoff** — if `time.Now().Before(c.forcedBackoff)`, sleep `time.Until(c.forcedBackoff)`. Also check context cancellation during sleep.
4. Build request URL with `c.baseURL + path` + `&key=<apikey>` if `c.apiKey != ""`
5. Retry loop (same retry logic as HN):
   - On success response (2xx):
     - **Parse `seAPIResponse` wrapper from body** to extract `Backoff`, `QuotaRemaining`, and `ErrorID`
     - **If `ErrorID != 0`, return `ErrAPIError`** with error_name/error_message context (the SE API may return business-logic errors with HTTP 200)
     - **If `backoff > 0`, set `c.forcedBackoff = time.Now().Add(time.Duration(backoff) * time.Second)`** — this is global so all subsequent requests wait
     - Unmarshal `items` from the wrapper into `out`
     - Cache and increment request counter
   - Retry on 5xx, 429 (same as HN)
   - Return immediately on 4xx (non-retryable)
6. Same error wrapping as HN

**Endpoint-specific methods:**

```go
func (c *client) questions(ctx context.Context, site string, fromdate, todate int64, page, pagesize int) (*questionsResponse, error)
```

Constructs path: `/questions?site=` + url.QueryEscape(site) + `&sort=creation&order=desc&filter=withbody&page=N&pagesize=N` + optional `&fromdate=X&todate=Y` + `&key=...` if API key set.

```go
func (c *client) answers(ctx context.Context, site string, questionIDs []int, page, pagesize int) (*answersResponse, error)
```

Constructs path: `/questions/{ids}/answers?site=` + url.QueryEscape(site) + `&filter=withbody&page=N&pagesize=N` + optional `&key=...`. IDs are comma-separated. For MVP, called per-question with single ID (not batch), so questionIDs is always length 1.

```go
func (c *client) comments(ctx context.Context, site string, questionIDs []int, page, pagesize int) (*commentsResponse, error)
```

Constructs path: `/questions/{ids}/comments?site=` + url.QueryEscape(site) + `&filter=withbody&page=N&pagesize=N` + optional `&key=...`. Same pattern as answers.

**TTL values for caching:**
- Questions: 5 minutes (matching HN feeds — questions change frequently)
- Answers: 30 minutes (answers are more stable)
- Comments: 30 minutes

**Helper methods** — URL construction with proper query string building:
```go
func (c *client) questionsURL(...) string
func (c *client) answersURL(...) string
func (c *client) commentsURL(...) string
```

**readBody** — same bounded reader as HN.

#### 2b. Create `fake_transport.go`

Exact copy of HN's `fake_transport.go` pattern (same types: `fakeResponse`, `fakeTransport`, same methods: `newFakeTransport`, `addResponse`, `addSequentialResponses`, `findResponseLocked`, `nextResponseLocked`, `Do`, `callCountFor`, `resetCallCount`).

Add helper:
```go
func testClient(fake *fakeTransport) *client {
    if fake == nil {
        fake = newFakeTransport()
    }
    return newClient(fake, ConfigValues{})
}
```

#### 2c. Write comprehensive client tests in `client_test.go`

Following HN's `client_test.go` pattern (all tests using `t.Parallel()`):

**Basic endpoint tests:**
- `TestClient_questions_success` — fetch questions page, verify parsed question IDs, titles, scores
- `TestClient_answers_success` — fetch answers for a question
- `TestClient_comments_success` — fetch comments for a question

**SE-specific protocol tests:**
- `TestClient_backoff` — response includes `"backoff":5`, verify `forcedBackoff` is set and next request is delayed at least 5 seconds (use `time.Now()` diff)
- `TestClient_backoff_global` — two sequential requests to different endpoints, first returns `backoff=3`, second must also wait even though it's `/answers` not `/questions`
- `TestClient_backoff_context_cancellation` — context cancelled during backoff sleep, verify `context.Canceled` error
- `TestClient_quota_remaining` — parse quota fields, verify no failure when quota is 0
- `TestClient_api_key_added` — when `ConfigValues.APIKey` is set, verify `key=` parameter appears in URL

**Cache tests (same pattern as HN):**
- `TestClient_cache_hit_questions` — first call misses, second hits cache, verify counts
- `TestClient_cache_expiration` — pre-write expired cached entry, verify re-fetch

**Error handling tests (same pattern as HN):**
- `TestClient_retry_transient_recovers` — 500 then 200
- `TestClient_retry_exhaustion` — all 500 → ErrRetriesExhausted
- `TestClient_context_cancellation` — cancelled context
- `TestClient_request_cap_reached` — pre-fill to cap, verify ErrRequestCap
- `TestClient_malformed_json` — invalid response body → ErrMalformedResponse (wrap through seAPIResponse parsing)
- `TestClient_non_retryable_error` — 403, not retried
- `TestClient_429_retry` — 429 then 200
- `TestClient_response_size_limit_exceeded` — oversized body
- `TestClient_no_store_never_caches` — without store, each call makes HTTP request

**Stats tests (same pattern as HN):**
- `TestClient_stats_tracking` — request counts accurate
- `TestClient_stats_after_cache_hit` — cache hits tracked correctly

**Concurrency:**
- `TestClient_concurrent_access` — multiple goroutines, -race clean (same pattern as HN)

**Pagination:**
- `TestClient_questions_pagination` — two pages, verify `has_more` handling and page increment (this is primarily tested at the client level to verify the response parsing works, full pagination loop is tested in collector)

---

### Task 3: Parse Stack Exchange responses into normalized RawSignal records

**Files:** `internal/sources/stackexchange/parser.go`, `internal/sources/stackexchange/parser_test.go`, `testdata/stackexchange/questions.json`, `testdata/stackexchange/answers.json`, `testdata/stackexchange/comments.json`

#### 3a. Create `parser.go`

**`parseQuestion(item, answers, comments, site, collectedAt)` returning `domain.RawSignal`:**
```go
func parseQuestion(item *questionResponse, answers []domain.Comment, comments []domain.Comment, site string, collectedAt time.Time) domain.RawSignal {
    body := item.Body  // SE API returns HTML body with filter=withbody

    s := domain.RawSignal{
        ID:           fmt.Sprintf("%s:%d", SignalIDPrefix, item.QuestionID),
        Source:       SourceName,
        SourceID:     strconv.Itoa(item.QuestionID),
        SourceType:   SourceType,
        URL:          item.Link,
        Title:        item.Title,
        Body:         body,
        Comments:     mergeAndSortComments(answers, comments),
        Community:    site,
        Category:     "question",
        Tags:         item.Tags,
        Score:        item.Score,
        CommentCount: len(item.Tags), // wait — NO. CommentCount should be total comments+answers count
        ViewCount:    item.ViewCount,
        AnswerCount:  item.AnswerCount,
        CreatedAt:    time.Unix(item.CreationDate, 0).UTC(),
        UpdatedAt:    time.Unix(item.LastActivityDate, 0).UTC(),
        CollectedAt:  collectedAt,
    }
    // Build metadata
    metadata := map[string]string{
        MetaKeySite:          site,
        MetaKeyQuestionScore: strconv.Itoa(item.Score),
        MetaKeyAnswerCount:   strconv.Itoa(item.AnswerCount),
        MetaKeyViewCount:     strconv.Itoa(item.ViewCount),
        MetaKeyIsAnswered:    strconv.FormatBool(item.IsAnswered),
    }
    if len(item.Tags) > 0 {
        metadata[MetaKeyTags] = strings.Join(item.Tags, ",")
    }
    if item.AcceptedAnswerID > 0 {
        metadata[MetaKeyAcceptedAnswerID] = strconv.Itoa(item.AcceptedAnswerID)
    }
    if item.Owner != nil && item.Owner.DisplayName != "" {
        metadata[MetaKeyAuthor] = item.Owner.DisplayName
    }
    s.Metadata = metadata

    // Compute content hash
    commentBodies := make([]string, len(s.Comments))
    for i, c := range s.Comments {
        commentBodies[i] = c.Body
    }
    s.ContentHash = storage.ContentHash(append([]string{s.Title, s.Body}, commentBodies...)...)

    // Fix CommentCount
    s.CommentCount = len(s.Comments)

    return s
}
```

**`parseAnswer(item) domain.Comment`:**
```go
func parseAnswer(item *answerResponse) domain.Comment {
    return domain.Comment{
        ID:        fmt.Sprintf("se_answer:%d", item.AnswerID),
        Body:      item.Body,
        Score:     item.Score,
        CreatedAt: time.Unix(item.CreationDate, 0).UTC(),
    }
}
```

**`parseComment(item) domain.Comment`:**
```go
func parseComment(item *commentResponse) domain.Comment {
    return domain.Comment{
        ID:        fmt.Sprintf("se_comment:%d", item.CommentID),
        Body:      item.Body,
        Score:     item.Score,
        CreatedAt: time.Unix(item.CreationDate, 0).UTC(),
    }
}
```

**`eligibleQuestion(item, scope) bool`:**
```go
func eligibleQuestion(item *questionResponse, scope collectionScope) bool {
    if item == nil {
        return false
    }
    if item.Score < scope.minimumScore {
        return false
    }
    if item.ViewCount < scope.minimumViews {
        return false
    }
    if !scope.since.IsZero() && time.Unix(item.CreationDate, 0).Before(scope.since) {
        return false
    }
    return true
}
```

**`mergeAndSortComments(answers, questionComments)` — order accepted answer first, then chronological:**
```go
func mergeAndSortComments(answers []domain.Comment, questionComments []domain.Comment) []domain.Comment {
    // If there's an accepted answer, ensure it comes first among answers.
    // (The accepted answer identification is handled in the collector before calling parseAnswer.)
    // Then sort everything by CreatedAt ascending.
    combined := make([]domain.Comment, 0, len(answers)+len(questionComments))
    combined = append(combined, answers...)
    combined = append(combined, questionComments...)
    sort.Slice(combined, func(i, j int) bool {
        return combined[i].CreatedAt.Before(combined[j].CreatedAt)
    })
    return combined
}
```

#### 3b. Create test fixtures

**`testdata/stackexchange/questions.json`** — a full `questionsResponse` JSON with:
- 3+ questions at varying scores/view counts
- Mix of answered/unanswered
- Tags array
- Owner info
- Various creation dates

**`testdata/stackexchange/answers.json`** — a full `answersResponse` JSON with:
- Multiple answers for a single question
- Mix of accepted and non-accepted
- Various scores

**`testdata/stackexchange/comments.json`** — a full `commentsResponse` JSON with:
- Multiple comments on a question
- Various scores

#### 3c. Write parser tests in `parser_test.go`

- `TestParseQuestion` — full mapping: ID prefix "se:", SourceID, URL, Title, Body, Score, ViewCount, Tags, Metadata keys (site, question_score, answer_count, view_count, tags, accepted_answer_id, author, is_answered), ContentHash not empty, CommentCount
- `TestParseQuestion_noOwner` — Owner is nil, ensure no author metadata
- `TestParseQuestion_noTags` — empty tags, ensure no tags metadata key (or empty string)
- `TestParseQuestion_noAcceptedAnswer` — AcceptedAnswerID = 0, verify metadata key absent
- `TestParseQuestion_notAnswered` — IsAnswered = false, verify metadata
- `TestParseAnswer` — answer maps to domain.Comment with ID prefix "se_answer:"
- `TestParseComment` — comment maps to domain.Comment with ID prefix "se_comment:"
- `TestEligibleQuestion` — table-driven: score threshold, views threshold, since filter, nil item
- `TestMergeAndSortComments` — accepted answer first, then chronological order
- `TestMergeAndSortComments_cap` — max comments cap (0 = unlimited)
- `TestContentHashDeterminism` — same inputs produce same hash
- `TestParseQuestion_fromFixture` — load `questions.json`, parse one item, verify all fields

Run `go test -v -count=1 -race ./internal/sources/stackexchange/...` and fix failures before Task 4.

---

### Task 4: Build the collector with bounded concurrency and site iteration

**Files:** `internal/sources/stackexchange/collector.go`, `internal/sources/stackexchange/collector_test.go`, `internal/sources/stackexchange/integration_test.go`

#### 4a. Create `collector.go`

**Collector struct** (mirrors HN):
```go
type Collector struct {
    config    ConfigValues
    client    *client
    now       func() time.Time
    mu        sync.Mutex
    requests  int
    cacheHits int
}
```

**Constructor `New(cfg *ConfigValues) (*Collector, error)`:**
- Returns `ErrDisabled` if `!cfg.Enabled`
- Validates each site against `SupportedSites` (case-insensitive); returns `ErrInvalidSite` for unknown
- Creates `httpTransport` wrapping `&http.Client{Timeout: 30 * time.Second}`
- Creates client via `newClient(transport, *cfg)`

**Name() → "stackexchange"** (same pattern)

**Test hooks:**
```go
func (c *Collector) WithTransport(t transport) *Collector
func (c *Collector) WithNow(now func() time.Time) *Collector
func (c *Collector) WithCache(store *storage.Storage) *Collector
```

**Stats()** — same as HN.

**Collect(ctx, req) implementation:**

1. **Derive scope** from config + req.Since

2. **Create dedup set** `map[int]struct{}` for SE question IDs across sites

3. **Iterate over sites** with semaphore-bounded worker pool (3 concurrent workers — SE backoff is global, high concurrency is wasteful):
   ```go
   sem := make(chan struct{}, 3)
   ```

4. **For each site, paginate through all question pages** until:
   - `has_more` is false, OR
   - collected items for this site >= `scope.maxItemsPerSite`, OR
   - request cap reached (`c.client.requestCapReached()`)
   
   Pagination logic:
   ```go
   page := 1
   pagesize := 100  // max per SE API
   fromdate := int64(0)
   if !scope.since.IsZero() {
       fromdate = scope.since.Unix()
   }
   ```

5. **For each qualifying question** (after `eligibleQuestion` check), fetch answers and comments:
   - Answers: `c.client.answers(ctx, site, []int{questionID}, 1, 100)`
   - Comments: `c.client.comments(ctx, site, []int{questionID}, 1, 100)`
   - Note: SE API supports up to 100 answers/comments per page; for MVP we only fetch the first page (most common case). Add a TODO comment about multi-page answer fetching for future.

6. **Merge and flatten** answers + comments:
   - Answers are parsed via `parseAnswer()` → `domain.Comment`
   - Comments are parsed via `parseComment()` → `domain.Comment`
   - If there's an accepted answer (identified by `questionsResponse.Items[i].AcceptedAnswerID`), ensure it comes first among answers
   - Merge and sort: `mergeAndSortComments(answers, questionComments)`
   - Apply `scope.maxCommentsPerItem` cap (0 = unlimited)

7. **Parse question** via `parseQuestion()` into `domain.RawSignal`

8. **Sort all signals** by `CreatedAt` descending (newest first)

9. **Apply global max items cap** — `scope.maxItemsPerSite * len(scope.sites)` as the total max items across all sites. Actually, re-read the requirement: the config has `MaxItemsPerSite`, so each site independently limits. But we should also have a combined hard cap — use `scope.maxItemsPerSite` per site and a global `len(sites) * scope.maxItemsPerSite` limit.

10. **Store per-run stats delta** (same pattern as HN)

11. **Return** signals with any partial errors joined

**Important note on backoff handling in collector:** Backoff is handled transparently by the client's `get` method (it parses the `backoff` field and sets `forcedBackoff` globally). The collector doesn't need special backoff logic beyond what the client provides.

**SE-specific consideration:** The SE API `questions` endpoint with `fromdate` only filters by creation date. Questions are sorted `creation=desc`, so once we hit a question older than `since`, we can stop paginating for that site (because subsequent pages will be even older). This optimization avoids unnecessary API calls.

#### 4b. Write collector unit tests in `collector_test.go`

Following HN's `collector_test.go` pattern with `testCollector` helper:

- `TestCollector_New_disabled` — returns ErrDisabled
- `TestCollector_New_invalidSite` — returns ErrInvalidSite
- `TestCollector_happyPath` — single site, single page of questions, no answers/comments fetched (or fetched but empty). Verify signal count, sorting, stats.
- `TestCollector_multipleSites` — two sites (stackoverflow + superuser), verify signals from both
- `TestCollector_dedupAcrossSites` — same question ID in both sites (edge case: unlikely but possible if same question is cross-posted), only first kept
- `TestCollector_scoreFiltering` — minimumScore=10, one question has score 5, excluded
- `TestCollector_viewsFiltering` — minimumViews=100, one question has 50 views, excluded
- `TestCollector_sinceFiltering` — since cutoff excludes old questions, newer included
- `TestCollector_maxItemsPerSiteCap` — per-site cap truncates results
- `TestCollector_requestCap` — pre-fill counter, returns partial + ErrRequestCap
- `TestCollector_emptySite` — site with no questions (empty items array)
- `TestCollector_answersAndComments` — question with answers and comments, verify merged comment output
- `TestCollector_acceptedAnswerFirst` — accepted answer comes first in comment list
- `TestCollector_contextCancellation` — cancelled context
- `TestCollector_cachedRepeat` — second call hits cache, identical results, stats show cache hits
- `TestCollector_stats` — request counter accurate
- `TestCollector_partialFailure` — one site fails, others succeed, partial results returned
- `TestCollector_pagination` — multiple pages of questions, verify all pages fetched and deduped
- `TestCollector_concurrentAccess` — multiple calls, -race clean

#### 4c. Write integration test in `integration_test.go`

Following HN's `integration_test.go` pattern with `integrationFixture`:

- Multi-site fixture with 2 sites, overlapping question IDs, mixed eligibility
- Questions with answers+comments to test full flatten pipeline
- Verify signal count, ContentHash determinism
- Verify sorting, metadata correctness
- Test with cache: second call uses fewer HTTP calls, results identical
- Test concurrent safety (multiple goroutines)
- Test pagination end-to-end (2+ pages per site)
- Test backoff behavior (client-level; collector-level verifies backoff doesn't block indefinitely)

Run `go test -v -count=1 -race ./internal/sources/stackexchange/...` and fix failures before Task 5.

---

### Task 5: Add SE request/cache-hit tracking to memory

**Files:** `internal/memory/memory.go`, `internal/memory/memory_test.go`

#### 5a. Add methods to `memory.go`

Following the pattern of `AddHNRequests` and `AddHNCacheHits`:

```go
// AddStackExchangeRequests increments the Stack Exchange request count.
func (m *DefaultMemory) AddStackExchangeRequests(count int) {
    if count <= 0 {
        return
    }
    m.mu.Lock()
    defer m.mu.Unlock()
    m.mem.Stats.StackExchangeReqs += count
}

// AddStackExchangeCacheHits increments the Stack Exchange cache hit count.
func (m *DefaultMemory) AddStackExchangeCacheHits(count int) {
    if count <= 0 {
        return
    }
    m.mu.Lock()
    defer m.mu.Unlock()
    m.mem.Stats.StackExchangeCache += count
}
```

#### 5b. Create/update `memory_test.go`

Check if file exists; if not, create it with tests:
- `TestAddStackExchangeRequests` — add 5, verify stats.StackExchangeReqs == 5
- `TestAddStackExchangeCacheHits` — add 3, verify stats.StackExchangeCache == 3
- `TestAddStackExchangeRequests_zero` — add 0, no change
- `TestAddStackExchangeRequests_negative` — add -1, no change
- `TestAddStackExchangeRequests_accumulation` — add 2, add 3, total = 5
- `TestAddStackExchangeConcurrent` — 10 goroutines adding 1 each, verify total = 10 (with t.Parallel(), -race)

Run `go test -v -count=1 -race ./internal/memory/...` and fix failures before Task 6.

---

### Task 6: Wire Stack Exchange into the CLI collect command and reporting

**Files:** `internal/cli/collect.go`, `internal/cli/collect_test.go`

#### 6a. Modify `collect.go`

**Add import:**
```go
"github.com/moontechs/signalforge/internal/sources/stackexchange"
```

**Add SE case in `buildCollector` switch (after HN case):**
```go
case "stackexchange":
    if !cfg.Sources.StackExchange.Enabled {
        return nil, errors.New("stackexchange collection is disabled in config")
    }
    apiKey := os.Getenv("STACKEXCHANGE_API_KEY")
    seCfg := &stackexchange.ConfigValues{
        Enabled:            cfg.Sources.StackExchange.Enabled,
        Sites:              cfg.Sources.StackExchange.Sites,
        MaxItemsPerSite:    cfg.Sources.StackExchange.MaxItemsPerSite,
        MaxCommentsPerItem: 20, // hardcode reasonable default; config doesn't have this field yet
        MinimumScore:       cfg.Sources.StackExchange.MinimumScore,
        MinimumViews:       cfg.Sources.StackExchange.MinimumViews,
        MaxRequests:        cfg.Limits.MaxStackExchangeReqs,
        APIKey:             apiKey,
    }
    collector, err := stackexchange.New(seCfg)
    if err != nil {
        return nil, fmt.Errorf("create stackexchange collector: %w", err)
    }
    collector.WithCache(store)
    return collector, nil
```

Note: `MaxCommentsPerItem` is now defined in `StackExchangeConfig` (added in Task 1a with default 30). It is mapped to `ConfigValues.MaxCommentsPerItem` here.

**Update `collectStatsDelta` struct — add SE fields:**
```go
type collectStatsDelta struct {
    collected   int
    skipped     int
    requests    int       // GitHub requests
    hnRequests  int
    hnCacheHits int
    seRequests  int
    seCacheHits int
}
```

**Update `statsDelta` function:**
```go
return collectStatsDelta{
    collected:   after.RawSignalsCollected - before.RawSignalsCollected,
    skipped:     after.RawSignalsSkipped - before.RawSignalsSkipped,
    requests:    after.GitHubRequests - before.GitHubRequests,
    hnRequests:  after.HackerNewsRequests - before.HackerNewsRequests,
    hnCacheHits: after.HackerNewsCacheHits - before.HackerNewsCacheHits,
    seRequests:  after.StackExchangeReqs - before.StackExchangeReqs,
    seCacheHits: after.StackExchangeCache - before.StackExchangeCache,
}
```

**Add SE type assertion in `executeCollect` (after HN block):**
```go
if seCol, ok := collector.(*stackexchange.Collector); ok {
    stats := seCol.Stats()
    env.mem.AddStackExchangeRequests(stats.Requests)
    env.mem.AddStackExchangeCacheHits(stats.CacheHits)
}
```

**Update `reportCollectSummary` — add SE stats section:**
```go
if delta.seRequests > 0 {
    msg += fmt.Sprintf(", SE requests: %d (cache hits: %d)", delta.seRequests, delta.seCacheHits)
}
```

The function already uses a series of `if delta.XX > 0` checks, so adding SE follows naturally.

#### 6b. Update CLI tests in `collect_test.go`

Following the same patterns as existing tests:

- `TestResolveCollectSources_SE` — "se" → `["stackexchange"]`
- `TestResolveCollectSources_StackOverflowAlias` — "stack-overflow" → `["stackexchange"]`
- `TestBuildCollector_SE` — valid SE config returns collector with Name() == "stackexchange"
- `TestBuildCollector_SE_Disabled` — disabled config returns error
- `TestBuildCollector_SE_InvalidSite` — invalid site returns error
- `TestBuildCollector_SE_NoTokenRequired` — unlike GitHub, SE doesn't require GITHUB_TOKEN
- `TestStatsDelta_SE` — delta includes SE fields
- `TestReportCollectSummary_SE` — summary includes "SE requests:" when delta.seRequests > 0
- `TestReportCollectSummary_NoSE` — summary omits SE when delta is zero
- `TestReportCollectSummary_AllSources` — combined GH + HN + SE reporting

Run `go test -v -count=1 -race ./internal/cli/...` and fix failures before Task 7.

---

### Task 7: Update golangci-lint exclude rules

**Files:** `.golangci.yml`

Add the following exclude rules (following the same patterns as existing HN/GitHub rules):

# gosec/G404: weak RNG for jitter is intentional (SE)
- linters:
    - gosec
  text: "G404"
  path: internal/sources/stackexchange/client\\.go

# gochecknoglobals: SupportedSites and metadata key constants are intentional
- linters:
    - gochecknoglobals
  text: "SupportedSites|DefaultSites|MetaKey|SourceName|SourceType|SignalIDPrefix"
  path: internal/sources/stackexchange/types\\.go

# mnd: magic numbers for response size limits, page sizes, and retry
- linters:
    - mnd
  text: "10 \\* 1024 \\* 1024|30 \\* time|100|3|5 \\* time|30 \\* time"
  path: internal/sources/stackexchange/(client|collector)\\.go

# mnd: magic numbers in test constants
- linters:
    - mnd
  text: "10 \\* 1024 \\* 1024|30|100|5"
  path: internal/sources/stackexchange/(client_test|collector_test)\\.go

# funlen: Collect has many statements inherent to pipeline logic
- linters:
    - funlen
  text: "Collect"
  path: internal/sources/stackexchange/collector\.go

# gocritic/hugeParam: req is part of the SourceCollector interface
- linters:
    - gocritic
  text: "hugeParam: req is heavy"
  path: internal/sources/stackexchange/collector\.go

# unparam: WithTransport/WithNow/WithCache return *Collector for builder pattern chaining
- linters:
    - unparam
  text: "WithTransport|WithNow|WithCache"
  path: internal/sources/stackexchange/collector\.go

# unparam: WithCache returns *client for builder pattern
- linters:
    - unparam
  text: "WithCache"
  path: internal/sources/stackexchange/client\.go

# wrapcheck: io.ReadAll is a standard library call
- linters:
    - wrapcheck
  text: "io.ReadAll"
  path: internal/sources/stackexchange/client\.go
```

Run `golangci-lint run ./...` and fix any lint issues found before Task 8.

---

### Task 8: Final validation — tests, vet, lint, build, coverage

**Files:** `AGENTS.md` (update SE collector docs)

#### Validation commands (run in order):

```bash
# 1. Build
go build ./cmd/signalforge/

# 2. Vet
go vet ./...

# 3. All tests (with race detector)
go test -race -count=1 ./...

# 4. Lint
golangci-lint run ./...

# 5. Coverage per package
go test -cover -count=1 ./internal/sources/stackexchange/...
go test -cover -count=1 ./internal/config/...
go test -cover -count=1 ./internal/cli/...
go test -cover -count=1 ./internal/memory/...
```

#### Update AGENTS.md

Add SE collector documentation:
```markdown
## Stack Exchange collector (M2-T7) — complete

- SE collector is now fully wired end-to-end in the CLI: `signalforge collect --sources stackexchange --since 30d`
- No API keys required for MVP — optional `STACKEXCHANGE_API_KEY` env var raises rate limit from 300→10,000 req/day
- API base: `https://api.stackexchange.com/2.3/`
- Default sites: `stackoverflow`, `superuser`, `webapps` (configurable)
- Backoff handling: mandatory pause enforced from `backoff` response field; SE bans clients that ignore it
- Quota tracking: `quota_remaining` parsed and logged but does not fail requests
- Pagination: automatic page iteration until `has_more=false`, per-site `max_items_per_site` cap, global request cap
- Questions filtered by: minimum score, minimum views, creation date window
- Comments: question comments + answer comments merged and flattened (accepted answer first, then chronological)
- Caching: TTL-based on-disk cache (5 min questions, 30 min answers/comments)
- Supported sites: stackoverflow, superuser, serverfault, webapps, askubuntu, unix, softwareengineering, diy, gaming, workplace, security, webmasters, dba, salesforce, wordpress, apple, meta
- Config example:
  ```json
  {
    "sources": {
      "stackexchange": {
        "enabled": true,
        "sites": ["stackoverflow", "superuser"],
        "max_items_per_site": 200,
        "max_comments_per_item": 30,
        "minimum_score": 0,
        "minimum_views": 0
      }
    }
  }
  ```
```

## Edge cases and gotchas

| Issue | Resolution |
|-------|-----------|
| SE API returns `backoff` parameter | Client stores `forcedBackoff` globally (mutex-protected), all subsequent requests wait |
| SE API returns `quota_remaining: 0` | Continue best-effort; don't fail. `maxRequests` config cap is primary throttle |
| Pagination with `fromdate` | Once a page has all items older than `since`, stop paginating (items are sorted by creation descending) |
| Cross-posted questions (same ID on multiple sites) | Dedup by question ID across all sites within a run |
| Very long HTML bodies | 10MB max body size limit on response reading |
| Rate limiting (429) | Retry with exponential backoff + jitter (same as HN) |
| No answers/comments for a question | Empty arrays returned — handled gracefully |
| Site name case | `IsValidSESite()` is case-insensitive; client lowercases before API call |
| Accepted answer identification | Use `AcceptedAnswerID` from question object when fetching answers |
| Missing `owner` field | Check for nil before accessing DisplayName |

## Future considerations (post-MVP)

- Support `/search/advanced` endpoint for tag-filtered queries
- Multi-page answer/comment fetching (for questions with 100+ answers)
- Concurrent answer/comment fetching per question (currently sequential within each question)
- Resumable cursor-based pagination (currently page-based)
