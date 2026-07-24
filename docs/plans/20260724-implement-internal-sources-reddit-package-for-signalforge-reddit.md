# Plan: Implement Reddit OAuth Collector and Collect CLI Integration

### Task 1: Extend Reddit configuration and source contracts

- [x] Modify `internal/config/config.go` to retain the existing opt-in `RedditConfig` fields and add JSON-backed `Sort` and `Time` fields for Reddit listing selection.
- [x] Define supported sort values (`hot`, `new`, `top`, `rising`) and supported time values (`hour`, `day`, `week`, `month`, `year`, `all`); validate normalized, non-empty values when Reddit is enabled.
- [x] Add `RedditConfig.Validate()` to require, when enabled: at least one non-empty subreddit, positive `MaxPostsPerRun`, and non-negative `MaxCommentsPerPost`; invoke it from `Config.Validate()`.
- [x] Preserve Reddit's existing defaults of disabled, no subreddits, 200 maximum posts, and 20 maximum comments; set explicit safe listing defaults (for example `new` and `week`) consistent with the chosen validation contract.
- [x] Add `internal/sources/reddit/doc.go`, `types.go`, and `errors.go`, mirroring the Hacker News package layout.
- [x] In `types.go`, define source constants, public collector `ConfigValues`, derived collection scope, request/cache `Stats`, OAuth token response DTO, Reddit listing/post/comment DTOs, metadata keys, and supported sort/time constants.
- [x] Define typed sentinel errors for disabled collection, missing client credentials, invalid subreddit/sort/time, malformed API response, request-cap exhaustion, token/authentication failure, and retries exhausted.
- [x] Add configuration and source-type tests covering defaults, disabled validation behavior, invalid values, valid sort/time combinations, and scope derivation.

### Task 2: Implement the Reddit OAuth HTTP client

- [x] Create `internal/sources/reddit/client.go` with the same injectable `transport` abstraction, `httpTransport`, bounded retries, context-aware request construction, response-size limit, request counters, cache-hit counters, and test hooks used by `internal/sources/hackernews`.
- [x] Read `REDDIT_CLIENT_ID` and `REDDIT_CLIENT_SECRET` only at collector construction or CLI wiring time; never serialize, log, expose through stats, or include either value in a cache key.
- [x] Implement client-credentials token acquisition with `POST https://www.reddit.com/api/v1/access_token`, HTTP Basic authentication, `Content-Type: application/x-www-form-urlencoded`, and `grant_type=client_credentials`.
- [x] Use the acquired bearer token only for API calls to `https://oauth.reddit.com`; set a stable, descriptive User-Agent on token and API requests.
- [x] Keep the access token in client memory only, reuse it until shortly before `expires_in`, and safely refresh it under concurrent collection without writing tokens to `cache/reddit` or any JSON output.
- [x] Count token acquisition and Reddit API network calls against `MaxRedditRequests`; ensure concurrent callers cannot exceed the configured cap.
- [x] Implement authenticated GET helpers for subreddit listings and post comment trees, retrying transient transport, 429, and 5xx failures with exponential backoff while returning non-retryable 4xx/auth failures promptly.
- [x] Attach the existing shared cache through `cache.NewCache(store, "reddit")`; cache only public response bodies under deterministic keys derived from endpoint path and non-secret query parameters, with no Authorization header or token material represented in cache paths.
- [x] Select and document bounded TTLs for listing and comment-tree responses, preserve cache-hit/request statistics, and treat cache read/write failures as non-fatal misses.
- [x] Create `internal/sources/reddit/fake_transport.go` by adapting the Hacker News concurrency-safe fake transport, including sequential canned responses, request recording, headers, method, and request-body inspection helpers.
- [x] Add client tests for OAuth request method/form/basic auth/user-agent, missing credentials, token reuse/expiry refresh, bearer headers on OAuth API requests, request-cap enforcement, retries, cancellation, malformed JSON, response-size limits, non-success responses, and cache behavior without real network calls.

### Task 3: Parse Reddit listings and comment trees into raw signals

- [ ] Create `internal/sources/reddit/parser.go` to convert Reddit listing children (`kind: "t3"`) into `domain.RawSignal` values with stable IDs such as `reddit:<post-id>`.
- [ ] Map each post’s permalink into an absolute `https://www.reddit.com/...` URL, title, selftext body, subreddit community, score, comment count, author/subreddit metadata, UTC creation time, collection time, and deterministic content hash using `storage.ContentHash`.
- [ ] Use a source type consistent with Hacker News (`discussion`), and distinguish the configured listing sort in metadata or category only if it is stable and useful to downstream consumers.
- [ ] Parse `/comments/{post-id}.json` listing responses, recursively flatten comment nodes (`kind: "t1"`) in deterministic order, skip deleted/removed/empty bodies and `more` placeholders, and stop at `MaxCommentsPerPost`.
- [ ] Map retained comments to `domain.Comment` with Reddit comment ID, body, score, and UTC creation time; include safe parent/depth metadata conventions only where supported by the existing `RawSignal.Metadata` shape.
- [ ] Add parser tests using `testdata/reddit/` fixtures for post mapping, permalink construction, Unicode/escaped content, deleted/removed nodes, nested replies, `more` children, comment caps, timestamp conversion, metadata, and content-hash stability. Create `testdata/reddit/` directory with sample JSON fixtures.

### Task 4: Implement collector orchestration

- [ ] Create `internal/sources/reddit/collector.go` following the Hacker News collector structure: validated construction, `Name`, `WithTransport`, `WithNow`, `WithCache`, `Stats`, and `domain.SourceCollector` interface assertion.
- [ ] Validate subreddit names before building URLs: trim whitespace, reject empty values and separators/path traversal characters, normalize optional `r/` prefixes, and de-duplicate configured communities while preserving configured order.
- [ ] Derive a collection scope from `ConfigValues` and `domain.CollectRequest`, including configured subreddits, sort, time, limits, and `Since`; honor a positive CLI `MaxItems` as a stricter per-run cap if it is lower than the configured post cap.
- [ ] Request each configured listing through `/r/{subreddit}/{sort}.json` with a bounded `limit` and `t=<time>` only for applicable sorts; apply `Since` filtering after parsing timestamps.
- [ ] Fetch comment trees only for retained posts, using a bounded worker pool and the shared request-limited client; preserve deterministic final signal ordering by `CreatedAt` descending.
- [ ] Return successful signals together with `errors.Join` partial failures for individual subreddit/listing/post-comment failures, while returning context cancellation immediately after outstanding work is reconciled.
- [ ] Ensure no production path panics and no collector behavior requires credentials when Reddit is not selected or is disabled.
- [ ] Add collector tests for disabled and invalid configuration, source naming, subreddit normalization/deduplication, sort/time request construction, `Since` and max-post filtering, max-comment enforcement, partial failures, request caps, context cancellation, bounded concurrency, cached repeat runs, and stable ordering.

### Task 5: Wire Reddit into collection CLI and memory

- [ ] Add `AddRedditRequests` and `AddRedditCacheHits` methods to `internal/memory/memory.go` (RedditRequests/RedditCacheHits already exist in domain.ResearchStats).
- [ ] Add focused memory tests for Reddit statistics, including zero/negative inputs, accumulation, concurrency, and persistence compatibility.
- [ ] Modify `internal/cli/collect.go` to import the Reddit source package, include `reddit` in deterministic source order, and add a `buildCollector` case.
- [ ] In the Reddit factory case, reject disabled Reddit configuration and missing `REDDIT_CLIENT_ID` or `REDDIT_CLIENT_SECRET` with clear errors; map existing config, new sort/time fields, and `Limits.MaxRedditRequests` into `reddit.ConfigValues`.
- [ ] Attach `cache.NewCache(store, "reddit")` to the Reddit collector, relying on the existing `cache/reddit` directory structure.
- [ ] Extend dry-run target construction and request estimation for Reddit so selected subreddit, sort, time, configured caps, and one OAuth token request are represented without making calls or requiring credentials.
- [ ] Extend collector-stat tracking, stats deltas, and collection summary output to report Reddit requests and cache hits independently in multi-source runs.
- [ ] Update command help text to list Reddit as a supported opt-in source and show a credential/configuration example without displaying actual secret values.
- [ ] Add CLI tests for `reddit` source resolution, deterministic ordering with other sources, disabled and missing-credential errors, collector construction with test credentials, dry-run output, request estimation, and Reddit summary/stat isolation.

### Task 6: Document and verify the implementation

- [ ] Update `README.md` and/or `CONTEXT.md` with Reddit’s opt-in status, required `REDDIT_CLIENT_ID` and `REDDIT_CLIENT_SECRET` environment variables, configurable subreddits/sort/time, supported values, request cap, cache location, and example `signalforge collect --sources reddit --since 30d` invocation.
- [ ] Keep ADR-005’s disabled-by-default invariant intact; do not add unauthenticated scraping, database storage, embeddings, new Go dependencies, token/cost accounting, or secret persistence.
- [ ] Run `gofmt` on all modified Go files.
- [ ] Run focused package tests before full validation.
- [ ] Run the repository’s required linter sequence and correct all issues without adding unexplained `//nolint` directives.
- [ ] Confirm `go.sum` has no dependency changes unless explicitly required and justified.

## Validation Commands

gofmt -w internal/config/*.go internal/domain/*.go internal/memory/*.go internal/cli/*.go internal/sources/reddit/*.go

go test ./internal/config/... ./internal/domain/... ./internal/memory/...

go test ./internal/sources/reddit/...

go test ./internal/cli/...

go test ./...

go test -race ./internal/sources/reddit/... ./internal/memory/... ./internal/cli/...

go vet ./...

golangci-lint run --fix ./...

golangci-lint run ./...
