# Implement the Hacker News Source Collector

## Overview

Add a Hacker News Firebase API collector that retrieves configurable story
feeds, maps qualifying stories and bounded flattened comments into
domain.RawSignal, caches public responses on disk, and exposes Hacker News
through the existing collect command.

## Context

• Files involved:
    • internal/sources/hackernews/collector.go
    • internal/sources/hackernews/client.go
    • internal/sources/hackernews/parser.go
    • internal/sources/hackernews/types.go
    • internal/sources/hackernews/errors.go
    • internal/sources/hackernews/doc.go
    • internal/sources/hackernews/cache.go
    • internal/cli/collect.go
    • internal/config/config.go
    • internal/config/config_test.go
    • testdata/hackernews/
• Related patterns: the GitHub collector's injectable HTTP transport,
  context-aware retry/request-limit client, TTL cache backed by storage.
  Storage, parser-to-RawSignal mapping, and collector integration tests.
• Dependencies: Hacker News Firebase API (https://hacker-news.firebaseio.
  com/v0), Go standard library HTTP/JSON/concurrency primitives, and existing
  storage/domain packages. No authentication or new dependency is required.
• Scope decisions:
    • Support only askstories, showstories, newstories, topstories, and
    beststories; reject unsupported configured feed names.
    • Apply configured minimum score and request Since cutoff AFTER fetching
    story items (HN API has no time-range query support). Scan feeds newest-
    first and stop at the first item older than the Since window.
    • Skip non-story (type != "story"), deleted (deleted=true), dead
    (dead=true), and other unusable records.
    • Flatten retained comment trees via BFS traversal (max depth 50) in
    deterministic order and record each comment's ancestor ID chain in
    RawSignal.Metadata, bounding total flattened comments per story.
    • Use a bounded worker pool for concurrent item fetches (default 5
    workers, errgroup with semaphore channel). The client enforces the
    configured global HN request ceiling across all concurrent workers.
    • URL fallback: when an item has no external url, construct the HN item
    URL as https://news.ycombinator.com/item?id={id}.

## Development Approach

• **Testing approach**: TDD for HTTP/client and parser behavior, then
  collector and CLI integration tests.
• Complete each task fully before moving to the next.
• Reuse the source-package structure and fake transport pattern established
  by internal/sources/github; keep all HTTP requests cancellable, timeout-
  bound, retried, and free of secrets.
• Keep cache keys deterministic and based only on public endpoint paths;
  cache writes remain non-fatal and use existing atomic storage methods.
  IMPORTANT: HN Firebase API has NO ETags, NO Last-Modified, NO conditional
  requests. Cache is simple TTL-only (24h for items, 5min for feed ID lists).
  No 304 handling, no conditional request code.
• Memory tracking: add AddHNRequests()/AddHNCacheHits() methods to memory
  (following the AddGitHubRequests pattern in internal/memory/memory.go).
• **CRITICAL: every task MUST include new/updated tests**
• **CRITICAL: all tests must pass before starting next task**

## Implementation Steps

### Task 1: Finalize Hacker News configuration and source contracts

**Files:**

• Modify: internal/config/config.go
• Modify: internal/config/config_test.go
• Create: internal/sources/hackernews/doc.go
• Create: internal/sources/hackernews/types.go
• Create: internal/sources/hackernews/types_test.go
[x] Add Hacker News configuration validation for enabled collection,
  positive per-run item limits, non-negative comment and minimum-score limits,
  and the supported feed-name allowlist.
[x] Preserve the existing defaults: askstories, showstories, and newstories;
  300 items; 30 comments; minimum score 2; and Limits.MaxHNRequests.
[x] Define minimal Firebase feed/item response structures, collector
  configuration, collection scope, cache entry, and source constants needed to
  map HN data without extending shared domain models.
[x] Define normalized signal IDs/types and the metadata key convention for
  comment parent chains.
[x] Add tests for defaults, valid/invalid HN configuration, feed validation,
  and source type/scope helpers.
[x] Run go test ./internal/config/... ./internal/sources/hackernews/...
  before task 2.

### Task 2: Implement the Firebase API client and disk cache

**Files:**

• Create: internal/sources/hackernews/client.go
• Create: internal/sources/hackernews/cache.go
• Create: internal/sources/hackernews/errors.go
• Create: internal/sources/hackernews/client_test.go
• Create: internal/sources/hackernews/cache_test.go
• Create: testdata/hackernews/feeds.json
• Create: testdata/hackernews/items.json
[ ] Implement an injectable HTTP transport and Firebase client with a 30-
  second timeout, propagated contexts, bounded retries/backoff for transient
  failures, response-size-safe JSON decoding, and typed errors for request-
  limit, HTTP-status, transport, and malformed-response failures.
[ ] Implement requests for feed ID lists and individual item records,
  applying the configured request cap across all requests and never requiring
  or sending authentication.
[ ] Add TTL response caching under cache/hackernews, using hashed public
  endpoint cache keys and storage.Storage atomic JSON persistence; return
  fresh cached results without a network request and expose cache-hit/request
  counters.
[ ] Ensure failed, cancelled, malformed, and non-success responses are not
  cached and cache corruption is treated as a miss.
[ ] Add fake-transport tests for request construction, cache hit/miss
  persistence, retries, context cancellation, request caps, response errors,
  and invalid JSON.
[ ] Run go test ./internal/sources/hackernews/... before task 3.

### Task 3: Parse Firebase items into normalized signals and comments

**Files:**

• Create: internal/sources/hackernews/parser.go
• Create: internal/sources/hackernews/parser_test.go
• Create: testdata/hackernews/story.json
• Create: testdata/hackernews/comment-tree.json
[ ] Map qualifying story items to domain.RawSignal with normalized IDs,
  hackernews source/community, HN item URL, title, HTML text body, score,
  author metadata, creation time, comment count, collection time, and
  deterministic content hash.
[ ] Convert fetched nested comments into flat domain.Comment entries,
  excluding deleted/dead/empty unusable nodes, respecting the configured cap,
  and preserving each retained comment's ancestor ID chain in signal metadata.
[ ] Keep traversal ordering deterministic and ensure title/body/comment
  content produces stable hashes regardless of map iteration or concurrent
  fetch completion.
[ ] Add fixture-driven parser tests for Ask/Show/story mapping, missing
  external URLs, HTML text, score and author metadata, deleted records, nested
  parent-chain extraction, comment truncation, and hash stability.
[ ] Run go test ./internal/sources/hackernews/... before task 4.

### Task 4: Build bounded-concurrency collector orchestration

**Files:**

• Modify: internal/memory/memory.go
• Create: internal/sources/hackernews/collector.go
• Create: internal/sources/hackernews/collector_test.go
• Create: internal/sources/hackernews/integration_test.go
[ ] Add AddHNRequests(count) and AddHNCacheHits(count) methods to DefaultMemory
  (following the AddGitHubRequests pattern).
[ ] Implement domain.SourceCollector for Hacker News and construct it from
  validated HN config plus MaxHNRequests.
[ ] Retrieve configured feeds, de-duplicate overlapping item IDs, fetch
  candidate stories through a bounded worker pool, then filter by item type,
  availability, minimum score, CollectRequest.Since, and per-run maximum.
[ ] Fetch each retained story's comment tree through the same request-
  limited client, flatten bounded comments, and return successfully collected
  signals alongside joined partial errors when one feed/item fails.
[ ] Provide test hooks equivalent to GitHub's transport/time/cache injection
  so external calls are fully deterministic.
[ ] Add collector tests for feed de-duplication, all supported feeds, score
  and since filtering, max-item/max-comment enforcement, worker-bound behavior,
  request-cap failures, partial failures, and cached repeat collection.
[ ] Add an integration-style fake-transport test spanning feeds, stories,
  nested comments, parser output, and cache behavior.
[ ] Run go test ./internal/sources/hackernews/... before task 5.

### Task 5: Wire Hacker News into the collect CLI and reporting

**Files:**

• Modify: internal/cli/collect.go
• Create: internal/cli/collect_test.go
[ ] Add the hackernews case to buildCollector, mapping config.
  HackerNewsConfig and Limits.MaxHNRequests into the new collector and
  attaching the existing storage-backed cache.
[ ] Keep HN collection credential-free while retaining GitHub token
  validation only for GitHub selections.
[ ] Extend collection statistics deltas and summary output to report Hacker
  News requests and cache hits alongside existing GitHub metrics without
  misattributing counts in multi-source runs.
[ ] Add CLI tests for HN source resolution (including hn alias), collector
  construction, disabled-source errors, no-token collection setup, and HN
  stats/report formatting.
[ ] Run go test ./internal/cli/... before task 6.

### Task 6: Verify acceptance criteria and update documentation

**Files:**

• Modify: AGENTS.md
• Modify: CLAUDE.md
• Modify: CONTEXT.md (if metadata/parent-chain terminology needs
  documenting)
[ ] Run go test ./...
[ ] Run go vet ./...
[ ] Run golangci-lint run ./...
[ ] Verify touched-package test coverage is at least 80%.
[ ] Update documentation with supported HN feeds, credential-free Firebase
  API behavior, filtering/defaults, bounded concurrency/comment traversal,
  cache location, and CLI usage such as signalforge collect --sources
  hackernews --since 30d.