# Plan: M2-T8 Collect CLI Command and Source Orchestration

### Task 1: Extend collect command configuration and flags

- [ ] Update `internal/cli/collect.go` to add `collectEnv` fields for `until`, `maxItems`, `language`, `force`, `dryRun`, and `resume`.
- [ ] Register `--until` as a string flag accepting an ISO date or existing duration-style window format.
- [ ] Register `--max-items` as an integer flag with validation that rejects negative values.
- [ ] Register `--language` as an optional string filter passed through the collection request.
- [ ] Register boolean `--force`, `--dry-run`, and `--resume` flags.
- [ ] Preserve current `--sources` and `--since` behavior and defaults.

### Task 2: Parse and construct complete collection requests

- [ ] Add parsing for `--until`, accepting ISO-8601 calendar dates and duration/window formats compatible with `ParseSinceWindow`.
- [ ] Return actionable CLI errors for invalid `--until` values or invalid date ranges where `until` precedes `since`.
- [ ] Update `executeCollect` to populate `domain.CollectRequest` with `Since`, `Until`, `MaxItems`, `Force`, `DryRun`, `Sources`, and `Cursor`.
- [ ] Pass the language option through the existing request/configuration path used by source construction, without modifying collector implementations.
- [ ] Ensure omitted values retain the existing collector defaults rather than overriding them with invalid zero values.

### Task 3: Add resumable per-source cursor state to memory

- [ ] Extend `internal/memory/memory.go` persisted memory schema with a per-source cursor map of type `map[string]string`.
- [ ] Initialize the cursor map safely when loading older `memory.json` files that do not contain cursor state.
- [ ] Add memory accessors to read, update, and persist a cursor for a named source.
- [ ] When `--resume` is enabled, load each source’s stored cursor and set it on that source’s `CollectRequest`.
- [ ] After a successful source collection, persist its returned/updated cursor before proceeding to the next source.
- [ ] Ensure cursor persistence uses the project’s existing atomic JSON-write behavior.
- [ ] Keep cursor state scoped by source name so GitHub, Hacker News, and Stack Exchange do not overwrite one another.

### Task 4: Implement deterministic source orchestration and force behavior

- [ ] Normalize and validate requested source names before collection begins.
- [ ] Order selected collectors deterministically as GitHub, Hacker News, then Stack Exchange, regardless of `--sources` input order.
- [ ] Continue using `buildCollector` to construct collectors from configuration.
- [ ] Update the CLI-side deduplication path so `--force` bypasses both `mem.HasRawSignal` and `mem.HasContentHash` checks.
- [ ] Preserve normal deduplication and memory recording when `--force` is not set.
- [ ] Keep source-specific failures and summary reporting consistent with existing CLI error-handling conventions.

### Task 5: Implement dry-run planning output

- [ ] Add a dry-run branch before any collector API invocation.
- [ ] For every selected source, print the planned source name, relevant endpoint/feed/query targets, estimated request count, since/until window, max-items limit, language filter, and resume cursor status.
- [ ] Reuse collector configuration and known source request shapes to calculate plans without making HTTP requests.
- [ ] Return successfully after printing the plan and do not call `Collect`, mutate deduplication memory, update cursors, or write collection results.
- [ ] Ensure dry-run output follows the same fixed source order as real collection.

### Task 6: Update collection statistics and summary reporting

- [ ] Extend `statsDelta` to capture source-level attempted, collected, skipped/deduplicated, failed, and dry-run-planned work as applicable.
- [ ] Update `reportCollectSummary` to report all selected sources in deterministic order.
- [ ] Include force, dry-run, and resume/cursor information in the summary where useful for operator review.
- [ ] Ensure dry-run summaries clearly state that no API calls were made and no data was persisted.
- [ ] Preserve existing aggregate statistics and avoid double-counting records skipped by deduplication.

### Task 7: Add and update tests

- [ ] Add CLI tests covering parsing and forwarding of `--until`, `--max-items`, `--language`, `--force`, `--dry-run`, and `--resume`.
- [ ] Add tests for valid ISO-date and duration-style `--until` inputs, invalid values, and invalid since/until ranges.
- [ ] Add orchestration tests proving source execution order is GitHub, Hacker News, Stack Exchange.
- [ ] Add dry-run tests proving planned output is produced and collectors are not invoked.
- [ ] Add force tests proving duplicate source IDs and content hashes are collected when `--force` is enabled.
- [ ] Add non-force regression tests proving existing deduplication behavior remains intact.
- [ ] Add memory tests for cursor-map initialization, round-trip persistence, per-source isolation, and backward compatibility with existing memory files.
- [ ] Add resume tests proving saved cursors are supplied to matching sources only.
- [ ] Update existing summary/statistics tests for new counters and dry-run behavior.

## Validation Commands

```bash
gofmt -w internal/cli/collect.go internal/memory/memory.go
go test ./internal/cli/... ./internal/memory/...
go test ./...
go vet ./...
golangci-lint run ./...
go build ./cmd/signalforge/
```