package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConfigGitHubDefaults(t *testing.T) {
	cfg := DefaultConfig()

	if !cfg.Sources.GitHub.Enabled {
		t.Fatal("expected github source to be enabled by default")
	}
	if !cfg.Sources.GitHub.SearchIssues || !cfg.Sources.GitHub.SearchDiscussions {
		t.Fatal("expected github collector to search issues and discussions by default")
	}
	if cfg.Sources.GitHub.MaxItemsPerRun != 500 {
		t.Fatalf("expected max items per run 500, got %d", cfg.Sources.GitHub.MaxItemsPerRun)
	}
	if cfg.Sources.GitHub.MaxCommentsPerItem != 20 {
		t.Fatalf("expected max comments per item 20, got %d", cfg.Sources.GitHub.MaxCommentsPerItem)
	}
}

func TestGitHubConfigValidate(t *testing.T) {
	valid := DefaultConfig().Sources.GitHub
	valid.Repositories = []string{"openai/codex"}
	valid.Languages = []string{"go"}
	valid.Labels = []string{"bug"}

	tests := []struct {
		name    string
		cfg     GitHubConfig
		wantErr string
	}{
		{
			name: "valid",
			cfg:  valid,
		},
		{
			name: "invalid max items",
			cfg: func() GitHubConfig {
				cfg := valid
				cfg.MaxItemsPerRun = 0
				return cfg
			}(),
			wantErr: "max_items_per_run",
		},
		{
			name: "invalid max comments",
			cfg: func() GitHubConfig {
				cfg := valid
				cfg.MaxCommentsPerItem = -1
				return cfg
			}(),
			wantErr: "max_comments_per_item",
		},
		{
			name: "both searches disabled",
			cfg: func() GitHubConfig {
				cfg := valid
				cfg.SearchIssues = false
				cfg.SearchDiscussions = false
				return cfg
			}(),
			wantErr: "at least one",
		},
		{
			name: "invalid repo format",
			cfg: func() GitHubConfig {
				cfg := valid
				cfg.Repositories = []string{"openai"}
				return cfg
			}(),
			wantErr: "owner/name",
		},
		{
			name: "blank language",
			cfg: func() GitHubConfig {
				cfg := valid
				cfg.Languages = []string{" "}
				return cfg
			}(),
			wantErr: "languages",
		},
		{
			name: "blank label",
			cfg: func() GitHubConfig {
				cfg := valid
				cfg.Labels = []string{""}
				return cfg
			}(),
			wantErr: "labels",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestLoadConfigValidatesGitHubConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	data := `{"sources":{"github":{"max_items_per_run":0}}}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := LoadConfig(dir)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "max_items_per_run") {
		t.Fatalf("expected max_items_per_run validation error, got %v", err)
	}
}

func TestNormalizeSourceName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
		ok    bool
	}{
		{input: "github", want: "github", ok: true},
		{input: "GH", want: "github", ok: true},
		{input: "hn", want: "hackernews", ok: true},
		{input: " stackexchange ", want: "stackexchange", ok: true},
		{input: "unknown", ok: false},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, ok := NormalizeSourceName(tc.input)
			if ok != tc.ok {
				t.Fatalf("NormalizeSourceName(%q) ok = %v, want %v", tc.input, ok, tc.ok)
			}
			if got != tc.want {
				t.Fatalf("NormalizeSourceName(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
