// Package clustering provides deterministic text normalization, fingerprinting,
// similarity scoring, and cluster construction for problem signals.
package clustering

import (
	"slices"
	"strings"
	"unicode"
)

// canonicalActions maps equivalent action verb phrases to a canonical form.
// When a phrase is not recognised it is preserved as-is after normalisation.
var canonicalActions = map[string]string{
	"install":   "install",
	"setup":     "install",
	"set up":    "install",
	"configure": "install",
	"deploy":    "deploy",
	"migrate":   "migrate",
}

// NormalizeText lowercases the input, splits on whitespace and punctuation/
// symbol boundaries, discards empty tokens, and returns a sorted,
// deduplicated slice of tokens.
func NormalizeText(text string) []string {
	if text == "" {
		return nil
	}

	// Split on any rune that is whitespace, punctuation, or symbol.
	// This handles hyphenated words ("well-known" → "well", "known"),
	// dotted numbers ("2.0" → "2", "0"), and currency prefixes ("€uro" → "euro").
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r)
	})

	seen := make(map[string]struct{}, len(fields))
	tokens := make([]string, 0, len(fields))

	for _, f := range fields {
		if f == "" {
			continue
		}
		if _, ok := seen[f]; ok {
			continue
		}
		seen[f] = struct{}{}
		tokens = append(tokens, f)
	}

	slices.Sort(tokens)
	return tokens
}

// CanonicalizeAction maps an action phrase to its canonical form. Known
// equivalent phrases (e.g. "setup", "set up", "configure") are collapsed
// into the same canonical verb. Unknown phrases are lowercased, stripped
// of punctuation, and returned as a normalised token sequence joined by
// spaces.
func CanonicalizeAction(action string) string {
	if action == "" {
		return ""
	}

	normalised := strings.ToLower(strings.TrimSpace(action))

	if canon, ok := canonicalActions[normalised]; ok {
		return canon
	}

	// For unknown actions, normalise token-by-token and join.
	tokens := strings.Fields(normalised)
	if len(tokens) == 0 {
		return ""
	}

	cleaned := make([]string, 0, len(tokens))
	for _, t := range tokens {
		t = strings.TrimFunc(t, func(r rune) bool {
			return unicode.IsPunct(r) || unicode.IsSymbol(r)
		})
		if t != "" {
			cleaned = append(cleaned, t)
		}
	}

	if len(cleaned) == 0 {
		return ""
	}

	return strings.Join(cleaned, " ")
}

// CanonicalizeActions applies CanonicalizeAction to each entry in the input
// slice, deduplicates the results, and returns a sorted slice.
func CanonicalizeActions(actions []string) []string {
	if len(actions) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(actions))
	result := make([]string, 0, len(actions))

	for _, a := range actions {
		canon := CanonicalizeAction(a)
		if canon == "" {
			continue
		}
		if _, ok := seen[canon]; ok {
			continue
		}
		seen[canon] = struct{}{}
		result = append(result, canon)
	}

	slices.Sort(result)
	return result
}
