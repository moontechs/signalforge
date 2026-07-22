# SignalForge

SignalForge — automated problem discovery engine. Collects public signals from GitHub, Hacker News, and Stack Exchange, classifies them, clusters recurring problems, and generates evidence-backed product hypotheses.

## Architecture

```
cmd/signalforge/main.go          — CLI entrypoint (Cobra)
internal/
  cli/                           — CLI command implementations
  config/                        — Config loading/saving
  domain/                        — All domain types
  storage/                       — JSON/JSONL file storage
  memory/                        — Dedup tracking
  cache/                         — TTL-based response cache
  normalize/                     — Text normalization
  dedup/                         — Duplicate detection
  classify/                      — LLM-based classification
  clustering/                    — Jaccard + rule clustering
  jtbd/                          — Jobs To Be Done generation
  ideation/                      — Solution hypothesis generation
  producttype/                   — Product type classification
  scoring/                       — Problem/Solution scoring
  confidence/                    — Confidence calculation
  ranking/                       — Opportunity ranking
  pipeline/                      — Multi-stage pipeline orchestration
  limits/                        — Technical request limits
  report/                        — Human-readable output
  sources/
    github/                      — GitHub Issues + Discussions
    hackernews/                  — Hacker News Firebase API
    stackexchange/               — Stack Exchange API
    reddit/                      — Optional Reddit collector
  openrouter/                    — OpenRouter LLM client
  brightdata/                    — Bright Data SERP + Unlocker
prompts/                         — LLM prompt templates
testdata/                        — Test fixtures
```

## Tech Stack

- **Language:** Go 1.24+
- **CLI:** Cobra
- **Storage:** JSON files (config, domain objects) + JSONL (raw signals)
- **LLM:** OpenRouter (OpenAI-compatible API, free models supported)
- **APIs:** GitHub REST + GraphQL, HN Firebase, Stack Exchange, optional Reddit

## Conventions

### Code style
- Standard Go formatting (`gofmt`)
- Standard library over external dependencies
- `context.Context` as first arg
- `log/slog` for logging
- Error wrapping with `fmt.Errorf("context: %w", err)`
- No panic in production paths

### Testing
- `go test ./...` — must pass without API keys
- Fake implementations for all external APIs
- Each milestone has corresponding tests
- E2E tests use fake clients

### Git workflow (mandatory)
1. Every code change starts with a GitHub issue
2. Branch from issue: `feat/issue-N-description` or `fix/issue-N-description`
3. Conventional Commits for messages
4. PR with `Closes #N` in body
5. Code review before merge
6. No direct commits to main

### Project management
- Kanban board `signalforge` — all tasks tracked here
- Tasks created in kanban BEFORE GitHub issues
- GitHub issue created only when taking a task into work
- Post-MVP tasks are blocked, not deleted

### Ralphex usage
- Complex coding: `ralphex --config-dir /opt/ralphex-profiles/codex docs/plans/<plan>.md`
- Light tasks: `ralphex --config-dir /opt/ralphex-profiles/pi docs/plans/<plan>.md`
- Plans are markdown files in `.hermes/plans/` or `docs/plans/`

## Pipeline stages

```
collect → classify → cluster → discover → research → rank
```

Each stage is resumable. The `pipeline` command runs all stages sequentially.

## GitHub collector MVP

- The implemented CLI collection path is `signalforge collect --sources github --since 30d`
- GitHub collection requires `GITHUB_TOKEN` before network calls begin
- The collector reads public GitHub Issues through REST and Discussions through GraphQL
- Raw signals are appended under `raw-signals/`, and dedup state is persisted in `memory.json`
- Repeat runs skip previously seen GitHub source IDs and duplicate content hashes
- Cursor-aware request fields exist in the collector, but the current MVP CLI uses a since-window rather than saved resume cursors

## MVP scope (4 milestones)

| Milestone | What | Status |
|-----------|------|--------|
| M1: Foundation | go.mod, domain, config, storage, memory, cache, CLI skeleton | In progress |
| M2: Collection | GitHub, HN, StackExchange collectors + collect command | Todo |
| M3: Intelligence | OpenRouter classification, clustering, JTBD, solutions | Todo |
| M4: Pipeline | pipeline command, export, rank, stats, e2e tests, README | Todo |

Post-MVP: Reddit collector, Bright Data SERP/Unlocker, competitor research, solution scoring, brainstorm command.

## Key design decisions

1. **No database** — JSON files + JSONL. SQLite/Postgres are post-MVP.
2. **No embeddings in MVP** — Jaccard similarity + weighted token overlap for clustering.
3. **Scores are heuristics** — ProblemScore and SolutionScore are weighted averages, not facts.
4. **Bright Data is post-MVP** — requires paid subscription.
5. **Reddit is optional** — disabled by default, requires explicit opt-in.
6. **OpenRouter free models** — supported but may have reliability issues. Fallback models required.
