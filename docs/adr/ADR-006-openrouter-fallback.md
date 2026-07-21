# ADR-006: OpenRouter Free Models with Fallback Chain

**Date:** 2026-07-21
**Status:** Accepted

## Context

SignalForge depends on LLM calls for classification, analysis, and generation. Using a single provider creates a single point of failure.

Options:
1. OpenAI direct — reliable but expensive, requires API key, no free tier
2. Anthropic direct — similar to OpenAI
3. OpenRouter with free routing — single API key, access to many models, free tier available
4. Multi-provider — complex, each with different auth and API shapes

## Decision

Use **OpenRouter as the sole LLM gateway** with:
- Primary model: user-configurable (default: empty → OpenRouter's default)
- Fallback models: user-configurable list
- Free model support: `:free` suffix on model name
- Retry: 3 attempts with exponential backoff
- JSON validation: LLM output must be valid JSON; max 1 repair attempt

## Consequences

### Positive
- Single API key for all LLM operations
- Free models available with `:free` suffix
- Fallback chain handles rate limits and outages
- OpenAI-compatible API shape — standard tooling works

### Negative
- OpenRouter is an intermediary — adds latency
- Free models have reliability issues (timeouts, rate limits, downtimes)
- Cannot use provider-specific features (Anthropic's extended thinking, etc.)

### Mitigation
- Fallback models are required in config (not optional)
- Retry with backoff handles transient failures
- Free model failures are logged but not fatal — pipeline continues with remaining signals
- Repair attempts are limited to 1 per response to prevent infinite loops