# AGENTS.md — SignalForge

## Overview

SignalForge is a Go CLI application that discovers recurring user problems from public sources. It follows a pipeline: collect signals → classify → cluster → generate product hypotheses.

## Quick Start

```bash
# Build
go build ./cmd/signalforge/

# Init (creates ~/.signalforge/)
./signalforge init

# Check configuration
./signalforge doctor

# Collect signals from sources
./signalforge collect --sources github

# Classify raw signals
./signalforge classify

# Cluster problems
./signalforge cluster

# Generate solutions
./signalforge discover

# Full pipeline
./signalforge pipeline --sources github,hn,stackexchange --since 30d
```

## Required environment variables

| Variable | Required | Description |
|----------|----------|-------------|
| `GITHUB_TOKEN` | Yes | GitHub personal access token (repo scope) |
| `OPENROUTER_API_KEY` | No (for collection) | OpenRouter API key (required for classification) |
| `OPENROUTER_MODEL` | No | OpenRouter model override (default: from config) |
| `BRIGHTDATA_API_KEY` | No (post-MVP) | Bright Data API key |
| `SIGNALFORGE_HOME` | No | Overrides ~/.signalforge data directory |

## GitHub collector MVP behavior

- `collect` currently supports `github` end to end in the CLI flow
- GitHub collection requires `GITHUB_TOKEN` and only reads public Issues and Discussions
- `--since` accepts Go duration values such as `24h` and day shorthands such as `7d`
- Deduplication is persisted in `memory.json`, so repeat runs skip already-seen source IDs and duplicate content hashes
- Initial MVP runs use a since-window and per-run limits; cursor inputs are accepted internally but not persisted as resumable state yet

## Running tests

```bash
# All tests (no API keys needed)
go test ./...

# With verbose output
go test -v ./...

# Specific package
go test ./internal/storage/...

# Run linter
go vet ./...
```

### Linting

This project uses **golangci-lint v1.64+** with strict rules.

- Config: `.golangci.yml` at repo root
- **Pre-push hook:** `.githooks/pre-push` runs `golangci-lint run ./...` automatically on every `git push` and blocks if lint fails. Enable with `git config core.hooksPath .githooks` (one-time setup after clone).
- **Run locally:** `golangci-lint run ./...` before every commit
- **Auto-fix:** `golangci-lint run --fix ./...` for formatting, imports, and simple fixes
- **Install:** `go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest`

## Project structure rules

- `internal/` — not importable from outside the module
- `cmd/signalforge/main.go` — only CLI entrypoint
- `prompts/` — LLM prompt templates (loaded at runtime, not embedded)
- `testdata/` — fixtures for tests (not loaded at runtime)

## Kanban board

All tasks tracked on the `signalforge` kanban board. See `hermes kanban list --board signalforge`.
Tasks are organized by milestone (M1-M4). Post-MVP tasks are blocked.

## Key constraints

- Do NOT add database dependencies (no SQLite, no Postgres, no Redis)
- Do NOT add embeddings or vector search in MVP
- Do NOT calculate API costs or token prices
- Do NOT scrape private pages or require authentication for sources
- All HTTP clients must have timeouts, retries, and context cancellation
- JSON writes must be atomic (temp file → sync → rename)
- Cache keys must not include secrets
- Never store API tokens in logs, exports, or JSON data

## Architecture patterns

Each external source implements `SourceCollector` interface:
```go
type SourceCollector interface {
    Name() string
    Collect(ctx context.Context, req CollectRequest) ([]RawSignal, error)
}
```

Each source has its own package under `internal/sources/` with:
- `client.go` — HTTP client
- `parser.go` — response parsing
- `errors.go` — typed errors

LLM operations go through `OpenRouter` package with:
- Free model support (`:free` suffix)
- Fallback models
- Retry with exponential backoff
- JSON validation + repair (max 1 repair attempt)

## Scoring model

ProblemScore: weighted average of 8 dimensions (0-10 scale), multiplied by 10.
SolutionScore: weighted average of 9 dimensions (0-10 scale), multiplied by 10.
Confidence: 0-100, calculated separately from scores.
Recommendation: rules-based from ProblemScore + SolutionScore + confidence + risks.
