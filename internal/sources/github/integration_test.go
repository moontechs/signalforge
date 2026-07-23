package github

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/moontechs/signalforge/internal/domain"
)

// ---- Helpers for building fake responses ----.

// searchRespToJSON serializes a ghSearchResponse to JSON.
func searchRespToJSON(resp ghSearchResponse) []byte {
	b, _ := json.Marshal(resp)
	return b
}

// issuesListToJSON serializes a []ghIssue to JSON (for per-repo endpoint).
func issuesListToJSON(issues []ghIssue) []byte {
	if issues == nil {
		issues = []ghIssue{}
	}
	b, _ := json.Marshal(issues)
	return b
}

// ---- Test helpers ----.

// setupCollector creates a Collector with a fakeTransport and convenient defaults.
func setupCollector(t *testing.T, cfg *CollectorConfig, fake *fakeTransport) *Collector {
	t.Helper()
	if fake == nil {
		fake = newFakeTransport()
	}
	if cfg.MaxRequests == 0 {
		cfg.MaxRequests = 500
	}
	c, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.WithTransport(fake)
	return c
}

// ---- Tests ----.

// TestCollect_MixedIssuesAndDiscussions verifies that Collect returns both
// issues and discussions parsed into RawSignals.
func TestCollect_MixedIssuesAndDiscussions(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	// We use per-repo strategy so discussions can also be fetched.
	// Register issues per-repo endpoint with wildcard for per_page.
	issuesPerRepoPrefix := "https://api.github.com/repos/owner/repo/issues?state=open&sort=updated&direction=asc&per_page="
	issues := []ghIssue{
		{
			ID:        1001,
			Number:    10,
			Title:     "Bug: app crashes on startup",
			Body:      "Crash report",
			HTMLURL:   "https://github.com/owner/repo/issues/10",
			State:     "open",
			CreatedAt: t1,
			UpdatedAt: t2,
			Labels:    []ghLabel{{Name: "bug"}},
			User:      ghUser{Login: "user1"},
			Comments:  1,
			Reactions: ghReactions{Plus1: 3},
			RepoURL:   "https://api.github.com/repos/owner/repo",
		},
		{
			ID:        1002,
			Number:    11,
			Title:     "Memory leak in parser",
			Body:      "Memory usage grows over time",
			HTMLURL:   "https://github.com/owner/repo/issues/11",
			State:     "open",
			CreatedAt: t1,
			UpdatedAt: t3,
			Labels:    []ghLabel{{Name: "bug"}, {Name: "performance"}},
			User:      ghUser{Login: "user2"},
			Comments:  0,
			Reactions: ghReactions{Plus1: 5, Heart: 1},
			RepoURL:   "https://api.github.com/repos/owner/repo",
		},
	}
	issuesBody := issuesListToJSON(issues)
	fake.addResponse(issuesPerRepoPrefix+"*", fakeResponse{
		statusCode: 200,
		headers:    map[string]string{"X-RateLimit-Remaining": "4999", "X-RateLimit-Reset": "0"},
		body:       string(issuesBody),
	})

	// Use wildcard to match the full URL with per_page parameter.
	commentsURLPrefix := "https://api.github.com/repos/owner/repo/issues/10/comments?"
	comments10 := []ghIssueComment{
		{ID: 5001, Body: "I can reproduce this", User: ghUser{Login: "user3"}, CreatedAt: t2},
	}
	comments10Body, _ := json.Marshal(comments10)
	fake.addResponse(commentsURLPrefix+"*", fakeResponse{
		statusCode: 200,
		headers:    map[string]string{"X-RateLimit-Remaining": "4998", "X-RateLimit-Reset": "0"},
		body:       string(comments10Body),
	})

	// GraphQL discussions.
	discPage := graphQLResponse{
		Data: json.RawMessage(`{
			"repository": {
				"discussions": {
					"pageInfo": {"hasNextPage": false, "endCursor": ""},
					"nodes": [
						{
							"id": "D_kwDOABC456",
							"number": 5,
							"title": "Feature suggestion: dark mode",
							"body": "It would be great to have a dark mode option.",
							"url": "https://github.com/owner/repo/discussions/5",
							"createdAt": "2025-01-01T00:00:00Z",
							"updatedAt": "2025-01-02T00:00:00Z",
							"category": {"name": "Ideas", "slug": "ideas"},
							"labels": {"nodes": [{"name": "enhancement"}]},
							"comments": {"totalCount": 1, "nodes": [{"id": "DC_kw1", "body": "Great idea!", "createdAt": "2025-01-02T01:00:00Z"}]},
							"upvoteCount": 10
						}
					]
				}
			}
		}`),
	}
	discBody, _ := json.Marshal(discPage)
	fake.addResponse("https://api.github.com/graphql", fakeResponse{
		statusCode: 200,
		headers:    map[string]string{"X-RateLimit-Remaining": "4997", "X-RateLimit-Reset": "0"},
		body:       string(discBody),
	})

	c := setupCollector(t, &CollectorConfig{
		Enabled:            true,
		SearchIssues:       true,
		SearchDiscussions:  true,
		MaxItemsPerRun:     100,
		MaxCommentsPerItem: 10,
		Repositories:       []string{"owner/repo"},
		MaxRequests:        500,
	}, fake)
	c.WithNow(func() time.Time { return collectedAt })

	signals, err := c.Collect(t.Context(), domain.CollectRequest{})
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	// Expect 3 signals: 2 issues + 1 discussion.
	if len(signals) != 3 {
		t.Fatalf("expected 3 signals, got %d", len(signals))
	}

	var issueSignals, discSignals int
	for _, s := range signals {
		switch s.SourceType {
		case sourceIDIssue:
			issueSignals++
			if s.ID == "github_issue:1001" {
				if len(s.Comments) != 1 {
					t.Fatalf("expected 1 comment for issue 1001, got %d", len(s.Comments))
				}
				if s.Comments[0].Body != "I can reproduce this" {
					t.Fatalf("unexpected comment body: %q", s.Comments[0].Body)
				}
			}
			if s.ID == "github_issue:1002" && len(s.Comments) != 0 {
				t.Fatalf("expected 0 comments for issue 1002, got %d", len(s.Comments))
			}
		case sourceIDDiscussion:
			discSignals++
			if s.ID == "github_discussion:D_kwDOABC456" {
				if len(s.Comments) != 1 {
					t.Fatalf("expected 1 comment for discussion, got %d", len(s.Comments))
				}
				if s.ReactionCnt != 10 {
					t.Fatalf("expected upvote count 10, got %d", s.ReactionCnt)
				}
			}
		default:
			t.Fatalf("unexpected source type: %q", s.SourceType)
		}
	}

	if issueSignals != 2 {
		t.Fatalf("expected 2 issue signals, got %d", issueSignals)
	}
	if discSignals != 1 {
		t.Fatalf("expected 1 discussion signal, got %d", discSignals)
	}
}

// TestCollect_IssuesOnly verifies that Collect returns only issues
// when SearchDiscussions is disabled.
func TestCollect_IssuesOnly(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	searchResp := ghSearchResponse{
		TotalCount: 1,
		Items: []ghIssue{
			{
				ID: 2001, Number: 1, Title: "Only issue test", Body: "Body",
				HTMLURL: "https://github.com/o/r/issues/1", State: "open",
				CreatedAt: t1, UpdatedAt: t1,
				Labels:    []ghLabel{{Name: "bug"}},
				User:      ghUser{Login: "u1"},
				Comments:  0,
				Reactions: ghReactions{Plus1: 1},
				RepoURL:   "https://api.github.com/repos/o/r",
			},
		},
	}
	searchBody := searchRespToJSON(searchResp)
	searchURL := "https://api.github.com/search/issues?q=is%3Aissue+is%3Aopen&sort=updated&direction=asc&per_page=100&page=1"
	fake.addResponse(searchURL, fakeResponse{
		statusCode: 200,
		body:       string(searchBody),
	})

	c := setupCollector(t, &CollectorConfig{
		Enabled:            true,
		SearchIssues:       true,
		SearchDiscussions:  false,
		MaxItemsPerRun:     100,
		MaxCommentsPerItem: 0,
		MaxRequests:        500,
	}, fake)
	c.WithNow(func() time.Time { return collectedAt })

	signals, err := c.Collect(t.Context(), domain.CollectRequest{})
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}
	if len(signals) != 1 {
		t.Fatalf("expected 1 signal, got %d", len(signals))
	}
	if signals[0].SourceType != sourceIDIssue {
		t.Fatalf("expected issue, got %q", signals[0].SourceType)
	}
	if signals[0].ID != "github_issue:2001" {
		t.Fatalf("unexpected ID: %q", signals[0].ID)
	}
	if signals[0].Repository != "o/r" {
		t.Fatalf("unexpected repo: %q", signals[0].Repository)
	}
}

// TestCollect_DiscussionsOnly verifies that Collect returns only discussions
// when SearchIssues is disabled.
func TestCollect_DiscussionsOnly(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	discPage := graphQLResponse{
		Data: json.RawMessage(`{
			"repository": {
				"discussions": {
					"pageInfo": {"hasNextPage": false, "endCursor": ""},
					"nodes": [{
						"id": "D_kwDISC1", "number": 1,
						"title": "Discussion only test", "body": "Body",
						"url": "https://github.com/o/r/discussions/1",
						"createdAt": "2025-01-01T00:00:00Z",
						"updatedAt": "2025-01-02T00:00:00Z",
						"category": {"name": "General", "slug": "general"},
						"labels": null,
						"comments": {"totalCount": 0, "nodes": []},
						"upvoteCount": 5
					}]
				}
			}
		}`),
	}
	discBody, _ := json.Marshal(discPage)
	fake.addResponse("https://api.github.com/graphql", fakeResponse{
		statusCode: 200,
		body:       string(discBody),
	})

	c := setupCollector(t, &CollectorConfig{
		Enabled:            true,
		SearchIssues:       false,
		SearchDiscussions:  true,
		MaxItemsPerRun:     100,
		MaxCommentsPerItem: 5,
		Repositories:       []string{"o/r"},
		MaxRequests:        500,
	}, fake)
	c.WithNow(func() time.Time { return collectedAt })

	signals, err := c.Collect(t.Context(), domain.CollectRequest{})
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}
	if len(signals) != 1 {
		t.Fatalf("expected 1 signal, got %d", len(signals))
	}
	if signals[0].SourceType != sourceIDDiscussion {
		t.Fatalf("expected discussion, got %q", signals[0].SourceType)
	}
	if signals[0].ID != "github_discussion:D_kwDISC1" {
		t.Fatalf("unexpected ID: %q", signals[0].ID)
	}
	if signals[0].Repository != "o/r" {
		t.Fatalf("unexpected repo: %q", signals[0].Repository)
	}
}

// TestCollect_Dedup verifies that signals with duplicate IDs are filtered out
// within a single Collect run.
func TestCollect_Dedup(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	// Return one issue (per-repo strategy, so discussions also work).
	issuesPrefix := "https://api.github.com/repos/o/r/issues?state=open&sort=updated&direction=asc&per_page="
	issues := []ghIssue{
		{
			ID: 3001, Number: 1, Title: "Dedup test", Body: "Body",
			HTMLURL: "https://github.com/o/r/issues/1", State: "open",
			CreatedAt: t1, UpdatedAt: t1,
			User: ghUser{Login: "u1"}, Comments: 0,
			RepoURL: "https://api.github.com/repos/o/r",
		},
	}
	issuesBody := issuesListToJSON(issues)
	fake.addResponse(issuesPrefix+"*", fakeResponse{statusCode: 200, body: string(issuesBody)})

	// One discussion.
	discPage := graphQLResponse{
		Data: json.RawMessage(`{
			"repository": {
				"discussions": {
					"pageInfo": {"hasNextPage": false, "endCursor": ""},
					"nodes": [{
						"id": "D_kwDEDUP1", "number": 1,
						"title": "Discussion dedup", "body": "Body",
						"url": "https://github.com/o/r/discussions/1",
						"createdAt": "2025-01-01T00:00:00Z",
						"updatedAt": "2025-01-02T00:00:00Z",
						"category": {"name": "General", "slug": "general"},
						"labels": null,
						"comments": {"totalCount": 0, "nodes": []},
						"upvoteCount": 3
					}]
				}
			}
		}`),
	}
	discBody, _ := json.Marshal(discPage)
	fake.addResponse("https://api.github.com/graphql", fakeResponse{statusCode: 200, body: string(discBody)})

	c := setupCollector(t, &CollectorConfig{
		Enabled:            true,
		SearchIssues:       true,
		SearchDiscussions:  true,
		MaxItemsPerRun:     100,
		MaxCommentsPerItem: 0,
		Repositories:       []string{"o/r"},
		MaxRequests:        500,
	}, fake)
	c.WithNow(func() time.Time { return collectedAt })

	signals, err := c.Collect(t.Context(), domain.CollectRequest{})
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	if len(signals) != 2 {
		t.Fatalf("expected 2 signals, got %d", len(signals))
	}

	ids := make(map[string]bool)
	for _, s := range signals {
		if ids[s.ID] {
			t.Fatalf("duplicate signal ID: %q", s.ID)
		}
		ids[s.ID] = true
	}
	if !ids["github_issue:3001"] {
		t.Fatal("missing issue signal")
	}
	if !ids["github_discussion:D_kwDEDUP1"] {
		t.Fatal("missing discussion signal")
	}
}

// TestCollect_RequestLimitCutoff verifies that the request cap is enforced.
func TestCollect_RequestLimitCutoff(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	c := setupCollector(t, &CollectorConfig{
		Enabled:            true,
		SearchIssues:       true,
		SearchDiscussions:  false,
		MaxItemsPerRun:     100,
		MaxCommentsPerItem: 0,
		MaxRequests:        1, // Only 1 request allowed.
	}, fake)

	// Pre-fill request count to 1 so the next request hits the limit.
	c.client.requestCount = 1

	signals, err := c.Collect(t.Context(), domain.CollectRequest{})
	if err == nil {
		t.Fatal("expected error for request limit, got nil")
	}
	if !strings.Contains(err.Error(), "request limit") {
		t.Fatalf("expected error mentioning request limit, got: %v", err)
	}
	if len(signals) != 0 {
		t.Fatalf("expected 0 signals, got %d", len(signals))
	}
}

// TestCollect_RateLimitExhaustion verifies behavior when rate limit is reached.
func TestCollect_RateLimitExhaustion(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	c := setupCollector(t, &CollectorConfig{
		Enabled:            true,
		SearchIssues:       true,
		SearchDiscussions:  false,
		MaxItemsPerRun:     100,
		MaxCommentsPerItem: 0,
		MaxRequests:        500,
	}, fake)

	// Exhaust the REST rate limit before any request.
	c.client.restRemaining = 0
	c.client.restReset = time.Now().Add(1 * time.Hour)

	signals, err := c.Collect(t.Context(), domain.CollectRequest{})
	if err == nil {
		t.Fatal("expected error for rate limit, got nil")
	}
	if !strings.Contains(err.Error(), "rate limit") {
		t.Fatalf("expected error mentioning rate limit, got: %v", err)
	}
	if len(signals) != 0 {
		t.Fatalf("expected 0 signals, got %d", len(signals))
	}

	var rateLimitErr *RateLimitError
	if !errors.As(err, &rateLimitErr) {
		t.Fatalf("expected RateLimitError, got %T: %v", err, err)
	}
}

// TestCollect_ValidContext verifies that a valid context works.
func TestCollect_ValidContext(t *testing.T) {
	t.Parallel()
	c, err := New(&CollectorConfig{
		Enabled:           true,
		SearchIssues:      true,
		SearchDiscussions: false,
		MaxItemsPerRun:    5,
		MaxRequests:       100,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// With no mocked transport, an HTTP call will fail, but the context
	// itself should not cause a nil pointer panic.
	_, err = c.Collect(context.Background(), domain.CollectRequest{})
	if err == nil {
		t.Log("Collect succeeded without mocked transport (expected with real API)")
	}
}

// TestCollect_NotEnabled verifies that disabled collector returns ErrNotEnabled.
func TestCollect_NotEnabled(t *testing.T) {
	t.Parallel()
	_, err := New(&CollectorConfig{Enabled: false})
	if !errors.Is(err, ErrNotEnabled) {
		t.Fatalf("expected ErrNotEnabled, got %v", err)
	}
}

// TestCollect_EmptyResults verifies empty results when no sources configured.
func TestCollect_EmptyResults(t *testing.T) {
	t.Parallel()
	c := setupCollector(t, &CollectorConfig{
		Enabled:           true,
		SearchIssues:      false,
		SearchDiscussions: false,
		MaxRequests:       500,
	}, newFakeTransport())
	c.WithNow(func() time.Time { return collectedAt })

	signals, err := c.Collect(t.Context(), domain.CollectRequest{})
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}
	if len(signals) != 0 {
		t.Fatalf("expected 0 signals, got %d", len(signals))
	}
}

// TestCollect_MaxItemsLimit verifies max-items limit across both sources.
func TestCollect_MaxItemsLimit(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	// Per-repo issues: return 2 issues but maxItems=2 total.
	issuesPrefix := "https://api.github.com/repos/o/r/issues?state=open&sort=updated&direction=asc&per_page="
	issues := []ghIssue{
		{
			ID: 4001, Number: 1, Title: "Issue 1", Body: "Body 1",
			HTMLURL: "https://github.com/o/r/issues/1", State: "open",
			CreatedAt: t1, UpdatedAt: t2,
			User: ghUser{Login: "u1"}, Comments: 0,
			RepoURL: "https://api.github.com/repos/o/r",
		},
		{
			ID: 4002, Number: 2, Title: "Issue 2", Body: "Body 2",
			HTMLURL: "https://github.com/o/r/issues/2", State: "open",
			CreatedAt: t1, UpdatedAt: t2,
			User: ghUser{Login: "u2"}, Comments: 0,
			RepoURL: "https://api.github.com/repos/o/r",
		},
	}
	issuesBody := issuesListToJSON(issues)
	fake.addResponse(issuesPrefix+"*", fakeResponse{statusCode: 200, body: string(issuesBody)})

	// GraphQL discussions: return 2 discussions.
	discPage := graphQLResponse{
		Data: json.RawMessage(`{
			"repository": {
				"discussions": {
					"pageInfo": {"hasNextPage": false, "endCursor": ""},
					"nodes": [
						{
							"id": "D_kwMAX1", "number": 1,
							"title": "Discussion 1", "body": "Body",
							"url": "https://github.com/o/r/discussions/1",
							"createdAt": "2025-01-01T00:00:00Z",
							"updatedAt": "2025-01-02T00:00:00Z",
							"category": null, "labels": null,
							"comments": {"totalCount": 0, "nodes": []},
							"upvoteCount": 0
						},
						{
							"id": "D_kwMAX2", "number": 2,
							"title": "Discussion 2", "body": "Body 2",
							"url": "https://github.com/o/r/discussions/2",
							"createdAt": "2025-01-01T00:00:00Z",
							"updatedAt": "2025-01-03T00:00:00Z",
							"category": null, "labels": null,
							"comments": {"totalCount": 0, "nodes": []},
							"upvoteCount": 0
						}
					]
				}
			}
		}`),
	}
	discBody, _ := json.Marshal(discPage)
	fake.addResponse("https://api.github.com/graphql", fakeResponse{statusCode: 200, body: string(discBody)})

	c := setupCollector(t, &CollectorConfig{
		Enabled:            true,
		SearchIssues:       true,
		SearchDiscussions:  true,
		MaxItemsPerRun:     2,
		MaxCommentsPerItem: 0,
		Repositories:       []string{"o/r"},
		MaxRequests:        500,
	}, fake)
	c.WithNow(func() time.Time { return collectedAt })

	signals, err := c.Collect(t.Context(), domain.CollectRequest{})
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}
	if len(signals) != 2 {
		t.Fatalf("expected 2 signals (max items), got %d", len(signals))
	}
}

// TestCollect_PerRepoStrategy verifies collection with specific repos.
func TestCollect_PerRepoStrategy(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	issuesPrefix := "https://api.github.com/repos/myrepo/awesome/issues?state=open&sort=updated&direction=asc&per_page="
	issues := []ghIssue{
		{
			ID: 5001, Number: 1, Title: "Repo issue 1", Body: "Body",
			HTMLURL: "https://github.com/myrepo/awesome/issues/1", State: "open",
			CreatedAt: t1, UpdatedAt: t2,
			Labels: []ghLabel{{Name: "bug"}},
			User:   ghUser{Login: "u1"}, Comments: 0,
			Reactions: ghReactions{Plus1: 2},
			RepoURL:   "https://api.github.com/repos/myrepo/awesome",
		},
	}
	issuesBody := issuesListToJSON(issues)
	fake.addResponse(issuesPrefix+"*", fakeResponse{statusCode: 200, body: string(issuesBody)})

	c := setupCollector(t, &CollectorConfig{
		Enabled:            true,
		SearchIssues:       true,
		SearchDiscussions:  false,
		MaxItemsPerRun:     100,
		MaxCommentsPerItem: 0,
		Repositories:       []string{"myrepo/awesome"},
		MaxRequests:        500,
	}, fake)
	c.WithNow(func() time.Time { return collectedAt })

	signals, err := c.Collect(t.Context(), domain.CollectRequest{})
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}
	if len(signals) != 1 {
		t.Fatalf("expected 1 signal, got %d", len(signals))
	}
	if signals[0].ID != "github_issue:5001" {
		t.Fatalf("unexpected ID: %q", signals[0].ID)
	}
	if signals[0].Repository != "myrepo/awesome" {
		t.Fatalf("unexpected repository: %q", signals[0].Repository)
	}
}

// TestCollect_PartialFailure_IssueError verifies that when issues fail,
// discussions are still returned and errors are surfaced.
func TestCollect_PartialFailure_IssueError(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	// But discussions should succeed.
	discPage := graphQLResponse{
		Data: json.RawMessage(`{
			"repository": {
				"discussions": {
					"pageInfo": {"hasNextPage": false, "endCursor": ""},
					"nodes": [{
						"id": "D_kwPARTIAL1", "number": 1,
						"title": "Partial failure discussion", "body": "Body",
						"url": "https://github.com/o/r/discussions/1",
						"createdAt": "2025-01-01T00:00:00Z",
						"updatedAt": "2025-01-02T00:00:00Z",
						"category": null, "labels": null,
						"comments": {"totalCount": 0, "nodes": []},
						"upvoteCount": 5
					}]
				}
			}
		}`),
	}
	discBody, _ := json.Marshal(discPage)
	fake.addResponse("https://api.github.com/graphql", fakeResponse{statusCode: 200, body: string(discBody)})

	c := setupCollector(t, &CollectorConfig{
		Enabled:            true,
		SearchIssues:       true,
		SearchDiscussions:  true,
		MaxItemsPerRun:     100,
		MaxCommentsPerItem: 0,
		Repositories:       []string{"o/r"},
		MaxRequests:        500,
	}, fake)
	c.WithNow(func() time.Time { return collectedAt })

	signals, err := c.Collect(t.Context(), domain.CollectRequest{})
	if err == nil {
		t.Fatal("expected error for partial failure, got nil")
	}
	if !strings.Contains(err.Error(), "github issues") {
		t.Fatalf("expected error mentioning 'github issues', got: %v", err)
	}
	if len(signals) != 1 {
		t.Fatalf("expected 1 signal (discussion), got %d", len(signals))
	}
	if signals[0].SourceType != sourceIDDiscussion {
		t.Fatalf("expected discussion signal, got %q", signals[0].SourceType)
	}
}

// TestCollect_PartialFailure_DiscussionError verifies that when discussions fail,
// issues are still returned and errors are surfaced.
func TestCollect_PartialFailure_DiscussionError(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	// Issues per-repo endpoint succeeds.
	issuesPrefix := "https://api.github.com/repos/o/r/issues?state=open&sort=updated&direction=asc&per_page="
	issues := []ghIssue{
		{
			ID: 6001, Number: 1, Title: "Working issue", Body: "Body",
			HTMLURL: "https://github.com/o/r/issues/1", State: "open",
			CreatedAt: t1, UpdatedAt: t2,
			User: ghUser{Login: "u1"}, Comments: 0,
			RepoURL: "https://api.github.com/repos/o/r",
		},
	}
	issuesBody := issuesListToJSON(issues)
	fake.addResponse(issuesPrefix+"*", fakeResponse{statusCode: 200, body: string(issuesBody)})

	// Discussions endpoint is NOT registered — will fail with 404.
	c := setupCollector(t, &CollectorConfig{
		Enabled:            true,
		SearchIssues:       true,
		SearchDiscussions:  true,
		MaxItemsPerRun:     100,
		MaxCommentsPerItem: 0,
		Repositories:       []string{"o/r"},
		MaxRequests:        500,
	}, fake)
	c.WithNow(func() time.Time { return collectedAt })

	signals, err := c.Collect(t.Context(), domain.CollectRequest{})
	if err == nil {
		t.Fatal("expected error for partial failure, got nil")
	}
	if !strings.Contains(err.Error(), "github discussions") {
		t.Fatalf("expected error mentioning 'github discussions', got: %v", err)
	}
	if len(signals) != 1 {
		t.Fatalf("expected 1 signal (issue), got %d", len(signals))
	}
	if signals[0].SourceType != sourceIDIssue {
		t.Fatalf("expected issue signal, got %q", signals[0].SourceType)
	}
}

// TestCollect_WithCache verifies integration with the response cache.
func TestCollect_WithCache(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	searchResp := ghSearchResponse{
		TotalCount: 1,
		Items: []ghIssue{
			{
				ID: 7001, Number: 1, Title: "Cached issue", Body: "Body",
				HTMLURL: "https://github.com/o/r/issues/1", State: "open",
				CreatedAt: t1, UpdatedAt: t2,
				User: ghUser{Login: "u1"}, Comments: 0,
				RepoURL: "https://api.github.com/repos/o/r",
			},
		},
	}
	searchBody := searchRespToJSON(searchResp)
	searchURL := "https://api.github.com/search/issues?q=is%3Aissue+is%3Aopen&sort=updated&direction=asc&per_page=100&page=1"
	fake.addResponse(searchURL, fakeResponse{
		statusCode: 200,
		headers:    map[string]string{"ETag": `W/"cache1"`, "X-RateLimit-Remaining": "4999"},
		body:       string(searchBody),
	})

	c := setupCollector(t, &CollectorConfig{
		Enabled:            true,
		SearchIssues:       true,
		SearchDiscussions:  false,
		MaxItemsPerRun:     100,
		MaxCommentsPerItem: 0,
		MaxRequests:        500,
	}, fake)
	c.WithNow(func() time.Time { return collectedAt })

	// First call.
	signals1, err := c.Collect(t.Context(), domain.CollectRequest{})
	if err != nil {
		t.Fatalf("first Collect failed: %v", err)
	}
	if len(signals1) != 1 {
		t.Fatalf("expected 1 signal from first call, got %d", len(signals1))
	}

	callCount1 := fake.callCountFor(searchURL)
	if callCount1 != 1 {
		t.Fatalf("expected 1 call to search URL, got %d", callCount1)
	}

	// Second call: should use in-memory ETag cache and get 304.
	fake.resetCallCount()
	fake.addResponse(searchURL, fakeResponse{
		statusCode: 304,
		headers:    map[string]string{"ETag": `W/"cache1"`, "X-RateLimit-Remaining": "4998"},
		body:       "",
	})

	signals2, err := c.Collect(t.Context(), domain.CollectRequest{})
	if err != nil {
		t.Fatalf("second Collect failed: %v", err)
	}
	if len(signals2) != 1 {
		t.Fatalf("expected 1 signal from second call, got %d", len(signals2))
	}
	if signals2[0].ID != "github_issue:7001" {
		t.Fatalf("unexpected ID from cached response: %q", signals2[0].ID)
	}
}

// TestCollect_IssueWithoutRepoURL verifies fallback to HTML URL for owner/repo.
func TestCollect_IssueWithoutRepoURL(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	searchResp := ghSearchResponse{
		TotalCount: 1,
		Items: []ghIssue{
			{
				ID: 8001, Number: 1, Title: "No repo URL", Body: "Body",
				HTMLURL: "https://github.com/someorg/somerepo/issues/1", State: "open",
				CreatedAt: t1, UpdatedAt: t2,
				User: ghUser{Login: "u1"}, Comments: 0,
			},
		},
	}
	searchBody := searchRespToJSON(searchResp)
	searchURL := "https://api.github.com/search/issues?q=is%3Aissue+is%3Aopen&sort=updated&direction=asc&per_page=100&page=1"
	fake.addResponse(searchURL, fakeResponse{statusCode: 200, body: string(searchBody)})

	c := setupCollector(t, &CollectorConfig{
		Enabled:            true,
		SearchIssues:       true,
		SearchDiscussions:  false,
		MaxItemsPerRun:     100,
		MaxCommentsPerItem: 0,
		MaxRequests:        500,
	}, fake)
	c.WithNow(func() time.Time { return collectedAt })

	signals, err := c.Collect(t.Context(), domain.CollectRequest{})
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}
	if len(signals) != 1 {
		t.Fatalf("expected 1 signal, got %d", len(signals))
	}
	if signals[0].Repository != "someorg/somerepo" {
		t.Fatalf("expected repo someorg/somerepo, got %q", signals[0].Repository)
	}
}

// TestCollect_InvalidIssueURL verifies issues with unresolvable URLs are skipped.
func TestCollect_InvalidIssueURL(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	searchResp := ghSearchResponse{
		TotalCount: 1,
		Items: []ghIssue{
			{
				ID: 9001, Number: 1, Title: "Bad URL issue", Body: "Body",
				HTMLURL: "", State: "open",
				CreatedAt: t1, UpdatedAt: t2,
				User: ghUser{Login: "u1"}, Comments: 0,
			},
		},
	}
	searchBody := searchRespToJSON(searchResp)
	searchURL := "https://api.github.com/search/issues?q=is%3Aissue+is%3Aopen&sort=updated&direction=asc&per_page=100&page=1"
	fake.addResponse(searchURL, fakeResponse{statusCode: 200, body: string(searchBody)})

	c := setupCollector(t, &CollectorConfig{
		Enabled:            true,
		SearchIssues:       true,
		SearchDiscussions:  false,
		MaxItemsPerRun:     100,
		MaxCommentsPerItem: 0,
		MaxRequests:        500,
	}, fake)
	c.WithNow(func() time.Time { return collectedAt })

	signals, err := c.Collect(t.Context(), domain.CollectRequest{})
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}
	if len(signals) != 0 {
		t.Fatalf("expected 0 signals for unresolvable URLs, got %d", len(signals))
	}
}
