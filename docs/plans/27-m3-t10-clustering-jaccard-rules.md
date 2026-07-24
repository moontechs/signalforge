# Plan: M3-T10 Clustering (Jaccard + rules)

## Summary

Implement the clustering module for SignalForge. The module groups related problem signals into ProblemClusters using Jaccard similarity, token overlap, entity matching, canonical action matching, and optional OpenRouter semantic checks for edge cases. Each cluster receives a ProblemScorecard with all 8 dimensions, a weighted total, and a confidence score.

## Background

- `internal/clustering/` already exists with `normalize.go`, `normalize_test.go`, and `fingerprint.go`
- These provide: `NormalizeText`, `CanonicalizeAction`, `CanonicalizeActions`, `EntityFingerprint`, `EntityFingerprints`, `FingerprintOverlap`
- Domain types in `internal/domain/types.go` already have: `ProblemCluster`, `ProblemScorecard`, `ProblemScorecardWeights()`, `Total()`
- Memory in `internal/memory/memory.go` already has: `HasClusterFingerprint`, `AddClusterFingerprint`, `IncrementStat("clusters_created")`
- OpenRouter client (`domain.LLMClient` interface) is available

## Validation Commands

- `go build ./...`
- `go vet ./...`
- `go test ./internal/clustering/... -v`
- `go test ./... -v`

## Files to create

### 1. `internal/clustering/cluster.go` — Main clustering engine

**Structs:**

```go
// Config holds clustering parameters.
type Config struct {
    JaccardThreshold    float64  // default 0.3
    EntityWeight        float64  // default 0.25
    ActionWeight        float64  // default 0.25
    KeywordWeight       float64  // default 0.25
    TextWeight          float64  // default 0.25
    SemanticThreshold   float64  // Jaccard below this triggers semantic check (default 0.15)
    MinClusterSize      int      // default 1
    MaxClusters         int      // default 0 (unlimited)
}

// Clusterer groups related problem signals.
type Clusterer struct {
    cfg     Config
    llm     domain.LLMClient  // optional, for semantic edge checks
    storage *storage.Storage  // for loading/saving clusters
}
```

**Methods:**

- `New(cfg Config, llm domain.LLMClient, store *storage.Storage) *Clusterer`

- `(c *Clusterer) Cluster(ctx context.Context, signals []domain.ProblemSignal) ([]domain.ProblemCluster, error)`
  - Filters to only `IsProblemSignal == true` signals
  - Computes pairwise similarity matrix
  - Agglomerative clustering with threshold
  - Detects edge cases (similarity between SemanticThreshold and JaccardThreshold)
  - For edge cases, uses OpenRouter semantic check via `c.checkSemanticBoundary`
  - Creates ProblemCluster for each group
  - Computes ProblemScorecard, ProblemTotal, Confidence for each cluster
  - Returns clusters sorted by ProblemTotal descending

- `(c *Clusterer) signalFingerprint(sig domain.ProblemSignal) signalFP`
  - Returns a struct with normalized tokens, canonical actions, entity fingerprints, keywords
  - Uses `NormalizeText`, `CanonicalizeActions`, `EntityFingerprints`

- `(c *Clusterer) computeSimilarity(a, b signalFP) float64`
  - Weighted combination of:
    - Jaccard on normalized text tokens (TextWeight)
    - FingerprintOverlap on keywords (KeywordWeight)
    - FingerprintOverlap on entity fingerprints (EntityWeight)
    - FingerprintOverlap on canonical actions (ActionWeight)
  - Returns 0.0–1.0

- `(c *Clusterer) buildCluster(signals []domain.ProblemSignal) domain.ProblemCluster`
  - Populates all fields of ProblemCluster from constituent signals
  - Title = most common problem text or first signal's problem
  - Summary = combined from signals
  - Keywords, entities, actions = union of all signals' values
  - SignalIDs, SignalCount, IndependentSources, SourceTypes, FirstObservedAt, LastObservedAt, etc.

- `(c *Clusterer) computeProblemScore(signals []domain.ProblemSignal) domain.ProblemScorecard`
  - EvidenceStrength: average of relevance scores across signals
  - Recurrence: proportion of signals with Recurring == true
  - Severity: average of SeverityHint / 10 across signals
  - WorkaroundCost: average of non-empty CurrentWorkaround signals
  - SourceDiversity: based on number of unique sources
  - Longevity: based on time span between first and last observed
  - UserSpecificity: based on diversity of TargetUser values
  - ProductSolvability: proportion of signals with ProductSolvable == true
  - All dimensions 0.0–1.0

- `(c *Clusterer) computeConfidence(signals []domain.ProblemSignal, score domain.ProblemScorecard) float64`
  - Factors: signal count, source diversity, score magnitude, evidence of recurring pattern
  - Returns 0–100

- `(c *Clusterer) checkSemanticBoundary(ctx context.Context, a, b domain.ProblemSignal) (bool, error)`
  - Returns true if two signals should be in the same cluster
  - Uses OpenRouter with a prompt asking if they describe the same problem
  - Only called when similarity is between SemanticThreshold and JaccardThreshold

### 2. `internal/clustering/cluster_test.go` — Tests

- TestNewClusterer_Defaults
- TestCluster_EmptySignals
- TestCluster_NoProblemSignals
- TestComputeSimilarity_Identical
- TestComputeSimilarity_Different
- TestBuildCluster_SingleSignal
- TestBuildCluster_MultipleSignals
- TestComputeProblemScore
- TestComputeConfidence
- TestCluster_Integration (end-to-end with mock signals)

### 3. `internal/cli/cluster.go` — CLI command

```go
var ClusterCmd = newClusterCmd()

func newClusterCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "cluster",
        Short: "Cluster related problem signals",
        Long: `Groups related problem signals into clusters using Jaccard similarity,
entity matching, and canonical action matching. Edge cases use OpenRouter
for semantic boundary checks.

Example:
  signalforge cluster
  signalforge cluster --threshold 0.4
  signalforge cluster --dry-run`,
        RunE: runCluster,
    }

    cmd.Flags().Float64("threshold", 0.3, "Jaccard similarity threshold (0.0-1.0)")
    cmd.Flags().Bool("dry-run", false, "Print clustering plan and exit")
    cmd.Flags().Int("limit", 0, "Maximum number of clusters to create")
    cmd.Flags().Bool("no-semantic", false, "Skip OpenRouter semantic checks for edge cases")

    return cmd
}
```

- `runCluster` implementation mirrors `runClassify` pattern:
  - Load problem signals from `problem-signals/` directory
  - Filter to only `IsProblemSignal == true`
  - Create OpenRouter client (if not --no-semantic)
  - Create Clusterer with config
  - Run clustering
  - Save each cluster as JSON to `clusters/` directory
  - Update memory (cluster fingerprints, stats)
  - Print summary

### 4. `cmd/signalforge/main.go` — Add cluster command

Add `rootCmd.AddCommand(cli.ClusterCmd)` in `init()`.

### 5. `prompts/cluster_check.txt` — Semantic boundary prompt

A prompt template that asks the LLM whether two problem descriptions describe the same underlying problem. Variables: `{{.ProblemA}}`, `{{.ProblemB}}`, `{{.ContextA}}`, `{{.ContextB}}`.

## Task breakdown

### Task 1: Implement clustering.go
- [x] Create `internal/clustering/cluster.go` with all the structures and methods described above
- [x] Implement `Config`, `New`, `Cluster`, `signalFingerprint`, `computeSimilarity`
- [x] Implement `buildCluster`, `computeProblemScore`, `computeConfidence`
- [x] Implement `checkSemanticBoundary` using OpenRouter

### Task 2: Create semantic prompt
- [x] Create `prompts/cluster_check.txt` with a prompt for checking if two problems are the same

### Task 3: Write tests
- [x] Create `internal/clustering/cluster_test.go` with comprehensive tests
- [x] Tests should cover all edge cases: empty, single, identical, different, boundary

### Task 4: Create CLI command
- [ ] Create `internal/cli/cluster.go` with the cluster command
- [ ] Wire the command into `cmd/signalforge/main.go`

### Task 5: Verify everything compiles and tests pass
- [ ] `go build ./...`
- [ ] `go vet ./...`
- [ ] `go test ./internal/clustering/... -v`
- [ ] `go test ./... -v`