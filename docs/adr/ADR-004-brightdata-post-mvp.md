# ADR-004: Bright Data Is Post-MVP

**Date:** 2026-07-21
**Status:** Accepted

## Context

Bright Data provides SERP (Search Engine Results Page) data and Web Unlocker (renders JavaScript-heavy pages as readable text). These would enhance SignalForge's ability to:
- Find competitors and alternative solutions via search
- Read pages that block non-browser HTTP clients

However, Bright Data is a **paid subscription service** with no free tier.

## Decision

Bright Data integration is **designed and documented but deferred to post-MVP**. The MVP collects signals exclusively from free public APIs (GitHub, HN, Stack Exchange, optional Reddit).

- CLI skeleton includes `--brightdata` flag for future use
- Config structs for Bright Data are fully defined
- No code paths call Bright Data in MVP

## Consequences

### Positive
- MVP ships with zero paid dependencies
- Architecture accounts for Bright Data from day one (no retrofit)

### Negative
- Without Bright Data, SignalForge cannot discover problems from general web content
- Competitor research is limited to what appears on GitHub/HN/Stack Exchange

### Post-MVP migration
- Add `OPENROUTER_API_KEY`-style env var for `BRIGHTDATA_API_KEY`
- Implement BrightDataClient in `internal/brightdata/`
- Wire into the `research` pipeline stage