# Fix GitHub Stats Tracking (requests + cache_hits)

## Problem
GitHub collection collects signals but `memory.json` shows `github_requests=0`, `github_cache_hits=0`. The HN collector correctly tracks these stats.

## Root Cause
1. No `AddGitHubCacheHits()` method exists in `memory.go`
2. No code calls `memory.AddGitHubCacheHits(...)` anywhere
3. When `signals==0`, memory.Save() isn't called so AddGitHubRequests stats are lost (line 125 of collector.go: `if persisted && !req.DryRun`)
4. The collector never calls AddGitHubCacheHits because the method doesn't exist
5. Doctor command never displays any stats

## What "cache_hits" means in this codebase
`github_cache_hits` mirrors `hackernews_cache_hits`. In the HN collector, cache hits = signals where `HasRawSignal()` or `HasContentHash()` returned true (deduplication hits, i.e., signals skipped because already known).

## Scope
- Fix: Add missing methods, wire up stats, ensure persistence, display in doctor
- Out of scope: Response cache refactoring

## Plan

### Task 1: Add `AddGitHubCacheHits()` method to `memory.go`
**Files:**
- `internal/memory/memory.go`
- `internal/memory/memory_test.go`

- [x] Add `AddGitHubCacheHits(count int)` method following the exact pattern of `AddRedditCacheHits` and `AddHNCacheHits`: guard `count <= 0`, lock, `m.mem.Stats.GitHubCacheHits += count`
- [x] Add case `"github_cache_hits"` to `IncrementStat()` switch statement
- [x] Write unit test `TestAddGitHubCacheHits` with sub-tests: basic increment, zero no-change, negative no-change, accumulation, concurrent safety
- [x] Verify: `go test ./internal/memory/...` passes

### Task 2: Wire up cache hit tracking in collector
**Files:**
- `internal/sources/github/collector.go`
- `internal/sources/github/collector_test.go`

- [x] Add `cacheHits int` counter field to `Collector` struct
- [x] Record persistent-memory deduplication hits through `Collector.AddCacheHits()` (HasRawSignal/HasContentHash checks live in the CLI in this architecture)
- [x] Track per-run GitHub request counts and expose them with `Collector.Stats()` for the CLI stats pipeline
- [x] Write unit tests verifying cache-hit counting and per-run reset behavior
- [x] Verify: `go test ./internal/sources/github/...` passes

### Task 3: Fix stats persistence — always save stats
**Files:**
- `internal/sources/github/collector.go`

- [ ] Line 125-129 in collector.go only saves when `persisted && !req.DryRun`
- [ ] Move `m.Save()` call to after AddGitHubRequests/AddGitHubCacheHits, BEFORE the persisted check
- [ ] Stats should persist even when no signals collected (request counts are always valid)
- [ ] Write unit test verifying stats persist even when no signals collected
- [ ] Verify: `go test ./internal/sources/github/...` passes

### Task 4: Display GitHub/Never stats in doctor command
**Files:**
- `internal/cli/doctor.go`
- `internal/cli/doctor_test.go`

- [ ] Add a "Research Stats" section to doctor output showing GitHub/HN requests and cache hits
- [ ] Display in formatted block:
  ```
  Research Stats:
    GitHub Requests:     463
    GitHub Cache Hits:   127
    HN Requests:         2958
    HN Cache Hits:       4286
    Raw Signals:         354
  ```
- [ ] Add test for doctor stats display
- [ ] Verify: `go test ./internal/cli/...` passes

### Task 5: Run full test suite
- [ ] Run `go build ./cmd/signalforge/` — builds clean
- [ ] Run `go test ./...` — all tests pass
- [ ] Run `go vet ./...` — clean
- [ ] Run `golangci-lint run ./...` — clean

## Validation Commands

# Build
go build ./cmd/signalforge/

# Run all tests
go test ./...

# Run specific package tests
go test ./internal/sources/github/...
go test ./internal/memory/...
go test ./internal/cli/...

# Run linter
go vet ./...
golangci-lint run ./...
