# CONTEXT.md — SignalForge Domain Glossary

> Living document. Add new terms as they are introduced. Every domain type in `internal/domain/types.go` should be represented here.

---

## Core Entities

### RawSignal
A problem signal collected from an external source (GitHub Issue, HN comment, Stack Exchange question, Reddit post). The atomic unit of evidence.

- **Source**: which platform (github, hackernews, stackexchange, reddit)
- **SourceID**: platform-native identifier
- **ContentHash**: SHA-256 of title + body for deduplication
- **Comments**: nested child comments are included inline

### ProblemSignal
A classified RawSignal that the LLM judged to be a genuine problem statement, not noise. Carries classification dimensions.

- **IsProblemSignal**: boolean gate — everything else is noise
- **SeverityHint / FrequencyHint / PaymentHint / FrustrationHint**: 0–1 float signals from the LLM
- **Classification filters**: IsTemporaryIncident, IsSupportQuestion, IsExistingBug, IsConfigurationIssue, IsFeatureRequest — each set by the classifier

### ProblemCluster
A group of related ProblemSignals grouped by Jaccard similarity + rules. Represents a recurring, independently-corroborated problem.

- **SignalCount**: how many raw signals support this cluster
- **IndependentSources**: how many different platforms contributed
- **ProblemScorecard**: 8-dimension weighted score → `ProblemTotal` (0–100)

### JobToBeDone (JTBD)
A functional job derived from a ProblemCluster. Describes what the user is trying to accomplish, not how.

- **Statement**: "When [situation], I want to [motivation] so I can [expected outcome]"
- **CurrentSolutions**: what people do today (workarounds, existing tools)
- **Constraints**: non-negotiables from the evidence

### SolutionHypothesis
A proposed product/solution for a JTBD. Rich object with competitor analysis, evidence, implementation estimates, and scoring.

- **ProductType**: one of 12 types (saas, mobile_app, cli_tool, browser_extension, etc.)
- **Recommendation**: one of 10 (strong_candidate, investigate_further, too_competitive, etc.)
- **SolutionScorecard**: 9-dimension weighted score → `SolutionTotal` (0–100)

### PipelineRun
Tracks a single invocation of the pipeline. Resumable — carries cursor state for each source.

- **Stage**: which pipeline stage is currently executing
- **CursorState**: opaque map per source for resume-from-last-position
- **ResearchStats**: counters for every type of request, cache hit, and entity created

### Memory (persistent state)
Long-lived dedup store. Lives at `~/.signalforge/memory.json`. Versioned.

- **RawSignalIDs**: `source:sourceID` → sourceID (prevents re-collecting)
- **ContentHashes**: hash → signalID (prevents duplicate content from different IDs)
- **ProblemFingerprints / ClusterFingerprints / IdeaFingerprints**: dedup across classification runs
- **RejectedPatterns**: LLM-inferred patterns to skip in future runs

---

## Scoring Model

### ProblemScorecard (8 dimensions)
| Dimension | Weight | Scale |
|-----------|--------|-------|
| EvidenceStrength | 0.20 | 0–10 |
| Recurrence | 0.15 | 0–10 |
| Severity | 0.15 | 0–10 |
| WorkaroundCost | 0.15 | 0–10 |
| SourceDiversity | 0.10 | 0–10 |
| Longevity | 0.10 | 0–10 |
| UserSpecificity | 0.05 | 0–10 |
| ProductSolvability | 0.10 | 0–10 |

**ProblemTotal** = weighted average × 10 (range 0–100)

### SolutionScorecard (9 dimensions)
| Dimension | Weight | Scale |
|-----------|--------|-------|
| ProblemFit | 0.20 | 0–10 |
| ProductTypeFit | 0.15 | 0–10 |
| CompetitionGap | 0.15 | 0–10 |
| BuildSimplicity | 0.10 | 0–10 |
| DistributionPotential | 0.10 | 0–10 |
| MonetizationPotential | 0.10 | 0–10 |
| RetentionPotential | 0.08 | 0–10 |
| PlatformSafety | 0.07 | 0–10 |
| Defensibility | 0.05 | 0–10 |

**SolutionTotal** = weighted average × 10 (range 0–100)

---

## Pipeline Stages

```
collect → classify → cluster → discover → research → rank
```

1. **collect**: fetch raw signals from configured sources (GitHub, HN, Stack Exchange, optional Reddit)
2. **classify**: LLM-based binary classification + multi-dimension tagging → ProblemSignals
3. **cluster**: Jaccard similarity + rules → ProblemClusters
4. **discover**: LLM generates JTBD statements from clusters
5. **research**: LLM generates SolutionHypotheses with competitors, evidence, scores
6. **rank**: sort by score, apply confidence, generate recommendations

Each stage is resumable via PipelineRun cursor state.

---

## Key Invariants

1. **No database** — all persistence is JSON/JSONL files with atomic writes
2. **No embeddings in MVP** — Jaccard + weighted token overlap for clustering
3. **All LLM calls go through OpenRouter** — free models with fallback chain
4. **HTTP clients always have**: timeout, retry (3×), context cancellation
5. **JSON writes are atomic**: temp file → fsync → rename → directory sync
6. **Cache keys never include secrets**
7. **API tokens never in logs, exports, or serialized data**
8. **Scores are heuristics** — not ground truth, not comparable across runs
9. **Reddit is opt-in** — disabled by default
10. **Bright Data is post-MVP** — requires paid subscription