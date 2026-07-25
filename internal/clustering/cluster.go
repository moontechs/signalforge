// Package clustering provides deterministic text normalization, fingerprinting,
// similarity scoring, and cluster construction for problem signals.
package clustering

import (
	"context"
	"fmt"
	"sort"
	"strconv"
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
	JaccardThreshold  float64 // Default: 0.3.
	EntityWeight      float64 // Default: 0.25.
	ActionWeight      float64 // Default: 0.25.
	KeywordWeight     float64 // Default: 0.25.
	TextWeight        float64 // Default: 0.25.
	SemanticThreshold float64 // Jaccard below this triggers a semantic check. Default: 0.15.
	MinClusterSize    int     // Default: 1.
	MaxClusters       int     // Default: 0 (unlimited).
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
	llm     domain.LLMClient // Optional, for semantic edge checks.
	storage *storage.Storage // Used for loading and saving clusters.
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
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Filter to only problem signals.
	problemSignals := filterProblemSignals(signals)
	if len(problemSignals) == 0 {
		return nil, nil
	}

	// Precompute fingerprints for each signal.
	entries := make([]fpEntry, len(problemSignals))
	for index := range problemSignals {
		entries[index] = fpEntry{
			signal: problemSignals[index],
			fp:     c.signalFingerprint(&problemSignals[index]),
		}
	}

	// Compute pairwise similarity matrix.
	n := len(entries)
	similarity := make([][]float64, n)
	for i := range similarity {
		similarity[i] = make([]float64, n)
		for j := range similarity[i] {
			similarity[i][j] = c.computeSimilarity(&entries[i].fp, &entries[j].fp)
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
func (c *Clusterer) signalFingerprint(sig *domain.ProblemSignal) signalFP {
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
func (c *Clusterer) computeSimilarity(a, b *signalFP) float64 {
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

	title := mostCommonProblem(signals)
	keywords := collectUniqueStrings(signals, func(s *domain.ProblemSignal) []string { return s.Keywords })
	entities := collectUniqueStrings(signals, func(s *domain.ProblemSignal) []string { return s.Entities })
	actions := collectUniqueStrings(signals, func(s *domain.ProblemSignal) []string { return s.Actions })
	contexts := collectUniqueFields(signals, func(s *domain.ProblemSignal) string { return s.Context })
	targetUsers := collectUniqueFields(signals, func(s *domain.ProblemSignal) string { return s.TargetUser })
	workarounds := collectUniqueFields(signals, func(s *domain.ProblemSignal) string { return s.CurrentWorkaround })
	outcomes := collectUniqueFields(signals, func(s *domain.ProblemSignal) string { return s.DesiredOutcome })
	constraints := collectUniqueStrings(signals, func(s *domain.ProblemSignal) []string { return s.Constraints })

	evidence := summarizeEvidence(signals)
	scorecard := c.computeProblemScore(signals)
	confidence := c.computeConfidence(signals, scorecard)
	now := time.Now()

	return domain.ProblemCluster{
		ID:                      "cluster_" + evidence.signalIDs[0][:min(16, len(evidence.signalIDs[0]))],
		CreatedAt:               now,
		UpdatedAt:               now,
		Title:                   title,
		Summary:                 buildSummary(signals),
		Problem:                 title,
		TargetUsers:             targetUsers,
		Contexts:                contexts,
		CurrentWorkarounds:      workarounds,
		DesiredOutcomes:         outcomes,
		Constraints:             constraints,
		SignalIDs:               evidence.signalIDs,
		RepresentativeSignalIDs: evidence.representativeIDs,
		SignalCount:             len(signals),
		IndependentSources:      len(evidence.sourceTypes),
		IndependentDomains:      countDomains(signals),
		SourceTypes:             evidence.sourceTypes,
		FirstObservedAt:         evidence.firstObserved,
		LastObservedAt:          evidence.lastObserved,
		Keywords:                keywords,
		Entities:                entities,
		Actions:                 actions,
		ProblemScore:            scorecard,
		ProblemTotal:            scorecard.Total(),
		Confidence:              confidence,
		Status:                  "new",
	}
}

type clusterEvidence struct {
	signalIDs         []string
	representativeIDs []string
	sourceTypes       []string
	firstObserved     time.Time
	lastObserved      time.Time
}

type representativeCandidate struct {
	id        string
	relevance float64
}

func mostCommonProblem(signals []domain.ProblemSignal) string {
	counts := make(map[string]int)
	for index := range signals {
		counts[signals[index].Problem]++
	}

	title := signals[0].Problem
	for index := range signals {
		if counts[signals[index].Problem] > counts[title] {
			title = signals[index].Problem
		}
	}
	return title
}

func summarizeEvidence(signals []domain.ProblemSignal) clusterEvidence {
	evidence := clusterEvidence{
		signalIDs: make([]string, len(signals)),
	}
	sources := make(map[string]struct{})
	candidates := make([]representativeCandidate, len(signals))
	for index := range signals {
		signal := &signals[index]
		evidence.signalIDs[index] = signal.ID
		candidates[index] = representativeCandidate{id: signal.ID, relevance: signal.Relevance}
		sources[signal.Source] = struct{}{}
		if evidence.firstObserved.IsZero() || signal.ClassifiedAt.Before(evidence.firstObserved) {
			evidence.firstObserved = signal.ClassifiedAt
		}
		if signal.ClassifiedAt.After(evidence.lastObserved) {
			evidence.lastObserved = signal.ClassifiedAt
		}
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].relevance > candidates[j].relevance
	})
	limit := min(5, len(candidates))
	evidence.representativeIDs = make([]string, limit)
	for index := range evidence.representativeIDs {
		evidence.representativeIDs[index] = candidates[index].id
	}
	evidence.sourceTypes = make([]string, 0, len(sources))
	for source := range sources {
		evidence.sourceTypes = append(evidence.sourceTypes, source)
	}
	sort.Strings(evidence.sourceTypes)
	return evidence
}

// computeProblemScore computes the 8-dimension ProblemScorecard for a group
// of related signals.
func (c *Clusterer) computeProblemScore(signals []domain.ProblemSignal) domain.ProblemScorecard {
	if len(signals) == 0 {
		return domain.ProblemScorecard{}
	}

	n := float64(len(signals))
	var totalRelevance, recurringCount, totalSeverity, workaroundCount, solvableCount float64
	uniqueSources := make(map[string]struct{})
	uniqueUsers := make(map[string]struct{})
	var firstObserved, lastObserved time.Time
	for index := range signals {
		signal := &signals[index]
		totalRelevance += signal.Relevance
		totalSeverity += signal.SeverityHint
		if signal.Recurring {
			recurringCount++
		}
		if signal.CurrentWorkaround != "" {
			workaroundCount++
		}
		if signal.ProductSolvable {
			solvableCount++
		}
		uniqueSources[signal.Source] = struct{}{}
		if signal.TargetUser != "" {
			uniqueUsers[signal.TargetUser] = struct{}{}
		}
		if signal.ClassifiedAt.Before(firstObserved) || firstObserved.IsZero() {
			firstObserved = signal.ClassifiedAt
		}
		if signal.ClassifiedAt.After(lastObserved) {
			lastObserved = signal.ClassifiedAt
		}
	}

	return domain.ProblemScorecard{
		EvidenceStrength:   clamp01(totalRelevance / n),
		Recurrence:         clamp01(recurringCount / n),
		Severity:           clamp01(totalSeverity / (n * 10)),
		WorkaroundCost:     clamp01(workaroundCount / n),
		SourceDiversity:    clamp01(float64(len(uniqueSources)) / n),
		Longevity:          longevityScore(firstObserved, lastObserved),
		UserSpecificity:    clamp01(float64(len(uniqueUsers)) / n),
		ProductSolvability: clamp01(solvableCount / n),
	}
}

func longevityScore(firstObserved, lastObserved time.Time) float64 {
	if firstObserved.IsZero() || lastObserved.IsZero() || !lastObserved.After(firstObserved) {
		return 0
	}

	days := lastObserved.Sub(firstObserved).Hours() / 24
	switch {
	case days >= 90:
		return 1
	case days >= 30:
		return 0.75
	case days >= 7:
		return 0.5
	case days >= 1:
		return 0.25
	default:
		return 0.1
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
	for index := range signals {
		uniqueSources[signals[index].Source] = struct{}{}
	}
	sourceScore := float64(len(uniqueSources)) / 3.0
	if sourceScore > 1.0 {
		sourceScore = 1.0
	}

	// Score magnitude: how strong is the weighted total relative to max (10).
	scoreMagnitude := score.Total() / 10.0

	// Recurrence evidence.
	var recurringCount float64
	for index := range signals {
		if signals[index].Recurring {
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
func (c *Clusterer) checkSemanticBoundary(ctx context.Context, a, b *domain.ProblemSignal) (bool, error) {
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
	for index := range signals {
		if signals[index].IsProblemSignal {
			filtered = append(filtered, signals[index])
		}
	}
	return filtered
}

// jaccardTokens computes the Jaccard similarity coefficient between two sorted,
// deduplicated token slices. Returns 0 for empty sets.
func jaccardTokens(a, b []string) float64 {
	return FingerprintOverlap(a, b)
}

// agglomerate performs single-linkage agglomerative clustering. Each entry
// starts in its own group. Groups are merged when the similarity between any
// member of one group and any member of another is >= threshold.
func agglomerate(entries []fpEntry, similarity [][]float64, cfg Config) [][]int {
	n := len(entries)
	if n == 0 {
		return nil
	}

	set := newDisjointSet(n)

	// Single-linkage: merge groups if any inter-group similarity >= threshold.
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if similarity[i][j] >= cfg.JaccardThreshold {
				set.union(i, j)
			}
		}
	}

	// Collect groups.
	groupMap := make(map[int][]int)
	for i := 0; i < n; i++ {
		root := set.find(i)
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

	merged := newDisjointSet(len(groups))
	for g1 := 0; g1 < len(groups); g1++ {
		for g2 := g1 + 1; g2 < len(groups); g2++ {
			if merged.find(g1) == merged.find(g2) {
				continue
			}
			if c.groupsMatch(ctx, entries, similarity, groups[g1], groups[g2]) {
				merged.union(g1, g2)
			}
		}
	}

	mergedGroups := make(map[int][]int)
	for g := range groups {
		root := merged.find(g)
		mergedGroups[root] = append(mergedGroups[root], groups[g]...)
	}

	result := make([][]int, 0, len(mergedGroups))
	for _, members := range mergedGroups {
		result = append(result, members)
	}
	return result
}

func (c *Clusterer) groupsMatch(
	ctx context.Context,
	entries []fpEntry,
	similarity [][]float64,
	firstGroup, secondGroup []int,
) bool {
	for _, first := range firstGroup {
		for _, second := range secondGroup {
			score := similarity[first][second]
			if score < c.cfg.SemanticThreshold || score >= c.cfg.JaccardThreshold {
				continue
			}
			same, err := c.checkSemanticBoundary(ctx, &entries[first].signal, &entries[second].signal)
			if err == nil && same {
				return true
			}
		}
	}
	return false
}

type disjointSet []int

func newDisjointSet(size int) disjointSet {
	set := make(disjointSet, size)
	for index := range set {
		set[index] = index
	}
	return set
}

func (s disjointSet) find(value int) int {
	if s[value] != value {
		s[value] = s.find(s[value])
	}
	return s[value]
}

func (s disjointSet) union(first, second int) {
	firstRoot, secondRoot := s.find(first), s.find(second)
	if firstRoot != secondRoot {
		s[firstRoot] = secondRoot
	}
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
	for index := range signals {
		problem := signals[index].Problem
		if problem != "" {
			if _, ok := seen[problem]; !ok {
				seen[problem] = struct{}{}
				parts = append(parts, problem)
			}
		}
	}

	if len(parts) == 0 {
		return signals[0].Problem
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return parts[0] + " (and " + strconv.Itoa(len(parts)-1) + " related descriptions)"
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
func collectUniqueStrings(signals []domain.ProblemSignal, getter func(*domain.ProblemSignal) []string) []string {
	seen := make(map[string]struct{})
	var result []string

	for index := range signals {
		for _, item := range getter(&signals[index]) {
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
func collectUniqueFields(signals []domain.ProblemSignal, getter func(*domain.ProblemSignal) string) []string {
	seen := make(map[string]struct{})
	var result []string

	for index := range signals {
		v := getter(&signals[index])
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
	for index := range signals {
		// Use source as a proxy for domain.
		domains[signals[index].Source] = struct{}{}
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
