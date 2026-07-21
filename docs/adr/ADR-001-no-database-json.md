# ADR-001: No Database — JSON Files + JSONL

**Date:** 2026-07-21
**Status:** Accepted

## Context

SignalForge needs to persist raw signals, classified signals, clusters, JTBDs, solution hypotheses, pipeline runs, and memory state. MVP must ship without infrastructure dependencies.

Options considered:
1. SQLite — embedded, no server, but adds CGo dependency and schema migrations
2. Postgres — full-featured, but requires a running server and connection management
3. JSON files + JSONL — zero dependencies, simple, portable

## Decision

Use **JSON files for structured/versioned state** (config.json, memory.json) and **JSONL files for append-only collections** (raw signals, problem signals, clusters, etc.).

- JSON: atomic writes via temp file → fsync → rename → directory sync
- JSONL: append-only, one JSON object per line, suitable for streaming
- No schema migrations — version field on each file for forward compatibility

## Consequences

### Positive
- Zero external dependencies for storage
- Portable — data directory is a tarball
- Easy to debug — files are human-readable
- Atomic writes prevent corruption from crashes

### Negative
- No query language — filtering is O(n) scan
- No concurrency from multiple processes — single-process design enforced
- No indexing — large data sets will be slow
- SQLite or Postgres will be needed post-MVP if scale demands it

### Trade-off accepted
The MVP targets personal/team use, not SaaS. JSON files are the right trade-off for 0-100K signals.