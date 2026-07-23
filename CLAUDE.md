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
- Strict linting via `.golangci.yml` — `golangci-lint run ./...` must pass before pushing

### Linting (mandatory)
- **golangci-lint v1.64+** enforced via pre-push hook — push blocked if linting fails
- Config: `.golangci.yml` at repo root
- Run `golangci-lint run ./...` before every commit
- Auto-fix: `golangci-lint run --fix ./...` for formatting and simple fixes
- **Do not add `//nolint` directives** without a comment explaining why
- **Do not ignore linter warnings** — fix them or justify with a nolint comment
- **Pre-push hook:** `.githooks/pre-push` runs `golangci-lint run ./...` automatically and blocks pushes if lint fails. Enable with `git config core.hooksPath .githooks` (one-time setup after clone).

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

### Source collectors
- Each external source implements `SourceCollector` interface under `internal/sources/<name>/`
- Package structure follows: `client.go` (HTTP), `parser.go` (response→domain mapping), `errors.go` (typed errors), `collector.go` (orchestration)
- **Collection strategy:** If `Config.GitHub.Repositories` is non-empty, use per-repo API (`GET /repos/{owner}/{repo}/issues`) for precise collection; if empty, use search API (`GET /search/issues`) for broader coverage
- **Sort order:** Collect in ascending update time (`sort=updated&direction=asc`) so repeated runs pick up only new/updated items since the last cursor
- **Rate-limit tracking:** REST and GraphQL have separate rate-limit counters. Both check `x-ratelimit-remaining` headers before making requests. Secondary rate limits (HTTP 403 + `Retry-After`) are handled identically to primary limits (backoff + retry)
- **Conditional requests:** Store `ETag` / `Last-Modified` headers per endpoint. Send `If-None-Match` / `If-Modified-Since` on subsequent requests. HTTP 304 extends cache TTL without replacing content
- **Comments pagination:** Fetch comments via paginated endpoints (`per_page=100`) with a configurable `MaxCommentsPerItem` cap, ordered by `created_at` asc
- **Fake transport for tests:** Clients accept a `transport` interface; tests inject `fakeTransport` with registered responses, avoiding httptest.NewServer overhead and external credentials

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

## Hacker News collector (M2-T6)

- HN collector is fully wired: `signalforge collect --sources hackernews --since 30d`
- No authentication required — uses HN Firebase API at `https://hacker-news.firebaseio.com/v0/`
- Supported feeds: `askstories`, `showstories`, `newstories`, `topstories`, `beststories`
- Default feeds (in config): `askstories`, `showstories`, `newstories`
- **Collection pipeline:** scan feeds → dedup IDs across feeds → bounded worker pool (5 workers via `chan struct{}` semaphore) → fetch items → `eligibleStory()` filter (type, dead/deleted, score, since) → flatten comments via BFS (max depth 50) → sort by time descending → apply max-items cap
- **Comment flattening:** BFS traversal using a queue of `commentRef{id, depth}` refs. Dead/deleted/non-comment items are skipped and their children are not enqueued. Max depth 50 enforced. Configurable `maxComments` cap
- **BFS ordering:** comments are returned in breadth-first order (all depth-1, then depth-2, etc.). Dead comments at any level cause their entire subtree to be skipped (since kids are not enqueued from a dead item)
- **TTL caching:** 5 min for feed lists, 24h for items. Cache stored as `cachedResponse{Body, CollectedAt}` on disk under `cache/hackernews/`
- **No conditional requests** — HN Firebase API does not support ETags or If-Modified-Since
- **Error handling:** typed sentinel errors (`ErrDisabled`, `ErrInvalidFeed`, `ErrRequestCap`, `ErrMalformedResponse`, `ErrRetriesExhausted`). Partial failures joined with `errors.Join`
- **Dedup:** by item ID across feeds within a single run (`map[int]bool`). Content hash from title + body + sorted comment bodies
- **Stats:** `Stats{Requests, CacheHits}` exposed by both `client` and `Collector`. Tracked in memory via `AddHNRequests`/`AddHNCacheHits`
- **Test patterns:** `fakeTransport` with sequential responses per URL, `testClient()` helper, `testCollector()` helper. All tests use `t.Parallel()` where safe. Test fixtures in `testdata/hackernews/`

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
