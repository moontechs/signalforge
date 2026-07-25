package cli

import (
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

// newClassifyTestEnv creates a test environment for classify tests.
// It sets up a temp directory as SIGNALFORGE_HOME, writes a default config,
// and creates a raw-signals directory with optional pre-seeded signal files.
func newClassifyTestEnv(t *testing.T, signals []domain.RawSignal) (env *classifyEnv, homeDir string) {
	t.Helper()

	homeDir = t.TempDir()
	t.Setenv("SIGNALFORGE_HOME", homeDir)

	// Create default config. Use OpenRouter config with a model.
	cfg := config.DefaultConfig()
	cfg.OpenRouter.Model = "test-model"

	if err := config.SaveConfig(homeDir, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	// Create directory structure.
	dirs := config.DefaultDirStructure()
	for dir := range dirs {
		if err := os.MkdirAll(filepath.Join(homeDir, dir), 0o755); err != nil {
			t.Fatalf("create dir %s: %v", dir, err)
		}
	}

	store := storage.New(homeDir)

	// Save each raw signal as a JSON file.
	for i := range signals {
		filename := signals[i].ID + ".json"
		path := filepath.Join(homeDir, "raw-signals", filename)
		if err := store.SaveJSON(path, signals[i]); err != nil {
			t.Fatalf("save raw signal %s: %v", signals[i].ID, err)
		}
	}

	mem := memory.New(store)

	env = &classifyEnv{
		store:     store,
		mem:       mem,
		cfg:       cfg,
		promptDir: filepath.Join(homeDir, "..", "prompts"),
		limit:     0,
		batchSize: 0,
		model:     "",
		force:     false,
		dryRun:    false,
		resume:    false,
	}

	return env, homeDir
}

// testRawSignal creates a minimal RawSignal for testing.
func testRawSignal(id string) domain.RawSignal {
	return domain.RawSignal{
		ID:          id,
		Source:      "test",
		SourceID:    "src-" + id,
		URL:         "https://example.com/post/" + id,
		Title:       "Test signal " + id,
		Body:        "This is a test signal body describing a problem.",
		CollectedAt: time.Now(),
	}
}

func TestClassifyCmd_FlagsRegistered(t *testing.T) {
	t.Parallel()

	cmd := newClassifyCmd()
	f := cmd.Flags()

	flagNames := []string{"limit", "batch-size", "model", "force", "dry-run", "resume"}
	for _, name := range flagNames {
		flag := f.Lookup(name)
		if flag == nil {
			t.Errorf("flag %q is not registered", name)
		}
	}
}

func TestClassifyCmd_FlagDefaults(t *testing.T) {
	t.Parallel()

	cmd := newClassifyCmd()
	f := cmd.Flags()

	tests := []struct {
		name     string
		expected string
	}{
		{"limit", "0"},
		{"batch-size", "0"},
		{"model", ""},
		{"force", "false"},
		{"dry-run", "false"},
		{"resume", "false"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flag := f.Lookup(tt.name)
			if flag == nil {
				t.Fatalf("flag %q not found", tt.name)
			}
			if flag.DefValue != tt.expected {
				t.Errorf("flag %q default: expected %q, got %q", tt.name, tt.expected, flag.DefValue)
			}
		})
	}
}

func TestClassifyCmd_LimitRejectsNegative(t *testing.T) {
	t.Parallel()

	cmd := newClassifyCmd()
	cmd.SetArgs([]string{"--limit=-1"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for negative limit")
	}
}

func TestClassifyCmd_BatchSizeRejectsNegative(t *testing.T) {
	t.Parallel()

	cmd := newClassifyCmd()
	cmd.SetArgs([]string{"--batch-size=-1"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for negative batch-size")
	}
}

func TestLoadRawSignals_NoSignals(t *testing.T) {
	env, _ := newClassifyTestEnv(t, nil) // no signals

	signals, err := loadRawSignals(env)
	if err != nil {
		t.Fatalf("loadRawSignals: %v", err)
	}
	if len(signals) != 0 {
		t.Errorf("expected 0 signals, got %d", len(signals))
	}
}

func TestLoadRawSignals_WithSignals(t *testing.T) {
	signals := []domain.RawSignal{
		testRawSignal("sig-1"),
		testRawSignal("sig-2"),
	}

	env, _ := newClassifyTestEnv(t, signals)

	loaded, err := loadRawSignals(env)
	if err != nil {
		t.Fatalf("loadRawSignals: %v", err)
	}
	if len(loaded) != 2 {
		t.Errorf("expected 2 signals, got %d", len(loaded))
	}
}

func TestFilterClassifiedSignals_NoProblemSignals(t *testing.T) {
	signals := []domain.RawSignal{
		testRawSignal("sig-1"),
		testRawSignal("sig-2"),
	}

	env, _ := newClassifyTestEnv(t, signals)

	filtered := filterClassifiedSignals(signals, env)
	if len(filtered) != 2 {
		t.Errorf("expected 2 signals (none classified yet), got %d", len(filtered))
	}
}

func TestFilterClassifiedSignals_WithClassified(t *testing.T) {
	signals := []domain.RawSignal{
		testRawSignal("sig-1"),
		testRawSignal("sig-2"),
	}

	env, homeDir := newClassifyTestEnv(t, signals)

	// Create a problem signal that references sig-1.
	ps := domain.ProblemSignal{
		ID:          "ps_001",
		RawSignalID: "sig-1",
		Source:      "test",
	}

	store := storage.New(homeDir)
	if err := store.SaveJSON(filepath.Join(homeDir, "problem-signals", "ps_001.json"), ps); err != nil {
		t.Fatalf("save problem signal: %v", err)
	}

	filtered := filterClassifiedSignals(signals, env)
	if len(filtered) != 1 {
		t.Errorf("expected 1 unclassified signal, got %d", len(filtered))
	}
	if len(filtered) > 0 && filtered[0].ID == "sig-1" {
		t.Error("sig-1 should have been filtered out")
	}
	if len(filtered) > 0 && filtered[0].ID != "sig-2" {
		t.Errorf("expected sig-2 to remain, got %s", filtered[0].ID)
	}
}

func TestFilterClassifiedSignals_ForceReclassifies(t *testing.T) {
	signals := []domain.RawSignal{
		testRawSignal("sig-1"),
		testRawSignal("sig-2"),
	}

	env, homeDir := newClassifyTestEnv(t, signals)
	env.force = true

	// Create a problem signal that references sig-1.
	ps := domain.ProblemSignal{
		ID:          "ps_001",
		RawSignalID: "sig-1",
		Source:      "test",
	}

	store := storage.New(homeDir)
	if err := store.SaveJSON(filepath.Join(homeDir, "problem-signals", "ps_001.json"), ps); err != nil {
		t.Fatalf("save problem signal: %v", err)
	}

	filtered := filterClassifiedSignals(signals, env)
	if len(filtered) != 2 {
		t.Errorf("expected 2 signals (force re-classifies all), got %d", len(filtered))
	}
}

func TestFilterClassifiedSignals_Limit(t *testing.T) {
	signals := []domain.RawSignal{
		testRawSignal("sig-1"),
		testRawSignal("sig-2"),
		testRawSignal("sig-3"),
	}

	env, _ := newClassifyTestEnv(t, signals)
	env.limit = 2

	filtered := filterClassifiedSignals(signals, env)
	if len(filtered) != 3 {
		t.Errorf("expected 3 signals before limit, got %d", len(filtered))
	}

	// Apply limit.
	if env.limit > 0 && len(filtered) > env.limit {
		filtered = filtered[:env.limit]
	}
	if len(filtered) != 2 {
		t.Errorf("expected 2 signals after limit, got %d", len(filtered))
	}
}

func TestClassifyDryRun_Output(t *testing.T) {
	signals := []domain.RawSignal{
		testRawSignal("sig-1"),
		testRawSignal("sig-2"),
	}

	env, _ := newClassifyTestEnv(t, signals)
	env.dryRun = true

	cmd := &cobra.Command{}
	buf := new(strings.Builder)
	cmd.SetOut(buf)

	// Simulate dry-run path.
	printClassifyDryRun(cmd, env, signals, 20, "test-model", "/prompts/classify_signal.txt")

	output := buf.String()
	if !strings.Contains(output, "dry-run") {
		t.Errorf("expected dry-run header, got: %s", output)
	}
	if !strings.Contains(output, "Signals to classify: 2") {
		t.Errorf("expected signal count, got: %s", output)
	}
	if !strings.Contains(output, "Batch size: 20") {
		t.Errorf("expected batch size, got: %s", output)
	}
	if !strings.Contains(output, "Model: test-model") {
		t.Errorf("expected model, got: %s", output)
	}
	if !strings.Contains(output, "No API calls were made") {
		t.Errorf("expected no-api-calls message, got: %s", output)
	}
}

func TestExecuteClassify_NoSignals(t *testing.T) {
	env, _ := newClassifyTestEnv(t, nil) // no signals

	cmd := &cobra.Command{}
	buf := new(strings.Builder)
	cmd.SetOut(buf)

	err := executeClassify(cmd, env)
	if err != nil {
		t.Fatalf("executeClassify with no signals: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "No raw signals found") {
		t.Errorf("expected no-signals message, got: %s", output)
	}
}

func TestExecuteClassify_DryRun(t *testing.T) {
	signals := []domain.RawSignal{
		testRawSignal("sig-1"),
	}

	env, _ := newClassifyTestEnv(t, signals)
	env.dryRun = true

	cmd := &cobra.Command{}
	buf := new(strings.Builder)
	cmd.SetOut(buf)

	err := executeClassify(cmd, env)
	if err != nil {
		t.Fatalf("executeClassify dry-run: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "dry-run") {
		t.Errorf("expected dry-run in output, got: %s", output)
	}
	if !strings.Contains(output, "Signals to classify: 1") {
		t.Errorf("expected signal count in output, got: %s", output)
	}
}

func TestExecuteClassify_DryRunWithLimit(t *testing.T) {
	signals := []domain.RawSignal{
		testRawSignal("sig-1"),
		testRawSignal("sig-2"),
		testRawSignal("sig-3"),
	}

	env, _ := newClassifyTestEnv(t, signals)
	env.dryRun = true
	env.limit = 2

	cmd := &cobra.Command{}
	buf := new(strings.Builder)
	cmd.SetOut(buf)

	err := executeClassify(cmd, env)
	if err != nil {
		t.Fatalf("executeClassify dry-run with limit: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Signals to classify: 2") {
		t.Errorf("expected 2 signals in output (limited), got: %s", output)
	}
}

func TestExecuteClassify_ForceDryRun(t *testing.T) {
	signals := []domain.RawSignal{
		testRawSignal("sig-1"),
	}

	env, homeDir := newClassifyTestEnv(t, signals)
	env.dryRun = true
	env.force = true

	// Create a problem signal that would normally be skipped.
	ps := domain.ProblemSignal{
		ID:          "ps_001",
		RawSignalID: "sig-1",
		Source:      "test",
	}
	store := storage.New(homeDir)
	if err := store.SaveJSON(filepath.Join(homeDir, "problem-signals", "ps_001.json"), ps); err != nil {
		t.Fatalf("save problem signal: %v", err)
	}

	cmd := &cobra.Command{}
	buf := new(strings.Builder)
	cmd.SetOut(buf)

	err := executeClassify(cmd, env)
	if err != nil {
		t.Fatalf("executeClassify force dry-run: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Signals to classify: 1") {
		t.Errorf("expected 1 signal (force bypasses filter), got: %s", output)
	}
}

func TestExecuteClassify_WithAPIClientFailsNoKey(t *testing.T) {
	// This test verifies that without OPENROUTER_API_KEY, the command fails.
	signals := []domain.RawSignal{
		testRawSignal("sig-1"),
	}

	env, _ := newClassifyTestEnv(t, signals)
	env.dryRun = false

	cmd := &cobra.Command{}
	buf := new(strings.Builder)
	cmd.SetOut(buf)

	// Unset the API key by setting to a test-only value via env override.
	// We set it to empty to simulate missing key.
	t.Setenv("OPENROUTER_API_KEY", "")

	err := executeClassify(cmd, env)
	if err == nil {
		t.Fatal("expected error when OPENROUTER_API_KEY is empty")
	}
	if !strings.Contains(err.Error(), "OPENROUTER_API_KEY") {
		t.Errorf("expected OPENROUTER_API_KEY error, got: %v", err)
	}
}

func TestPrintClassifySummary(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	buf := new(strings.Builder)
	cmd.SetOut(buf)

	printClassifySummary(cmd, 5, 1, 3, 2, 6, false)

	output := buf.String()
	if !strings.Contains(output, "Classification Summary") {
		t.Errorf("expected summary header, got: %s", output)
	}
	if !strings.Contains(output, "Signals processed: 6") {
		t.Errorf("expected processed count, got: %s", output)
	}
	if !strings.Contains(output, "Problem signals found: 3") {
		t.Errorf("expected problem signal count, got: %s", output)
	}
	if !strings.Contains(output, "Noise signals: 2") {
		t.Errorf("expected noise count, got: %s", output)
	}
	if !strings.Contains(output, "Saved: 5") {
		t.Errorf("expected saved count, got: %s", output)
	}
	if !strings.Contains(output, "Classification failures: 1") {
		t.Errorf("expected failure count, got: %s", output)
	}
}

func TestPrintClassifySummary_Force(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	buf := new(strings.Builder)
	cmd.SetOut(buf)

	printClassifySummary(cmd, 2, 0, 2, 0, 2, true)

	output := buf.String()
	if !strings.Contains(output, "Mode: force") {
		t.Errorf("expected force mode, got: %s", output)
	}
}

func TestPrintClassifySummary_NoFailures(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	buf := new(strings.Builder)
	cmd.SetOut(buf)

	printClassifySummary(cmd, 3, 0, 2, 1, 3, false)

	output := buf.String()
	if strings.Contains(output, "Classification failures") {
		t.Errorf("unexpected failure count in output: %s", output)
	}
}

func TestFilterClassifiedSignals_EmptySignals(t *testing.T) {
	env, _ := newClassifyTestEnv(t, nil)

	filtered := filterClassifiedSignals(nil, env)
	if len(filtered) != 0 {
		t.Errorf("expected 0 for nil input, got %d", len(filtered))
	}

	filtered = filterClassifiedSignals([]domain.RawSignal{}, env)
	if len(filtered) != 0 {
		t.Errorf("expected 0 for empty input, got %d", len(filtered))
	}
}

func TestLoadRawSignals_InvalidFile(t *testing.T) {
	env, homeDir := newClassifyTestEnv(t, nil)

	// Write an invalid JSON file to raw-signals/.
	invalidPath := filepath.Join(homeDir, "raw-signals", "invalid.json")
	if err := os.WriteFile(invalidPath, []byte("{invalid json}"), 0o644); err != nil {
		t.Fatalf("write invalid file: %v", err)
	}

	// Also write a valid file to ensure it's not affected.
	store := storage.New(homeDir)
	validSignal := testRawSignal("valid-1")
	if err := store.SaveJSON(filepath.Join(homeDir, "raw-signals", "valid-1.json"), validSignal); err != nil {
		t.Fatalf("save valid signal: %v", err)
	}

	signals, err := loadRawSignals(env)
	if err != nil {
		t.Fatalf("loadRawSignals: %v", err)
	}
	// The invalid file should be skipped, and the valid one loaded.
	if len(signals) != 1 {
		t.Errorf("expected 1 valid signal, got %d", len(signals))
	}
	if len(signals) > 0 && signals[0].ID != "valid-1" {
		t.Errorf("expected valid-1, got %s", signals[0].ID)
	}
}

func TestExecuteClassify_WithoutAPIKey(t *testing.T) {
	// This test verifies the classify command fails cleanly without an API key.
	signals := []domain.RawSignal{
		testRawSignal("sig-1"),
		testRawSignal("sig-2"),
	}

	env, _ := newClassifyTestEnv(t, signals)

	cmd := &cobra.Command{}
	buf := new(strings.Builder)
	cmd.SetOut(buf)

	// Unset the API key.
	t.Setenv("OPENROUTER_API_KEY", "")

	err := executeClassify(cmd, env)
	if err == nil {
		t.Fatal("expected error without API key")
	}
	if !strings.Contains(err.Error(), "OPENROUTER_API_KEY") {
		t.Errorf("expected OPENROUTER_API_KEY error, got: %v", err)
	}
}
