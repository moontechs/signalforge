# ADR-002: No Embeddings in MVP — Jaccard Similarity

**Date:** 2026-07-21
**Status:** Accepted

## Context

SignalForge needs to cluster related problem signals. Common approaches:
1. Embedding-based (text-embedding-3-small, etc.) — accurate but costly per-signal
2. Jaccard similarity on token sets — simple, fast, no API calls
3. TF-IDF + cosine similarity — better than Jaccard, but still no API cost

The MVP requirement: cluster 1000s of signals without spending on embedding API calls.

## Decision

Use **Jaccard similarity on tokenized text** with weighted token overlap as the primary clustering algorithm. Embeddings are deferred to post-MVP.

- Tokenize: lowercase, split on non-alphanumeric, remove stop words
- Jaccard: `|A ∩ B| / |A ∪ B|`
- Weighted overlap: rare tokens get higher weight (inverse document frequency)
- Threshold: configurable, default 0.3

## Consequences

### Positive
- Zero API cost for clustering
- Works offline, no network dependency
- Fast — O(n²) for n candidates, but candidate set is limited by config

### Negative
- Misses semantic similarity (e.g., "can't install" vs "installation fails" have low Jaccard)
- Tuning threshold is empirical — may need adjustment per source
- No multilingual support without stemming/lemmatization

### Mitigation
- LLM-based classifier already enriches signals with keywords, entities, actions — these are used as additional features in the similarity function
- Configurable thresholds allow per-source tuning
- Embedding upgrade path is clean: replace the distance function, keep the cluster data model