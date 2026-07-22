package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/moontechs/signalforge/internal/config"
	"github.com/moontechs/signalforge/internal/domain"
	"github.com/moontechs/signalforge/internal/sources/github"
	"github.com/moontechs/signalforge/internal/storage"
)

func TestCollectCommandEndToEndPersistsAndDeduplicatesAcrossRuns(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SIGNALFORGE_HOME", dir)
	t.Setenv("GITHUB_TOKEN", "test-token")

	cfg := config.DefaultConfig()
	cfg.Sources.GitHub.Repositories = []string{"acme/api"}
	cfg.Sources.GitHub.Languages = []string{"go"}
	cfg.Sources.GitHub.Labels = []string{"bug"}
	cfg.Sources.GitHub.MaxItemsPerRun = 5
	cfg.Sources.GitHub.MaxCommentsPerItem = 2
	if err := config.SaveConfig(dir, cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	doer := &sequenceHTTPDoer{
		t: t,
		responders: []httpResponder{
			matchSearchIssuesResponse(t),
			matchIssueCommentsResponse(t),
			matchSearchDiscussionsResponse(t),
			matchSearchIssuesResponse(t),
			matchSearchDiscussionsResponse(t),
		},
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
		return github.NewClient(github.ClientConfig{
			Token:       "test-token",
			HTTPClient:  doer,
			MaxRetries:  cfg.MaxRetries,
			MaxRequests: cfg.MaxRequests,
			Sleep:       func(time.Duration) {},
		})
	}
	newGitHubCollector = func(cfg github.CollectorConfig) (domain.SourceCollector, error) {
		cfg.Now = func() time.Time { return now }
		return github.NewCollector(cfg)
	}

	firstOutput := runCollectCommand(t, []string{"--sources", "github", "--since", "7d"})
	if !strings.Contains(firstOutput, "Collected 2 signals from github. New: 2, skipped: 0, GitHub requests: 3") {
		t.Fatalf("unexpected first output: %s", firstOutput)
	}

	secondOutput := runCollectCommand(t, []string{"--sources", "github", "--since", "7d"})
	if !strings.Contains(secondOutput, "Collected 0 signals from github. New: 0, skipped: 2, GitHub requests: 2") {
		t.Fatalf("unexpected second output: %s", secondOutput)
	}

	if pending := doer.Pending(); pending != 0 {
		t.Fatalf("pending fake responses = %d, want 0", pending)
	}

	store := storage.New(dir)
	lines, err := store.ReadJSONL(filepath.Join(dir, "raw-signals", "github-2026-07-22.jsonl"))
	if err != nil {
		t.Fatalf("ReadJSONL() error = %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("persisted lines = %d, want 2", len(lines))
	}

	var mem domain.Memory
	if err := store.LoadJSON(filepath.Join(dir, "memory.json"), &mem); err != nil {
		t.Fatalf("LoadJSON(memory.json) error = %v", err)
	}
	if mem.Stats.RawSignalsCollected != 2 {
		t.Fatalf("raw_signals_collected = %d, want 2", mem.Stats.RawSignalsCollected)
	}
	if mem.Stats.RawSignalsSkipped != 2 {
		t.Fatalf("raw_signals_skipped = %d, want 2", mem.Stats.RawSignalsSkipped)
	}
	if mem.Stats.GitHubRequests != 5 {
		t.Fatalf("github_requests = %d, want 5", mem.Stats.GitHubRequests)
	}
	if len(mem.RawSignalIDs) != 2 {
		t.Fatalf("raw signal ids = %d, want 2", len(mem.RawSignalIDs))
	}
	if len(mem.ContentHashes) != 2 {
		t.Fatalf("content hashes = %d, want 2", len(mem.ContentHashes))
	}
}

func runCollectCommand(t *testing.T, args []string) string {
	t.Helper()

	cmd := newCollectCmd()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs(args)
	cmd.SetContext(context.Background())

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	return output.String()
}

type sequenceHTTPDoer struct {
	t          *testing.T
	mu         sync.Mutex
	responders []httpResponder
	index      int
}

func (d *sequenceHTTPDoer) Do(req *http.Request) (*http.Response, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.index >= len(d.responders) {
		return nil, fmt.Errorf("unexpected extra request: %s", req.URL.String())
	}

	responder := d.responders[d.index]
	d.index++
	return responder(req)
}

func (d *sequenceHTTPDoer) Pending() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.responders) - d.index
}

type httpResponder func(*http.Request) (*http.Response, error)

func matchSearchIssuesResponse(t *testing.T) httpResponder {
	t.Helper()

	return func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet {
			t.Fatalf("search issues method = %s, want GET", req.Method)
		}
		if req.URL.Path != "/search/issues" {
			t.Fatalf("search issues path = %s, want /search/issues", req.URL.Path)
		}
		query := req.URL.Query().Get("q")
		for _, part := range []string{"repo:acme/api", "language:go", "label:bug", "updated:>="} {
			if !strings.Contains(query, part) {
				t.Fatalf("search issues query %q missing %q", query, part)
			}
		}

		return jsonHTTPResponse(req, http.StatusOK, `{
			"total_count": 1,
			"items": [
				{
					"id": 101,
					"number": 8,
					"node_id": "I_issue_1",
					"html_url": "https://github.com/acme/api/issues/8",
					"title": "Collector misses retries",
					"body": "The collector should retry transient API failures.",
					"state": "open",
					"locked": false,
					"comments": 1,
					"created_at": "2026-07-20T10:00:00Z",
					"updated_at": "2026-07-21T10:00:00Z",
					"repository_url": "https://api.github.com/repos/acme/api",
					"labels": [{"name": "bug"}],
					"user": {"login": "dev1", "type": "User"},
					"reactions": {"total_count": 4}
				}
			]
		}`), nil
	}
}

func matchIssueCommentsResponse(t *testing.T) httpResponder {
	t.Helper()

	return func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet {
			t.Fatalf("issue comments method = %s, want GET", req.Method)
		}
		if req.URL.Path != "/repos/acme/api/issues/8/comments" {
			t.Fatalf("issue comments path = %s", req.URL.Path)
		}

		return jsonHTTPResponse(req, http.StatusOK, `[
			{
				"id": 501,
				"node_id": "IC_1",
				"html_url": "https://github.com/acme/api/issues/8#issuecomment-501",
				"body": "I hit the same problem in CI.",
				"created_at": "2026-07-21T12:00:00Z",
				"updated_at": "2026-07-21T12:00:00Z",
				"user": {"login": "dev2", "type": "User"},
				"reactions": {"total_count": 2}
			}
		]`), nil
	}
}

func matchSearchDiscussionsResponse(t *testing.T) httpResponder {
	t.Helper()

	return func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost {
			t.Fatalf("search discussions method = %s, want POST", req.Method)
		}
		if req.URL.Path != "/graphql" {
			t.Fatalf("search discussions path = %s, want /graphql", req.URL.Path)
		}

		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("ReadAll(graphql body) error = %v", err)
		}
		bodyText := string(body)
		if !strings.Contains(bodyText, `"query":"query SearchDiscussions`) {
			t.Fatalf("graphql body missing search query: %s", bodyText)
		}
		for _, part := range []string{"repo:acme/api", "language:go", "label:bug"} {
			if !strings.Contains(bodyText, part) {
				t.Fatalf("graphql body %q missing %q", bodyText, part)
			}
		}

		return jsonHTTPResponse(req, http.StatusOK, `{
			"data": {
				"search": {
					"pageInfo": {
						"endCursor": "",
						"hasNextPage": false
					},
					"nodes": [
						{
							"id": "D_discussion_1",
							"number": 15,
							"title": "Need discussion support",
							"body": "The collector should include GitHub Discussions.",
							"url": "https://github.com/acme/api/discussions/15",
							"locked": false,
							"closed": false,
							"createdAt": "2026-07-19T09:00:00Z",
							"updatedAt": "2026-07-21T09:00:00Z",
							"repository": {"nameWithOwner": "acme/api"},
							"category": {"name": "Ideas"},
							"labels": {"nodes": [{"name": "bug"}]},
							"comments": {
								"totalCount": 1,
								"pageInfo": {
									"endCursor": "",
									"hasNextPage": false
								},
								"nodes": [
									{
										"id": "DC_1",
										"body": "This would help our triage workflow.",
										"url": "https://github.com/acme/api/discussions/15#discussioncomment-1",
										"createdAt": "2026-07-21T11:00:00Z",
										"updatedAt": "2026-07-21T11:00:00Z",
										"replyCount": 0,
										"isAnswer": false,
										"author": {"login": "dev3"},
										"reactions": {"totalCount": 1}
									}
								]
							},
							"reactions": {"totalCount": 3},
							"upvoteCount": 5,
							"author": {"login": "dev4"}
						}
					]
				}
			}
		}`), nil
	}
}

func jsonHTTPResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       ioNopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

type nopCloser struct {
	*strings.Reader
}

func (n nopCloser) Close() error {
	return nil
}

func ioNopCloser(r *strings.Reader) nopCloser {
	return nopCloser{Reader: r}
}
