package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/moontechs/signalforge/internal/config"
	"github.com/moontechs/signalforge/internal/domain"
	"github.com/moontechs/signalforge/internal/memory"
	"github.com/moontechs/signalforge/internal/storage"
)

func TestCollectorMixedRunPersistsSignalsAndDeduplicates(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	store := storage.New(t.TempDir())
	mem := memory.New(store)

	mem.GetMemory().RawSignalIDs["github:issue:I_seen"] = "issue:I_seen"
	duplicateDiscussion := Discussion{
		ID:        "D_dup",
		Number:    14,
		Title:     "Duplicate discussion",
		Body:      "Need better retries",
		URL:       "https://github.com/acme/api/discussions/14",
		CreatedAt: now.Add(-24 * time.Hour),
		UpdatedAt: now.Add(-23 * time.Hour),
		Repository: Repository{
			NameWithOwner: "acme/api",
		},
		Category: DiscussionCategory{Name: "Ideas"},
		Comments: DiscussionCommentConnection{
			Nodes: []DiscussionComment{{ID: "DC_dup", Body: "same observation", CreatedAt: now.Add(-22 * time.Hour)}},
		},
		Author: Actor{Login: "dev1"},
	}
	mem.GetMemory().ContentHashes[ParseDiscussion(duplicateDiscussion, duplicateDiscussion.Comments.Nodes, ParseOptions{
		CollectedAt: now,
		MaxComments: 2,
	}).ContentHash] = "existing"

	api := &fakeGitHubAPI{
		searchIssuesFn: func(_ context.Context, params SearchIssuesParams) (SearchIssuesPage, error) {
			if !strings.Contains(params.Query, "repo:acme/api") {
				t.Fatalf("issue query missing repo filter: %q", params.Query)
			}
			if !strings.Contains(params.Query, "language:go") {
				t.Fatalf("issue query missing language filter: %q", params.Query)
			}
			if !strings.Contains(params.Query, "label:bug") {
				t.Fatalf("issue query missing label filter: %q", params.Query)
			}
			if !strings.Contains(params.Query, "updated:>=2026-07-20") {
				t.Fatalf("issue query missing since filter: %q", params.Query)
			}
			if params.PerPage != 2 {
				t.Fatalf("issue per_page = %d, want 2", params.PerPage)
			}

			return SearchIssuesPage{
				Response: SearchIssuesResponse{
					Items: []IssueItem{
						{
							ID:            1,
							Number:        7,
							NodeID:        "I_seen",
							Title:         "Seen already",
							Body:          "skip me",
							HTMLURL:       "https://github.com/acme/api/issues/7",
							RepositoryURL: "https://api.github.com/repos/acme/api",
							CreatedAt:     now.Add(-48 * time.Hour),
							UpdatedAt:     now.Add(-47 * time.Hour),
						},
						{
							ID:            2,
							Number:        8,
							NodeID:        "I_new",
							Title:         "Collector request",
							Body:          "Need better retries",
							HTMLURL:       "https://github.com/acme/api/issues/8",
							RepositoryURL: "https://api.github.com/repos/acme/api",
							Comments:      3,
							CreatedAt:     now.Add(-36 * time.Hour),
							UpdatedAt:     now.Add(-35 * time.Hour),
						},
					},
				},
			}, nil
		},
		listIssueCommentsFn: func(_ context.Context, params IssueCommentsParams) (IssueCommentsPage, error) {
			if params.Owner != "acme" || params.Repo != "api" || params.IssueNum != 8 {
				t.Fatalf("unexpected issue comment params: %+v", params)
			}
			if params.PerPage != 2 {
				t.Fatalf("issue comment per_page = %d, want 2", params.PerPage)
			}
			return IssueCommentsPage{
				Comments: []IssueComment{
					{ID: 11, NodeID: "IC_1", Body: "first useful comment", CreatedAt: now.Add(-34 * time.Hour)},
					{ID: 12, NodeID: "IC_2", Body: "second useful comment", CreatedAt: now.Add(-33 * time.Hour)},
				},
			}, nil
		},
		searchDiscussionsFn: func(_ context.Context, params DiscussionSearchParams) (DiscussionSearchPage, error) {
			if !strings.Contains(params.Query, "repo:acme/api") {
				t.Fatalf("discussion query missing repo filter: %q", params.Query)
			}
			return DiscussionSearchPage{
				Response: GraphQLResponse[DiscussionsQueryData]{
					Data: DiscussionsQueryData{
						Search: SearchResultConnection{
							Nodes: []Discussion{
								duplicateDiscussion,
								{
									ID:        "D_new",
									Number:    15,
									Title:     "Need discussion support",
									Body:      "The collector misses pagination.",
									URL:       "https://github.com/acme/api/discussions/15",
									CreatedAt: now.Add(-32 * time.Hour),
									UpdatedAt: now.Add(-31 * time.Hour),
									Repository: Repository{
										NameWithOwner: "acme/api",
									},
									Category: DiscussionCategory{Name: "Ideas"},
									Comments: DiscussionCommentConnection{
										PageInfo: PageInfo{HasNextPage: true, EndCursor: "cursor-1"},
										Nodes: []DiscussionComment{
											{ID: "DC_1", Body: "same problem here", CreatedAt: now.Add(-30 * time.Hour)},
										},
									},
									Author: Actor{Login: "dev2"},
								},
							},
						},
					},
				},
			}, nil
		},
		listDiscussionCommentsFn: func(_ context.Context, params DiscussionCommentsParams) (DiscussionCommentsPage, error) {
			if params.DiscussionID != "D_new" || params.After != "cursor-1" {
				t.Fatalf("unexpected discussion comment params: %+v", params)
			}
			return DiscussionCommentsPage{
				Response: GraphQLResponse[DiscussionCommentsQueryData]{
					Data: DiscussionCommentsQueryData{
						Node: DiscussionCommentsNode{
							Comments: DiscussionCommentConnection{
								PageInfo: PageInfo{},
								Nodes: []DiscussionComment{
									{ID: "DC_2", Body: "adding another detail", CreatedAt: now.Add(-29 * time.Hour)},
								},
							},
						},
					},
				},
			}, nil
		},
		stats: ClientStats{Requests: 4, RESTRequests: 2, GraphQLRequests: 2},
	}

	collector := newCollectorForTest(t, CollectorConfig{
		Config: config.GitHubConfig{
			Enabled:            true,
			SearchIssues:       true,
			SearchDiscussions:  true,
			MaxItemsPerRun:     2,
			MaxCommentsPerItem: 2,
			Repositories:       []string{"acme/api"},
			Languages:          []string{"go"},
			Labels:             []string{"bug"},
		},
		API:     api,
		Storage: store,
		Memory:  mem,
		Now:     func() time.Time { return now },
	})

	signals, err := collector.Collect(context.Background(), domain.CollectRequest{
		SinceWindow: 48 * time.Hour,
	})
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(signals) != 2 {
		t.Fatalf("len(signals) = %d, want 2", len(signals))
	}
	if signals[0].SourceType != sourceTypeIssue || len(signals[0].Comments) != 2 {
		t.Fatalf("first signal = %+v", signals[0])
	}
	if signals[1].SourceType != sourceTypeDiscussion || len(signals[1].Comments) != 2 {
		t.Fatalf("second signal = %+v", signals[1])
	}

	lines, err := store.ReadJSONL(filepath.Join(store.BaseDir(), "raw-signals", "github-2026-07-22.jsonl"))
	if err != nil {
		t.Fatalf("ReadJSONL() error = %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("persisted lines = %d, want 2", len(lines))
	}

	var savedMem domain.Memory
	if err := store.LoadJSON(filepath.Join(store.BaseDir(), "memory.json"), &savedMem); err != nil {
		t.Fatalf("LoadJSON(memory) error = %v", err)
	}
	if savedMem.Stats.RawSignalsCollected != 2 {
		t.Fatalf("raw_signals_collected = %d, want 2", savedMem.Stats.RawSignalsCollected)
	}
	if savedMem.Stats.RawSignalsSkipped != 2 {
		t.Fatalf("raw_signals_skipped = %d, want 2", savedMem.Stats.RawSignalsSkipped)
	}
	if savedMem.Stats.GitHubRequests != 4 {
		t.Fatalf("github_requests = %d, want 4", savedMem.Stats.GitHubRequests)
	}
}

func TestCollectorHandlesPartialFailuresAndCursorInputs(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 22, 15, 0, 0, 0, time.UTC)
	store := storage.New(t.TempDir())
	mem := memory.New(store)

	api := &fakeGitHubAPI{
		searchIssuesFn: func(_ context.Context, params SearchIssuesParams) (SearchIssuesPage, error) {
			if params.Page != 3 {
				t.Fatalf("issue page = %d, want 3", params.Page)
			}
			return SearchIssuesPage{
				Response: SearchIssuesResponse{
					Items: []IssueItem{
						{
							ID:            10,
							Number:        21,
							NodeID:        "I_partial",
							Title:         "Need exports",
							Body:          "CSV export support is missing",
							HTMLURL:       "https://github.com/acme/api/issues/21",
							RepositoryURL: "https://api.github.com/repos/acme/api",
							Comments:      1,
							CreatedAt:     now.Add(-5 * time.Hour),
							UpdatedAt:     now.Add(-4 * time.Hour),
						},
					},
				},
			}, nil
		},
		listIssueCommentsFn: func(_ context.Context, params IssueCommentsParams) (IssueCommentsPage, error) {
			if params.IssueNum != 21 {
				t.Fatalf("unexpected issue comment params: %+v", params)
			}
			return IssueCommentsPage{}, fmt.Errorf("comment fetch failed")
		},
		searchDiscussionsFn: func(_ context.Context, params DiscussionSearchParams) (DiscussionSearchPage, error) {
			if params.After != "cursor-x" {
				t.Fatalf("discussion cursor = %q, want cursor-x", params.After)
			}
			return DiscussionSearchPage{}, fmt.Errorf("discussion search failed")
		},
		stats: ClientStats{Requests: 3, RESTRequests: 2, GraphQLRequests: 1},
	}

	collector := newCollectorForTest(t, CollectorConfig{
		Config: config.GitHubConfig{
			Enabled:            true,
			SearchIssues:       true,
			SearchDiscussions:  true,
			MaxItemsPerRun:     3,
			MaxCommentsPerItem: 1,
		},
		API:     api,
		Storage: store,
		Memory:  mem,
		Now:     func() time.Time { return now },
	})

	signals, err := collector.Collect(context.Background(), domain.CollectRequest{
		MaxItems: 2,
		Cursor: map[string]string{
			"github_issues_page":       "3",
			"github_discussions_after": "cursor-x",
		},
	})
	if err == nil {
		t.Fatal("Collect() error = nil, want partial failure")
	}
	if len(signals) != 1 {
		t.Fatalf("len(signals) = %d, want 1", len(signals))
	}
	if len(signals[0].Comments) != 0 {
		t.Fatalf("len(signals[0].Comments) = %d, want 0", len(signals[0].Comments))
	}
	if !strings.Contains(err.Error(), "comment fetch failed") {
		t.Fatalf("error %q missing comment failure", err)
	}
	if !strings.Contains(err.Error(), "discussion search failed") {
		t.Fatalf("error %q missing discussion failure", err)
	}

	lines, err := store.ReadJSONL(filepath.Join(store.BaseDir(), "raw-signals", "github-2026-07-22.jsonl"))
	if err != nil {
		t.Fatalf("ReadJSONL() error = %v", err)
	}
	if len(lines) != 1 {
		t.Fatalf("persisted lines = %d, want 1", len(lines))
	}

	var savedSignal domain.RawSignal
	if err := json.Unmarshal(lines[0], &savedSignal); err != nil {
		t.Fatalf("unmarshal saved signal: %v", err)
	}
	if savedSignal.SourceID != "issue:I_partial" {
		t.Fatalf("saved SourceID = %q", savedSignal.SourceID)
	}

	var savedMem domain.Memory
	if err := store.LoadJSON(filepath.Join(store.BaseDir(), "memory.json"), &savedMem); err != nil {
		t.Fatalf("LoadJSON(memory) error = %v", err)
	}
	if savedMem.Stats.RawSignalsCollected != 1 {
		t.Fatalf("raw_signals_collected = %d, want 1", savedMem.Stats.RawSignalsCollected)
	}
	if savedMem.Stats.GitHubRequests != 3 {
		t.Fatalf("github_requests = %d, want 3", savedMem.Stats.GitHubRequests)
	}
}

type fakeGitHubAPI struct {
	searchIssuesFn           func(context.Context, SearchIssuesParams) (SearchIssuesPage, error)
	listIssueCommentsFn      func(context.Context, IssueCommentsParams) (IssueCommentsPage, error)
	searchDiscussionsFn      func(context.Context, DiscussionSearchParams) (DiscussionSearchPage, error)
	listDiscussionCommentsFn func(context.Context, DiscussionCommentsParams) (DiscussionCommentsPage, error)
	stats                    ClientStats
}

func (f *fakeGitHubAPI) SearchIssues(ctx context.Context, params SearchIssuesParams) (SearchIssuesPage, error) {
	if f.searchIssuesFn == nil {
		return SearchIssuesPage{}, errors.New("unexpected SearchIssues call")
	}
	return f.searchIssuesFn(ctx, params)
}

func (f *fakeGitHubAPI) ListIssueComments(ctx context.Context, params IssueCommentsParams) (IssueCommentsPage, error) {
	if f.listIssueCommentsFn == nil {
		return IssueCommentsPage{}, errors.New("unexpected ListIssueComments call")
	}
	return f.listIssueCommentsFn(ctx, params)
}

func (f *fakeGitHubAPI) SearchDiscussions(ctx context.Context, params DiscussionSearchParams) (DiscussionSearchPage, error) {
	if f.searchDiscussionsFn == nil {
		return DiscussionSearchPage{}, errors.New("unexpected SearchDiscussions call")
	}
	return f.searchDiscussionsFn(ctx, params)
}

func (f *fakeGitHubAPI) ListDiscussionComments(ctx context.Context, params DiscussionCommentsParams) (DiscussionCommentsPage, error) {
	if f.listDiscussionCommentsFn == nil {
		return DiscussionCommentsPage{}, errors.New("unexpected ListDiscussionComments call")
	}
	return f.listDiscussionCommentsFn(ctx, params)
}

func (f *fakeGitHubAPI) Stats() ClientStats {
	return f.stats
}

func newCollectorForTest(t *testing.T, cfg CollectorConfig) *Collector {
	t.Helper()

	collector, err := NewCollector(cfg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	return collector
}
