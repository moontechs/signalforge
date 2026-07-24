# Plan: Implement SignalForge M4-T12 CLI workflow, reporting, exports, E2E coverage, and documentation

## Context
SignalForge Go CLI app. Core domain types in `internal/domain/types.go`. Storage helpers in `internal/storage/storage.go`. Existing CLI commands in `internal/cli/` (collect.go, classify.go, cluster.go, brainstorm.go, rank.go etc.). The `rank` command (Task 1) is already implemented and registered.

### Key patterns
- Each command: `var XxxCmd = newXxxCmd()` with `RunE: runXxx`
- `runXxx(cmd, args)` parses flags, gets dir via `config.GetSignalForgeDir()`, calls `ensureStorageLayout(dir)`, creates env struct, calls `executeXxx(cmd, env)`
- `executeXxx` functions load data from storage, process, save results
- Storage: `storage.New(dir)` → `store.LoadJSON()`, `store.SaveJSON()`, `store.ListFiles()`, `store.Exists()`
- Memory: `memory.DefaultMemory` from `internal/memory/`
- `cmd.OutOrStdout()` for output, `cmd.ErrOrStderr()` for warnings
- `commandContext(cmd)` for ctx
- `loadProblemClusters(store)` from brainstorm.go
- `DiscoverResult` struct in brainstorm.go: `{JTBDs, Solutions, Duplicates}`
- `ResearchStats` in domain/types.go
- `ensureStorageLayout(dir)` in collect.go line 634

### State: rank.go already done (Task 1). Tasks 2-7 remain.

---

### Task 2: Add the `pipeline` command

- [x] Create `internal/cli/pipeline.go` with a Cobra `pipeline` command.
- [x] Add flags: `--sources` (string, default "github"), `--since` (string, default "30d"), `--dry-run` (bool), `--force` (bool).
- [x] Implement stage execution by calling the existing execute functions directly (not via subprocesses). Each stage writes to `cmd.OutOrStdout()`.
- [x] Stage order: collect → classify → cluster → discover → rank.
- [x] **Resumability**: before each stage, check if its output data already exists (raw-signals/ for collect, problem-signals/ for classify, clusters/ for cluster, discover.json for discover, skip rank check). If data exists, skip that stage with a message. `--force` re-runs all stages.
- [x] Stop on first stage error, print which stage failed and the error.
- [x] Print per-stage progress: `[pipeline] Stage 1/5: collect...` → `[pipeline] Stage 1/5: collect completed (N signals)`.
- [x] Dry-run: print the planned pipeline stages and exit without execution.
- [x] For pipeline, build a minimal collectEnv, classifyEnv, clusterEnv, discoverEnv, rankEnv for each stage. Use the same dir/store/config/memory setup.
- [x] Register `pipeline` with rootCmd in `cmd/signalforge/main.go`.

### Task 3: Add the `stats` command

- [x] Create `internal/cli/stats.go` with a Cobra `stats` command.
- [x] Load `memory.json` and read `ResearchStats` from it.
- [x] Print formatted stats: raw signals collected/skipped, problem signals found, noise, clusters, jobs, ideas, duplicates, per-source requests, per-source cache hits, LLM requests.
- [x] Handle missing memory.json gracefully (print "No stats available. Run 'signalforge collect' first.").
- [x] Add `--json` flag for machine-readable output.
- [x] Register `stats` with rootCmd in `cmd/signalforge/main.go`.

### Task 4: Add the `export` command with markdown, json, csv

- [ ] Create `internal/cli/export.go` with a Cobra `export` command.
- [ ] Add flags: `--format` (string, required: "markdown", "json", "csv"), `--output` (string, optional: writes to file instead of stdout).
- [ ] Validate format before processing.
- [ ] Read data sources: clusters from `clusters/*.json`, solutions from `discover.json`, stats from `memory.json`.
- [ ] **Markdown**: Generate a research report with timestamp, stats summary, ranked clusters table (title, score, confidence, sources), solutions section (title, score, recommendation, product type).
- [ ] **JSON**: Serialize the combined data (`{clusters, solutions, stats, exported_at}`) as pretty-printed JSON.
- [ ] **CSV**: Write header row + one row per cluster with columns: id, title, problem_total, confidence, sources, signal_count, solution_title, solution_score, recommendation.
- [ ] Atomic file write: write to temp file, fsync, rename (use `storage.SaveJSON` pattern).
- [ ] Handle empty data: valid empty markdown/json/csv output.
- [ ] Register `export` with rootCmd in `cmd/signalforge/main.go`.

### Task 5: Add E2E tests with fake clients

- [ ] Create `internal/cli/e2e_test.go` with integration tests.
- [ ] Add test helpers: `newTestCommand()` that creates a cobra command with test output buffer, `seedTestData()` that creates raw signals, problem signals, clusters, and discover.json in a temp SIGNALFORGE_HOME.
- [ ] Test: `rank` with various thresholds, empty results, invalid flags.
- [ ] Test: `pipeline` dry-run, full pipeline with fake data, failure propagation.
- [ ] Test: `stats` with seeded memory.json, with missing memory.json, with --json flag.
- [ ] Test: `export` with markdown/json/csv, with --output file, with empty data, with invalid --format.
- [ ] All tests run in parallel, use `t.TempDir()`, no API keys required, no HTTP calls.

### Task 6: Update README.md

- [ ] Create `README.md` with:
  - Project overview
  - Quick start (init → collect → classify → cluster → discover → rank → export)
  - Pipeline command documentation with examples
  - Rank command documentation with flags
  - Stats command documentation
  - Export command documentation with --format and --output flags
  - Configuration section
  - Environment variables section
  - Keep consistent with existing AGENTS.md style

### Task 7: Format, lint, and verify

- [ ] Run `gofmt -w .` on all changed files.
- [ ] Run `go vet ./...` and fix any issues.
- [ ] Run `golangci-lint run ./...` and fix any issues.
- [ ] Run `go test ./... -count=1` and verify all tests pass.
- [ ] Run `go build ./cmd/signalforge/` and verify build succeeds.
- [ ] Verify `signalforge --help` shows all new commands.

## Validation Commands

```bash
go build ./cmd/signalforge/
go vet ./...
golangci-lint run ./...
go test ./... -count=1 -v

go run ./cmd/signalforge --help
go run ./cmd/signalforge rank --help
go run ./cmd/signalforge pipeline --help
go run ./cmd/signalforge stats --help
go run ./cmd/signalforge export --help
```