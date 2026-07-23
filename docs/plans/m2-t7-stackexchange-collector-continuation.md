# Plan: M2-T7 — Stack Exchange Collector (continuation)

## Overview

Continue implementing the Stack Exchange API source collector. Tasks 1, 5, and 6 are already completed and committed:
- Task 1 (done): types.go, errors.go, doc.go, config validation + tests
- Task 5 (done): AddStackExchangeRequests/AddStackExchangeCacheHits in memory.go + tests
- Task 6 (done): CLI wiring in collect.go (buildCollector, statsDelta, reportCollectSummary) + tests

Remaining work: API client, parser, collector, lint excludes, and final validation.

## Context

- Existing files (committed): internal/sources/stackexchange/{types.go,errors.go,doc.go}
- Existing modified files (committed): internal/config/config.go, internal/config/config_test.go, internal/cli/collect.go, internal/cli/collect_test.go, internal/domain/types.go, internal/memory/memory.go
- Pattern to follow: internal/sources/hackernews/{client.go,fake_transport.go,parser.go,collector.go}
- New files to create: client.go, fake_transport.go, parser.go, collector.go, client_test.go, parser_test.go, collector_test.go, integration_test.go
- Test fixtures: testdata/stackexchange/{questions.json,answers.json,comments.json}
- Modified: .golangci.yml (add exclude rules), AGENTS.md (update docs)

### Task 1: Implement SE API client with backoff, retries, quota, caching

**Files:** internal/sources/stackexchange/client.go, internal/sources/stackexchange/fake_transport.go, internal/sources/stackexchange/client_test.go

- [x] Create `client.go` with:
  - `transport` interface with `Do(*http.Request) (*http.Response, error)` + `httpTransport` wrapper (same pattern as HN)
  - `client` struct with: transport, baseURL, timeout, retryMax, maxRequests, maxBodySize (10MB), apiKey, retryBackoff function, mutex, requests/cacheHits counters, storage.Store, forcedBackoff time.Time
  - `newClient(t transport, cfg ConfigValues) *client` — baseURL="https://api.stackexchange.com/2.3", timeout=30s, retryMax=3, retryBackoff=2^N sec + random jitter
  - `WithCache(s *storage.Storage) *client`, `Stats() Stats`, `cachePath(key)`, `cached(key, ttl)`, `save(key, body)`, `requestCapReached()`, `incrementRequests()`
  - `get(ctx, path, ttl)` — core HTTP method: check cache → check cap → check forcedBackoff → retry loop with backoff → parse seAPIResponse wrapper from body → check ErrorID != 0 (return ErrAPIError) → extract Backoff (set forcedBackoff) → extract QuotaRemaining → cache → return
  - `readBody(resp)` — bounded reader (same as HN)
  - `questions(ctx, site, fromdate, todate, page, pagesize)` → path `/questions?site=X&sort=creation&order=desc&filter=withbody&page=N&pagesize=N&fromdate=X&todate=Y` + optional `&key=K`
  - `answers(ctx, site, questionID, page, pagesize)` → path `/questions/{id}/answers?site=X&filter=withbody&page=N&pagesize=N`
  - `comments(ctx, site, questionID, page, pagesize)` → path `/questions/{id}/comments?site=X&filter=withbody&page=N&pagesize=N`
  - TTL: questions=5min, answers=30min, comments=30min
- [x] Create `fake_transport.go` — exact copy of HN's fake_transport.go pattern: fakeResponse, fakeTransport, newFakeTransport, addResponse, addSequentialResponses, findResponseLocked, nextResponseLocked, Do, callCountFor, resetCallCount + `testClient(fake *fakeTransport) *client` helper
- [x] Write client_test.go with: basic endpoint tests (questions/answers/comments success), backoff test (verify forcedBackoff delay + global scope), backoff context cancellation, quota parsing, API key in URL, cache hit + expiration, retry transient + exhaustion, context cancellation, request cap, malformed JSON, non-retryable 4xx, 429 retry, oversized body, stats tracking, concurrent access (-race), pagination

### Task 2: Parse SE responses into normalized RawSignal records

**Files:** internal/sources/stackexchange/parser.go, internal/sources/stackexchange/parser_test.go, testdata/stackexchange/{questions.json,answers.json,comments.json}

- [ ] Create `parser.go` with:
  - `parseQuestion(item, answers, comments, site, collectedAt)` → domain.RawSignal: ID="se:{question_id}", Source="stackexchange", SourceID=str(question_id), URL=item.Link, Title, Body (HTML from API), Comments merged+sort, Community=site, Tags=item.Tags, Score, ViewCount, AnswerCount, CreatedAt=Unix, UpdatedAt=LastActivityDate, CollectedAt, metadata with MetaKeySite/MetaKeyQuestionScore/MetaKeyAnswerCount/MetaKeyViewCount/MetaKeyIsAnswered/MetaKeyTags/MetaKeyAcceptedAnswerID/MetaKeyAuthor, ContentHash from title+body+comment bodies
  - `parseAnswer(item)` → domain.Comment with ID="se_answer:{id}"
  - `parseComment(item)` → domain.Comment with ID="se_comment:{id}"
  - `eligibleQuestion(item, scope)` → checks score threshold, views threshold, since window
  - `mergeAndSortComments(answers, comments)` → chronological order
- [ ] Create testdata/stackexchange/questions.json — 3+ questions with varying scores/views/tags/owners
- [ ] Create testdata/stackexchange/answers.json — multiple answers for a question, mix of accepted/non-accepted
- [ ] Create testdata/stackexchange/comments.json — multiple comments on a question
- [ ] Write parser_test.go with: TestParseQuestion (all fields), TestParseQuestion_noOwner, TestParseQuestion_noTags, TestParseQuestion_noAcceptedAnswer, TestParseQuestion_notAnswered, TestParseAnswer, TestParseComment, TestEligibleQuestion (table-driven), TestMergeAndSortComments, TestContentHashDeterminism, TestParseQuestion_fromFixture

### Task 3: Build the SE collector with bounded concurrency + site iteration

**Files:** internal/sources/stackexchange/collector.go, internal/sources/stackexchange/collector_test.go, internal/sources/stackexchange/integration_test.go

- [ ] Create `collector.go` with:
  - `Collector` struct: config, client, now func, mutex, requests/cacheHits counters
  - `New(cfg *ConfigValues) (*Collector, error)` — validates enabled, validates sites, creates httpTransport + client
  - `Name() string` → "stackexchange"
  - `WithTransport(t transport)`, `WithNow(now func())`, `WithCache(store)` — test hooks
  - `Stats()` — return request/cache-hit counters
  - `Collect(ctx, req)`:
    1. Derive scope (sites, maxItemsPerSite, maxCommentsPerItem, minScore, minViews, since, maxRequests)
    2. Client stats before snapshot
    3. Iterate sites sequentially (max 3 concurrent via semaphore)
    4. For each site: paginate questions (page=1, pagesize=100, fromdate=since). Stop on has_more=false, per-site cap, request cap
    5. Optimization: once questions are older than since, stop paginating (sorted by creation desc)
    6. For each qualifying question: fetch answers + comments (single page, single ID)
    7. Merge+sort comments, cap by maxCommentsPerItem
    8. Parse into RawSignal
    9. Dedup by question ID across all sites
    10. Sort all signals by CreatedAt descending
    11. Apply global items cap (len(sites) * maxItemsPerSite)
    12. Return with partial errors (per-site failures joined)
- [ ] Write collector_test.go with: TestCollector_New_disabled, New_invalidSite, happyPath, multipleSites, dedupAcrossSites, scoreFiltering, viewsFiltering, sinceFiltering, maxItemsPerSiteCap, requestCap, emptySite, answersAndComments, acceptedAnswerFirst, contextCancellation, cachedRepeat, stats, partialFailure, pagination, concurrentAccess
- [ ] Write integration_test.go — multi-site fixture with 2 sites, overlapping IDs, mixed eligibility, answers+comments, cache behavior, pagination

### Task 4: Update golangci-lint exclude rules

- [ ] Add to .golangci.yml:
  - gosec/G404 for client.go
  - gochecknoglobals for types.go (SupportedSites, constants)
  - mnd for magic numbers in client.go and collector.go
  - funlen for Collect method
  - gocritic/hugeParam for req in collector.go
  - unparam for WithTransport/WithNow/WithCache
  - wrapcheck for io.ReadAll in client.go

### Task 5: Final validation — build, test, vet, lint, coverage

- [ ] Run: go build ./cmd/signalforge/
- [ ] Run: go vet ./...
- [ ] Run: go test -race -count=1 ./internal/sources/stackexchange/...
- [ ] Run: go test -count=1 ./internal/config/...
- [ ] Run: go test -count=1 ./internal/cli/...
- [ ] Run: go test -count=1 ./internal/memory/...
- [ ] Run: go test -count=1 ./internal/domain/...
- [ ] Run: go test -race -count=1 ./...
- [ ] Run: golangci-lint run ./...
- [ ] Update AGENTS.md with SE collector docs

## Validation Commands

```bash
go build ./cmd/signalforge/
go vet ./...
go test -race -count=1 ./internal/sources/stackexchange/...
go test -count=1 ./internal/config/...
go test -count=1 ./internal/cli/...
go test -count=1 ./internal/memory/...
go test -count=1 ./internal/domain/...
go test -race -count=1 ./...
golangci-lint run ./...
```
