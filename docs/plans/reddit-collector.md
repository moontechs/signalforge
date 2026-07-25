# Plan: Reddit Collector Implementation

Implement a Reddit collector for SignalForge. The config layer already exists (`RedditConfig`, `LimitsConfig.MaxRedditRequests`, cache dirs). This plan adds the source package and wires it in.

### Task 1: Create reddit package skeleton (doc.go, errors.go, types.go)
Create `/home/app/signalforge/internal/sources/reddit/` with:
- [x] `doc.go` — package documentation following HN/SE pattern
- [x] `errors.go` — typed errors: ErrDisabled, ErrInvalidSubreddit, ErrRequestCap, ErrMalformedResponse, ErrAuthFailed, ErrRateLimited, ErrRetriesExhausted
- [x] `types.go` — source constants (SourceName="reddit", SourceType="discussion", SignalIDPrefix="rd"), ConfigValues struct (Enabled, Subreddits []string, MaxPostsPerRun int, MaxCommentsPerPost int, MaxRequests int, Sort string, TimeRange string), collectionScope, Stats, API response types (tokenResponse, listingResponse, postResponse, commentResponse), metadata key constants

### Task 2: Create fake_transport.go
Create `/home/app/signalforge/internal/sources/reddit/fake_transport.go` — test transport for injecting responses without network access, same pattern as stackexchange/fake_transport.go.

### Task 3: Create client.go
Create `/home/app/signalforge/internal/sources/reddit/client.go`:
- OAuth2 Client Credentials flow: POST to `https://www.reddit.com/api/v1/access_token` with Basic auth `client_id:client_secret`
- Token caching with expiry, auto-refresh on expiry
- GET endpoints: `/r/{subreddit}/{sort}.json` (listing with `?t={time_range}&limit={limit}&after={after}`) and `/r/{subreddit}/comments/{article_id}.json` (returns [post, commentsListing])
- Retry with exponential backoff, request cap, max body size 10MB
- On-disk caching via WithCache (5min TTL for listings, 24h for items)
- Transport interface for testability, Stats() method
- Default sort: "new", default time range: "all"

### Task 4: Create parser.go + collector.go
Create `/home/app/signalforge/internal/sources/reddit/parser.go`:
- `parsePost(post, comments, collectedAt) domain.RawSignal`
- `parseComments(commentData, maxComments) ([]domain.Comment, error)` — BFS flattening
- `eligiblePost(post, since, minimumScore) bool`

Create `/home/app/signalforge/internal/sources/reddit/collector.go`:
- SourceCollector implementation with Name(), Collect(), WithTransport(), WithNow(), WithCache(), Stats()
- Pipeline: for each subreddit → fetch posts → dedup by ID → fetch comments → filter → sort → cap → return
- Bounded worker pool (5 workers) for comment fetching

### Task 5: Write client_test.go + collector_test.go
Create `/home/app/signalforge/internal/sources/reddit/client_test.go` — test client auth flow and listing fetching with fake transport.
Create `/home/app/signalforge/internal/sources/reddit/collector_test.go` — test collector pipeline with fake transport.

### Task 6: Wire into CLI (collect.go, collect_test.go)
Modify `/home/app/signalforge/internal/cli/collect.go`:
- Add reddit import
- Add "reddit" to sourceOrder
- Add `case "reddit"` in buildCollector: check enabled, build ConfigValues, check env vars REDDIT_CLIENT_ID + REDDIT_CLIENT_SECRET, attach cache
- Add buildRedditTargets, estimateRedditRequests
- Add Reddit case in buildDryRunPlans
- Add `redditRequests` and `redditCacheHits` fields to collectStatsDelta
- Update statsDelta and trackCollectorStats to handle reddit
- Update reportCollectSummary to print reddit stats

Modify `/home/app/signalforge/internal/cli/collect_test.go`:
- Add TestBuildCollector_Reddit (disabled + enabled)
- Add TestStatsDelta_Reddit
- Add TestReportCollectSummary_Reddit

### Task 7: Wire into memory, config, doctor
Modify `/home/app/signalforge/internal/memory/memory.go`: add AddRedditRequests(count int) and AddRedditCacheHits(count int) methods (same pattern as HN methods).

Modify `/home/app/signalforge/internal/memory/memory_test.go`: add tests for both new methods.

Modify `/home/app/signalforge/internal/config/config.go`: add Validate() method for RedditConfig that checks: if Enabled, require at least 1 subreddit, MaxPostsPerRun > 0, MaxCommentsPerPost >= 0. Call it from Config.Validate().

Modify `/home/app/signalforge/internal/cli/doctor.go`: in checkEnvVars, add REDDIT_CLIENT_ID and REDDIT_CLIENT_SECRET checks (required if Reddit enabled, optional otherwise).

### Task 8: Final verification
Run: go vet ./...
Run: golangci-lint run ./...
Run: go test ./internal/sources/reddit/...
Run: go test ./internal/cli/...
Run: go test ./internal/memory/...
Run: go test ./internal/config/...
Run: go build ./cmd/signalforge/
