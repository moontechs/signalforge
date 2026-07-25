# Plan: Implement Reddit Collector for SignalForge

### Task 1: Complete Reddit source contracts and configuration validation

- [x] Complete the existing `internal/sources/reddit/` package stubs in `doc.go`, `types.go`, and `errors.go`; preserve the canonical source name `reddit`, discussion source type, `rd` signal ID prefix, request/cache-hit `Stats`, and typed errors for disabled configuration, invalid subreddit, authentication, rate limits, malformed responses, request cap exhaustion, and retry exhaustion.
- [x] Extend Reddit API DTOs so they correctly model OAuth token responses, listing pagination (`after`), posts, comments, nested comment replies, deleted/removed content, and the mixed listing child kinds returned by Reddit’s JSON API.
- [x] Define collector-local configuration and scope fields for enabled status, subreddits, post/comment limits, request cap, sort, time range, and since-window filtering; retain defaults of `new` and `all` for omitted sort/time range.
- [x] Add `RedditConfig.Validate()` in `internal/config/config.go`: when Reddit is enabled, require at least one non-empty subreddit, `MaxPostsPerRun > 0`, and `MaxCommentsPerPost >= 0`; return no Reddit-specific validation error while disabled so the default empty subreddit list remains valid.
- [x] Invoke Reddit validation from `Config.Validate()` with source-specific error context.
- [x] Add focused config tests for disabled defaults, valid opt-in configuration, empty/blank subreddit values, invalid post limits, invalid comment limits, and `LoadConfig` validation.

### Task 2: Implement the OAuth-aware Reddit API client

- [x] Add `internal/sources/reddit/client.go` with an injectable HTTP transport and a default `http.Client` configured with a timeout.
- [x] Create `internal/sources/reddit/client_test.go` with tests using fake transport: verify auth flow, request method/URL/headers, listing fetching, and response handling without network.
- [x] Implement client-credentials authentication against Reddit’s access-token endpoint using HTTP Basic authentication with `REDDIT_CLIENT_ID` and `REDDIT_CLIENT_SECRET`, form-encoded `grant_type=client_credentials`, and a descriptive User-Agent.
- [x] Cache the access token in memory with its expiry, refresh it before expiry, and ensure secrets are never added to cache keys, logs, errors, or persisted JSON.
- [x] Implement authenticated GET helpers for subreddit listings (`/r/{subreddit}/{sort}.json`) and post comments (`/r/{subreddit}/comments/{postID}.json`), including encoded query parameters for listing limit, `after`, and time range.
- [x] Enforce the configured per-run request cap before outbound token or API requests, count only actual network requests, and expose request/cache-hit counters through `Stats()`.
- [x] Apply bounded response reads (10 MiB maximum), context-aware retries with exponential backoff for transient transport failures, HTTP 408/429/5xx responses, and typed errors for auth failure, rate limiting, malformed JSON, cap exhaustion, and exhausted retries.
- [x] Integrate the shared `internal/cache` package through `WithCache`: use cache entries with a short listing TTL and a longer post/comments TTL, use stable request-path/query cache keys, and treat cache-write failures as non-fatal.
- [x] Add `fake_transport.go` following the Hacker News/Stack Exchange test seam so client tests can assert request method, URL, headers, authorization refresh behavior, and response handling without network access.

### Task 3: Parse Reddit content into normalized signals

- [x] Add `internal/sources/reddit/parser.go` to transform eligible Reddit posts into `domain.RawSignal` values with stable IDs, source IDs, canonical Reddit permalink URLs, subreddit community, title/selftext, score, comment count, creation time, collection time, and metadata such as author, subreddit, and score/count values.
- [x] Create `testdata/reddit/` directory with JSON fixtures covering normal posts, permalink construction, nested replies, deleted content, comment limits/depth limits, timestamp conversion
- [x] Convert comment trees to `domain.Comment` values with breadth-first traversal, deterministic order, UTC timestamps, a maximum nesting depth of 50, and the configured maximum number of comments; skip deleted, removed, malformed, and non-comment listing children safely.
- [x] Build each signal’s content hash from title, body, and flattened comment bodies using `storage.ContentHash`, matching the existing source deduplication convention.
- [x] Implement post eligibility checks for valid post kind, usable content, and the requested `Since` boundary; do not add unsupported score filtering because Reddit configuration does not define a minimum-score setting.
- [x] Add parser tests and Reddit JSON fixtures under `testdata/reddit/` covering normal posts, permalink construction, nested replies, deleted content, comment limits/depth limits, timestamp conversion, content hashing, and since-window exclusion.

### Task 4: Implement collector orchestration and source tests

- [x] Add `internal/sources/reddit/collector.go` implementing `domain.SourceCollector`, with `New`, `Name`, `Collect`, `WithTransport`, `WithNow`, `WithCache`, and `Stats` methods consistent with the existing collectors.
- [x] Make `New` reject disabled collectors and invalid configured subreddit names before creating network-capable clients.
- [x] In `Collect`, derive the effective scope from configuration and `domain.CollectRequest`, including the command-level `MaxItems` and `MaxCommentsPerItem` overrides when supplied.
- [x] Fetch configured subreddit listings, paginate only as needed to satisfy the effective post limit, deduplicate post IDs across subreddits/pages, filter ineligible posts, and stop cleanly when the request cap or item limit is reached.
- [x] Fetch comments for eligible posts with a bounded worker pool of five concurrent requests, preserve usable partial results, sort final signals newest-first, and return joined contextual errors only after collecting all possible source results.
- [x] Store per-run request/cache-hit deltas rather than cumulative client totals so CLI statistics remain accurate over repeated collector invocations.
- [x] Add collector tests for disabled/invalid configuration, multi-subreddit post deduplication, pagination/limits, since filtering, bounded comment concurrency, partial failures, request-cap behavior, caching statistics, result ordering, and `domain.SourceCollector` interface compliance.

### Task 5: Wire Reddit into collection orchestration and reporting

- [x] Update `internal/cli/collect.go` to import the Reddit package, include `reddit` in deterministic source ordering, and update command help text to state that Reddit is available only after configuration opt-in.
- [x] Add the `reddit` case to `buildCollector`: reject disabled Reddit config, require trimmed `REDDIT_CLIENT_ID` and `REDDIT_CLIENT_SECRET`, map `RedditConfig` plus `Limits.MaxRedditRequests` into `reddit.ConfigValues`, create the collector, and attach `cache.NewCache(store, "reddit")`.
- [x] Add Reddit dry-run targets (`subreddit: <name>`) and a request estimate based on configured/effective post limits, listings, and optional comment fetches; keep dry-run free of token checks and HTTP calls.
- [x] Extend `trackCollectorStats`, `collectStatsDelta`, `statsDelta`, and `reportCollectSummary` to persist and report Reddit request/cache-hit deltas using a `Reddit requests: N (cache hits: M)` summary line.
- [x] Update `internal/cli/collect_test.go` with Reddit builder tests for disabled config, missing credentials, and successful opt-in; add source resolution/order tests, dry-run target/estimate tests, stats-delta assertions, and summary-output assertions.

### Task 6: Persist Reddit statistics and extend doctor checks

- [x] Add `AddRedditRequests(int)` and `AddRedditCacheHits(int)` to `internal/memory/memory.go`, following the existing HN/Stack Exchange locking and non-positive no-op behavior while updating the already-present `domain.ResearchStats` Reddit fields.
- [x] Add memory tests for basic increments, zero/negative no-ops, accumulation, and concurrent calls for both Reddit counter methods.
- [x] Extend `checkEnvVars` in `internal/cli/doctor.go` to report `REDDIT_CLIENT_ID` and `REDDIT_CLIENT_SECRET` as required failures only when Reddit is enabled; report them as informationally not required while disabled.
- [x] Add doctor tests covering enabled/missing credentials, enabled/present credentials, and disabled configuration.
- [x] Update README.md with explicit opt-in configuration example, required `REDDIT_CLIENT_ID`/`REDDIT_CLIENT_SECRET` environment variables, and a `signalforge collect --sources reddit --since 30d` invocation; preserve that Reddit remains disabled by default.
- [x] Add `reddit` to the `sourceOrder` array in `internal/cli/collect.go` and update the collect command's help text to mention Reddit as an available source.

## Validation Commands

- [x] `gofmt -w internal/sources/reddit internal/config/config.go internal/config/config_test.go internal/cli/collect.go internal/cli/collect_test.go internal/cli/doctor.go internal/memory/memory.go internal/memory/memory_test.go`
- [x] `go test ./internal/sources/reddit/...`
- [x] `go test ./internal/config/... ./internal/memory/... ./internal/cli/...`
- [x] `go test ./...`
- [x] `go vet ./...`
- [x] `golangci-lint run ./...`
- [x] `go build ./cmd/signalforge/`
