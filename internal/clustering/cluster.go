// Package clustering provides deterministic text normalization, fingerprinting,
// similarity scoring, and cluster construction for problem signals.
package clustering

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/moontechs/signalforge/internal/domain"
	"github.com/moontechs/signalforge/internal/storage"
)

// default config values.
const (
	defaultJaccardThreshold  = 0.3
	defaultEntityWeight      = 0.25
	defaultActionWeight      = 0.25
	defaultKeywordWeight     = 0.25
	defaultTextWeight        = 0.25
	defaultSemanticThreshold = 0.15
	defaultMinClusterSize    = 1
)

// signalFP holds the precomputed fingerprint of a problem signal used for
// pairwise similarity computation.
type signalFP struct {
	tokens   []string
	actions  []string
	entities []string
	keywords []string
}

// fpEntry pairs a problem signal with its precomputed fingerprint.
type fpEntry struct {
	signal domain.ProblemSignal
	fp     signalFP
}

// Config holds clustering parameters.
type Config struct {
	JaccardThreshold  float64 // default 0.3
	EntityWeight      float64 // default 0.25
	ActionWeight      float64 // default 0.25
	KeywordWeight     float64 // default 0.25
	TextWeight        float64 // default 0.25
	SemanticThreshold float64 // Jaccard below this triggers semantic check (default 0.15)
	MinClusterSize    int     // default 1
	MaxClusters       int     // default 0 (unlimited)
}

// setDefaults fills zero-valued fields with sensible defaults.
func (c *Config) setDefaults() {
	if c.JaccardThreshold <= 0 {
		c.JaccardThreshold = defaultJaccardThreshold
	}
	if c.EntityWeight <= 0 {
		c.EntityWeight = defaultEntityWeight
	}
	if c.ActionWeight <= 0 {
		c.ActionWeight = defaultActionWeight
	}
	if c.KeywordWeight <= 0 {
		c.KeywordWeight = defaultKeywordWeight
	}
	if c.TextWeight <= 0 {
		c.TextWeight = defaultTextWeight
	}
	if c.SemanticThreshold <= 0 {
		c.SemanticThreshold = defaultSemanticThreshold
	}
	if c.MinClusterSize <= 0 {
		c.MinClusterSize = defaultMinClusterSize
	}
}

// Clusterer groups related problem signals using Jaccard similarity, entity
// matching, canonical action matching, and optional semantic boundary checks.
type Clusterer struct {
	cfg     Config
	llm     domain.LLMClient // optional, for semantic edge checks
	storage *storage.Storage // for loading/saving clusters
}

// New creates a new Clusterer with the given config, optional LLM client, and
// storage backend.
func New(cfg Config, llm domain.LLMClient, store *storage.Storage) *Clusterer {
	cfg.setDefaults()
	return &Clusterer{
		cfg:     cfg,
		llm:     llm,
		storage: store,
	}
}

// Cluster groups related problem signals into ProblemClusters. Only signals
// with IsProblemSignal == true are considered. Returns clusters sorted by
// ProblemTotal descending.
func (c *Clusterer) Cluster(ctx context.Context, signals []domain.ProblemSignal) ([]domain.ProblemCluster, error) {
	// Filter to only problem signals.
	problemSignals := filterProblemSignals(signals)
	if len(problemSignals) == 0 {
		return nil, nil
	}

	// Precompute fingerprints for each signal.
	entries := make([]fpEntry, len(problemSignals))
	for i, s := range problemSignals {
		entries[i] = fpEntry{
			signal: s,
			fp:     c.signalFingerprint(s),
		}
	}

	// Compute pairwise similarity matrix.
	n := len(entries)
	similarity := make([][]float64, n)
	for i := range similarity {
		similarity[i] = make([]float64, n)
		for j := range similarity[i] {
			similarity[i][j] = c.computeSimilarity(entries[i].fp, entries[j].fp)
		}
	}

	// Agglomerative clustering (single-linkage) with threshold.
	groups := agglomerate(entries, similarity, c.cfg)

	// For edge cases (similarity between SemanticThreshold and JaccardThreshold),
	// use semantic check to decide if signals belong together.
	if c.llm != nil && c.cfg.SemanticThreshold < c.cfg.JaccardThreshold {
		groups = c.resolveEdgeCases(ctx, entries, similarity, groups)
	}

	// Build clusters from groups.
	clusters := make([]domain.ProblemCluster, 0, len(groups))
	for _, group := range groups {
		if len(group) < c.cfg.MinClusterSize {
			continue
		}

		signalsInGroup := make([]domain.ProblemSignal, len(group))
		for i, idx := range group {
			signalsInGroup[i] = entries[idx].signal
		}

		cluster := c.buildCluster(signalsInGroup)
		clusters = append(clusters, cluster)
	}

	// Sort by ProblemTotal descending.
	sortClustersByTotal(clusters)

	// Apply max clusters limit.
	if c.cfg.MaxClusters > 0 && len(clusters) > c.cfg.MaxClusters {
		clusters = clusters[:c.cfg.MaxClusters]
	}

	return clusters, nil
}

// signalFingerprint returns the normalized tokens, canonical actions, entity
// fingerprints, and keywords for a problem signal.
func (c *Clusterer) signalFingerprint(sig domain.ProblemSignal) signalFP {
	// Combine title-like fields for text tokens.
	textParts := []string{sig.Problem, sig.Context, sig.DesiredOutcome, sig.CurrentWorkaround}
	text := strings.Join(textParts, " ")
	tokens := NormalizeText(text)

	actions := CanonicalizeActions(sig.Actions)
	entities := EntityFingerprints(sig.Entities)
	keywords := make([]string, len(sig.Keywords))
	copy(keywords, sig.Keywords)

	return signalFP{
		tokens:   tokens,
		actions:  actions,
		entities: entities,
		keywords: keywords,
	}
}

// computeSimilarity returns a weighted combination of Jaccard similarity on
// normalized text tokens, and FingerprintOverlap on keywords, entity
// fingerprints, and canonical actions. Returns a value in [0.0, 1.0].
func (c *Clusterer) computeSimilarity(a, b signalFP) float64 {
	textSim := jaccardTokens(a.tokens, b.tokens)
	keywordSim := FingerprintOverlap(a.keywords, b.keywords)
	entitySim := FingerprintOverlap(a.entities, b.entities)
	actionSim := FingerprintOverlap(a.actions, b.actions)

	totalWeight := c.cfg.TextWeight + c.cfg.KeywordWeight + c.cfg.EntityWeight + c.cfg.ActionWeight
	if totalWeight == 0 {
		return 0
	}

	sim := (textSim*c.cfg.TextWeight +
		keywordSim*c.cfg.KeywordWeight +
		entitySim*c.cfg.EntityWeight +
		actionSim*c.cfg.ActionWeight) / totalWeight

	if sim > 1.0 {
		return 1.0
	}
	if sim < 0 {
		return 0
	}
	return sim
}

// buildCluster constructs a ProblemCluster from a group of related signals.
func (c *Clusterer) buildCluster(signals []domain.ProblemSignal) domain.ProblemCluster {
	if len(signals) == 0 {
		return domain.ProblemCluster{}
	}

	// Determine the most common problem text as the title.
	titleCounts := make(map[string]int)
	for _, s := range signals {
		titleCounts[s.Problem]++
	}
	title := signals[0].Problem
	maxCount := 0
	for t, count := range titleCounts {
		if count > maxCount {
			title = t
			maxCount = count
		}
	}

	// Collect all unique values.
	keywords := collectUniqueStrings(signals, func(s domain.ProblemSignal) []string { return s.Keywords })
	entities := collectUniqueStrings(signals, func(s domain.ProblemSignal) []string { return s.Entities })
	actions := collectUniqueStrings(signals, func(s domain.ProblemSignal) []string { return s.Actions })
	contexts := collectUniqueFields(signals, func(s domain.ProblemSignal) string { return s.Context })
	targetUsers := collectUniqueFields(signals, func(s domain.ProblemSignal) string { return s.TargetUser })
	workarounds := collectUniqueFields(signals, func(s domain.ProblemSignal) string { return s.CurrentWorkaround })
	outcomes := collectUniqueFields(signals, func(s domain.ProblemSignal) string { return s.DesiredOutcome })
	constraints := collectUniqueStrings(signals, func(s domain.ProblemSignal) []string { return s.Constraints })

	// Build summary from combined signals.
	summary := buildSummary(signals)

	// Signal IDs and time bounds.
	signalIDs := make([]string, len(signals))
	sourceTypes := make(map[string]struct{})
	var firstObserved, lastObserved time.Time
	representativeIDs := make([]string, 0, len(signals))

	for i, s := range signals {
		signalIDs[i] = s.ID
		sourceTypes[s.Source] = struct{}{}

		if s.ClassifiedAt.Before(firstObserved) || firstObserved.IsZero() {
			firstObserved = s.ClassifiedAt
		}
		if s.ClassifiedAt.After(lastObserved) {
			lastObserved = s.ClassifiedAt
		}

		// Representative signals: those with highest relevance.
		if len(representativeIDs) < 5 {
			representativeIDs = append(representativeIDs, s.ID)
		}
	}

	// Sort representative IDs by relevance (descending) and take top.
	type repCandidate struct {
		id        string
		relevance float64
	}
	candidates := make([]repCandidate, len(signals))
	for i, s := range signals {
		candidates[i] = repCandidate{id: s.ID, relevance: s.Relevance}
	}
	// Simple bubble sort by relevance descending, limited to top 5.
	for i := 0; i < len(candidates) && i < 5; i++ {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[j].relevance > candidates[i].relevance {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}
	repLimit := 5
	if len(candidates) < repLimit {
		repLimit = len(candidates)
	}
	representativeIDs = make([]string, repLimit)
	for i := 0; i < repLimit; i++ {
		representativeIDs[i] = candidates[i].id
	}

	// Unique sources.
	uniqueSources := make(map[string]struct{})
	for _, s := range signals {
		uniqueSources[s.Source] = struct{}{}
	}

	sourceTypesList := make([]string, 0, len(sourceTypes))
	for st := range sourceTypes {
		sourceTypesList = append(sourceTypesList, st)
	}

	// Compute scorecard.
	scorecard := c.computeProblemScore(signals)
	confidence := c.computeConfidence(signals, scorecard)

	// Generate a stable cluster ID based on first signal's ID.
	clusterID := fmt.Sprintf("cluster_%s", signalIDs[0][:min(16, len(signalIDs[0]))])

	return domain.ProblemCluster{
		ID:                      clusterID,
		CreatedAt:               time.Now(),
		UpdatedAt:               time.Now(),
		Title:                   title,
		Summary:                 summary,
		Problem:                 title,
		TargetUsers:             targetUsers,
		Contexts:                contexts,
		CurrentWorkarounds:      workarounds,
		DesiredOutcomes:         outcomes,
		Constraints:             constraints,
		SignalIDs:               signalIDs,
		RepresentativeSignalIDs: representativeIDs,
		SignalCount:             len(signals),
		IndependentSources:      len(uniqueSources),
		IndependentDomains:      countDomains(signals),
		SourceTypes:             sourceTypesList,
		FirstObservedAt:         firstObserved,
		LastObservedAt:          lastObserved,
		Keywords:                keywords,
		Entities:                entities,
		Actions:                 actions,
		ProblemScore:            scorecard,
		ProblemTotal:            scorecard.Total(),
		Confidence:              confidence,
		Status:                  "new",
	}
}

// computeProblemScore computes the 8-dimension ProblemScorecard for a group
// of related signals.
func (c *Clusterer) computeProblemScore(signals []domain.ProblemSignal) domain.ProblemScorecard {
	if len(signals) == 0 {
		return domain.ProblemScorecard{}
	}

	n := float64(len(signals))

	// EvidenceStrength: average of relevance scores across signals.
	var totalRelevance float64
	for _, s := range signals {
		totalRelevance += s.Relevance
	}
	evidenceStrength := totalRelevance / n

	// Recurrence: proportion of signals with Recurring == true.
	var recurringCount float64
	for _, s := range signals {
		if s.Recurring {
			recurringCount++
		}
	}
	recurrence := recurringCount / n

	// Severity: average of SeverityHint / 10 across signals.
	var totalSeverity float64
	for _, s := range signals {
		totalSeverity += s.SeverityHint
	}
	severity := totalSeverity / (n * 10)

	// WorkaroundCost: average of non-empty CurrentWorkaround signals.
	var workaroundTotal float64
	var workaroundCount float64
	for _, s := range signals {
		if s.CurrentWorkaround != "" {
			workaroundTotal += 1.0
			workaroundCount++
		}
	}
	workaroundCost := 0.0
	if workaroundCount > 0 {
		workaroundCost = workaroundTotal / n
	}

	// SourceDiversity: based on number of unique sources.
	uniqueSources := make(map[string]struct{})
	for _, s := range signals {
		uniqueSources[s.Source] = struct{}{}
	}
	sourceDiversity := float64(len(uniqueSources)) / n
	if sourceDiversity > 1.0 {
		sourceDiversity = 1.0
	}

	// Longevity: based on time span between first and last observed.
	var firstObserved, lastObserved time.Time
	for _, s := range signals {
		if s.ClassifiedAt.Before(firstObserved) || firstObserved.IsZero() {
			firstObserved = s.ClassifiedAt
		}
		if s.ClassifiedAt.After(lastObserved) {
			lastObserved = s.ClassifiedAt
		}
	}
	longevity := 0.0
	if !firstObserved.IsZero() && !lastObserved.IsZero() && lastObserved.After(firstObserved) {
		span := lastObserved.Sub(firstObserved)
		// Scale: 1 day = 0.25, 7 days = 0.5, 30 days = 0.75, 90+ days = 1.0
		days := span.Hours() / 24
		switch {
		case days >= 90:
			longevity = 1.0
		case days >= 30:
			longevity = 0.75
		case days >= 7:
			longevity = 0.5
		case days >= 1:
			longevity = 0.25
		default:
			longevity = 0.1
		}
	}

	// UserSpecificity: based on diversity of TargetUser values.
	uniqueUsers := make(map[string]struct{})
	for _, s := range signals {
		if s.TargetUser != "" {
			uniqueUsers[s.TargetUser] = struct{}{}
		}
	}
	userSpecificity := float64(len(uniqueUsers)) / n
	if userSpecificity > 1.0 {
		userSpecificity = 1.0
	}

	// ProductSolvability: proportion of signals with ProductSolvable == true.
	var solvableCount float64
	for _, s := range signals {
		if s.ProductSolvable {
			solvableCount++
		}
	}
	productSolvability := solvableCount / n

	return domain.ProblemScorecard{
		EvidenceStrength:   clamp01(evidenceStrength),
		Recurrence:         clamp01(recurrence),
		Severity:           clamp01(severity),
		WorkaroundCost:     clamp01(workaroundCost),
		SourceDiversity:    clamp01(sourceDiversity),
		Longevity:          clamp01(longevity),
		UserSpecificity:    clamp01(userSpecificity),
		ProductSolvability: clamp01(productSolvability),
	}
}

// computeConfidence returns a confidence score (0-100) for a cluster based on
// signal count, source diversity, score magnitude, and evidence of recurrence.
func (c *Clusterer) computeConfidence(signals []domain.ProblemSignal, score domain.ProblemScorecard) float64 {
	if len(signals) == 0 {
		return 0
	}

	n := float64(len(signals))

	// Signal count factor: sigmoid-like, 0.2 for 1 signal, 0.7 for 5, 1.0 for 10+.
	countScore := n / (n + 4)

	// Source diversity factor.
	uniqueSources := make(map[string]struct{})
	for _, s := range signals {
		uniqueSources[s.Source] = struct{}{}
	}
	sourceScore := float64(len(uniqueSources)) / 3.0
	if sourceScore > 1.0 {
		sourceScore = 1.0
	}

	// Score magnitude: how strong is the weighted total relative to max (10).
	scoreMagnitude := score.Total() / 10.0

	// Recurrence evidence.
	var recurringCount float64
	for _, s := range signals {
		if s.Recurring {
			recurringCount++
		}
	}
	recurrenceScore := recurringCount / n

	// Weighted combination.
	confidence := (countScore*0.25 +
		sourceScore*0.25 +
		scoreMagnitude*0.30 +
		recurrenceScore*0.20) * 100

	if confidence > 100 {
		return 100
	}
	if confidence < 0 {
		return 0
	}
	return confidence
}

// checkSemanticBoundary uses the LLM to determine whether two problem signals
// describe the same underlying problem. Returns true if they should be in the
// same cluster.
func (c *Clusterer) checkSemanticBoundary(ctx context.Context, a, b domain.ProblemSignal) (bool, error) {
	if c.llm == nil {
		return false, nil
	}

	prompt := fmt.Sprintf(`You are a clustering assistant. Determine if the following two problem descriptions describe the SAME underlying problem that users face.

Problem A:
- Title: %s
- Context: %s
- Workaround: %s
- Desired outcome: %s
- Target user: %s

Problem B:
- Title: %s
- Context: %s
- Workaround: %s
- Desired outcome: %s
- Target user: %s

Answer with exactly one word: "yes" if they describe the same problem, or "no" if they describe different problems.`,
		a.Problem, a.Context, a.CurrentWorkaround, a.DesiredOutcome, a.TargetUser,
		b.Problem, b.Context, b.CurrentWorkaround, b.DesiredOutcome, b.TargetUser)

	resp, err := c.llm.Complete(ctx, domain.CompletionRequest{
		Prompt:      prompt,
		Temperature: 0,
		MaxTokens:   10,
	})
	if err != nil {
		return false, fmt.Errorf("semantic boundary check: %w", err)
	}

	answer := strings.TrimSpace(strings.ToLower(resp.Content))
	return strings.Contains(answer, "yes"), nil
}

// --- internal helpers ---

// filterProblemSignals returns only signals with IsProblemSignal == true.
func filterProblemSignals(signals []domain.ProblemSignal) []domain.ProblemSignal {
	filtered := make([]domain.ProblemSignal, 0, len(signals))
	for _, s := range signals {
		if s.IsProblemSignal {
			filtered = append(filtered, s)
		}
	}
	return filtered
}

// jaccardTokens computes the Jaccard similarity coefficient between two sorted,
// deduplicated token slices. Returns 0 for empty sets.
func jaccardTokens(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}

	setA := make(map[string]struct{}, len(a))
	for _, t := range a {
		setA[t] = struct{}{}
	}

	intersection := 0
	for _, t := range b {
		if _, ok := setA[t]; ok {
			intersection++
		}
	}

	union := len(setA)
	for _, t := range b {
		if _, ok := setA[t]; !ok {
			union++
		}
	}

	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

// agglomerate performs single-linkage agglomerative clustering. Each entry
// starts in its own group. Groups are merged when the similarity between any
// member of one group and any member of another is >= threshold.
func agglomerate(entries []fpEntry, similarity [][]float64, cfg Config) [][]int {
	n := len(entries)
	if n == 0 {
		return nil
	}

	// Each entry starts in its own group.
	parents := make([]int, n)
	for i := range parents {
		parents[i] = i
	}

	var find func(int) int
	find = func(x int) int {
		if parents[x] != x {
			parents[x] = find(parents[x])
		}
		return parents[x]
	}
	union := func(a, b int) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parents[ra] = rb
		}
	}

	// Single-linkage: merge groups if any inter-group similarity >= threshold.
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if similarity[i][j] >= cfg.JaccardThreshold {
				union(i, j)
			}
		}
	}

	// Collect groups.
	groupMap := make(map[int][]int)
	for i := 0; i < n; i++ {
		root := find(i)
		groupMap[root] = append(groupMap[root], i)
	}

	groups := make([][]int, 0, len(groupMap))
	for _, members := range groupMap {
		groups = append(groups, members)
	}
	return groups
}

// resolveEdgeCases checks pairs across groups whose similarity falls between
// SemanticThreshold and JaccardThreshold. Merges groups if semantic check
// indicates they describe the same problem.
func (c *Clusterer) resolveEdgeCases(
	ctx context.Context,
	entries []fpEntry,
	similarity [][]float64,
	groups [][]int,
) [][]int {
	if len(groups) <= 1 || c.llm == nil {
		return groups
	}

	// Build a map from original index to group index.
	idxToGroup := make(map[int]int)
	for g, members := range groups {
		for _, idx := range members {
			idxToGroup[idx] = g
		}
	}

	merged := make([]bool, len(groups))
	// Union-Find on groups.
	groupParents := make([]int, len(groups))
	for i := range groupParents {
		groupParents[i] = i
	}
	var findGroup func(int) int
	findGroup = func(x int) int {
		if groupParents[x] != x {
			groupParents[x] = findGroup(groupParents[x])
		}
		return groupParents[x]
	}
	unionGroup := func(a, b int) {
		ra, rb := findGroup(a), findGroup(b)
		if ra != rb {
			groupParents[ra] = rb
		}
	}

	// For each pair of groups, check if any cross-group pair falls in the
	// edge zone and merge if semantic check passes.
	for g1 := 0; g1 < len(groups); g1++ {
		for g2 := g1 + 1; g2 < len(groups); g2++ {
			if merged[g1] && merged[g2] {
				continue
			}
			shouldMerge := false
			for _, i := range groups[g1] {
				for _, j := range groups[g2] {
					sim := similarity[i][j]
					if sim >= c.cfg.SemanticThreshold && sim < c.cfg.JaccardThreshold {
						same, err := c.checkSemanticBoundary(ctx, entries[i].signal, entries[j].signal)
						if err == nil && same {
							shouldMerge = true
							break
						}
					}
				}
				if shouldMerge {
					break
				}
			}
			if shouldMerge {
				unionGroup(g1, g2)
				merged[g1] = true
				merged[g2] = true
			}
		}
	}

	// Re-collect groups.
	mergedGroups := make(map[int][]int)
	for g := range groups {
		root := findGroup(g)
		mergedGroups[root] = append(mergedGroups[root], groups[g]...)
	}

	result := make([][]int, 0, len(mergedGroups))
	for _, members := range mergedGroups {
		result = append(result, members)
	}
	return result
}

// buildSummary creates a human-readable summary from a group of signals.
func buildSummary(signals []domain.ProblemSignal) string {
	if len(signals) == 0 {
		return ""
	}
	if len(signals) == 1 {
		return signals[0].Problem
	}

	parts := make([]string, 0, len(signals))
	seen := make(map[string]struct{})
	for _, s := range signals {
		if s.Problem != "" {
			if _, ok := seen[s.Problem]; !ok {
				seen[s.Problem] = struct{}{}
				parts = append(parts, s.Problem)
			}
		}
	}

	if len(parts) == 0 {
		return signals[0].Problem
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return parts[0] + " (and " + fmt.Sprintf("%d", len(parts)-1) + " related descriptions)"
}

// sortClustersByTotal sorts clusters in descending order by ProblemTotal.
func sortClustersByTotal(clusters []domain.ProblemCluster) {
	for i := 0; i < len(clusters); i++ {
		for j := i + 1; j < len(clusters); j++ {
			if clusters[j].ProblemTotal > clusters[i].ProblemTotal {
				clusters[i], clusters[j] = clusters[j], clusters[i]
			}
		}
	}
}

// collectUniqueStrings collects unique non-empty strings from a getter that
// returns a slice of strings for each signal.
func collectUniqueStrings(signals []domain.ProblemSignal, getter func(domain.ProblemSignal) []string) []string {
	seen := make(map[string]struct{})
	var result []string

	for _, s := range signals {
		for _, item := range getter(s) {
			if item != "" {
				if _, ok := seen[item]; !ok {
					seen[item] = struct{}{}
					result = append(result, item)
				}
			}
		}
	}
	return result
}

// collectUniqueFields collects unique non-empty strings from a getter that
// returns a single string for each signal.
func collectUniqueFields(signals []domain.ProblemSignal, getter func(domain.ProblemSignal) string) []string {
	seen := make(map[string]struct{})
	var result []string

	for _, s := range signals {
		v := getter(s)
		if v != "" {
			if _, ok := seen[v]; !ok {
				seen[v] = struct{}{}
				result = append(result, v)
			}
		}
	}
	return result
}

// countDomains returns an estimate of the number of independent domains
// represented by the signals' sources.
func countDomains(signals []domain.ProblemSignal) int {
	domains := make(map[string]struct{})
	for _, s := range signals {
		// Use source as a proxy for domain.
		domains[s.Source] = struct{}{}
	}
	return len(domains)
}

// clamp01 clamps a float64 to the [0.0, 1.0] range.
func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1.0 {
		return 1.0
	}
	return v
}

// min returns the smaller of two ints.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
