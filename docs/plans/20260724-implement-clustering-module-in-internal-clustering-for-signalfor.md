# Plan: Implement SignalForge Problem Clustering Module

### Task 1: Create text normalization and feature-fingerprinting utilities

- [ ] Create `internal/clustering/normalize.go` with deterministic text normalization: lowercase, strip punctuation noise, tokenize words, discard empty tokens, return stable token ordering.
- [ ] Create `internal/clustering/normalize.go` with canonical action normalization mapping equivalent action phrases to canonical forms (e.g., "install" → "install", "setup" → "install", "configure" → "install", "deploy" → "deploy", "migrate" → "migrate"). Preserve unknown actions as normalized token sequences.
- [ ] Create `internal/clustering/fingerprint.go` with entity fingerprinting: normalize entity names, remove superficial formatting variation, deduplicate entities, produce deterministic fingerprints for equality/overlap checks.
- [ ] All helpers tolerate blank fields, duplicate values, and partially populated ProblemSignal records without panics.
- [ ] Add comprehensive unit tests in `internal/clustering/normalize_test.go` and `internal/clustering/fingerprint_test.go` covering casing, punctuation, duplicates, empty inputs, and canonical action mapping.

### Task 2: Implement deterministic similarity and weighted-overlap scoring

- [ ] Create `internal/clustering/similarity.go` with Jaccard similarity over normalized token sets. Explicit behavior for empty inputs. No mutation of caller-owned data.
- [ ] Implement weighted token overlap: keywords at 3x weight, entities at 2x weight, actions at 1x weight.
- [ ] Define combined deterministic match score = Jaccard(signal_body_tokens) * 0.4 + weighted_overlap(features) * 0.6.
- [ ] Define thresholds: match_threshold=0.35 (minimum combined score for cluster membership), semantic_threshold=0.25 (below this → no match, above → use semantic checker for edge cases).
- [ ] Keep scoring deterministic by sorting map-derived values before constructing cluster metadata.
- [ ] Add unit tests in `internal/clustering/similarity_test.go` covering exact matches, disjoint signals, partial keyword overlap, entity-only overlap, action-only overlap, duplicates, casing/punctuation variation, and empty feature sets.

### Task 3: Implement cluster construction and metadata aggregation

- [ ] Create `internal/clustering/cluster.go` with ProblemCluster creation from matching ProblemSignal values.
- [ ] Populate cluster metadata: SignalIDs, SignalCount, IndependentSources (count of unique Source values), SourceTypes, FirstObservedAt (min), LastObservedAt (max), Keywords (union of member keywords), Entities (union), Actions (union).
- [ ] Select canonical cluster Title from the most frequent problem text among members (deterministic: first alphabetically on tie).
- [ ] Implement incremental placement: compare each signal against existing clusters, attach to best qualifying cluster (highest combined score above match_threshold), or create new cluster when no match.
- [ ] Resolve ties deterministically using stable cluster ID (lexicographic sort).
- [ ] Create `internal/clustering/errors.go` with typed errors: ErrEmptyInput, ErrNoMatch, ErrSemanticCheckFailed. Wrap underlying errors for errors.Is/errors.As.
- [ ] Emit structured slog diagnostics: "clustering: N signals processed, M clusters created, K semantic checks".
- [ ] Add unit tests in `internal/clustering/cluster_test.go` for incremental placement, tie resolution, empty input, single-signal cluster, multi-signal cluster.

### Task 4: Implement ProblemScorecard and Confidence calculation

- [ ] Create `internal/clustering/scorecard.go` with scorecard calculation using ProblemScorecardWeights() from domain types.
- [ ] Calculate each dimension:
  - EvidenceStrength: average of signal relevance scores (0-10 scale)
  - Recurrence: 10 if any signal has Recurring=true, else 0
  - Severity: average of SeverityHint * 10 across member signals
  - WorkaroundCost: average of signals where CurrentWorkaround != "" (10 if all have workarounds, 0 if none)
  - SourceDiversity: min(IndependentSources * 3, 10)
  - Longevity: days between FirstObservedAt and LastObservedAt, capped at 10
  - UserSpecificity: average of non-empty TargetUser across signals (10 if all have target users, 0 if none)
  - ProductSolvability: (count of ProductSolvable=true / total signals) * 10
- [ ] Calculate ProblemTotal = ProblemScorecard.Total() (weighted average * 10, range 0-100).
- [ ] Calculate Confidence separately: (SignalCount / max_signals_expected) * 0.4 + (SourceDiversity / 10) * 0.3 + (1 - noise_ratio) * 0.3 where noise_ratio = (signals with IsProblemSignal=false / total signals in cluster). Clamp 0-100.
- [ ] Clamp all score and confidence outputs to documented ranges. Deterministic neutral behavior for sparse clusters.
- [ ] Add table-driven tests in `internal/clustering/scorecard_test.go` for high-evidence clusters, single-signal clusters, mixed-source clusters, sparse metadata, score bounds, and stable output across input order permutations.

### Task 5: Add semantic-check abstraction and implementations

- [ ] Create `internal/clustering/semantic.go` with SemanticChecker interface:
  ```
  type SemanticChecker interface {
      ShouldMerge(ctx context.Context, a, b ProblemSignal, score float64) (bool, error)
  }
  ```
- [ ] Implement NoopSemanticChecker: always returns (false, nil) — no merge, no error. Default when no checker configured.
- [ ] Implement OpenRouterSemanticChecker using existing internal/openrouter client. Sends a prompt with normalized signal content (never API keys). Uses JSON schema for structured output. Falls back to no-merge on error/abstention.
- [ ] Restrict OpenRouter checks to signals in the semantic_threshold range (0.25-0.35). Clear matches and clear non-matches skip the check.
- [ ] Add fake-checker tests in `internal/clustering/semantic_test.go` for accepted merge, rejected merge, abstention, error fallback, and context cancellation.

### Task 6: Wire the cluster CLI command

- [ ] Create `internal/cli/cluster.go` following the same pattern as classify.go:
  - newClusterCmd() with flags: --limit, --model, --force, --dry-run, --threshold (override match threshold)
  - clusterEnv struct (store, mem, cfg, limit, model, force, dryRun, threshold)
  - executeCluster() loads problem signals from problem-signals/ directory, runs clustering, saves clusters to clusters/ directory
  - printClusterDryRun() and printClusterSummary() functions
- [ ] Register clusterCmd in cmd/signalforge/main.go: rootCmd.AddCommand(cli.ClusterCmd)
- [ ] Save each ProblemCluster as JSON file in clusters/<cluster_id>.json
- [ ] Update memory: track cluster fingerprints via ClusterFingerprints map, increment ClustersCreated stat
- [ ] Add CLI tests in `internal/cli/cluster_test.go` following the same pattern as classify_test.go: flag registration, flag defaults, dry-run output, no-signals handling, force re-cluster.

### Task 7: Run verification and fix issues

- [ ] Run: gofmt -w internal/clustering internal/cli
- [ ] Run: go test ./internal/clustering/... — all tests pass
- [ ] Run: go test ./internal/cli/... — all tests pass
- [ ] Run: go vet ./...
- [ ] Run: golangci-lint run ./... — exit 0
- [ ] Run: go build ./cmd/signalforge/ — compiles

## Validation Commands

gofmt -w internal/clustering internal/cli
go test ./internal/clustering/...
go test ./internal/cli/...
go test ./...
go vet ./...
golangci-lint run ./...
go build ./cmd/signalforge/