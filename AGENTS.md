# SignalForge — Agent Instructions

You are coding for **SignalForge**, a Go CLI that discovers recurring user problems from public sources (GitHub, HN, Stack Exchange). Collect → classify → cluster → generate product hypotheses.

## Hard Constraints (DO NOT VIOLATE)

- **No database dependencies** — no SQLite, Postgres, Redis, or any persistent DB. Storage is JSON files + JSONL only.
- **No vector search / embeddings in MVP** — clustering uses Jaccard + weighted token overlap.
- **No scraping private pages** — all sources are public APIs only. No auth-required endpoints.
- **No API cost/token price calculations** — never count or log token usage or cost.
- **No panic in production paths** — always return errors.
- **No storing API tokens in logs, exports, or JSON output** — never leak secrets.

## Tech Stack

- **Language:** Go 1.24+
- **CLI framework:** Cobra
- **Storage:** `encoding/json` for config/domain objects, JSONL for raw signals
- **LLM:** OpenRouter (OpenAI-compatible). Free models (`:free` suffix) supported.
- **APIs:** GitHub REST + GraphQL, HN Firebase, Stack Exchange API

## Project Structure Rules

- `internal/` — never importable from outside the module
- `cmd/signalforge/main.go` — only CLI entrypoint (Cobra root command)
- `internal/sources/<name>/` — one package per external source
- `prompts/` — LLM prompt templates (loaded at runtime from disk, NOT embedded)
- `testdata/` — test fixtures (not loaded at runtime)

Each source package follows this structure:
```
client.go      — HTTP client
parser.go      — response → domain mapping
errors.go      — typed sentinel errors
collector.go   — SourceCollector orchestration
```

## Coding Conventions

### General
- Standard library over external dependencies. Verify `go.sum` has no new deps before adding.
- `context.Context` as first function argument
- `log/slog` for all logging (not `log`, not `fmt.Print`)
- Error wrapping: `fmt.Errorf("context: %w", err)` — always add context
- No global state — pass dependencies explicitly

### Interface patterns
- Each external source implements `SourceCollector`:
  ```go
  type SourceCollector interface {
      Name() string
      Collect(ctx context.Context, req CollectRequest) ([]RawSignal, error)
  }
  ```
- HTTP clients accept a `transport` interface for testability (fakeTransport, not httptest.NewServer)
- All HTTP clients: timeouts, retries, context cancellation, typed errors

### LLM calls
- Go through `internal/openrouter/` package
- Support free models (`:free` suffix)
- Implement fallback models
- Retry with exponential backoff
- JSON validation + repair (max 1 attempt, then error)

### JSON file I/O
- **Atomic writes only** — write to temp file → `Sync()` → rename over target
- Never write directly to the target path
- Cache keys must not include secrets

## Linting (MANDATORY — pre-push hook blocks pushes)

```bash
golangci-lint run ./...          # before every commit
golangci-lint run --fix ./...    # auto-fix formatting/imports
golangci-lint run ./...          # verify fix didn't break anything
```

- **Pre-push hook** at `.githooks/pre-push` — runs linter, blocks on failure.
- Enable: `git config core.hooksPath .githooks` (one-time after clone).
- **Never add `//nolint` without a comment explaining why.**
- **Lint fix order:** typecheck errors first → auto-fix → structural fixes → nolint for intentional patterns → tests pass.

## Testing

- `go test ./...` — must pass without API keys
- **Never use real API calls in tests** — fake implementations for all external APIs
- Sources: use `fakeTransport` (registered responses per URL pattern), NOT `httptest.NewServer`
- E2E tests use fake clients
- `t.Parallel()` where safe
- Test fixtures go in `testdata/<source-name>/`

## Git Workflow (MANDATORY)

1. Every code change starts with a GitHub issue
2. Branch from issue: `feat/issue-N-description` or `fix/issue-N-description`
3. Conventional Commits for commit messages
4. PR body: `Closes #N` to auto-close issue on merge
5. Code review before merge
6. No direct commits to main

## Common Gotchas (read before coding)

### Pre-push hook traps
- First push of a PR that ADDS the hook itself: use `git push --no-verify -u origin HEAD` once. After that, every push runs the hook.
- `golangci-lint run --fix` can remove needed imports (e.g. drops `fmt` when `fmt.Errorf` becomes `errors.New`). Always run `go test ./...` after `--fix`.

### OpenRouter auth
- `OPENROUTER_API_KEY` must be set before any LLM call. Source `.env` at session start.
- The `.env` file is NOT auto-sourced by the shell. Always `source /home/app/.hermes/profiles/pm/.env` before running.

### Codex CLI auth
- ChatGPT session tokens expire ~2 days. If codex fails with "stream disconnected", use pi profile instead.
- Codex profiles use `codex_sandbox = workspace-write` (stable sandbox). Do NOT set `danger-full-access`.
- Direct codex CLI: always pass `--sandbox workspace-write` (or `--dangerously-bypass-approvals-and-sandbox` if sandbox is broken).

### Ralphex execution
- **Plan creation:** `ralphex-headless-plan --profile-dir /opt/ralphex-profiles/<executor>-planning "<query>"`
- **Plan execution:** `ralphex --config-dir /opt/ralphex-profiles/<executor> docs/plans/<plan>.md`
- Always use kanban workspace as working directory (not manual clone) — sandbox-safe.
- Always push branch before ralphex run to avoid losing work on sandbox cleanup.

### Scoring model reminder
- ProblemScore: weighted avg of 8 dimensions (0-10) × 10
- SolutionScore: weighted avg of 9 dimensions (0-10) × 10
- Confidence: 0-100 (separate from scores)
- Recommendation: rules-based from scores + confidence + risks