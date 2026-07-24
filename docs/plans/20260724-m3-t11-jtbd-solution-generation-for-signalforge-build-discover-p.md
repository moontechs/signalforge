# Plan: M3-T11 — JTBD and Solution Generation

### Task 1: Create discover package with LLM-driven generators

- [x] Create `internal/discover/` package with:
  - `generator.go` — JTBD generator that takes a `domain.ProblemCluster` and produces `domain.JobToBeDone` records via OpenRouter LLM calls
  - `solver.go` — solution generator that takes a `domain.JobToBeDone` and produces 3+ `domain.SolutionHypothesis` records with distinct product types
  - `classifier.go` — no-product classifier that returns `domain.ProductTypeNoProduct` for JTBDs not worth solving
  - `dedup.go` — deterministic idea deduplication using normalized JTBD/problem/audience/product-type signals (no embeddings, testable without network)
- [x] Use the existing `domain.JobToBeDone`, `domain.SolutionHypothesis`, `domain.ImplementationAnalysis`, `domain.SolutionScorecard`, `domain.ProductType`, `domain.Competitor`, `domain.Evidence` types — do NOT redefine them
- [x] Use the existing `domain.LLMClient` interface and `openrouter.Client` for all LLM calls (M3-T9 seam)
- [x] JTBD prompt: From cluster's Problem, TargetUsers, Contexts, RepresentativeSignalIDs, SignalCount, IndependentSources, Keywords, Entities, Actions, FirstObservedAt, LastObservedAt, ProblemScore, Confidence — produce 1-3 JTBDs in structured JSON
- [x] JTBD output validation: each must have Situation, Motivation, ExpectedOutcome, TargetUsers, the rendered canonical statement `When [situation], [user] wants to [motivation], so they can [outcome]`
- [x] Solution prompt: From each JTBD, produce 3+ distinct solutions with different product types. Each must include Title, Summary, ProductType (validated against `domain.IsValidProductType`), ProductTypeReason, TargetUser, Problem, ProposedSolution, CoreWorkflow, Differentiation, MustHaveFeatures, Competitors, Implementation (complexity/effort/risk), Strengths, Weaknesses, Risks, Unknowns
- [x] No-product classifier: evaluate JTBD feasibility before solution generation. If not worth solving, return `ProductTypeNoProduct` with rationale. Do NOT produce solution hypotheses for no-product JTBDs
- [x] Deduplication: exact-match on normalized title+product-type combination; conservative threshold (same title + same product type = duplicate). Record duplicate relationship for audit
- [x] All generators accept `context.Context` for cancellation, use `openrouter.Client` for completions with JSON validation + repair (1 repair attempt max)

### Task 2: Wire the discover CLI command

- [x] Replace the existing `internal/cli/brainstorm.go` stub with a full `signalforge discover` command using `cobra`
- [x] Register `discover` subcommand in `cmd/signalforge/main.go` following existing patterns (CollectCmd, ClusterCmd, etc.)
- [x] Command flags: `--limit` (max clusters to process), `--dry-run`, `--no-semantic` (skip LLM calls)
- [x] Implementation: load persisted clusters from storage → initialize OpenRouter client → run JTBD generation → no-product classification → solution generation → deduplication → save results
- [x] The `discover` command should save results to `discover.json` (JTBDs + solutions) under the SignalForge home directory using `storage.SaveJSON`
- [x] Handle: no clusters found, missing OpenRouter config, context cancellation, partial generation failures — all with clear errors and safe persistence
- [x] Support `OPENROUTER_MODEL` override from env/config

### Task 3: Add tests

- [ ] Unit tests for JTBD rendering and schema validation (no LLM needed — test with canned input)
- [ ] Unit tests for product-type validation using `domain.IsValidProductType`
- [ ] Unit tests for no-product classification parsing (mock LLM returns "no_product" → verify no solutions generated)
- [ ] Unit tests for solution generation: mock LLM returns 3+ solutions → verify all pass validation, distinct product types, minimum-3 requirement
- [ ] Unit tests for deduplication: exact-match, same-title-different-type should NOT dedupe, same-title-same-type SHOULD dedupe
- [ ] Unit tests for persistence: save/load discover results atomically
- [ ] CLI tests: empty clusters, missing OpenRouter config, dry-run mode
- [ ] All tests must pass without network access or API credentials

## Validation Commands

```bash
go test ./internal/discover/...
go test ./internal/...
go test ./...
go vet ./...
go build ./cmd/signalforge/
```
