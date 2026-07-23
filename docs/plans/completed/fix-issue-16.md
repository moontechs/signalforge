# Post-Review Cleanup (Issue #16)

## Context

Two minor style issues identified in PR #15 code review. Both are cosmetic — no behavioural changes.

Branch: `fix/issue-16-post-review-cleanup` (already created, based on main after PR #15 merge)

### Task 1: Fix map syntax in collector.go

**File:** `internal/sources/hackernews/collector.go`

- [x] Change `seen := make(map[int]bool)` to `seen := make(map[int]struct{})`
- [x] Change `seen[id] = true` to `seen[id] = struct{}{}`
- [x] Fix `!seen[id]` usage to comma-ok idiom `if _, ok := seen[id]; !ok` (struct{} can't be negated)

### Task 2: Fix reportCollectSummary formatting

**File:** `internal/cli/collect.go`

- [x] Replace the `reportCollectSummary` function with dynamic formatting that shows only active source stats
- [x] Add edge case tests (only HN requests, no requests at all)

```go
func reportCollectSummary(cmd *cobra.Command, source string, totalSignals int, delta collectStatsDelta) error {
	msg := fmt.Sprintf("Collected %d signals from %s. New: %d, skipped: %d",
		totalSignals, source, delta.collected, delta.skipped)

	if delta.requests > 0 {
		msg += fmt.Sprintf(", GitHub requests: %d", delta.requests)
	}
	if delta.hnRequests > 0 {
		msg += fmt.Sprintf(", HN requests: %d (cache hits: %d)", delta.hnRequests, delta.hnCacheHits)
	}
	msg += "\n"

	_, err := fmt.Fprint(cmd.OutOrStdout(), msg)
	if err != nil {
		return fmt.Errorf("write collection summary: %w", err)
	}
	return nil
}
```

### Task 3: Verify everything still works

- [x] Run `go build ./cmd/signalforge/` — builds
- [x] Run `go test ./...` — all pass
- [x] Run `go vet ./...` — clean
- [x] Run `golangci-lint run ./...` — clean
- [x] Commit and push