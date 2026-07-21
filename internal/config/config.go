// Package config handles configuration loading and validation.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config represents the entire application configuration.
type Config struct {
	OpenRouter  OpenRouterConfig  `json:"openrouter"`
	Sources     SourcesConfig     `json:"sources"`
	BrightData  BrightDataConfig  `json:"brightdata"`
	Pipeline    PipelineConfig    `json:"pipeline"`
	Limits      LimitsConfig      `json:"limits"`
}

// OpenRouterConfig holds OpenRouter-specific configuration.
type OpenRouterConfig struct {
	BaseURL               string   `json:"base_url"`
	Model                 string   `json:"model"`
	FallbackModels        []string `json:"fallback_models"`
	ClassificationTemp    float64  `json:"classification_temperature"`
	AnalysisTemp          float64  `json:"analysis_temperature"`
	GenerationTemp        float64  `json:"generation_temperature"`
	RepairTemp            float64  `json:"repair_temperature"`
	RequestTimeoutSeconds int      `json:"request_timeout_seconds"`
	MaxRetries            int      `json:"max_retries"`
	MaxOutputTokens       int      `json:"max_output_tokens"`
}

// SourcesConfig holds source-specific configuration.
type SourcesConfig struct {
	GitHub        GitHubConfig        `json:"github"`
	HackerNews    HackerNewsConfig    `json:"hackernews"`
	StackExchange StackExchangeConfig `json:"stackexchange"`
	Reddit        RedditConfig        `json:"reddit"`
}

// GitHubConfig holds GitHub-specific configuration.
type GitHubConfig struct {
	Enabled          bool     `json:"enabled"`
	SearchIssues     bool     `json:"search_issues"`
	SearchDiscussions bool    `json:"search_discussions"`
	MaxItemsPerRun   int      `json:"max_items_per_run"`
	MaxCommentsPerItem int    `json:"max_comments_per_item"`
	Repositories     []string `json:"repositories"`
	Languages        []string `json:"languages"`
	Labels           []string `json:"labels"`
}

// HackerNewsConfig holds Hacker News-specific configuration.
type HackerNewsConfig struct {
	Enabled          bool     `json:"enabled"`
	Feeds            []string `json:"feeds"`
	MaxItemsPerRun   int      `json:"max_items_per_run"`
	MaxCommentsPerItem int    `json:"max_comments_per_item"`
	MinimumScore     int      `json:"minimum_score"`
}

// StackExchangeConfig holds Stack Exchange-specific configuration.
type StackExchangeConfig struct {
	Enabled       bool     `json:"enabled"`
	Sites         []string `json:"sites"`
	MaxItemsPerSite int    `json:"max_items_per_site"`
	MinimumScore  int      `json:"minimum_score"`
	MinimumViews  int      `json:"minimum_views"`
}

// RedditConfig holds Reddit-specific configuration.
type RedditConfig struct {
	Enabled          bool     `json:"enabled"`
	Subreddits       []string `json:"subreddits"`
	MaxPostsPerRun   int      `json:"max_posts_per_run"`
	MaxCommentsPerPost int    `json:"max_comments_per_post"`
}

// BrightDataConfig holds Bright Data-specific configuration.
type BrightDataConfig struct {
	Endpoint            string `json:"endpoint"`
	Country             string `json:"country"`
	Language            string `json:"language"`
	RequestTimeoutSec   int    `json:"request_timeout_seconds"`
	MaxRetries          int    `json:"max_retries"`
	MaxConcurrency      int    `json:"max_concurrency"`
	MaxResponseBytes    int    `json:"max_response_bytes"`
}

// PipelineConfig holds pipeline-specific configuration.
type PipelineConfig struct {
	ClassificationBatchSize   int `json:"classification_batch_size"`
	ClusterCandidateLimit     int `json:"cluster_candidate_limit"`
	MaxRepresentativeSignals  int `json:"max_representative_signals"`
	MinimumClusterSignals     int `json:"minimum_cluster_signals"`
	MinimumIndependentSources int `json:"minimum_independent_sources"`
	SolutionHypothesesPer     int `json:"solution_hypotheses_per_cluster"`
	DeepResearchTop           int `json:"deep_research_top"`
}

// LimitsConfig holds technical request limits.
type LimitsConfig struct {
	MaxGitHubRequests     int `json:"max_github_requests_per_run"`
	MaxHNRequests         int `json:"max_hn_requests_per_run"`
	MaxStackExchangeReqs  int `json:"max_stackexchange_requests_per_run"`
	MaxRedditRequests     int `json:"max_reddit_requests_per_run"`
	MaxSERPRequests       int `json:"max_serp_requests_per_run"`
	MaxUnlockerRequests   int `json:"max_unlocker_requests_per_run"`
	MaxLLMRequests        int `json:"max_llm_requests_per_run"`
}

// DefaultConfig returns the default configuration.
func DefaultConfig() *Config {
	return &Config{
		OpenRouter: OpenRouterConfig{
			BaseURL:               "https://openrouter.ai/api/v1",
			Model:                 "",
			FallbackModels:        []string{},
			ClassificationTemp:    0.1,
			AnalysisTemp:          0.15,
			GenerationTemp:        0.7,
			RepairTemp:            0,
			RequestTimeoutSeconds: 120,
			MaxRetries:            3,
			MaxOutputTokens:       4000,
		},
		Sources: SourcesConfig{
			GitHub: GitHubConfig{
				Enabled:           true,
				SearchIssues:      true,
				SearchDiscussions: true,
				MaxItemsPerRun:    500,
				MaxCommentsPerItem: 20,
				Repositories:      []string{},
				Languages:         []string{},
				Labels:            []string{},
			},
			HackerNews: HackerNewsConfig{
				Enabled:            true,
				Feeds:              []string{"askstories", "showstories", "newstories"},
				MaxItemsPerRun:     300,
				MaxCommentsPerItem: 30,
				MinimumScore:       2,
			},
			StackExchange: StackExchangeConfig{
				Enabled:        true,
				Sites:          []string{"stackoverflow", "superuser", "webapps"},
				MaxItemsPerSite: 200,
				MinimumScore:   0,
				MinimumViews:   0,
			},
			Reddit: RedditConfig{
				Enabled:          false,
				Subreddits:       []string{},
				MaxPostsPerRun:   200,
				MaxCommentsPerPost: 20,
			},
		},
		BrightData: BrightDataConfig{
			Endpoint:          "https://api.brightdata.com/request",
			Country:           "us",
			Language:          "en",
			RequestTimeoutSec: 90,
			MaxRetries:        3,
			MaxConcurrency:    3,
			MaxResponseBytes:  5242880,
		},
		Pipeline: PipelineConfig{
			ClassificationBatchSize:   20,
			ClusterCandidateLimit:     100,
			MaxRepresentativeSignals:  20,
			MinimumClusterSignals:     3,
			MinimumIndependentSources: 2,
			SolutionHypothesesPer:     3,
			DeepResearchTop:           10,
		},
		Limits: LimitsConfig{
			MaxGitHubRequests:    500,
			MaxHNRequests:        1000,
			MaxStackExchangeReqs: 500,
			MaxRedditRequests:    300,
			MaxSERPRequests:      300,
			MaxUnlockerRequests:  50,
			MaxLLMRequests:       300,
		},
	}
}

// LoadConfig loads configuration from the given directory.
func LoadConfig(dir string) (*Config, error) {
	cfg := DefaultConfig()
	path := filepath.Join(dir, "config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}

// SaveConfig saves configuration to the given directory.
func SaveConfig(dir string, cfg *Config) error {
	path := filepath.Join(dir, "config.json")
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

// Env retrieves an environment variable with a default fallback.
func Env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// GetSignalForgeDir returns the signalforge data directory.
func GetSignalForgeDir() (string, error) {
	if v := os.Getenv("SIGNALFORGE_HOME"); v != "" {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".signalforge"), nil
}

// DefaultDirStructure returns the default directory structure.
func DefaultDirStructure() map[string]string {
	return map[string]string{
		"sources/github":        "",
		"sources/hackernews":    "",
		"sources/stackexchange": "",
		"sources/reddit":        "",
		"raw-signals":           "",
		"problem-signals":       "",
		"clusters":              "",
		"jobs":                  "",
		"ideas":                 "",
		"runs":                  "",
		"cache/github":          "",
		"cache/hackernews":      "",
		"cache/stackexchange":   "",
		"cache/reddit":          "",
		"cache/brightdata/serp": "",
		"cache/brightdata/unlocker": "",
		"cache/openrouter":      "",
		"backups":               "",
		"exports":               "",
	}
}