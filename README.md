# SignalForge

**Automated problem discovery engine** — collects public signals from GitHub, Hacker News, and Stack Exchange, classifies them, clusters recurring problems, and generates evidence-backed product hypotheses.

## Quick Start

```bash
# Initialize the SignalForge data directory
signalforge init

# Check configuration
signalforge doctor

# Full pipeline
signalforge pipeline --sources github,hn,stackexchange --since 30d

# Or run individual stages:
signalforge collect --sources github --since 30d
signalforge classify
signalforge cluster
signalforge discover
signalforge rank
```

## Commands

### `pipeline` — Run the full pipeline

Runs all stages in sequence: collect → classify → cluster → discover → rank. Each stage checks for existing output data and skips if already present (unless `--force` is set).

```bash
# Run pipeline with GitHub and Hacker News
signalforge pipeline --sources github,hn --since 30d

# Dry-run: see what stages would run
signalforge pipeline --dry-run

# Force re-run all stages
signalforge pipeline --force
```

**Flags:**
| Flag | Default | Description |
|------|---------|-------------|
| `--sources` | `github` | Comma-separated sources |
| `--since` | `30d` | Look back window |
| `--dry-run` | `false` | Print plan and exit |
| `--force` | `false` | Re-run all stages |

### `rank` — Rank problem clusters and solutions

Loads problem clusters and solution hypotheses, applies score and confidence thresholds, and prints them sorted by score descending.

```bash
# Show all ranked results
signalforge rank

# Filter by minimum scores
signalforge rank --problem-score 5.0 --confidence 0.3

# Show top 10 solutions
signalforge rank --solution-score 4.0 --limit 10
```

**Flags:**
| Flag | Default | Range | Description |
|------|---------|-------|-------------|
| `--problem-score` | `0` | 0.0–10.0 | Minimum problem score |
| `--solution-score` | `0` | 0.0–10.0 | Minimum solution score |
| `--confidence` | `0` | 0.0–1.0 | Minimum confidence |
| `--limit` | `0` | ≥0 | Max results (0 = no limit) |

### `stats` — Show research statistics

Displays collection, classification, clustering, and API usage statistics from `memory.json`.

```bash
# Human-readable stats
signalforge stats

# Machine-readable JSON
signalforge stats --json
```

**Flags:**
| Flag | Default | Description |
|------|---------|-------------|
| `--json` | `false` | Output as JSON |

### `export` — Export research data

Exports clusters, solutions, and statistics in markdown, JSON, or CSV format.

```bash
# Markdown report to stdout
signalforge export --format markdown

# JSON export to file
signalforge export --format json --output report.json

# CSV export to file
signalforge export --format csv --output report.csv
```

**Flags:**
| Flag | Default | Description |
|------|---------|-------------|
| `--format` | (required) | Output format: `markdown`, `json`, or `csv` |
| `--output` | stdout | Output file path |

### `collect` — Collect raw signals

```bash
signalforge collect --sources github,hn,stackexchange --since 30d
signalforge collect --sources github --since 7d --max-items 50 --force
signalforge collect --sources github --dry-run
```

### `classify` — Classify raw signals

```bash
signalforge classify
signalforge classify --limit 50 --model google/gemini-pro:free
signalforge classify --dry-run
```

### `cluster` — Cluster related problems

```bash
signalforge cluster
signalforge cluster --threshold 0.4
signalforge cluster --dry-run
```

### `discover` — Generate product hypotheses

```bash
signalforge discover
signalforge discover --dry-run
signalforge discover --no-semantic
```

### Other commands

- `init` — Initialize SignalForge directory structure
- `doctor` — Check configuration and environment
- `list` — List stored items (signals, clusters, etc.)
- `show` — Show details of a specific item

## Configuration

SignalForge stores configuration at `~/.signalforge/config.yaml`. Created on first `init`.

### Data directory structure

```
~/.signalforge/
├── config.yaml        # Configuration
├── memory.json        # Persistent memory (dedup, stats, cursors)
├── raw-signals/       # Collected raw signals
├── problem-signals/   # Classified problem signals
├── clusters/          # Problem clusters
├── prompts/           # LLM prompt templates
└── backups/           # Automatic backups
```

## Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `GITHUB_TOKEN` | For GitHub collection | GitHub personal access token |
| `OPENROUTER_API_KEY` | For classification/discover | OpenRouter API key |
| `OPENROUTER_MODEL` | No | Model override |
| `SIGNALFORGE_HOME` | No | Override data directory |

## Scoring Model

**ProblemScore:** Weighted average of 8 dimensions (0–10 scale), multiplied by 10.
**SolutionScore:** Weighted average of 9 dimensions (0–10 scale), multiplied by 10.
**Confidence:** 0–100, calculated separately from scores.
**Recommendation:** Rules-based from ProblemScore + SolutionScore + confidence + risks.

## Building

```bash
go build ./cmd/signalforge/
```

## Running Tests

```bash
# All tests
go test ./...

# With verbose output
go test -v ./...

# Specific package
go test ./internal/storage/...

# Run linter
go vet ./...
```

## License

MIT