# Plan: CLI Skeleton — init, doctor, list, show

## Goal
Build the Cobra CLI skeleton for SignalForge. The existing codebase has `internal/config/`, `internal/storage/`, `internal/memory/`, and `internal/domain/` packages. This plan adds the CLI entrypoint (`cmd/signalforge/main.go`) and command implementations (`internal/cli/`).

## Validation Commands
- `cd /tmp/signalforge-clone && go build ./cmd/signalforge/`
- `cd /tmp/signalforge-clone && go vet ./...`
- `cd /tmp/signalforge-clone && go test ./...`

### Task 1: Add Cobra dependency and create entrypoint
- [ ] `cd /tmp/signalforge-clone && go get github.com/spf13/cobra@latest`
- [ ] Create `cmd/signalforge/main.go` with root Cobra command that includes `PersistentPreRunE` for SIGINT handling and common flags (--config-dir, --verbose)
- [ ] Root command has `--help` that shows all subcommands
- [ ] Root command has a `Version` field set to `0.1.0`

### Task 2: Create init command
- [ ] Create `internal/cli/init.go` with `initCmd` Cobra command
- [ ] `initCmd` calls `config.GetSignalForgeDir()` to get the target directory
- [ ] Creates the full directory structure using `config.DefaultDirStructure()` — creates all directories listed
- [ ] Creates a default `config.json` using `config.DefaultConfig()` + `config.SaveConfig()`
- [ ] Prints success message with the path to `~/.signalforge/`
- [ ] If directory already exists, prompts for confirmation (--force flag to skip)
- [ ] Returns error if home directory is unreachable

### Task 3: Create doctor command
- [ ] Create `internal/cli/doctor.go` with `doctorCmd` Cobra command
- [ ] Checks that `~/.signalforge/` exists and is readable/writable
- [ ] Checks that `config.json` exists and is valid JSON via `config.LoadConfig()`
- [ ] Checks that all directories from `config.DefaultDirStructure()` exist
- [ ] Checks required env vars: `GITHUB_TOKEN`, `OPENROUTER_API_KEY` (warn on missing, not error)
- [ ] Checks that `memory.json` can be loaded
- [ ] Reports results in a structured table format with ✅/❌/⚠️ indicators
- [ ] Returns exit code 1 if critical checks fail (missing dir, missing config)

### Task 4: Create list command
- [ ] Create `internal/cli/list.go` with `listCmd` Cobra command
- [ ] `listCmd` has a positional argument for the type: `signals`, `clusters`, `jobs`, `ideas`, `runs`, `all`
- [ ] Uses `storage.New()` + `storage.ListFiles()` to find matching JSON files
- [ ] Displays items in a table format: ID, title, created_at, source
- [ ] `list all` shows all types grouped by category
- [ ] Handles empty directories gracefully (prints "No items found")
- [ ] Supports `--limit` and `--offset` flags for pagination

### Task 5: Create show command
- [ ] Create `internal/cli/show.go` with `showCmd` Cobra command
- [ ] `showCmd` takes two positional args: `<type> <id>` (type: signals, clusters, jobs, ideas, runs)
- [ ] Looks up the file by type and ID in the storage directory
- [ ] Displays all fields of the item in a readable format (key: value pairs)
- [ ] Handles not-found gracefully with "Item not found" message
- [ ] Supports `--json` flag to output raw JSON

### Task 6: Create stub commands (analyze, brainstorm)
- [ ] Create `internal/cli/analyze.go` with a stub `analyzeCmd` — prints "Not implemented in MVP" and exits 0
- [ ] Create `internal/cli/brainstorm.go` with a stub `brainstormCmd` — prints "Not implemented in MVP" and exits 0

### Task 7: Wire all commands to root
- [ ] In `cmd/signalforge/main.go`, add all commands to root: init, doctor, list, show, analyze, brainstorm
- [ ] Set up proper command grouping and descriptions
- [ ] Verify `go build ./cmd/signalforge/` passes
- [ ] Verify `go vet ./...` passes
- [ ] Verify `go test ./...` passes