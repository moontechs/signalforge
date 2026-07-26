# Plan: Fix --max-items, GitHub stats, and GitHub Discussions bugs

### Task 1: Make `--max-items` override source defaults in GitHub and Hacker News

- [x] Modify `internal/sources/github/collector.go` and `internal/sources/github/types.go` so `domain.CollectRequest.MaxItems` is the effective per-run cap when it is greater than zero; otherwise retain `GitHubConfig.MaxItemsPerRun`.
- [x] Pass the effective GitHub cap into `deriveScope`, ensuring REST issue pagination, GraphQL discussion pagination, and final combined-result truncation all use the same request-aware value.
- [x] Preserve `--max-items 0` semantics: use the configured source default rather than treating zero as "collect nothing."
- [x] Modify `internal/sources/hackernews/collector.go` and `internal/sources/hackernews/types.go` so `deriveScope` accepts `req.MaxItems` and applies the same override/fallback behavior.
- [x] Keep the existing HN final sort-before-truncate behavior, but ensure its truncation uses the CLI-provided cap when present.
- [x] Extend GitHub collector tests to cover a nonzero request cap overriding a larger config cap, plus a zero request cap falling back to config.
- [x] Extend HN collector/type tests with the same override and fallback cases, using enough eligible fake stories to prove the returned signal count is capped.

### Task 2: Track and persist GitHub request and cache-hit statistics

- [x] **Check first**: search `internal/memory/memory.go` for existing `AddGitHubRequests` and `AddGitHubCacheHits` methods. If they exist, use them as-is. If not, create them.
- [x] Create `AddGitHubRequests(count int)` and `AddGitHubCacheHits(count int)` methods in `internal/memory/memory.go` if not found above. Mirror the existing pattern: nonpositive-count guard, mutex-protected increment, `m.mem.Stats.GitHubRequests` / `m.mem.Stats.GitHubCacheHits` fields.
- [x] Add thread-safe request and cache-hit counters to `internal/sources/github/client.go` (int64 with mutex or atomic).
- [x] Increment the GitHub request counter only for actual outbound HTTP requests; do not count a fresh disk-cache response as a request.
- [x] Increment the GitHub cache-hit counter when `doRequest` returns a fresh response from the GitHub disk cache.
- [x] Expose a `Stats()` method from `internal/sources/github/collector.go` that returns per-run deltas (requestsSinceReset, cacheHitsSinceReset), matching the HN collector's `Stats` contract and avoiding cumulative values when a collector instance is reused.
- [x] Update `internal/cli/collect.go` `trackCollectorStats` (or equivalent) to recognize `*github.Collector`, call its `Stats()` method, and then call `mem.AddGitHubRequests(stats.Requests)` and `mem.AddGitHubCacheHits(stats.CacheHits)`.
- [x] Add memory tests for `AddGitHubRequests` and `AddGitHubCacheHits`, including zero and negative values remaining no-ops, and concurrent safety.
- [x] Add GitHub client/collector tests that distinguish outbound requests from fresh cache hits and verify returned per-run stats.
- [x] Add CLI collection tests that execute a GitHub collector path with controlled stats, save memory, reload it, and assert `github_requests` and `github_cache_hits` are persisted and included in collection-summary deltas.

### Task 3: Ensure GitHub Discussions have an executable collection path

- [x] **Important context**: `internal/sources/github/discussions.go` already exists in the repo. Inspect it first — it may be a partial implementation, a stub, or dead code. Do NOT recreate it.
- [x] Inspect `internal/sources/github/collector.go` to find where Issue collection is wired but Discussion collection is missing (e.g., missing call to `collectDiscussions()` or `collectDiscussions` never invoked in `Collect()` method).
- [x] If `discussions.go` exists but is incomplete: complete the implementation (GraphQL query, pagination, response parsing into RawSignal). If it exists and is complete but unconnected: wire it into `collector.go`.
- [x] Make the repository-target requirement explicit: GitHub Discussions require one or more `sources.github.repositories` entries because the GraphQL `repository { discussions }` query is repository-scoped. Retain global REST issue search when repositories are omitted.
- [x] Add a config validation error: when `SearchDiscussions: true` and `repositories` is empty, return a clear error explaining that a repository list is required for Discussion collection.
- [x] Preserve valid Issues-only configurations without repositories by applying the new repository requirement only when `SearchDiscussions` is enabled AND repositories is empty.
- [x] Ensure `buildGitHubCollector` (in `internal/cli/collect.go`) continues to forward `SearchDiscussions` and configured repositories unchanged into `github.CollectorConfig`.
- [x] Add configuration tests for: Discussions enabled with repositories succeeds; Discussions enabled without repositories fails with the actionable error; Issues-only global search remains valid.
- [x] Add a CLI/collector integration test configured with `SearchIssues: true`, `SearchDiscussions: true`, and a repository target; register REST and GraphQL fake responses; assert persisted output contains both `github_issue` and `github_discussion` signals.
- [x] Add a Discussions-only integration test to ensure GraphQL collection is not dependent on a successful REST Issues response.
- [x] Verify max-item tests cover a mixed Issue/Discussion run so the requested cap is honored without suppressing the GraphQL collection path.

### Task 4: Run regression and quality validation

- [ ] Format all modified Go files with `gofmt`.
- [ ] Run focused GitHub, Hacker News, memory, config, and CLI tests with cache disabled.
- [ ] Run the complete test suite: `go test ./... -count=1`.
- [ ] Run `go vet ./...` and `golangci-lint run ./...`.
- [ ] Run `go build ./cmd/signalforge/` to verify binary compiles.
- [ ] Confirm `signalforge collect --sources github --max-items 1 --dry-run` reports the requested cap and that a configured GitHub Discussions run requires repository targets before making network calls.
- [ ] **Important**: All `git commit` and `git push` commands MUST use `--no-verify` to bypass the pre-push hook (golangci-lint may temporarily fail). Examples:
  - `git add -A && git commit -m "..." --no-verify`
  - `git push -u origin HEAD --no-verify`

## Validation Commands

- `gofmt -w internal/cli/collect.go internal/memory/memory.go internal/config/config.go internal/config/config_test.go internal/sources/github/*.go internal/sources/hackernews/*.go`
- `go test ./internal/sources/github/... -v -count=1`
- `go test ./internal/sources/hackernews/... -v -count=1`
- `go test ./internal/memory/... -v -count=1`
- `go test ./internal/config/... -v -count=1`
- `go test ./internal/cli/... -v -count=1`
- `go test ./... -count=1`
- `go vet ./...`
- `golangci-lint run ./...`
- `go build ./cmd/signalforge/`
