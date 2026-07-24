// Package cli implements the SignalForge CLI commands.
package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/moontechs/signalforge/internal/config"
	"github.com/moontechs/signalforge/internal/domain"
	"github.com/moontechs/signalforge/internal/memory"
	"github.com/moontechs/signalforge/internal/storage"
)

// ExportCmd represents the signalforge export command.
var ExportCmd = newExportCmd()

func newExportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export research data in various formats",
		Long: `Exports problem clusters, solution hypotheses, and statistics in
markdown, JSON, or CSV format.

Example:
  signalforge export --format markdown
  signalforge export --format json --output report.json
  signalforge export --format csv --output report.csv`,
		RunE: runExport,
	}

	cmd.Flags().String("format", "", "Output format: markdown, json, or csv (required)")
	cmd.Flags().String("output", "", "Output file path (optional; defaults to stdout)")
	cmd.MarkFlagRequired("format")

	return cmd
}

type exportEnv struct {
	store  *storage.Storage
	mem    *memory.DefaultMemory
	format string
	output string
}

type exportData struct {
	ExportedAt string                     `json:"exported_at"`
	Clusters   []domain.ProblemCluster    `json:"clusters,omitempty"`
	Solutions  []domain.SolutionHypothesis `json:"solutions,omitempty"`
	Stats      domain.ResearchStats       `json:"stats,omitempty"`
}

func runExport(cmd *cobra.Command, _ []string) error {
	format, _ := cmd.Flags().GetString("format")
	output, _ := cmd.Flags().GetString("output")

	format = strings.ToLower(strings.TrimSpace(format))
	switch format {
	case "markdown", "json", "csv":
		// valid
	default:
		return fmt.Errorf("invalid format %q: must be markdown, json, or csv", format)
	}

	dir, err := config.GetSignalForgeDir()
	if err != nil {
		return fmt.Errorf("determine signalforge dir: %w", err)
	}

	if err := ensureStorageLayout(dir); err != nil {
		return fmt.Errorf("initialize storage layout: %w", err)
	}

	store := storage.New(dir)
	mem := memory.New(store)
	memoryPath := filepath.Join(dir, "memory.json")
	if store.Exists(memoryPath) {
		if err := mem.Load(); err != nil {
			return fmt.Errorf("load memory: %w", err)
		}
	}

	env := &exportEnv{
		store:  store,
		mem:    mem,
		format: format,
		output: output,
	}

	return executeExport(cmd, env)
}

func executeExport(cmd *cobra.Command, env *exportEnv) error {
	// Gather data.
	clusters, err := loadProblemClusters(env.store)
	if err != nil {
		return fmt.Errorf("load clusters: %w", err)
	}

	solutions, _ := loadSolutions(env.store)
	stats := env.mem.GetStats()

	data := exportData{
		ExportedAt: time.Now().Format(time.RFC3339),
		Clusters:   clusters,
		Solutions:  solutions,
		Stats:      stats,
	}

	// Generate output.
	var output string
	switch env.format {
	case "markdown":
		output = generateMarkdown(data)
	case "json":
		output = generateJSON(data)
	case "csv":
		output = generateCSV(data)
	}

	// Write output.
	if env.output != "" {
		return writeExportFile(env.output, output)
	}

	_, err = fmt.Fprint(cmd.OutOrStdout(), output)
	//nolint:wrapcheck // Fprint returns underlying I/O error, caller can wrap
	return err
}

func generateMarkdown(data exportData) string {
	var b strings.Builder

	b.WriteString("# SignalForge Research Report\n\n")
	b.WriteString(fmt.Sprintf("**Exported at:** %s\n\n", data.ExportedAt))

	// Stats section.
	b.WriteString("## Statistics\n\n")
	b.WriteString(fmt.Sprintf("| Metric | Value |\n"))
	b.WriteString(fmt.Sprintf("|--------|-------|\n"))
	b.WriteString(fmt.Sprintf("| Raw signals collected | %d |\n", data.Stats.RawSignalsCollected))
	b.WriteString(fmt.Sprintf("| Raw signals skipped | %d |\n", data.Stats.RawSignalsSkipped))
	b.WriteString(fmt.Sprintf("| Problem signals found | %d |\n", data.Stats.ProblemSignalsFound))
	b.WriteString(fmt.Sprintf("| Noise signals | %d |\n", data.Stats.NoiseSignals))
	b.WriteString(fmt.Sprintf("| Clusters created | %d |\n", data.Stats.ClustersCreated))
	b.WriteString(fmt.Sprintf("| Jobs created | %d |\n", data.Stats.JobsCreated))
	b.WriteString(fmt.Sprintf("| Ideas created | %d |\n", data.Stats.IdeasCreated))
	b.WriteString(fmt.Sprintf("| Duplicate ideas | %d |\n", data.Stats.DuplicateIdeas))
	b.WriteString(fmt.Sprintf("| LLM requests | %d |\n", data.Stats.LLMRequests))
	b.WriteString("\n")

	// Clusters section.
	b.WriteString("## Problem Clusters\n\n")
	if len(data.Clusters) == 0 {
		b.WriteString("_No clusters found._\n\n")
	} else {
		b.WriteString("| # | Title | Problem Score | Confidence | Sources | Signals |\n")
		b.WriteString("|---|-------|--------------|------------|---------|---------|\n")
		for i, c := range data.Clusters {
			total := c.ProblemScore.Total()
			if c.ProblemTotal > 0 {
				total = c.ProblemTotal
			}
			title := truncateString(c.Title, 50)
			sources := strings.Join(c.SourceTypes, ", ")
			row := fmt.Sprintf("| %d | %s | %.1f | %.2f | %s | %d |\n",
				i+1, title, total, c.Confidence, sources, c.SignalCount)
			b.WriteString(row)
		}
		b.WriteString("\n")
	}

	// Solutions section.
	b.WriteString("## Solution Hypotheses\n\n")
	if len(data.Solutions) == 0 {
		b.WriteString("_No solutions found._\n\n")
	} else {
		b.WriteString("| # | Title | Solution Score | Confidence | Recommendation | Product Type |\n")
		b.WriteString("|---|-------|---------------|------------|----------------|-------------|\n")
		for i, s := range data.Solutions {
			total := s.SolutionScore.Total()
			if s.SolutionTotal > 0 {
				total = s.SolutionTotal
			}
			title := truncateString(s.Title, 50)
			rec := string(s.Recommendation)
			pt := string(s.ProductType)
			row := fmt.Sprintf("| %d | %s | %.1f | %.2f | %s | %s |\n",
				i+1, title, total, s.Confidence, rec, pt)
			b.WriteString(row)
		}
		b.WriteString("\n")
	}

	return b.String()
}

func generateJSON(data exportData) string {
	enc, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Sprintf("{\"error\": %q}", err.Error())
	}
	return string(enc)
}

func generateCSV(data exportData) string {
	var b strings.Builder

	// Header row.
	b.WriteString("id,title,problem_total,confidence,sources,signal_count,solution_title,solution_score,recommendation\n")

	// Build a map of cluster ID to solution info.
	solutionMap := make(map[string][]domain.SolutionHypothesis)
	for _, s := range data.Solutions {
		solutionMap[s.ProblemClusterID] = append(solutionMap[s.ProblemClusterID], s)
	}

	for _, c := range data.Clusters {
		total := c.ProblemScore.Total()
		if c.ProblemTotal > 0 {
			total = c.ProblemTotal
		}
		title := csvEscape(c.Title)
		sources := csvEscape(strings.Join(c.SourceTypes, "; "))

		solutions := solutionMap[c.ID]
		if len(solutions) == 0 {
			// Write a row with no solution info.
			b.WriteString(fmt.Sprintf("%s,%s,%.1f,%.2f,%s,%d,,,\n",
				c.ID, title, total, c.Confidence, sources, c.SignalCount))
			continue
		}

		for _, s := range solutions {
			sTotal := s.SolutionScore.Total()
			if s.SolutionTotal > 0 {
				sTotal = s.SolutionTotal
			}
			sTitle := csvEscape(s.Title)
			rec := string(s.Recommendation)
			b.WriteString(fmt.Sprintf("%s,%s,%.1f,%.2f,%s,%d,%s,%.1f,%s\n",
				c.ID, title, total, c.Confidence, sources, c.SignalCount,
				sTitle, sTotal, rec))
		}
	}

	return b.String()
}

func csvEscape(s string) string {
	if strings.ContainsAny(s, ",\"\n") {
		s = strings.ReplaceAll(s, "\"", "\"\"")
		return `"` + s + `"`
	}
	return s
}

func writeExportFile(path, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	tmpFile, err := os.CreateTemp(dir, "tmp-export-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.WriteString(content); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write: %w", err)
	}

	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("sync: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename: %w", err)
	}

	// Sync directory.
	if f, err := os.Open(dir); err == nil {
		_ = f.Sync()
		_ = f.Close()
	}

	return nil
}