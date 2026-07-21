# ADR-003: Scores Are Heuristics, Not Facts

**Date:** 2026-07-21
**Status:** Accepted

## Context

SignalForge produces ProblemScore (0-100) and SolutionScore (0-100) to rank opportunities. The user needs to decide which problems to solve.

Key constraints:
- Scores are based on LLM classifications, which are probabilistic
- Evidence is incomplete — we only see what's public
- A "high score" could mean "well-documented popular problem" not "good business opportunity"

## Decision

Scores are **weighted averages of 8 (ProblemScorecard) or 9 (SolutionScorecard) dimensions**, each 0-10. The total is multiplied by 10 for a 0-100 range.

- Scores are explicitly **heuristics, not ground truth**
- Weights are configurable, not hardcoded
- Confidence is a separate 0-100 score, calculated from evidence quality, not from scores
- Recommendation is rule-based, not score-based — a high score alone never triggers a "strong_candidate" recommendation without sufficient confidence

## Consequences

### Positive
- Transparent — every dimension is visible and debuggable
- Configurable — users can tune weights for their domain
- Honest — scores don't pretend to be more accurate than they are

### Negative
- Two different runs may produce different scores for the same data (LLM non-determinism)
- Scores are not comparable across different data sets
- Users may over-trust the numbers

### Mitigation
- AGENTS.md explicitly states "Scores are heuristics"
- Recommendations always include reasoning text
- Confidence is displayed alongside scores