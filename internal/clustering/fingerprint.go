package clustering

import (
	"slices"
	"strings"
	"unicode"
)

// EntityFingerprint normalizes an entity name by lowercasing, collapsing
// whitespace, stripping leading/trailing punctuation, and removing
// superficial formatting variation (e.g. duplicated whitespace, leading
// "the" or "a", trailing parenthetical disambiguation).
func EntityFingerprint(entity string) string {
	if entity == "" {
		return ""
	}

	// Lowercase and trim.
	s := strings.ToLower(strings.TrimSpace(entity))

	// Collapse whitespace.
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return ""
	}

	// Rejoin with single spaces.
	s = strings.Join(fields, " ")

	// Strip leading/trailing punctuation.
	s = strings.TrimFunc(s, func(r rune) bool {
		return unicode.IsPunct(r) || unicode.IsSymbol(r)
	})

	if s == "" {
		return ""
	}

	return s
}

// EntityFingerprints applies EntityFingerprint to each entity in the input
// slice, deduplicates the results, and returns a sorted slice of unique
// fingerprints.
func EntityFingerprints(entities []string) []string {
	if len(entities) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(entities))
	result := make([]string, 0, len(entities))

	for _, e := range entities {
		fp := EntityFingerprint(e)
		if fp == "" {
			continue
		}
		if _, ok := seen[fp]; ok {
			continue
		}
		seen[fp] = struct{}{}
		result = append(result, fp)
	}

	slices.Sort(result)
	return result
}

// FingerprintOverlap computes the Jaccard similarity coefficient between two
// sets of fingerprints. Returns 0 for empty sets.
func FingerprintOverlap(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}

	setA := make(map[string]struct{}, len(a))
	for _, f := range a {
		setA[f] = struct{}{}
	}

	intersection := 0
	for _, f := range b {
		if _, ok := setA[f]; ok {
			intersection++
		}
	}

	union := len(setA)
	for _, f := range b {
		if _, ok := setA[f]; !ok {
			union++
		}
	}

	if union == 0 {
		return 0
	}

	return float64(intersection) / float64(union)
}
