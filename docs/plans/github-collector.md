# GitHub Issues + Discussions Collector for SignalForge

## Overview

Implement the MVP GitHub collector that gathers public GitHub Issues and
Discussions, converts them into RawSignal records, deduplicates them through
persistent memory, and persists them into the existing file-based pipeline.
The work should follow the project's source-collector pattern, use GitHub
REST and GraphQL where appropriate, respect request limits and retries, and
expose the collector through a new collect CLI command.

## Context

- Files involved: internal/domain/types.go, internal/config/config.go,
  internal/storage/storage.go, internal/memory/memory.go, cmd/signalforge/main.go,
  internal/cli/, internal/sources/github/, testdata/
- Related patterns: SourceCollector interface in internal/domain/types.go,
  atomic JSON/JSONL persistence in internal/storage/storage.go, persistent
  dedup state in internal/memory/memory.go, Cobra command structure in
  internal/cli/init.go and related commands
- Dependencies: standard library net/http and encoding/json, existing Cobra
  dependency, GitHub REST API for issues and comments, GitHub GraphQL API for
  discussions and discussion comments
- Constraints to preserve: no database, no secret-bearing cache keys, all
  HTTP calls must support timeout/retry/context cancellation, tests must run
  without API keys, fake clients for external APIs

## Implementation Steps

### Task 1: Define collection orchestration contracts and GitHub-specific inputs

**Files:**
- Modify: internal/domain/types.go
- Modify: internal/config/config.go
- Create: internal/sources/github/doc.go
- Create: internal/sources/github/types.go
- Create: internal/sources/github/types_test.go

[x] align the SourceCollector interface and CollectRequest signature with
  context.Context so collector packages can implement the AGENTS.md contract
  cleanly
[x] add any missing collection request fields needed by source collectors,
  such as since-window, per-run limits, and optional cursor state, while
  keeping the shape minimal for MVP
[x] define GitHub package request/response model types for issues, issue
  comments, discussions, and discussion comments, including only the fields
  needed to map into RawSignal
[x] confirm config defaults and validation expectations for GitHub
  repositories, labels, language filters, max items, and max comments so the
  collector has one clear source of truth
[x] write/update tests covering config defaults and any new type or
  validation behavior
[x] run package tests for touched packages before moving to task 2

### Task 2: Build the GitHub API clients with retries, limits, and typed errors

**Files:**
- Create: internal/sources/github/client.go
- Create: internal/sources/github/errors.go
- Create: internal/sources/github/client_test.go
- Create: testdata/github/

[ ] implement an HTTP-backed GitHub client with explicit timeout,
  retry/backoff, request context propagation, and token-based authentication
  via GITHUB_TOKEN without logging secrets
[ ] support REST requests for issues and issue comments, plus GraphQL
  requests for discussions and discussion comments, keeping request
  construction isolated from parsing
[ ] add typed error handling for rate limiting, authentication failure,
  malformed responses, and non-retryable API errors so the collector can make
  deterministic decisions
[ ] enforce configured request ceilings and expose request counters/hooks
  needed for run stats
[ ] create fake transport or fake client fixtures so tests cover success,
  pagination, retry, rate-limit, and failure paths with no network access
[ ] run package tests for the new GitHub client before moving to task 3

### Task 3: Parse GitHub responses into RawSignal records

**Files:**
- Create: internal/sources/github/parser.go
- Create: internal/sources/github/parser_test.go
- Modify: internal/domain/types.go (only if additional metadata fields are
  strictly necessary)

[ ] implement mapping from GitHub issues and discussions into domain.
  RawSignal, including title, body, URL, labels/tags, repository, community,
  timestamps, engagement counts, and bounded comments
[ ] normalize source_type values so issues and discussions remain
  distinguishable while still using source=github
[ ] compute stable content hashes from normalized content and ensure
  metadata captures GitHub-specific facts that are useful later without
  leaking tokens or over-modeling
[ ] define how empty bodies, deleted comments, locked/closed items, and
  items with partial data are handled in MVP and encode that behavior in
  parser tests
[ ] add fixture-driven tests for issue-to-RawSignal and discussion-to-
  RawSignal conversion, including comment truncation and missing-field edge
  cases
[ ] run package tests for parser coverage before moving to task 4

### Task 4: Implement collector orchestration, deduplication, caching, and persistence hooks

**Files:**
- Create: internal/sources/github/collector.go
- Create: internal/sources/github/collector_test.go
- Modify: internal/memory/memory.go
- Modify: internal/storage/storage.go (only if a minimal helper is needed
  for cache or JSONL persistence)
- Modify: internal/domain/types.go (only if stats/cursor fields already
  defined need small corrections)

[ ] implement the GitHub collector to satisfy SourceCollector, coordinating
  issue/discussion search, per-item comment fetches, parser conversion, dedup
  checks, and request accounting
[ ] use persistent memory to skip already-seen source IDs and duplicate
  content hashes before writing new raw signals
[ ] store raw signals in the existing file-based layout and use the existing
  cache directory conventions for GitHub response caching if cache helpers are
  added during implementation
[ ] keep collector flow resumable-friendly by accepting cursor/since inputs
  now, even if the first MVP execution uses a simple since-window rather than
  full resume persistence
[ ] add tests for collector behavior with fake clients covering mixed
  issue/discussion runs, dedup skips, max-item enforcement, comment limits,
  and partial failures
[ ] run package tests for collector and dependent packages before moving to
  task 5

### Task 5: Add the collect command and wire GitHub into the CLI workflow

**Files:**
- Create: internal/cli/collect.go
- Create: internal/cli/collect_test.go
- Modify: cmd/signalforge/main.go
- Modify: internal/cli/doctor.go
- Modify: internal/config/config.go

[ ] add a collect command that loads config, initializes storage and memory,
  resolves source selection from flags, and invokes the GitHub collector for
  MVP-supported sources
[ ] add CLI flags for sources and since-window in a way that matches AGENTS.
  md examples and does not overcommit to future pipeline orchestration details
[ ] make doctor validate the presence of GITHUB_TOKEN when GitHub collection
  is enabled so failures happen before network calls
[ ] ensure collect output reports collected/skipped counts and actionable
  errors without exposing tokens or raw API payloads
[ ] add command-level tests using fake collectors or fake clients to verify
  source selection, config loading, token validation, and persistence side
  effects
[ ] run CLI/package tests before moving to task 6

### Task 6: Verify end-to-end collector behavior and update documentation

**Files:**
- Modify: AGENTS.md
- Modify: CLAUDE.md
- Create: internal/sources/github/e2e_test.go or
  internal/cli/collect_e2e_test.go

[ ] add an end-to-end test around the collect flow using fake GitHub
  responses and a temporary SIGNALFORGE_HOME to verify raw-signal persistence,
  memory updates, and repeat-run dedup behavior
[ ] run the full test suite with go test ./...
[ ] run the linter/vet command with go vet ./...
[ ] verify the new GitHub collector tests keep overall coverage at or above
  the requested threshold for touched packages
[ ] update project docs to describe the GitHub collector behavior, required
  token, and any MVP limitations such as search scope or cursor handling
