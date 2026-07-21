# ADR-005: Reddit Is Optional (Disabled by Default)

**Date:** 2026-07-21
**Status:** Accepted

## Context

Reddit is a rich source of problem signals (r/Startups, r/SaaS, r/ProductManagement, etc.). However:
- Reddit API rate limits are strict
- Content quality is highly variable
- Privacy concerns — Reddit users may not expect their comments to be mined for product ideas
- Reddit's API changes (2023 pricing controversy) make long-term stability uncertain

## Decision

Reddit collector is **built and documented but disabled by default**. Users must explicitly opt in via config (`sources.reddit.enabled: true`) and set specific subreddits.

- Collector code lives in `internal/sources/reddit/`
- CLI flag `--reddit` enables it at runtime
- Default config: `enabled: false`, `subreddits: []`

## Consequences

### Positive
- Clear opt-in prevents accidental data collection
- No rate-limit issues for users who don't want Reddit
- If Reddit changes API terms, only opt-in users are affected

### Negative
- Reddit is disabled for the default pipeline — users miss a rich signal source
- More config complexity for users who want Reddit

### Invariant
- Never enable Reddit by default. The opt-in is intentional.