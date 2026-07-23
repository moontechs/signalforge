# Plan: M2-T7 Stack Exchange collector

### Task 1: Define Stack Exchange source types

- [x] Create `internal/sources/stackexchange/types.go` with:
  - API DTOs: `searchResponse` (items, has_more, quota_max, quota_remaining, backoff, error_id, error_name, error_message), `questionDTO` (question_id, title, body_markdown, link, owner.display_name, creation_date, last_activity_date, score, answer_count, view_count, tag_count, tags, is_answered, accepted_answer_id, closed_date, protected_date), `ownerDTO` (display_name, user_id, reputation, user_type, accept_rate)
  - Collector config struct following HN's pattern (injectable HTTP client, base URL, test transport)
  - SiteConfig struct with: name, minimum_score, minimum_views, page_size, max_pages
  - `SignalIDPrefix = "se"` for stable source IDs
  - `SourceName = "stackexchange"`, `SourceType = "discussion"`
  - Default sites: stackoverflow, serverfault, superuser (matching HN's `defaultFeeds` pattern)
  - API filter string that requests fields: `!m(fMuA8s6W7k*5U0oVWnI*C)lS` (custom filter for minimal payload)
  - Note: API key is NEVER stored in types, cache keys, or persisted data

- [x] Add `internal/sources/stackexchange/doc.go` with package documentation (mirror HN pattern)
- [x] Add `internal/sources/stackexchange/errors.go` with typed errors:
  - `ErrDisabled` — stackexchange disabled in config
  - `ErrNoSitesConfigured` — empty site list
  - `ErrRequestCap` — per-run request limit reached
  - `ErrQuotaExhausted` — API quota_remaining == 0
  - `ErrMalformedResponse` — unparseable API response
  - `ErrRetriesExhausted` — all retry attempts consumed
  - `ErrBackoffCancelled` — context cancelled during backoff wait
  - `ErrAPIError` — wraps API error_id + error_name (context.Canceled/DeadlineExceeded detectable with errors.Is)

### Task 2: Extend existing config (config.go)

- [ ] Add fields to existing `StackExchangeConfig`:
  - `APIKey string \`json:"api_key"\`` — read from STACKEXCHANGE_API_KEY env var, never serialized to JSON
  - `MaxPagesPerSite int` and `PageSize int` defaulting to 25 (API max is 100)
  - `Enabled bool` — already exists, keep as-is
- [ ] Add `Validate() error` to StackExchangeConfig (mirror HackerNewsConfig.Validate):
  - validate MaxItemsPerSite, MinimumScore, MinimumViews >= 0
  - validate Sites is non-empty and each is a valid SE site
  - validate PageSize in range [1, 100]
  - validate MaxPagesPerSite > 0
- [ ] Add SE default section in `DefaultConfig()`:
  - `Enabled: true`
  - `Sites: []string{"stackoverflow", "serverfault", "superuser"}`
  - `MaxItemsPerSite: 300`, `MinimumScore: 0`, `MinimumViews: 0`, `PageSize: 25`, `MaxPagesPerSite: 10`
- [ ] Add `c.Sources.StackExchange.Validate()` call in `config.Validate()` after HackerNews validation

### Task 3: Add ResearchStats fields + stats plumbing

- [ ] Add to `domain.ResearchStats`:
  - `StackExchangeRequests int \`json:"stackexchange_requests"\``
  - `StackExchangeCacheHits int \`json:"stackexchange_cache_hits"\``
- [ ] In `cli/collect.go`, add to `collectStatsDelta`:
  - `seRequests int` and `seCacheHits int`
- [ ] In `statsDelta()`, compute `seRequests` and `seCacheHits` from before/after stats
- [ ] In `reportCollectSummary()`, add SE stats formatting (mirror HN pattern lines 308-309)
- [ ] In `collector.Collect()` return value, track `Stats` with Request/CacheHit counters (match HN's Stats struct in types.go)

### Task 4: Implement Stack Exchange API client

- [ ] Create `internal/sources/stackexchange/client.go` with a context-aware HTTP client:
  - Constructor takes base URL, optional API key, injectable http.RoundTripper for tests
  - Single `getQuestions(ctx, site, fromUnix, toUnix, page, pageSize, filter) (*searchResponse, error)` method
  - Build URL with query params: site, fromdate, todate, page, pagesize, filter, order=desc, sort=creation, optional key
  - Implement bounded retry with exponential backoff + jitter for transport errors, 408, 429, 5xx (mirror HN client pattern)
  - Respect Retry-After header from SE responses (API may return it)
  - Parse JSON response envelope (searchResponse) BEFORE returning
  - **backoff handling**: when response has `backoff > 0`, set a per-method deadline; before next request to same method, wait until deadline passes (context-cancellable via time.After or time.NewTimer with context.Done)
  - On quota_remaining == 0: set typed error but return successfully parsed items (collector decides what to do)
  - On API error (error_id != null): return typed ErrAPIError
  - On malformed JSON: return ErrMalformedResponse

### Task 5: Parse and normalize Stack Exchange questions

- [ ] Create `internal/sources/stackexchange/parser.go`:
  - `parseQuestions(site string, questions []questionDTO) []domain.RawSignal` with content hash dedup
  - Stable source ID: `se:<site>:<question_id>` (e.g. `se:stackoverflow:12345`)
  - Source name: `"stackexchange"`
  - Extract title/body, clean HTML: decode entities (&amp; → &), strip `<code>` blocks, convert `<p>` to paragraphs, strip remaining tags
  - Content hash: SHA256 of (normalized title + normalized body + sorted tags), same pattern as HN
  - Map metadata: `MetaKeyStoryScore` → score, `MetaKeyAuthor` → owner.display_name, `MetaKeyViewCount` → view_count, tags comma-joined, `MetaKeySiteName` → site
  - Skip rules: empty/deleted body, score below configured threshold, views below threshold — return skipped count in stats
  - Add test fixtures with realistic SE HTML payloads

### Task 6: Implement Stack Exchange collector

- [ ] Create `internal/sources/stackexchange/collector.go` implementing `domain.SourceCollector`:
  - `Name() string` returns `"stackexchange"`
  - Constructor: `New(cfg *ConfigValues, client *client) *Collector`
  - `WithCache(store *storage.Storage) *Collector` — attach TTL cache (mirror HN pattern)
  - `Collect(ctx, req domain.CollectRequest) ([]domain.RawSignal, error)`:
    1. Validate config (disabled? → ErrDisabled)
    2. Derive since window from req.Since (Unix timestamps)
    3. Iterate configured sites **sequentially**
    4. For each site: paginate through `/search/advanced` sequentially (page 1 → N)
    5. Respect per-site MaxItemsPerSite, per-run MaxPagesPerSite, global MaxStackExchangeReqs
    6. Check context before each request, between pages, between sites, during backoff wait
    7. Apply backoff from server response before next request to same method
    8. Apply existing persisted-memory deduplication (source ID + content hash)
    9. Track Stats.Requests and Stats.CacheHits
    10. On quota exhaustion: collect what we have, return partial results + error
    11. On per-site failure: return successfully collected signals from other sites + error for failed site

### Task 7: Wire collector into CLI

- [ ] Add `"github.com/moontechs/signalforge/internal/sources/stackexchange"` import to `cli/collect.go`
- [ ] Add `case "stackexchange":` in `buildCollector()`:
  - Check `cfg.Sources.StackExchange.Enabled`
  - Build `stackexchange.ConfigValues` from cfg.Sources.StackExchange + cfg.Limits.MaxStackExchangeReqs
  - Use `strings.TrimSpace(os.Getenv("STACKEXCHANGE_API_KEY"))` for optional API key
  - Constructor: `stackexchange.New(cfg, client)`, then `collector.WithCache(store)`
- [ ] Update `--sources` help text to include `stackexchange`
- [ ] Ensure `signalforge collect --sources stackexchange --since 30d` works without API key
- [ ] Ensure all existing tests for `buildCollector("hackernews"...)` still pass
- [ ] Update `collectStatsDelta` zero-value initialization (line ~285) to include seRequests/seCacheHits

### Task 8: Tests and quality gates

- [ ] **client_test.go**: httptest-based tests for:
  - URL construction with site/dates/filter/key
  - Optional API key omitted vs included
  - Transient retry success + retry exhaustion
  - Retry-After header handling
  - API error payloads (error_id, error_name)
  - backoff delay + context cancellation during backoff
  - quota_remaining == 0 behaviour
  - Malformed JSON response
- [ ] **parser_test.go**: fixture-based tests for:
  - Realistic SE question HTML → clean plain text
  - Stable source ID format: `se:<site>:<id>`
  - Metadata mapping (score, author, tags, view count)
  - Content hash determinism
  - Skip rules (empty body, below score threshold)
- [ ] **collector_test.go**: integration-style tests for:
  - Multi-site sequential pagination
  - has_more pagination stop
  - Per-site/per-run limits
  - Context cancellation mid-collection
  - Duplicate suppression (same ID across pages)
  - Source name correctness
  - Stats tracking (Requests, CacheHits)
- [ ] Add fake transport / test server (mirror HN's fake_transport.go)
- [ ] Add CLI test in `collect_test.go`: `buildCollector("stackexchange", cfg, store)` succeeds

### Task 9: Verify end-to-end

- [ ] Run: `go test ./internal/sources/stackexchange/...`
- [ ] Run: `go test ./internal/config/...` (config validation tests pass)
- [ ] Run: `go test ./internal/cli/...` (buildCollector tests pass)
- [ ] Run: `go test ./internal/domain/...` (ResearchStats unchanged except new fields)
- [ ] Run: `go test ./...` (full suite)
- [ ] Run: `go vet ./...`
- [ ] Run: `golangci-lint run ./...`
- [ ] Run: `go build ./cmd/signalforge/`
- [ ] Verify: `./signalforge collect --help` shows stackexchange
- [ ] Verify: `./signalforge collect --sources stackexchange --since 30d --dry-run` prints planned endpoints

## Validation Commands

```bash
gofmt -w internal/sources/stackexchange/*.go
go test ./internal/sources/stackexchange/... -v -count=1
go test ./internal/config/... -v -count=1
go test ./internal/cli/... -v -count=1
go test ./internal/domain/... -v -count=1
go test ./... -count=1
go vet ./...
golangci-lint run ./...
go build ./cmd/signalforge/
```
