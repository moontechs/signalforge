package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConfigGitHubDefaults(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
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
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	data := `{"sources":{"github":{"max_items_per_run":0}}}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
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

func TestDefaultConfigHackerNewsDefaults(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()

	if !cfg.Sources.HackerNews.Enabled {
		t.Fatal("expected hackernews source to be enabled by default")
	}
	if cfg.Sources.HackerNews.MaxItemsPerRun != 300 {
		t.Fatalf("expected max items per run 300, got %d", cfg.Sources.HackerNews.MaxItemsPerRun)
	}
	if cfg.Sources.HackerNews.MaxCommentsPerItem != 30 {
		t.Fatalf("expected max comments per item 30, got %d", cfg.Sources.HackerNews.MaxCommentsPerItem)
	}
	if cfg.Sources.HackerNews.MinimumScore != 2 {
		t.Fatalf("expected minimum score 2, got %d", cfg.Sources.HackerNews.MinimumScore)
	}
	if len(cfg.Sources.HackerNews.Feeds) != 3 {
		t.Fatalf("expected 3 feeds, got %d", len(cfg.Sources.HackerNews.Feeds))
	}
	expectedFeeds := []string{"askstories", "showstories", "newstories"}
	for i, f := range expectedFeeds {
		if cfg.Sources.HackerNews.Feeds[i] != f {
			t.Fatalf("feed[%d] = %q, want %q", i, cfg.Sources.HackerNews.Feeds[i], f)
		}
	}
	if cfg.Limits.MaxHNRequests != 1000 {
		t.Fatalf("expected MaxHNRequests 1000, got %d", cfg.Limits.MaxHNRequests)
	}
}

func TestHackerNewsConfigValidate(t *testing.T) {
	t.Parallel()
	valid := DefaultConfig().Sources.HackerNews

	tests := []struct {
		name    string
		cfg     HackerNewsConfig
		wantErr string
	}{
		{
			name: "valid enabled",
			cfg:  valid,
		},
		{
			name: "disabled is valid",
			cfg: func() HackerNewsConfig {
				cfg := valid
				cfg.Enabled = false
				cfg.MaxItemsPerRun = 0 // even with bad values, disabled should pass
				return cfg
			}(),
		},
		{
			name: "invalid max items",
			cfg: func() HackerNewsConfig {
				cfg := valid
				cfg.MaxItemsPerRun = 0
				return cfg
			}(),
			wantErr: "max_items_per_run",
		},
		{
			name: "invalid max comments",
			cfg: func() HackerNewsConfig {
				cfg := valid
				cfg.MaxCommentsPerItem = -1
				return cfg
			}(),
			wantErr: "max_comments_per_item",
		},
		{
			name: "invalid minimum score",
			cfg: func() HackerNewsConfig {
				cfg := valid
				cfg.MinimumScore = -1
				return cfg
			}(),
			wantErr: "minimum_score",
		},
		{
			name: "no feeds",
			cfg: func() HackerNewsConfig {
				cfg := valid
				cfg.Feeds = []string{}
				return cfg
			}(),
			wantErr: "at least one feed",
		},
		{
			name: "unsupported feed",
			cfg: func() HackerNewsConfig {
				cfg := valid
				cfg.Feeds = []string{"jobstories"}
				return cfg
			}(),
			wantErr: "unsupported feed",
		},
		{
			name: "mixed valid and invalid feeds",
			cfg: func() HackerNewsConfig {
				cfg := valid
				cfg.Feeds = []string{"askstories", "jobstories"}
				return cfg
			}(),
			wantErr: "unsupported feed",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
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

func TestLoadConfigValidatesHackerNewsConfig(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	data := `{"sources":{"hackernews":{"max_items_per_run":0}}}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
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

func TestValidHNFeeds(t *testing.T) {
	t.Parallel()

	feeds := ValidHNFeeds()
	expected := []string{"askstories", "showstories", "newstories", "topstories", "beststories"}
	if len(feeds) != len(expected) {
		t.Fatalf("ValidHNFeeds() returned %d feeds, want %d", len(feeds), len(expected))
	}
	for i, f := range expected {
		if feeds[i] != f {
			t.Fatalf("ValidHNFeeds()[%d] = %q, want %q", i, feeds[i], f)
		}
	}
}

func TestIsValidHNFeed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		feed string
		want bool
	}{
		{feed: "askstories", want: true},
		{feed: "showstories", want: true},
		{feed: "newstories", want: true},
		{feed: "topstories", want: true},
		{feed: "beststories", want: true},
		{feed: "AskStories", want: true}, // case-insensitive
		{feed: "jobstories", want: false},
		{feed: "", want: false},
		{feed: "unknown", want: false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.feed, func(t *testing.T) {
			t.Parallel()
			got := IsValidHNFeed(tc.feed)
			if got != tc.want {
				t.Fatalf("IsValidHNFeed(%q) = %v, want %v", tc.feed, got, tc.want)
			}
		})
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
		tc := tc
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
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
