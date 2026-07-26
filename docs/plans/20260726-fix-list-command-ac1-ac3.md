# Plan: Fix TEST-8 AC1 (raw-signals alias) and AC3 (detailed output)

## Bug 1: `raw-signals` not accepted (AC1)

`list.go` has `validTypes = {"signals": "raw-signals", ...}` — maps "signals" to dir "raw-signals", but the command doesn't accept "raw-signals" as input.

**Fix:** Add alias `"raw-signals": "raw-signals"` to `validTypes`.

## Bug 2: Output lacks source, title, URL, created_at (AC3)

Plan cli-skeleton.md Task 4 requires: "Displays items in a table format: ID, title, created_at, source"

Current output: `0000be33...  (modified: 2026-07-25T22:23:37Z, size: 2379 bytes)`

**Fix:** Modify `listItems()` to read each JSON file, parse RawSignal struct, and output:
`ID  source: HN  title: "..."  url: "..."  created: 2026-07-25T22:23:37Z`

## Files to modify

1. **`internal/cli/list.go`** — add alias + improve output
2. **`internal/cli/list_test.go`** — add tests for alias and detailed output

## Implementation Steps

### Task 1: Add `raw-signals` alias

- [ ] In `validTypes` map, add `"raw-signals": "raw-signals"` entry
- [ ] Also add alias to help text: "signals (raw-signals)"

### Task 2: Improve signal listing output

- [ ] Import `encoding/json` and `github.com/moontechs/signalforge/internal/domain` in `list.go`
- [ ] In `listItems()`, after getting file path, read and parse JSON into `domain.RawSignal`
- [ ] Format output as: `ID  source: {Source}  title: "{Title[:80]}..."  url: {URL}  created: {CreatedAt.Format(time.RFC3339)}`
- [ ] If JSON parse fails or file not a signal, fall back to current format (ID + modified + size)
- [ ] Keep limit/offset logic unchanged

### Task 3: Test coverage

- [ ] Add `TestList_RawSignalsAlias` — verify `raw-signals` type accepted
- [ ] Add `TestList_SignalDetailFields` — verify source, title, URL, created_at in output
- [ ] Add `TestList_SignalJsonParseError` — verify fallback for non-signal JSON files

### Task 4: Regression validation

- [ ] `go test ./internal/cli/... -v -count=1`
- [ ] `go test ./... -count=1`
- [ ] `go vet ./...` and `golangci-lint run ./...`
- [ ] `go build ./cmd/signalforge/`
- [ ] **All git commands use `--no-verify`**
