package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/moontechs/signalforge/internal/config"
	"github.com/moontechs/signalforge/internal/domain"
	"github.com/moontechs/signalforge/internal/memory"
	"github.com/moontechs/signalforge/internal/sources/github"
	"github.com/moontechs/signalforge/internal/storage"
)

func TestParseSinceWindow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input   string
		want    time.Duration
		wantErr string
	}{
		{input: "30d", want: 30 * 24 * time.Hour},
		{input: "24h", want: 24 * time.Hour},
		{input: "0d", wantErr: "greater than zero"},
		{input: "tomorrow", wantErr: "invalid since window"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, err := parseSinceWindow(tc.input)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("parseSinceWindow(%q) error = %v", tc.input, err)
				}
				if got != tc.want {
					t.Fatalf("parseSinceWindow(%q) = %s, want %s", tc.input, got, tc.want)
				}
				return
			}
			if err == nil {
				t.Fatalf("parseSinceWindow(%q) expected error containing %q", tc.input, tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("parseSinceWindow(%q) error = %v, want substring %q", tc.input, err, tc.wantErr)
			}
		})
	}
}

func TestResolveCollectSources(t *testing.T) {
	t.Parallel()

	got, err := resolveCollectSources("github, GH")
	if err != nil {
		t.Fatalf("resolveCollectSources() error = %v", err)
	}
	if len(got) != 1 || got[0] != "github" {
		t.Fatalf("resolveCollectSources() = %v, want [github]", got)
	}

	if _, err := resolveCollectSources("unknown"); err == nil {
		t.Fatal("resolveCollectSources() expected error for unsupported source")
	}
}

func TestRunCollectInvokesGitHubCollector(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SIGNALFORGE_HOME", dir)
	t.Setenv("GITHUB_TOKEN", "test-token")

	cfg := config.DefaultConfig()
	cfg.Sources.GitHub.Repositories = []string{"acme/api"}
	if err := config.SaveConfig(dir, cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	originalGetDir := getSignalForgeDir
	originalLoadConfig := loadConfig
	originalNewClient := newGitHubClient
	originalNewCollector := newGitHubCollector
	defer func() {
		getSignalForgeDir = originalGetDir
		loadConfig = originalLoadConfig
		newGitHubClient = originalNewClient
		newGitHubCollector = originalNewCollector
	}()

	getSignalForgeDir = func() (string, error) { return dir, nil }
	loadConfig = config.LoadConfig
	newGitHubClient = func(cfg github.ClientConfig) (*github.Client, error) {
		if cfg.MaxRequests != config.DefaultConfig().Limits.MaxGitHubRequests {
			t.Fatalf("MaxRequests = %d, want default limit", cfg.MaxRequests)
		}
		return &github.Client{}, nil
	}

	var gotReq domain.CollectRequest
	newGitHubCollector = func(cfg github.CollectorConfig) (domain.SourceCollector, error) {
		if cfg.Config.Repositories[0] != "acme/api" {
			t.Fatalf("collector config repositories = %v", cfg.Config.Repositories)
		}
		return fakeCollector{
			name: "github",
			collectFn: func(ctx context.Context, req domain.CollectRequest) ([]domain.RawSignal, error) {
				gotReq = req
				cfg.Memory.AddRawSignal("github", "issue:1")
				cfg.Memory.IncrementStat("raw_signals_skipped")
				cfg.Memory.AddGitHubRequests(2)
				if err := cfg.Memory.Save(); err != nil {
					return nil, err
				}
				path := filepath.Join(cfg.Storage.BaseDir(), "raw-signals", "github-test.jsonl")
				if err := cfg.Storage.SaveJSONL(path, map[string]string{"id": "issue:1"}); err != nil {
					return nil, err
				}
				return []domain.RawSignal{{ID: "raw_1", Source: "github", SourceID: "issue:1"}}, nil
			},
		}, nil
	}

	cmd := newCollectCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	cmd.SetArgs([]string{"--sources", "github", "--since", "7d"})
	cmd.SetContext(context.Background())

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if gotReq.SinceWindow != 7*24*time.Hour {
		t.Fatalf("SinceWindow = %s, want 168h", gotReq.SinceWindow)
	}
	if len(gotReq.Sources) != 1 || gotReq.Sources[0] != "github" {
		t.Fatalf("Sources = %v, want [github]", gotReq.Sources)
	}
	if !strings.Contains(stdout.String(), "Collected 1 signals from github. New: 1, skipped: 1, GitHub requests: 2") {
		t.Fatalf("unexpected output: %s", stdout.String())
	}

	if _, err := os.Stat(filepath.Join(dir, "raw-signals", "github-test.jsonl")); err != nil {
		t.Fatalf("expected persisted raw signals file: %v", err)
	}

	store := storage.New(dir)
	mem := memory.New(store)
	if err := mem.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	stats := mem.GetStats()
	if stats.RawSignalsCollected != 1 || stats.RawSignalsSkipped != 1 || stats.GitHubRequests != 2 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

func TestRunCollectRequiresGitHubToken(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SIGNALFORGE_HOME", dir)
	t.Setenv("GITHUB_TOKEN", "")

	cfg := config.DefaultConfig()
	if err := config.SaveConfig(dir, cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	originalGetDir := getSignalForgeDir
	originalLoadConfig := loadConfig
	defer func() {
		getSignalForgeDir = originalGetDir
		loadConfig = originalLoadConfig
	}()

	getSignalForgeDir = func() (string, error) { return dir, nil }
	loadConfig = config.LoadConfig

	cmd := newCollectCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	cmd.SetArgs([]string{"--sources", "github"})
	cmd.SetContext(context.Background())

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute() expected token error")
	}
	if !strings.Contains(err.Error(), "GITHUB_TOKEN is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCheckEnvVarsHonorsGitHubEnabled(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")

	enabled := config.DefaultConfig()
	results := checkEnvVars(enabled)
	if results[0].Status != "❌" {
		t.Fatalf("enabled github status = %s, want ❌", results[0].Status)
	}

	disabled := config.DefaultConfig()
	disabled.Sources.GitHub.Enabled = false
	results = checkEnvVars(disabled)
	if results[0].Status != "ℹ️" {
		t.Fatalf("disabled github status = %s, want ℹ️", results[0].Status)
	}
}

type fakeCollector struct {
	name      string
	collectFn func(context.Context, domain.CollectRequest) ([]domain.RawSignal, error)
}

func (f fakeCollector) Name() string {
	return f.name
}

func (f fakeCollector) Collect(ctx context.Context, req domain.CollectRequest) ([]domain.RawSignal, error) {
	if f.collectFn == nil {
		return nil, fmt.Errorf("collectFn is nil")
	}
	return f.collectFn(ctx, req)
}
