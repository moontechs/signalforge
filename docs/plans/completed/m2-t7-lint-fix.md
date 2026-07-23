# Plan: M2-T7 Stack Exchange collector — lint fixes

Fix remaining golangci-lint issues in `internal/sources/stackexchange/` after auto-fix.

## Current lint issues (auto-fix already applied)

1. **dupl** — `answers()` and `comments()` in client.go lines 344-391 are near-duplicates. Merge into a shared helper `fetchItems` that takes the path suffix ("answers" vs "comments") and the response type.
2. **gocognit** — `Collect()` (50 > 30) and `get()` (34 > 30) have high cognitive complexity. Extract smaller helpers.
3. **gosec** — `rand.Intn` → use `crypto/rand` backed jitter (or nolint with explanation)
4. **revive** — unused `body` params in `response()` and `bodyResponse()` test helpers → rename to `_`
5. **unused** — `callCountFor` is unused → remove it
6. **gocritic** — `hugeParam`: pass `questionDTO`, `answerDTO`, `commentDTO`, `ConfigValues` by pointer. `rangeValCopy`: use index or pointer in loops. `unnamedResult`: name return params.
7. **inamedparam** — `Do(*http.Request)` needs named params
8. **unparam** — `WithCache` result unused → make it void
9. **wrapcheck** — unwrapped `io.ReadAll` error → wrap with `%w`

## Tasks

### Task 1: Fix client.go structural issues
- Merge `answers()` and `comments()` into a shared `fetchItems(ctx, site, questionID, page, pageSize, suffix string, target interface{})` helper
- Extract smaller methods from `get()` to reduce cognitive complexity from 34 to ≤30
- Rename `Do(*http.Request)` → `Do(req *http.Request)`
- `WithCache` → return void, or use the return value
- `rand.Intn` → use `//nolint:gosec` with comment (crypto/rand not needed for jitter)
- Wrap `io.ReadAll` error with `%w`

### Task 2: Fix client_test.go and fake_transport.go
- Rename unused `body` params → `_` in `response()` and `bodyResponse()`
- Remove unused `callCountFor`

### Task 3: Fix collector.go
- Extract methods from `Collect()` to reduce cognitive complexity from 50 to ≤30
- Fix `rangeValCopy` in signal iteration loop

### Task 4: Fix parser.go
- Pass `questionDTO`, `answerDTO`, `commentDTO` by pointer (hugeParam)
- Fix `rangeValCopy` in questions loop
- Name return params in `parseQuestionsWithStats`

### Task 5: Verify
- [x] `go build ./internal/sources/stackexchange/...`
- [x] `go test ./internal/sources/stackexchange/... -count=1`
- [x] `golangci-lint run ./...` exit 0
- [x] `go build ./cmd/signalforge/`
- [x] Commit all changes with message: "fix: resolve lint issues in SE collector package"