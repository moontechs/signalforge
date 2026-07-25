// Package cli implements the SignalForge CLI commands.
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/moontechs/signalforge/internal/config"
	"github.com/moontechs/signalforge/internal/domain"
	"github.com/moontechs/signalforge/internal/memory"
	"github.com/moontechs/signalforge/internal/storage"
)

// ---------- helpers ----------

func newTestCommand(t *testing.T) *testCommand {
	t.Helper()
	homeDir := t.TempDir()
	t.Setenv("SIGNALFORGE_HOME", homeDir)

	cfg := config.DefaultConfig()
	if err := config.SaveConfig(homeDir, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	dirs := config.DefaultDirStructure()
	for dir := range dirs {
		if err := os.MkdirAll(filepath.Join(homeDir, dir), 0o755); err != nil {
			t.Fatalf("create dir %s: %v", dir, err)
		}
	}

	store := storage.New(homeDir)
	mem := memory.New(store)

	return &testCommand{
		homeDir: homeDir,
		store:   store,
		mem:     mem,
	}
}

type testCommand struct {
	homeDir string
	store   *storage.Storage
	mem     *memory.DefaultMemory
}

func (tc *testCommand) newCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "test"}
	cmd.SetContext(context.Background())
	return cmd
}

func (tc *testCommand) seedRawSignals(t *testing.T, count int) {
	t.Helper()
	for i := 0; i < count; i++ {
		signal := domain.RawSignal{
			ID:          "raw-" + itoa(i),
			Source:      "github",
			SourceID:    "gh-" + itoa(i),
			URL:         "https://github.com/user/repo/issues/" + itoa(i),
			Title:       "Test signal " + itoa(i),
			Body:        "This is a test signal body.",
			ContentHash: "hash-" + itoa(i),
			CreatedAt:   time.Now().Add(-time.Duration(i) * time.Hour),
			CollectedAt: time.Now(),
		}
		path := filepath.Join(tc.homeDir, "raw-signals", "raw-"+itoa(i)+".json")
		if err := tc.store.SaveJSON(path, signal); err != nil {
			t.Fatalf("save raw signal: %v", err)
		}
	}
}

func (tc *testCommand) seedProblemSignals(t *testing.T, count int) {
	t.Helper()
	for i := 0; i < count; i++ {
		ps := domain.ProblemSignal{
			ID:              "ps-" + itoa(i),
			RawSignalID:     "raw-" + itoa(i),
			Source:          "github",
			IsProblemSignal: true,
			Problem:         "Problem " + itoa(i),
			Relevance:       0.8,
			SeverityHint:    0.5,
			FrequencyHint:   0.7,
			ClassifiedAt:    time.Now(),
		}
		path := filepath.Join(tc.homeDir, "problem-signals", "ps-"+itoa(i)+".json")
		if err := tc.store.SaveJSON(path, ps); err != nil {
			t.Fatalf("save problem signal: %v", err)
		}
	}
}

func (tc *testCommand) seedClusters(t *testing.T, count int) {
	t.Helper()
	for i := 0; i < count; i++ {
		c := domain.ProblemCluster{
			ID:           "cluster-" + itoa(i),
			Title:        "Cluster " + itoa(i),
			Summary:      "Summary for cluster " + itoa(i),
			SignalCount:  3,
			ProblemScore: domain.ProblemScorecard{EvidenceStrength: 6.0, Recurrence: 7.0, Severity: 5.0, WorkaroundCost: 4.0, SourceDiversity: 3.0, Longevity: 5.0, UserSpecificity: 6.0, ProductSolvability: 7.0},
			Confidence:   0.8 + float64(i)*0.05,
			SourceTypes:  []string{"github"},
			CreatedAt:    time.Now(),
		}
		path := filepath.Join(tc.homeDir, "clusters", "cluster-"+itoa(i)+".json")
		if err := tc.store.SaveJSON(path, c); err != nil {
			t.Fatalf("save cluster: %v", err)
		}
	}
}

func (tc *testCommand) seedDiscoverResult(t *testing.T, solutionCount int) {
	t.Helper()
	solutions := make([]domain.SolutionHypothesis, solutionCount)
	for i := 0; i < solutionCount; i++ {
		solutions[i] = domain.SolutionHypothesis{
			ID:               "sol-" + itoa(i),
			ProblemClusterID: "cluster-" + itoa(i),
			Title:            "Solution " + itoa(i),
			Summary:          "Summary for solution " + itoa(i),
			ProductType:      domain.ProductTypeSaaS,
			SolutionScore:    domain.SolutionScorecard{ProblemFit: 7.0, ProductTypeFit: 6.0, CompetitionGap: 5.0, BuildSimplicity: 4.0, DistributionPotential: 3.0, MonetizationPotential: 6.0, RetentionPotential: 5.0, PlatformSafety: 4.0, Defensibility: 3.0},
			Confidence:       0.7 + float64(i)*0.05,
			Recommendation:   domain.RecommendationStrongCandidate,
			CreatedAt:        time.Now(),
		}
	}
	result := DiscoverResult{
		JTBDs:     []domain.JobToBeDone{{ID: "job-0", Statement: "Statement for job"}},
		Solutions: solutions,
	}
	path := filepath.Join(tc.homeDir, "discover.json")
	if err := tc.store.SaveJSON(path, result); err != nil {
		t.Fatalf("save discover result: %v", err)
	}
}

func (tc *testCommand) seedMemoryStats(t *testing.T) {
	t.Helper()
	tc.mem.IncrementStat("raw_signals_collected")
	tc.mem.IncrementStat("raw_signals_collected")
	tc.mem.IncrementStat("raw_signals_collected")
	tc.mem.IncrementStat("problem_signals_found")
	tc.mem.IncrementStat("noise_signals")
	tc.mem.IncrementStat("clusters_created")
	tc.mem.IncrementStat("llm_requests")
	tc.mem.IncrementStat("github_requests")
	tc.mem.IncrementStat("github_requests")
	tc.mem.IncrementStat("github_cache_hits")
	if err := tc.mem.Save(); err != nil {
		t.Fatalf("save memory: %v", err)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	s := ""
	for i > 0 {
		s = string(rune('0'+i%10)) + s
		i /= 10
	}
	return s
}

// ---------- Rank tests ----------

func TestExecuteRankEmptyData(t *testing.T) {
	tc := newTestCommand(t)
	cmd := tc.newCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	env := &rankEnv{store: tc.store, problemScore: 0, solutionScore: 0, confidence: 0, limit: 0}
	if err := executeRank(cmd, env); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "No data to rank") {
		t.Fatalf("unexpected output: %s", out.String())
	}
}

func TestExecuteRankWithClustersAndSolutions(t *testing.T) {
	tc := newTestCommand(t)
	tc.seedClusters(t, 3)
	tc.seedDiscoverResult(t, 2)

	cmd := tc.newCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	env := &rankEnv{store: tc.store, problemScore: 0, solutionScore: 0, confidence: 0, limit: 0}
	if err := executeRank(cmd, env); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Ranked Results") {
		t.Fatalf("expected ranked results, got: %s", out.String())
	}
	if !strings.Contains(out.String(), "Cluster 0") {
		t.Fatalf("expected cluster in output, got: %s", out.String())
	}
}

func TestExecuteRankWithFilters(t *testing.T) {
	tc := newTestCommand(t)
	tc.seedClusters(t, 3)
	tc.seedDiscoverResult(t, 2)

	// High threshold should filter everything out.
	cmd := tc.newCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	env := &rankEnv{store: tc.store, problemScore: 100, solutionScore: 100, confidence: 1.0, limit: 0}
	if err := executeRank(cmd, env); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "No results match") {
		t.Fatalf("expected no results, got: %s", out.String())
	}
}

func TestValidateScoreRanges(t *testing.T) {
	tests := []struct {
		name          string
		problemScore  float64
		solutionScore float64
		confidence    float64
		expectError   bool
	}{
		{"valid zero", 0, 0, 0, false},
		{"valid max", 10, 10, 1, false},
		{"valid mid", 5, 5, 0.5, false},
		{"problem too high", 11, 0, 0, true},
		{"problem negative", -1, 0, 0, true},
		{"solution too high", 0, 11, 0, true},
		{"solution negative", 0, -1, 0, true},
		{"confidence too high", 0, 0, 1.1, true},
		{"confidence negative", 0, 0, -0.1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateScoreRanges(tt.problemScore, tt.solutionScore, tt.confidence)
			if tt.expectError && err == nil {
				t.Fatal("expected error got nil")
			}
			if !tt.expectError && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// ---------- Pipeline tests ----------

func TestExecutePipelineDryRun(t *testing.T) {
	tc := newTestCommand(t)
	cmd := tc.newCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	env := &pipelineEnv{dir: tc.homeDir, store: tc.store, mem: tc.mem, cfg: &config.Config{}, sources: "github", since: "30d", dryRun: true, force: false}
	if err := executePipeline(cmd, env); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Pipeline Plan") {
		t.Fatalf("expected pipeline plan, got: %s", out.String())
	}
}

func TestExecutePipelineWithExistingData(t *testing.T) {
	tc := newTestCommand(t)
	// Seed complete data so every stage can be skipped.
	tc.seedRawSignals(t, 10)
	tc.seedProblemSignals(t, 5)
	tc.seedClusters(t, 3)
	tc.seedDiscoverResult(t, 2)

	cmd := tc.newCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	env := &pipelineEnv{dir: tc.homeDir, store: tc.store, mem: tc.mem, cfg: &config.Config{}, sources: "github", since: "30d", dryRun: false, force: false}
	if err := executePipeline(cmd, env); err != nil {
		t.Fatal(err)
	}
	// Collect, classify, cluster, and discover should all be skipped.
	if !strings.Contains(out.String(), "already up-to-date") {
		t.Fatalf("expected skip messages, got: %s", out.String())
	}
	// Rank should run (no data check).
	if !strings.Contains(out.String(), "rank completed") {
		t.Fatalf("expected rank completion, got: %s", out.String())
	}
}

func TestExecutePipelineForce(t *testing.T) {
	tc := newTestCommand(t)
	// Seed complete data so every stage has data, but force should re-run them.
	tc.seedRawSignals(t, 10)
	tc.seedProblemSignals(t, 5)
	tc.seedClusters(t, 3)
	tc.seedDiscoverResult(t, 2)

	// Force re-run will try to collect again, which needs GITHUB_TOKEN.
	// We expect it to fail at collect stage because the token is invalid.
	t.Setenv("GITHUB_TOKEN", "invalid-token")

	cmd := tc.newCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	env := &pipelineEnv{dir: tc.homeDir, store: tc.store, mem: tc.mem, cfg: &config.Config{}, sources: "github", since: "30d", dryRun: false, force: true}
	err := executePipeline(cmd, env)
	// We expect an error because the pipeline tried to re-run collect with a bad token.
	if err == nil {
		t.Fatal("expected error with bad credentials, got nil")
	}
	// The output should show that collect was NOT skipped (force mode).
	if strings.Contains(out.String(), "already up-to-date") {
		t.Fatalf("force mode should not skip stages, but output shows skip: %s", out.String())
	}
	// The stage should have been attempted.
	if !strings.Contains(out.String(), "Stage 1/5: collect") {
		t.Fatalf("expected collect stage to be attempted, got: %s", out.String())
	}
}

// ---------- Stats tests ----------

func TestExecuteStatsMissingMemory(t *testing.T) {
	tc := newTestCommand(t)
	cmd := tc.newCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	// executeStats does not check for missing memory.json — it just shows zero stats.
	env := &statsEnv{store: tc.store, mem: tc.mem, json: false}
	if err := executeStats(cmd, env); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Research Statistics") {
		t.Fatalf("expected stats output, got: %s", out.String())
	}
}

func TestExecuteStatsWithMemory(t *testing.T) {
	tc := newTestCommand(t)
	tc.seedMemoryStats(t)

	cmd := tc.newCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	env := &statsEnv{store: tc.store, mem: tc.mem, json: false}
	if err := executeStats(cmd, env); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Research Statistics") {
		t.Fatalf("expected stats, got: %s", out.String())
	}
	if !strings.Contains(out.String(), "Raw signals collected:") {
		t.Fatalf("expected collection stats, got: %s", out.String())
	}
}

func TestExecuteStatsJSON(t *testing.T) {
	tc := newTestCommand(t)
	tc.seedMemoryStats(t)

	cmd := tc.newCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	env := &statsEnv{store: tc.store, mem: tc.mem, json: true}
	if err := executeStats(cmd, env); err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(out.Bytes(), &parsed); err != nil {
		t.Fatalf("invalid JSON output: %v\noutput: %s", err, out.String())
	}
	if _, ok := parsed["stats"]; !ok {
		t.Fatalf("expected 'stats' key in JSON output, got: %s", out.String())
	}
}

// ---------- Export tests ----------

func TestExecuteExportMarkdown(t *testing.T) {
	tc := newTestCommand(t)
	tc.seedClusters(t, 2)
	tc.seedDiscoverResult(t, 1)

	cmd := tc.newCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	env := &exportEnv{store: tc.store, mem: tc.mem, format: "markdown", output: ""}
	if err := executeExport(cmd, env); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "SignalForge Research Report") {
		t.Fatalf("expected markdown report title, got: %s", out.String())
	}
	if !strings.Contains(out.String(), "Problem Clusters") {
		t.Fatalf("expected clusters section, got: %s", out.String())
	}
	if !strings.Contains(out.String(), "Solution Hypotheses") {
		t.Fatalf("expected solutions section, got: %s", out.String())
	}
}

func TestExecuteExportJSON(t *testing.T) {
	tc := newTestCommand(t)
	tc.seedClusters(t, 2)
	tc.seedDiscoverResult(t, 1)

	cmd := tc.newCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	env := &exportEnv{store: tc.store, mem: tc.mem, format: "json", output: ""}
	if err := executeExport(cmd, env); err != nil {
		t.Fatal(err)
	}
	var parsed exportData
	if err := json.Unmarshal(out.Bytes(), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, out.String())
	}
	if len(parsed.Clusters) != 2 {
		t.Fatalf("expected 2 clusters, got %d", len(parsed.Clusters))
	}
	if len(parsed.Solutions) != 1 {
		t.Fatalf("expected 1 solution, got %d", len(parsed.Solutions))
	}
}

func TestExecuteExportCSV(t *testing.T) {
	tc := newTestCommand(t)
	tc.seedClusters(t, 2)
	tc.seedDiscoverResult(t, 1)

	cmd := tc.newCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	env := &exportEnv{store: tc.store, mem: tc.mem, format: "csv", output: ""}
	if err := executeExport(cmd, env); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected header + data rows, got %d lines", len(lines))
	}
	if !strings.Contains(lines[0], "id,title") {
		t.Fatalf("expected CSV header, got: %s", lines[0])
	}
}

func TestExecuteExportEmptyData(t *testing.T) {
	tc := newTestCommand(t)

	cmd := tc.newCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	env := &exportEnv{store: tc.store, mem: tc.mem, format: "markdown", output: ""}
	if err := executeExport(cmd, env); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "No clusters found") {
		t.Fatalf("expected empty clusters message, got: %s", out.String())
	}
}

func TestExecuteExportWithOutputFile(t *testing.T) {
	tc := newTestCommand(t)
	tc.seedClusters(t, 1)

	outputPath := filepath.Join(tc.homeDir, "report.md")
	cmd := tc.newCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	env := &exportEnv{store: tc.store, mem: tc.mem, format: "markdown", output: outputPath}
	if err := executeExport(cmd, env); err != nil {
		t.Fatal(err)
	}

	// Output should be empty (written to file instead).
	if out.Len() > 0 {
		t.Fatalf("expected empty stdout when writing to file, got: %s", out.String())
	}
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Fatalf("export file not created: %s", outputPath)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read export file: %v", err)
	}
	if !strings.Contains(string(data), "SignalForge Research Report") {
		t.Fatalf("export file missing expected content")
	}
}

func TestValidateFormat(t *testing.T) {
	valid := []string{"markdown", "json", "csv", "Markdown", "JSON", "CSV", "MARKDOWN"}
	invalid := []string{"html", "pdf", "xml", "yaml", "txt"}

	for _, f := range valid {
		t.Run("valid/"+f, func(t *testing.T) {
			if !validExportFormat(f) {
				t.Errorf("expected %q to be a valid export format", f)
			}
		})
	}
	for _, f := range invalid {
		t.Run("invalid/"+f, func(t *testing.T) {
			if validExportFormat(f) {
				t.Errorf("expected %q to be rejected as an export format", f)
			}
		})
	}
}
