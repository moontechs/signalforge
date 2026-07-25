# Plan: Resolve Reddit merge artifacts on main

### Task 1: Remove duplicate Reddit configuration validation and its duplicate test

- [x] In `internal/config/config.go`, delete the second `(*RedditConfig).Validate` declaration at lines 219–242 and retain the canonical implementation at lines 186–217, including sort and time validation through `IsValidRedditSort`, `IsValidRedditTime`, and their value-list helpers.
- [x] In `internal/config/config_test.go`, remove the later duplicate `TestRedditConfigValidate` and `TestLoadConfigValidatesRedditConfig` declarations; retain the earlier declarations and the distinct Reddit sort/time helper tests.
- [x] Confirm the retained validation tests continue to cover disabled configuration, subreddit requirements, post/comment limits, and sort/time validation.

### Task 2: Remove duplicated Reddit memory-counter test block

- [x] In `internal/memory/memory_test.go`, retain the existing Reddit request and cache-hit tests at lines 287–395.
- [x] Delete the duplicate block beginning with `TestAddRedditRequests` at line 397 through the paired duplicate `TestAddRedditCacheHits` function.
- [x] Preserve coverage for positive increments, zero/negative no-ops, accumulation, and concurrent counter updates from the retained tests.

### Task 3: Reconcile stale Reddit type tests with the current package API

- [x] Update `internal/sources/reddit/types_test.go` to stop referencing removed symbols such as `SupportedSortValues`, `SupportedTimeValues`, `DefaultSort`, `DefaultTime`, removed metadata keys, `ConfigValues.Time`, and `collectionScope.timeFilter`.
- [x] Keep the test package focused on symbols actually owned by `internal/sources/reddit`: source constants (`SourceName`, `SourceType`, `SignalIDPrefix`), supported metadata constants, `ConfigValues`, `deriveScope`, and `Stats`.
- [x] Update `deriveScope` assertions to use the current fields and defaults: `ConfigValues.TimeRange`, `collectionScope.timeRange`, default sort `new`, and default time range `all`.
- [x] Remove obsolete sort/time-value constant tests rather than introducing duplicate configuration constants into the Reddit collector package; the canonical supported-value API is `config.ValidRedditSortValues` and `config.ValidRedditTimeValues`, already tested in `internal/config/config_test.go`.
- [x] Source ID-prefix remains `rd` as declared in `types.go` — keep `TestSourceConstants` and parser tests asserting the stable `rd:<post-id>` format.

### Task 4: Format and verify the repaired main branch

- [x] Run `gofmt` on each modified Go file.
- [x] Verify no duplicate Reddit validation or memory test declarations remain with targeted symbol searches.
- [x] Remove the remaining duplicate Reddit CLI tests and reconcile stale `ConfigValues.Time` and `reddit.New` usage in `internal/cli/collect_test.go`.
- [x] Run focused config, memory, Reddit source, and CLI tests before the full suite, vet, lint, and build.

### Task 5: Address review findings

- [x] Add direct `RedditConfig.Validate` coverage for empty and unsupported sort/time values, including a `LoadConfig` integration case.
- [x] Forward configured Reddit sort and time values through `buildRedditCollector` and verify them at the outgoing listing request boundary.
- [x] Persist each collected `RawSignal` atomically under `raw-signals/` before recording its source ID and content hash in `memory.json`; do not advance a cursor or record deduplication state for failed writes.
- [x] Preserve successfully persisted partial results while returning joined collection and persistence errors.

## Validation Commands

gofmt -w internal/cli/collect.go internal/cli/collect_test.go internal/config/config.go internal/config/config_test.go internal/memory/memory_test.go internal/sources/reddit/parser_test.go internal/sources/reddit/types.go internal/sources/reddit/types_test.go
rg -n 'func \(c \*RedditConfig\) Validate|func TestRedditConfigValidate|func TestLoadConfigValidatesRedditConfig|func TestAddRedditRequests|func TestAddRedditCacheHits|func TestResolveCollectSources_Reddit|func TestStatsDelta_Reddit|func TestReportCollectSummary_Reddit|SupportedSortValues|ConfigValues\.Time' internal/cli internal/config internal/memory internal/sources/reddit
go test ./internal/config/...
go test ./internal/memory/...
go test ./internal/sources/reddit/...
go test ./internal/cli/...
go test ./...
go vet ./...
golangci-lint run ./...
go build ./cmd/signalforge/
