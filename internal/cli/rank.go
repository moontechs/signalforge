// Package cli implements the SignalForge CLI commands.
package cli

import (
	"errors"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	"github.com/moontechs/signalforge/internal/config"
	"github.com/moontechs/signalforge/internal/domain"
	"github.com/moontechs/signalforge/internal/storage"
)

// RankCmd represents the signalforge rank command.
var RankCmd = newRankCmd()

func newRankCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rank",
		Short: "Rank problem clusters and solutions by score",
		Long: `Loads problem clusters and solution hypotheses, applies score and confidence
thresholds, and prints them sorted by score descending.

Example:
  signalforge rank
  signalforge rank --problem-score 5.0 --confidence 0.3
  signalforge rank --solution-score 4.0 --limit 10`,
		RunE: runRank,
	}

	cmd.Flags().Float64("problem-score", 0, "Minimum problem score threshold (0.0-10.0)")
	cmd.Flags().Float64("solution-score", 0, "Minimum solution score threshold (0.0-10.0)")
	cmd.Flags().Float64("confidence", 0, "Minimum confidence threshold (0.0-1.0)")
	cmd.Flags().Int("limit", 0, "Maximum number of results to show (0 = no limit)")

	return cmd
}

type rankEnv struct {
	store         *storage.Storage
	problemScore  float64
	solutionScore float64
	confidence    float64
	limit         int
}

type rankedCluster struct {
	Cluster      domain.ProblemCluster
	ProblemTotal float64
}

type rankedSolution struct {
	Solution      domain.SolutionHypothesis
	SolutionTotal float64
}

func runRank(cmd *cobra.Command, _ []string) error {
	problemScore, _ := cmd.Flags().GetFloat64("problem-score")
	solutionScore, _ := cmd.Flags().GetFloat64("solution-score")
	confidence, _ := cmd.Flags().GetFloat64("confidence")
	limit, _ := cmd.Flags().GetInt("limit")

	// Validate score ranges.
	if err := validateScoreRanges(problemScore, solutionScore, confidence); err != nil {
		return err
	}
	if limit < 0 {
		return errors.New("--limit must be a non-negative integer")
	}

	dir, err := config.GetSignalForgeDir()
	if err != nil {
		return fmt.Errorf("determine signalforge dir: %w", err)
	}

	if err := ensureStorageLayout(dir); err != nil {
		return fmt.Errorf("initialize storage layout: %w", err)
	}

	env := &rankEnv{
		store:         storage.New(dir),
		problemScore:  problemScore,
		solutionScore: solutionScore,
		confidence:    confidence,
		limit:         limit,
	}

	return executeRank(cmd, env)
}

func validateScoreRanges(problemScore, solutionScore, confidence float64) error {
	if problemScore < 0 || problemScore > 10.0 {
		return errors.New("--problem-score must be between 0.0 and 10.0")
	}
	if solutionScore < 0 || solutionScore > 10.0 {
		return errors.New("--solution-score must be between 0.0 and 10.0")
	}
	if confidence < 0 || confidence > 1.0 {
		return errors.New("--confidence must be between 0.0 and 1.0")
	}
	return nil
}

func executeRank(cmd *cobra.Command, env *rankEnv) error {
	w := cmd.OutOrStdout()

	// Load clusters.
	clusters, err := loadProblemClusters(env.store)
	if err != nil {
		return fmt.Errorf("load clusters: %w", err)
	}

	// Load solutions from discover.json.
	solutions, err := loadSolutions(env.store)
	if err != nil {
		return fmt.Errorf("load solutions: %w", err)
	}

	if len(clusters) == 0 && len(solutions) == 0 {
		_, _ = fmt.Fprintln(w, "No data to rank. Run 'signalforge pipeline' or individual commands first.")
		return nil
	}

	// Filter and rank clusters.
	rankedClusters := rankClusters(clusters, env.problemScore, env.confidence)

	// Filter and rank solutions.
	rankedSolutions := rankSolutions(solutions, env.solutionScore, env.confidence)

	// Print results.
	printRankedResults(w, rankedClusters, rankedSolutions, env.limit)

	return nil
}

func loadSolutions(store *storage.Storage) ([]domain.SolutionHypothesis, error) {
	discoverPath := filepath.Join(store.BaseDir(), "discover.json")
	if !store.Exists(discoverPath) {
		return nil, nil
	}

	var result DiscoverResult
	if err := store.LoadJSON(discoverPath, &result); err != nil {
		return nil, fmt.Errorf("load discover.json: %w", err)
	}

	return result.Solutions, nil
}

func rankClusters(clusters []domain.ProblemCluster, minProblemScore, minConfidence float64) []rankedCluster {
	return filterAndRank(
		clusters,
		func(cluster *domain.ProblemCluster) (float64, float64, float64) {
			return cluster.ProblemScore.Total(), cluster.ProblemTotal, cluster.Confidence
		},
		func(cluster domain.ProblemCluster, total float64) rankedCluster {
			return rankedCluster{Cluster: cluster, ProblemTotal: total}
		},
		func(cluster *rankedCluster) float64 { return cluster.ProblemTotal },
		minProblemScore,
		minConfidence,
	)
}

func rankSolutions(solutions []domain.SolutionHypothesis, minSolutionScore, minConfidence float64) []rankedSolution {
	return filterAndRank(
		solutions,
		func(solution *domain.SolutionHypothesis) (float64, float64, float64) {
			return solution.SolutionScore.Total(), solution.SolutionTotal, solution.Confidence
		},
		func(solution domain.SolutionHypothesis, total float64) rankedSolution {
			return rankedSolution{Solution: solution, SolutionTotal: total}
		},
		func(solution *rankedSolution) float64 { return solution.SolutionTotal },
		minSolutionScore,
		minConfidence,
	)
}

func filterAndRank[T, R any](
	values []T,
	metrics func(*T) (calculated, stored, confidence float64),
	build func(T, float64) R,
	score func(*R) float64,
	minScore, minConfidence float64,
) []R {
	result := make([]R, 0, len(values))
	for index := range values {
		calculated, stored, confidence := metrics(&values[index])
		total, eligible := eligibleRank(calculated, stored, confidence, minScore, minConfidence)
		if eligible {
			result = append(result, build(values[index], total))
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return score(&result[i]) > score(&result[j])
	})
	return result
}

func eligibleRank(calculated, stored, confidence, minScore, minConfidence float64) (float64, bool) {
	const epsilon = 1e-9
	if stored > 0 {
		calculated = stored
	}
	return calculated, calculated+epsilon >= minScore && confidence+epsilon >= minConfidence
}

func printRankedResults(w io.Writer, clusters []rankedCluster, solutions []rankedSolution, limit int) {
	printf := func(format string, args ...any) {
		_, _ = fmt.Fprintf(w, format, args...)
	}

	totalResults := len(clusters) + len(solutions)

	// Merge clusters and solutions into a single sorted list.
	type rankedItem struct {
		score     float64
		isCluster bool
		cluster   *rankedCluster
		solution  *rankedSolution
	}

	items := make([]rankedItem, 0, totalResults)
	for i := range clusters {
		items = append(items, rankedItem{
			score:     clusters[i].ProblemTotal,
			isCluster: true,
			cluster:   &clusters[i],
		})
	}
	for i := range solutions {
		items = append(items, rankedItem{
			score:     solutions[i].SolutionTotal,
			isCluster: false,
			solution:  &solutions[i],
		})
	}

	// Sort all items by score descending.
	sort.Slice(items, func(i, j int) bool {
		return items[i].score > items[j].score
	})

	// Apply limit.
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}

	if len(items) == 0 {
		printf("No results match the given thresholds.\n")
		return
	}

	printf("=== Ranked Results ===\n")
	printf("%-4s %-10s %-40s %-10s %-12s %s\n", "#", "Type", "Title", "Score", "Confidence", "Recommendation")
	printf("---- ---------- ---------------------------------------- ---------- ------------ ------------\n")

	for i, item := range items {
		num := i + 1
		if item.isCluster {
			c := item.cluster
			title := truncateString(c.Cluster.Title, 40)
			conf := formatFloat(c.Cluster.Confidence, 2)
			printf("%-4d %-10s %-40s %-10.1f %-12s %s\n", num, "cluster", title, math.Round(c.ProblemTotal*10)/10, conf, "")
		} else {
			s := item.solution
			title := truncateString(s.Solution.Title, 40)
			conf := formatFloat(s.Solution.Confidence, 2)
			rec := string(s.Solution.Recommendation)
			printf("%-4d %-10s %-40s %-10.1f %-12s %s\n", num, "solution", title, math.Round(s.SolutionTotal*10)/10, conf, rec)
		}
	}

	printf("\n")
	printf("Showing %d of %d total items.\n", len(items), totalResults)
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func formatFloat(v float64, decimals int) string {
	format := fmt.Sprintf("%%.%df", decimals)
	return fmt.Sprintf(format, v)
}
