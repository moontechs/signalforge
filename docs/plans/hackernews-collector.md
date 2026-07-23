# Hacker News Source Collector

## Overview

Implement a public, unauthenticated Hacker News collector under
internal/sources/hackernews/. It will retrieve configured Firebase feeds,
fetch eligible story items and their recursive comment trees with a bounded
worker pool, map them to domain.RawSignal, persist short-lived API-response
cache entries, and make signalforge collect --sources hackernews functional.

## Context

- Files involved:
    - internal/sources/hackernews/ (new package)
    - internal/config/config.go
    - internal/config/config_test.go
    - internal/cli/collect.go
    - internal/cli/collect_test.go
    - testdata/hackernews/ (new fixtures)
- Existing domain contracts:
    - domain.SourceCollector, domain.CollectRequest, domain.RawSignal, and domain.Comment
    - domain.ResearchStats already exposes Hacker News request/cache-hit fields.
- Existing storage support:
    - storage.Storage.Path, Exists, LoadJSON, SaveJSON, and ContentHash
    - Atomic JSON writes are already provided by SaveJSON.
- Related patterns:
    - internal/sources/github/ for transport injection, HTTP timeout/retry/context behavior, typed errors, TTL cache, parser separation, and fixture-backed fake-transport tests.
    - internal/cli/collect.go collector factory, source aliases, persistent-memory lifecycle, and CLI summary.
- Dependencies:
    - Go 1.24 standard library only, especially net/http, encoding/json, sync, and context.
    - Hacker News Firebase API at https://hacker-news.firebaseio.com/v0/; no authentication or token handling.

## Development Approach

- Testing approach: TDD for the HTTP/cache/parser boundaries, then integrate orchestration and CLI wiring.
- Keep the Hacker News package self-contained and use a small package-local transport interface so tests use the same fake-transport pattern as GitHub.
- Use only bounded goroutines: feed/story/comment fetch work must obey one collector concurrency limit and stop submitting or waiting promptly when the context is canceled.
- Treat cache hits separately from outbound requests; neither cache keys nor fixture output may include secrets.
- Complete each task fully before moving to the next.
- Every task includes new or updated tests, and all relevant tests must pass before starting the next task.

## Implementation Steps

### Task 1: Define Hacker News contracts, API models, and configuration validation

**Files:**
- Create: internal/sources/hackernews/doc.go
- Create: internal/sources/hackernews/types.go
- Create: internal/sources/hackernews/types_test.go
- Modify: internal/config/config.go
- Modify: internal/config/config_test.go

**Subtasks:**
- Add package documentation and define only the Firebase response fields required for collection and parsing: item ID, type, author, time, title, text, URL, score, descendants, deleted/dead flags, and recursive kids.
- Define the collector's public configuration type, mirroring config.HackerNewsConfig plus LimitsConfig.MaxHNRequests, and package-local collection scope/options derived from config and domain.CollectRequest.
- Set explicit package defaults for HTTP timeout, retry count/backoff, cache TTL, API base URL, and bounded worker count; make per-run item, comment, minimum-score, and request ceilings configurable from existing config/request inputs.
- Extend Config.Validate to call HackerNewsConfig.Validate.
- Implement Hacker News config validation: positive item limit, non-negative comment and minimum-score limits, non-empty supported feed names, and no blank or duplicate feeds after normalization.
- Add tests for valid defaults, each invalid config condition, config-load validation, scope precedence between configured values and request overrides, and API-model JSON decoding.
- Run go test ./internal/config/... ./internal/sources/hackernews/...

### Task 2: Implement Firebase client, response cache, and typed errors

**Files:**
- Create: internal/sources/hackernews/client.go
- Create: internal/sources/hackernews/cache.go
- Create: internal/sources/hackernews/errors.go
- Create: internal/sources/hackernews/client_test.go
- Create: internal/sources/hackernews/cache_test.go
- Create: testdata/hackernews/feed_askstories.json
- Create: testdata/hackernews/item_story.json
- Create: testdata/hackernews/item_comment.json
- Create: testdata/hackernews/item_deleted.json

**Subtasks:**
- Create an injectable HTTP transport and production http.Client with the project-standard timeout; construct GET requests with caller context and a non-secret User-Agent.
- Implement feed and item fetch methods using the exact Firebase paths, strict JSON decoding, response-body closure, bounded response reads if needed by existing client conventions, and a per-run outbound-request counter guarded for concurrent access.
- Implement retryable behavior for transient transport errors and HTTP 429/5xx responses, honoring context cancellation during backoff; fail immediately for malformed JSON and other non-retryable 4xx responses.
- Define typed/sentinel errors for disabled source, request limit reached, invalid feed, malformed Firebase response, retry exhaustion, and HTTP/API failures, with wrapping compatible with errors.Is/errors.As.
- Add a disk cache under cache/hackernews, keyed from method/path only and hashed to a filesystem-safe filename. Store body plus collection timestamp using Storage.SaveJSON; return fresh entries within the TTL and refetch expired entries.
- Expose thread-safe request and cache-hit counter accessors for later stats/reporting integration.
- Build fake-transport tests for request path construction, status handling, retry success/exhaustion, request-cap enforcement under concurrent callers, context cancellation, malformed payloads, cache persistence/freshness/expiry, cache-key secrecy, and cache concurrency.
- Run go test ./internal/sources/hackernews/...

### Task 3: Implement parser and deterministic RawSignal conversion

**Files:**
- Create: internal/sources/hackernews/parser.go
- Create: internal/sources/hackernews/parser_test.go
- Create: testdata/hackernews/item_ask_story.json
- Create: testdata/hackernews/item_url_story.json
- Create: testdata/hackernews/comment_tree.json
- Create: testdata/hackernews/item_dead.json

**Subtasks:**
- Convert HN story and ask items into domain.RawSignal with stable IDs such as hackernews_story:<id>, Source set to hackernews, HN item URL, title/body normalization, score, descendant count, timestamps, and meaningful source metadata such as author and external-link URL.
- Normalize HTML text returned by Firebase into readable signal/comment text using a minimal standard-library approach; preserve plain text safely and define empty-text behavior.
- Convert fetched comments into domain.Comment, preserving ID, body, author-independent score semantics, and creation time; order comments deterministically before applying the configured maximum.
- Skip non-story feed items and deleted/dead stories/comments; retain a story even when individual comment retrieval fails, with the partial failure handled by collector orchestration.
- Calculate ContentHash from the normalized title, body, and retained comment bodies using storage.ContentHash.
- Add fixture-driven parser tests for Ask HN and linked stories, HTML normalization, source URL/metadata mapping, stable ID/hash behavior, comment ordering/truncation, and deleted/dead/missing-content edge cases.
- Run go test ./internal/sources/hackernews/...

### Task 4: Implement bounded-concurrency collection and recursive comment extraction

**Files:**
- Create: internal/sources/hackernews/collector.go
- Create: internal/sources/hackernews/collector_test.go
- Create: internal/sources/hackernews/integration_test.go
- Create: testdata/hackernews/feed_newstories.json
- Create: testdata/hackernews/comment_parent.json
- Create: testdata/hackernews/comment_child.json
- Create: testdata/hackernews/comment_grandchild.json

**Subtasks:**
- Implement Collector.New, Name, WithTransport, WithCache, and a test-only clock override consistent with the GitHub collector's construction and dependency-injection pattern.
- Fetch each configured feed, deduplicate story IDs across feeds while retaining deterministic feed order, then apply configured/request item limits before scheduling story work.
- Filter fetched stories by valid type, dead/deleted state, and MinimumScore; apply Since/Until bounds from CollectRequest when supplied.
- Use a fixed-size worker pool for item retrieval and comment-tree work. Ensure job/result channels, wait groups, and result collection are cancellation-safe and cannot leak goroutines.
- Recursively traverse each accepted story's kids, fetch comments through the same bounded work queue, skip deleted/dead/non-comment nodes, deduplicate comment IDs, and stop at MaxCommentsPerItem without scheduling unnecessary descendants.
- Preserve deterministic signal ordering despite concurrent retrieval, deduplicate by signal ID, return successfully parsed signals together with joined partial errors, and return a full error when no usable collection can complete.
- Add fake-transport integration tests covering multi-feed deduplication, score/date filtering, per-run limits, recursive nested comments, comment caps, request caps, cache reuse, mixed successes/failures, worker-bound enforcement, and canceled contexts.
- Run go test ./internal/sources/hackernews/... and go test -race ./internal/sources/hackernews/...

### Task 5: Wire Hacker News into the collect command and reporting

**Files:**
- Modify: internal/cli/collect.go
- Modify: internal/cli/collect_test.go
- Modify: internal/config/config_test.go

**Subtasks:**
- Import the Hacker News package and add a hackernews case to buildCollector; reject disabled HN configuration, construct its collector config from Sources.HackerNews and Limits.MaxHNRequests, and attach the shared storage-backed cache.
- Do not require any environment variable for HN collection; retain GitHub's token check solely in the GitHub branch.
- Update collect command help text/examples to list Hacker News as an MVP-supported source and demonstrate --sources hn/hackernews.
- Generalize collection stats and summary reporting so selected HN runs report HackerNewsRequests and cache hits correctly, including multi-source summaries without labeling all requests as GitHub requests.
- Add CLI tests for HN alias resolution, disabled-source error, collector factory construction without credentials, collector cache attachment, mixed GitHub/HN source selection, and HN-specific summary output.
- Run go test ./internal/cli/... ./internal/config/...

### Task 6: Verify the complete feature and update user-facing documentation

**Files:**
- Modify: README.md (if collection-source documentation is present)
- Modify: AGENTS.md (only if its documented MVP behavior is intentionally updated)
- Modify: relevant default config documentation/sample, if one exists outside internal/config/config.go

**Subtasks:**
- Document supported HN feeds, no-auth operation, score/item/comment limits, recursive-comment behavior, cache location/TTL behavior, and the lack of resumable cursors in the initial collector.
- Run go test ./...
- Run go test -race ./internal/sources/hackernews/...
- Run go vet ./...
- Run golangci-lint run ./... when installed by the repository toolchain.
- Verify the new Hacker News package reaches at least 80% test coverage with go test -cover ./internal/sources/hackernews/...

## Acceptance Criteria

- signalforge collect --sources hackernews --since 30d creates and runs a no-auth HN collector from validated configuration.
- All five supported feed names are accepted; malformed, blank, duplicate, or unsupported feed configuration fails before API access.
- The collector observes configured item, score, comment, request, and bounded-concurrency limits.
- Recursive comments are extracted deterministically, skip dead/deleted nodes, and never exceed the configured per-story cap.
- Firebase responses are cached atomically under cache/hackernews and fresh hits avoid outbound requests.
- HTTP calls use timeout, retry, context cancellation, and fake-transport coverage; no live network or credentials are needed for tests.
- Existing project tests, vet, and configured lint checks pass.