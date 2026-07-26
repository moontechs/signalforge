# Plan: Wire GitHub Discussions into collector.go

**Bug:** moontechs/signalforge#45 — Discussions code exists in `discussions.go` but is never called from `collector.go Collect()`. Only Issues are collected.

## Context

- `internal/sources/github/discussions.go` — fully implemented: GraphQL query, response types, `parseDiscussions()` function, `fetchDiscussions()` function
- `internal/sources/github/collector.go` — Collect() calls `fetchIssues()` but never calls any discussion function
- `internal/sources/github/collector.go` — already parses discussions with `parseDiscussions(discussions, scope)` — this line EXISTS in the diff but the `fetchDiscussions` call is missing
- `internal/config/config.go` — validation now requires repos when Discussions enabled ✅ (already in PR #48)
- `internal/sources/github/types.go` — `collectionScope` has `searchDiscussions bool` field

## Files to modify

1. **`internal/sources/github/collector.go`** — add Discussion collection step in Collect()
2. **`internal/sources/github/collector_test.go`** — add tests for Discussion collection path
3. **`internal/sources/github/integration_test.go`** — add integration test for Issue+Discussion combined run

## Implementation Steps

### Task 1: Wire Discussion collection into collector.go Collect()

- [x] Inspect `discussions.go` to find the exported function to call (likely `fetchDiscussions(ctx, *githubClient, *collectionScope)`)
- [x] In `collector.go Collect()`, after the Issues fetch section and before the combined results section, add:
  ```go
  // If discussions are enabled and we have repos, fetch discussions.
  var discussions []rawDiscussionSignal
  if scope.searchDiscussions && len(scope.repos) > 0 {
      fetched, err := fetchDiscussions(ctx, c.client, &scope)
      if err != nil {
          errs = append(errs, fmt.Errorf("fetch discussions: %w", err))
      } else {
          discussions = fetched
      }
  }
  ```
- [x] Add the discussion signals to the combined results alongside issues
- [x] Preserve existing: `signals = signals[:scope.maxItems]` truncation applies to combined list
- [x] Ensure `c.storeStatsDelta(beforeStats)` is called after all collection steps

### Task 2: Test coverage for Discussion collection

- [x] Add `TestCollector_DiscussionsOnly` — config with `SearchIssues: false`, `SearchDiscussions: true`, repo target; fake GraphQL response; verify discussion signals returned with `source_type: github_discussion`
- [x] Add `TestCollector_IssueAndDiscussion` — both enabled; verify both types present in combined results
- [x] Add `TestCollector_DiscussionsDisabled` — `SearchDiscussions: false`; verify no GraphQL call
- [x] Add `TestCollector_DiscussionsNoRepos` — `SearchDiscussions: true`, no repos; verify error returned (config validation handles this, but test the boundary)
- [x] Add to integration test: fake REST + GraphQL responses; verify both issue and discussion RawSignals persisted

### Task 3: Regression validation

- [ ] Run `go test ./internal/sources/github/... -v -count=1`
- [ ] Run `go test ./internal/cli/... -v -count=1`
- [ ] Run `go test ./... -count=1`
- [ ] Run `go vet ./...` and `golangci-lint run ./...`
- [ ] Run `go build ./cmd/signalforge/`
- [ ] **All `git commit` and `git push` MUST use `--no-verify`**

## Validation Commands

- `go test ./internal/sources/github/... -v -count=1`
- `go test ./... -count=1`
- `go vet ./...`
- `golangci-lint run ./...`
- `go build ./cmd/signalforge/`
