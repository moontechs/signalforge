# M2-T5: GitHub Issues + Discussions Collector

## Overview

Implement the GitHub source collector under internal/sources/github/ to fetch GitHub issues via REST and discussions via GraphQL, handle pagination and rate limits safely, cache responses on disk, transform upstream payloads into domain.RawSignal, and provide fake HTTP-based tests that run without API keys.

## Context

- Files involved: internal/domain/types.go, internal/config/config.go, internal/storage/storage.go, internal/memory/memory.go, internal/sources/github/*.go, internal/sources/github/*_test.go
- Related patterns: source packages live under internal/sources/; HTTP clients must use timeout, retry, and context cancellation; cache data belongs under cache/github; persistence should reuse existing atomic JSON helpers in internal/storage
- Dependencies: standard library only
- Existing constraints to preserve:
    - no database
    - cache keys must not include secrets
    - tokens must never appear in logs or serialized cache data
    - tests must pass without external credentials

## Key Design Decisions (from critique)

### 1. SourceCollector interface: use `context.Context`, not `any`
The current `SourceCollector` interface has `Collect(ctx any, ...)`. This MUST be changed to `context.Context` — all HTTP clients require it for cancellation, timeouts, and graceful shutdown. This is a one-line change in `internal/domain/types.go`.

### 2. Repositories: empty list = search API, populated list = repo API
- If `Config.GitHub.Repositories` is non-empty → use `GET /repos/{owner}/{repo}/issues` per repo (precise, lower rate-limit cost)
- If empty → use `GET /search/issues?q=...` across all public repos (broader, higher rate-limit cost, pagination via `&page=`)
- Both paths return the same paginated issue list; the collector picks the strategy based on config

### 3. Issue sort order: `sort=updated&direction=asc`
Issues MUST be collected in ascending order of last update time (`sort=updated&direction=asc`). This ensures that repeated runs can pick up only new/updated issues since the last cursor without re-processing old items.

### 4. `since` parameter support
The collector must accept and pass `since` (ISO date) to the GitHub API as `&since=2026-01-01T00:00:00Z`. This integrates with the `CollectRequest.Since` field.

### 5. Rate-limit tracking: REST and GraphQL are separate
- **REST API**: 5000 requests/hour. Track via `x-ratelimit-remaining` header.
- **GraphQL API**: 5000 points/hour. Track via `x-ratelimit-remaining` in response. One discussion query may cost multiple points (typically 1-5 for a simple query, more with nested fields).
- Implement two separate counters; the collector checks both before making requests.
- **Secondary rate limit**: GitHub returns HTTP 403 with `Retry-After` header when abuse detection triggers. Must be handled identically to primary rate limit (backoff + retry).

### 6. ETag / If-None-Match conditional caching
Before the TTL-based disk cache, add a lightweight conditional request layer:
- Store the `ETag` and `Last-Modified` response headers per endpoint
- Send `If-None-Match` or `If-Modified-Since` on subsequent requests
- On HTTP 304, serve from cache (extend TTL instead of replacing)
- This is the standard GitHub API caching pattern and significantly reduces rate-limit consumption

### 7. GraphQL query for Discussions
The GitHub Discussions API is only accessible via GraphQL. The query must follow this structure:

```graphql
query($owner: String!, $repo: String!, $cursor: String, $first: Int = 50) {
  repository(owner: $owner, name: $repo) {
    discussions(first: $first, after: $cursor, orderBy: {field: UPDATED_AT, direction: ASC}) {
      pageInfo { hasNextPage endCursor }
      nodes {
        id
        number
        title
        body
        url
        createdAt
        updatedAt
        category { name slug }
        labels(first: 10) { nodes { name } }
        comments(first: 20) { totalCount nodes { id body createdAt } }
        upvoteCount
      }
    }
  }
}
```

### 8. Comment pagination
Issues with many comments require separate pagination of comments. The collector should:
- Fetch comments via `GET /repos/{owner}/{repo}/issues/{number}/comments?per_page=100&page=N`
- Apply `Config.GitHub.MaxCommentsPerItem` cap
- Preserve deterministic ordering (by `created_at` asc)

### 9. Fake client pattern: interface-based
Client implementations follow this pattern:
```go
// Transport interface for testability
type transport interface {
    Do(req *http.Request) (*http.Response, error)
}

type realTransport struct {
    client *http.Client
}

type fakeTransport struct {
    responses map[string]fakeResponse
}
```
The collector accepts a `transport` in its constructor. Tests inject `fakeTransport`; production uses `realTransport`. This avoids httptest.NewServer overhead in unit tests.

## Development Approach

- Testing approach: TDD for parser and client boundary behavior, then regular implementation for orchestration glue
- Complete each task fully before moving to the next
- Keep the package small and concrete: one collector, one transport/client layer, one parser layer, minimal shared helpers
- Reuse internal/storage for cache persistence rather than introducing a second storage abstraction
- Keep REST and GraphQL request/response models private to the package
- **CRITICAL: every task MUST include new/updated tests**
- **CRITICAL: all tests must pass before starting next task**

## Implementation Steps

### Task 1: Define GitHub package structure and collector contract

**Files:**
- Create: internal/sources/github/collector.go
- Create: internal/sources/github/types.go
- Create: internal/sources/github/errors.go
- Modify: internal/domain/types.go — change `Collect(ctx any, ...)` to `Collect(ctx context.Context, ...)`
- Add tests: internal/sources/github/collector_test.go

- [x] change `SourceCollector.Collect` first arg from `ctx any` to `ctx context.Context` in internal/domain/types.go
- [x] define the package-level collector type, constructor inputs, and private collaborators for REST, GraphQL, cache, and time
- [x] map configuration and request inputs into a concrete GitHub collection scope: repositories, labels, language filters, item limits, comment limits, and since-window behavior
- [x] define typed package errors for rate limits (primary + secondary), authentication failures, retry exhaustion, and malformed upstream responses
- [x] define the `transport` interface for testability (see Fake client pattern above)
- [x] write tests covering collector construction defaults and collection-scope derivation from config/request inputs
- [x] run go test ./internal/sources/github/... and fix failures before task 2

### Task 2: Implement REST and GraphQL client layers with retries, pagination, rate-limit handling, and conditional requests

**Files:**
- Create: internal/sources/github/client.go
- Create: internal/sources/github/issues.go
- Create: internal/sources/github/discussions.go
- Add tests: internal/sources/github/client_test.go

- [ ] implement an HTTP-backed client with explicit timeout, context-aware requests, GitHub auth header injection, and safe user-agent/header defaults
- [ ] implement the `realTransport` wrapper and `fakeTransport` for tests
- [ ] add REST issue search/listing support with pagination (`&page=N` + `Link` header parsing)
- [ ] add REST issue collection per-repo (`GET /repos/{owner}/{repo}/issues`) with `sort=updated&direction=asc` and `&since=` support
- [ ] add REST issue search (`GET /search/issues`) with query construction for repositories, labels, languages, and time range
- [ ] add GraphQL discussion search/listing support with end-cursor pagination and the query from design decisions
- [ ] implement retry with exponential backoff for transient failures (5xx, timeouts, connection errors)
- [ ] implement rate-limit handling: parse `x-ratelimit-remaining` headers, backoff on limit reached, handle `Retry-After` for both primary and secondary rate limits
- [ ] implement separate rate-limit counters for REST and GraphQL
- [ ] implement ETag/If-None-Match conditional request support: store ETag per endpoint, send on subsequent requests, handle 304 responses
- [ ] enforce request counting so the client can respect Limits.MaxGitHubRequests
- [ ] write fake transport tests for REST pagination, GraphQL pagination, transient retry success, hard failure after retries, primary rate-limit, secondary rate-limit, 304 conditional response, and request-limit cutoff
- [ ] run go test ./internal/sources/github/... and fix failures before task 3

### Task 3: Add on-disk GitHub response caching

**Files:**
- Create: internal/sources/github/cache.go
- Add tests: internal/sources/github/cache_test.go

- [ ] implement a small cache helper backed by internal/storage.Storage and the existing cache/github directory
- [ ] design stable cache keys from request shape only (method + endpoint + normalized params), excluding tokens and other secrets
- [ ] store response body + ETag + Last-Modified + collected-at timestamp
- [ ] cache hit logic: if within TTL → return cached; if expired but has ETag/LM → conditional request; if expired and no ETag → re-fetch
- [ ] integrate cache lookup/write/conditional paths for both REST and GraphQL requests in the client layer
- [ ] write tests for key generation, secret exclusion, cache hit/miss behavior, stale/conditional handling, and storage round-trips
- [ ] run go test ./internal/sources/github/... and fix failures before task 4

### Task 4: Parse GitHub issues and discussions into RawSignal

**Files:**
- Create: internal/sources/github/parser.go
- Add tests: internal/sources/github/parser_test.go

- [ ] define private upstream response structs for issue, discussion, label, repository, reaction, and comment payloads
- [ ] implement issue-to-domain.RawSignal mapping, including source metadata, repository/community fields, labels/tags, counts, timestamps, URLs, and content hash generation
- [ ] implement discussion-to-domain.RawSignal mapping with equivalent normalization for titles, bodies, comments, categories, and upvote/reaction counters
- [ ] normalize source IDs: `github_issue:{issue_id}` for issues, `github_discussion:{discussion_id}` for discussions
- [ ] normalize source types: `github_issue`, `github_discussion`
- [ ] truncate or cap comments according to `Config.MaxCommentsPerItem` while preserving deterministic ordering (created_at asc)
- [ ] write focused parser tests using fixture-like inline payloads for empty fields, missing optional values, comment caps, and content-hash stability
- [ ] run go test ./internal/sources/github/... and fix failures before task 5

### Task 5: Orchestrate end-to-end collection behavior

**Files:**
- Modify: internal/sources/github/collector.go
- Modify: internal/sources/github/client.go
- Add tests: internal/sources/github/integration_test.go

- [ ] implement `Collect(ctx, req)` to: (1) check config enabled, (2) derive collection scope (search vs per-repo), (3) fetch issues (REST), (4) fetch discussions (GraphQL), (5) parse both into RawSignals, (6) enforce max-item limit, (7) return combined results
- [ ] honor config toggles for SearchIssues and SearchDiscussions
- [ ] filter out already-seen signals using existing source/sourceID conventions so the collector is compatible with memory-based dedup
- [ ] ensure partial-source failures are surfaced clearly — the collector should return partial results with wrapped errors rather than fail-fast
- [ ] write end-to-end fake transport tests covering mixed issue/discussion collection, disabled source modes, deduped items, request-limit cutoff, and rate-limit exhaustion
- [ ] run go test ./internal/sources/github/... and fix failures before task 6

### Task 6: Verify acceptance criteria

- [ ] run full test suite with go test ./...
- [ ] run linter-equivalent validation with go vet ./...
- [ ] verify new GitHub package tests cover client retry/rate-limit paths, cache behavior, parser mapping, and collector orchestration with at least 80% package coverage

### Task 7: Update documentation

- [ ] document the GitHub collector pattern in CLAUDE.md (empty repos → search API, populated → per-repo, rate-limit tracking, ETag caching)
- [ ] keep docs unchanged if no new durable project pattern was introduced