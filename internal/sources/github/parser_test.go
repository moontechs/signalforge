package github

import (
	"testing"
	"time"

	"github.com/moontechs/signalforge/internal/domain"
)

var (
	t1          = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	t2          = time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)
	t3          = time.Date(2025, 1, 3, 0, 0, 0, 0, time.UTC)
	collectedAt = time.Date(2025, 2, 1, 12, 0, 0, 0, time.UTC)
)

// ---- Issue parsing tests ----

// TestParseIssueToSignal_Basic verifies a basic issue is correctly mapped.
func TestParseIssueToSignal_Basic(t *testing.T) {
	t.Parallel()
	issue := &ghIssue{
		ID:        42,
		Number:    1,
		Title:     "App crashes on startup",
		Body:      "When I open the app, it crashes immediately.",
		HTMLURL:   "https://github.com/owner/repo/issues/1",
		State:     "open",
		CreatedAt: t1,
		UpdatedAt: t2,
		Labels: []ghLabel{
			{Name: "bug", Color: "d73a4a"},
			{Name: "high-priority", Color: "b60205"},
		},
		User:     ghUser{Login: "user1", ID: 100},
		Comments: 3,
		Reactions: ghReactions{
			Plus1: 5,
			Heart: 2,
			Laugh: 1,
		},
	}

	comments := []ghIssueComment{
		{ID: 1001, Body: "I have the same issue", User: ghUser{Login: "user2", ID: 101}, CreatedAt: t2},
		{ID: 1002, Body: "Workaround: restart the app", User: ghUser{Login: "user3", ID: 102}, CreatedAt: t3},
	}

	signal := parseIssueToSignal(issue, "owner", "repo", comments, 10, collectedAt)

	// Check basic fields
	if signal.ID != "github_issue:42" {
		t.Fatalf("expected ID github_issue:42, got %q", signal.ID)
	}
	if signal.Source != "github" {
		t.Fatalf("expected source github, got %q", signal.Source)
	}
	if signal.SourceID != "42" {
		t.Fatalf("expected SourceID 42, got %q", signal.SourceID)
	}
	if signal.SourceType != "github_issue" {
		t.Fatalf("expected SourceType github_issue, got %q", signal.SourceType)
	}
	if signal.URL != "https://github.com/owner/repo/issues/1" {
		t.Fatalf("unexpected URL: %q", signal.URL)
	}
	if signal.Title != "App crashes on startup" {
		t.Fatalf("unexpected Title: %q", signal.Title)
	}
	if signal.Body != "When I open the app, it crashes immediately." {
		t.Fatalf("unexpected Body: %q", signal.Body)
	}
	if signal.Community != "github" {
		t.Fatalf("unexpected Community: %q", signal.Community)
	}
	if signal.Repository != "owner/repo" {
		t.Fatalf("unexpected Repository: %q", signal.Repository)
	}
	if signal.CommentCount != 3 {
		t.Fatalf("expected CommentCount 3, got %d", signal.CommentCount)
	}
	if signal.ReactionCnt != 8 {
		t.Fatalf("expected ReactionCnt 8 (5+2+1), got %d", signal.ReactionCnt)
	}
	if signal.Score != 8 {
		t.Fatalf("expected Score 8, got %d", signal.Score)
	}
	if !signal.CreatedAt.Equal(t1) {
		t.Fatalf("unexpected CreatedAt: %v", signal.CreatedAt)
	}
	if !signal.UpdatedAt.Equal(t2) {
		t.Fatalf("unexpected UpdatedAt: %v", signal.UpdatedAt)
	}
	if !signal.CollectedAt.Equal(collectedAt) {
		t.Fatalf("unexpected CollectedAt: %v", signal.CollectedAt)
	}

	// Check labels
	if len(signal.Labels) != 2 {
		t.Fatalf("expected 2 labels, got %d", len(signal.Labels))
	}
	if signal.Labels[0] != "bug" || signal.Labels[1] != "high-priority" {
		t.Fatalf("unexpected labels: %v", signal.Labels)
	}

	// Check tags match labels
	if len(signal.Tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(signal.Tags))
	}

	// Check comments
	if len(signal.Comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(signal.Comments))
	}
	// Comments should be sorted by CreatedAt ascending
	if signal.Comments[0].ID != "1001" || signal.Comments[1].ID != "1002" {
		t.Fatalf("unexpected comment order: IDs are %q, %q",
			signal.Comments[0].ID, signal.Comments[1].ID)
	}

	// Content hash should be non-empty
	if signal.ContentHash == "" {
		t.Fatal("expected non-empty ContentHash")
	}
}

// TestParseIssueToSignal_EmptyFields verifies parsing with minimal/empty fields.
func TestParseIssueToSignal_EmptyFields(t *testing.T) {
	t.Parallel()
	issue := &ghIssue{
		ID:        1,
		Number:    1,
		Title:     "Empty issue",
		Body:      "",
		HTMLURL:   "",
		CreatedAt: t1,
		UpdatedAt: t1,
		Labels:    nil,
		User:      ghUser{},
		Comments:  0,
		Reactions: ghReactions{},
	}

	signal := parseIssueToSignal(issue, "", "", nil, 0, collectedAt)

	if signal.Body != "" {
		t.Fatalf("expected empty Body, got %q", signal.Body)
	}
	if signal.URL != "" {
		t.Fatalf("expected empty URL, got %q", signal.URL)
	}
	if signal.CommentCount != 0 {
		t.Fatalf("expected CommentCount 0, got %d", signal.CommentCount)
	}
	if signal.ReactionCnt != 0 {
		t.Fatalf("expected ReactionCnt 0, got %d", signal.ReactionCnt)
	}
	if signal.Score != 0 {
		t.Fatalf("expected Score 0, got %d", signal.Score)
	}
	if len(signal.Labels) != 0 {
		t.Fatalf("expected 0 labels, got %d", len(signal.Labels))
	}
	if len(signal.Tags) != 0 {
		t.Fatalf("expected 0 tags, got %d", len(signal.Tags))
	}
	if len(signal.Comments) != 0 {
		t.Fatalf("expected 0 comments, got %d", len(signal.Comments))
	}
	if signal.Repository != "/" { // owner and repo empty strings
		t.Fatalf("expected Repository '/', got %q", signal.Repository)
	}

	// Content hash should still be non-empty even with empty fields
	if signal.ContentHash == "" {
		t.Fatal("expected non-empty ContentHash even with empty fields")
	}
}

// TestParseIssueToSignal_MissingOptionalValues verifies parsing when optional
// fields like reactions are absent.
func TestParseIssueToSignal_MissingOptionalValues(t *testing.T) {
	t.Parallel()
	issue := &ghIssue{
		ID:        99,
		Number:    99,
		Title:     "Missing optional fields",
		Body:      "Some body",
		HTMLURL:   "https://github.com/o/r/issues/99",
		CreatedAt: t1,
		UpdatedAt: t2,
		Labels:    []ghLabel{},
		User:      ghUser{Login: "testuser"},
		Comments:  0,
		// Reactions is zero-value
	}

	signal := parseIssueToSignal(issue, "o", "r", nil, 5, collectedAt)

	if signal.ReactionCnt != 0 {
		t.Fatalf("expected ReactionCnt 0, got %d", signal.ReactionCnt)
	}
	if signal.Score != 0 {
		t.Fatalf("expected Score 0, got %d", signal.Score)
	}
	if signal.CommentCount != 0 {
		t.Fatalf("expected CommentCount 0, got %d", signal.CommentCount)
	}
	if len(signal.Comments) != 0 {
		t.Fatalf("expected 0 comments, got %d", len(signal.Comments))
	}
	if len(signal.Labels) != 0 {
		t.Fatalf("expected 0 labels, got %d", len(signal.Labels))
	}
	if signal.Repository != "o/r" {
		t.Fatalf("expected Repository 'o/r', got %q", signal.Repository)
	}
}

// TestParseIssueToSignal_CommentCap verifies that comments are truncated
// to the maxComments limit and sorted by CreatedAt ascending.
func TestParseIssueToSignal_CommentCap(t *testing.T) {
	t.Parallel()
	issue := &ghIssue{
		ID:        5,
		Number:    5,
		Title:     "Many comments",
		Body:      "This issue has many comments.",
		HTMLURL:   "https://github.com/o/r/issues/5",
		CreatedAt: t1,
		UpdatedAt: t2,
		Comments:  5,
		Reactions: ghReactions{Plus1: 1},
	}

	// Create comments in reverse chronological order to test sorting
	comments := []ghIssueComment{
		{ID: 5, Body: "Fifth comment", CreatedAt: t3},
		{ID: 4, Body: "Fourth comment", CreatedAt: t2.Add(3 * time.Hour)},
		{ID: 3, Body: "Third comment", CreatedAt: t2.Add(2 * time.Hour)},
		{ID: 2, Body: "Second comment", CreatedAt: t2.Add(1 * time.Hour)},
		{ID: 1, Body: "First comment", CreatedAt: t2},
	}

	// Cap at 3 comments
	signal := parseIssueToSignal(issue, "o", "r", comments, 3, collectedAt)

	if len(signal.Comments) != 3 {
		t.Fatalf("expected 3 comments (capped), got %d", len(signal.Comments))
	}

	// Should be sorted by CreatedAt ascending (IDs 1, 2, 3)
	if signal.Comments[0].ID != "1" || signal.Comments[1].ID != "2" || signal.Comments[2].ID != "3" {
		t.Fatalf("unexpected comment order after cap: IDs %q, %q, %q (expected 1,2,3)",
			signal.Comments[0].ID, signal.Comments[1].ID, signal.Comments[2].ID)
	}

	// Verify bodies match
	if signal.Comments[0].Body != "First comment" {
		t.Fatalf("unexpected body: %q", signal.Comments[0].Body)
	}
}

// TestParseIssueToSignal_NoComments verifies handling when no comments are provided.
func TestParseIssueToSignal_NoComments(t *testing.T) {
	t.Parallel()
	issue := &ghIssue{
		ID:        10,
		Number:    10,
		Title:     "No comments",
		Body:      "Body text",
		HTMLURL:   "https://github.com/o/r/issues/10",
		CreatedAt: t1,
		UpdatedAt: t1,
		Comments:  0,
		Reactions: ghReactions{},
	}

	signal := parseIssueToSignal(issue, "o", "r", nil, 10, collectedAt)

	if len(signal.Comments) != 0 {
		t.Fatalf("expected 0 comments, got %d", len(signal.Comments))
	}
}

// ---- Discussion parsing tests ----

// TestParseDiscussionToSignal_Basic verifies a basic discussion is correctly mapped.
func TestParseDiscussionToSignal_Basic(t *testing.T) {
	t.Parallel()
	disc := &graphQLDiscussionNode{
		ID:        "D_kwDOABC123",
		Number:    1,
		Title:     "How about a dark mode?",
		Body:      "I think the app would benefit from a dark mode option.",
		URL:       "https://github.com/owner/repo/discussions/1",
		CreatedAt: t1,
		UpdatedAt: t2,
		Category: &struct {
			Name string `json:"name"`
			Slug string `json:"slug"`
		}{Name: "Ideas", Slug: "ideas"},
		Labels: &struct {
			Nodes []struct {
				Name string `json:"name"`
			} `json:"nodes"`
		}{Nodes: []struct {
			Name string `json:"name"`
		}{
			{Name: "enhancement"},
			{Name: "feature"},
		}},
		Comments: &struct {
			TotalCount int                        `json:"totalCount"`
			Nodes      []graphQLDiscussionComment `json:"nodes"`
		}{
			TotalCount: 2,
			Nodes: []graphQLDiscussionComment{
				{ID: "DC_kw1", Body: "Great idea!", CreatedAt: t2},
				{ID: "DC_kw2", Body: "I would use this", CreatedAt: t3},
			},
		},
		UpvoteCount: 15,
	}

	signal := parseDiscussionToSignal(disc, "owner", "repo", 10, collectedAt)

	// Check basic fields
	if signal.ID != "github_discussion:D_kwDOABC123" {
		t.Fatalf("expected ID github_discussion:D_kwDOABC123, got %q", signal.ID)
	}
	if signal.Source != "github" {
		t.Fatalf("expected source github, got %q", signal.Source)
	}
	if signal.SourceID != "D_kwDOABC123" {
		t.Fatalf("expected SourceID D_kwDOABC123, got %q", signal.SourceID)
	}
	if signal.SourceType != "github_discussion" {
		t.Fatalf("expected SourceType github_discussion, got %q", signal.SourceType)
	}
	if signal.URL != "https://github.com/owner/repo/discussions/1" {
		t.Fatalf("unexpected URL: %q", signal.URL)
	}
	if signal.Title != "How about a dark mode?" {
		t.Fatalf("unexpected Title: %q", signal.Title)
	}
	if signal.Body != "I think the app would benefit from a dark mode option." {
		t.Fatalf("unexpected Body: %q", signal.Body)
	}
	if signal.Community != "github" {
		t.Fatalf("unexpected Community: %q", signal.Community)
	}
	if signal.Repository != "owner/repo" {
		t.Fatalf("unexpected Repository: %q", signal.Repository)
	}
	if signal.Category != "Ideas" {
		t.Fatalf("expected Category 'Ideas', got %q", signal.Category)
	}
	if signal.CommentCount != 2 {
		t.Fatalf("expected CommentCount 2, got %d", signal.CommentCount)
	}
	if signal.ReactionCnt != 15 {
		t.Fatalf("expected ReactionCnt 15, got %d", signal.ReactionCnt)
	}
	if signal.Score != 15 {
		t.Fatalf("expected Score 15, got %d", signal.Score)
	}
	if !signal.CreatedAt.Equal(t1) {
		t.Fatalf("unexpected CreatedAt: %v", signal.CreatedAt)
	}
	if !signal.UpdatedAt.Equal(t2) {
		t.Fatalf("unexpected UpdatedAt: %v", signal.UpdatedAt)
	}
	if !signal.CollectedAt.Equal(collectedAt) {
		t.Fatalf("unexpected CollectedAt: %v", signal.CollectedAt)
	}

	// Check labels
	if len(signal.Labels) != 2 {
		t.Fatalf("expected 2 labels, got %d", len(signal.Labels))
	}
	if signal.Labels[0] != "enhancement" || signal.Labels[1] != "feature" {
		t.Fatalf("unexpected labels: %v", signal.Labels)
	}

	// Tags should include labels + category
	if len(signal.Tags) != 3 {
		t.Fatalf("expected 3 tags (labels+category), got %d: %v", len(signal.Tags), signal.Tags)
	}

	// Check comments
	if len(signal.Comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(signal.Comments))
	}
	if signal.Comments[0].ID != "DC_kw1" || signal.Comments[1].ID != "DC_kw2" {
		t.Fatalf("unexpected comment order: IDs %q, %q", signal.Comments[0].ID, signal.Comments[1].ID)
	}

	// Content hash should be non-empty
	if signal.ContentHash == "" {
		t.Fatal("expected non-empty ContentHash")
	}
}

// TestParseDiscussionToSignal_EmptyFields verifies parsing with minimal/empty discussion fields.
func TestParseDiscussionToSignal_EmptyFields(t *testing.T) {
	t.Parallel()
	disc := &graphQLDiscussionNode{
		ID:          "D_kwEMPTY",
		Number:      0,
		Title:       "",
		Body:        "",
		URL:         "",
		CreatedAt:   t1,
		UpdatedAt:   t1,
		Category:    nil,
		Labels:      nil,
		Comments:    nil,
		UpvoteCount: 0,
	}

	signal := parseDiscussionToSignal(disc, "", "", 0, collectedAt)

	if signal.Title != "" {
		t.Fatalf("expected empty Title, got %q", signal.Title)
	}
	if signal.Body != "" {
		t.Fatalf("expected empty Body, got %q", signal.Body)
	}
	if signal.URL != "" {
		t.Fatalf("expected empty URL, got %q", signal.URL)
	}
	if signal.Category != "" {
		t.Fatalf("expected empty Category, got %q", signal.Category)
	}
	if signal.CommentCount != 0 {
		t.Fatalf("expected CommentCount 0, got %d", signal.CommentCount)
	}
	if signal.ReactionCnt != 0 {
		t.Fatalf("expected ReactionCnt 0, got %d", signal.ReactionCnt)
	}
	if signal.Score != 0 {
		t.Fatalf("expected Score 0, got %d", signal.Score)
	}
	if len(signal.Labels) != 0 {
		t.Fatalf("expected 0 labels, got %d", len(signal.Labels))
	}
	if len(signal.Tags) != 0 {
		t.Fatalf("expected 0 tags, got %d", len(signal.Tags))
	}
	if len(signal.Comments) != 0 {
		t.Fatalf("expected 0 comments, got %d", len(signal.Comments))
	}
	if signal.Repository != "/" {
		t.Fatalf("expected Repository '/', got %q", signal.Repository)
	}
	if signal.ContentHash == "" {
		t.Fatal("expected non-empty ContentHash even with empty fields")
	}
}

// TestParseDiscussionToSignal_MissingLabels verifies parsing when labels and category are nil.
func TestParseDiscussionToSignal_MissingLabels(t *testing.T) {
	t.Parallel()
	disc := &graphQLDiscussionNode{
		ID:        "D_kwMISS",
		Number:    1,
		Title:     "No labels discussion",
		Body:      "Body text here",
		URL:       "https://github.com/o/r/discussions/1",
		CreatedAt: t1,
		UpdatedAt: t2,
		Category:  nil,
		Labels:    nil,
		Comments: &struct {
			TotalCount int                        `json:"totalCount"`
			Nodes      []graphQLDiscussionComment `json:"nodes"`
		}{
			TotalCount: 0,
			Nodes:      nil,
		},
		UpvoteCount: 0,
	}

	signal := parseDiscussionToSignal(disc, "owner", "repo", 5, collectedAt)

	if len(signal.Labels) != 0 {
		t.Fatalf("expected 0 labels, got %d", len(signal.Labels))
	}
	if signal.Category != "" {
		t.Fatalf("expected empty Category, got %q", signal.Category)
	}
	// Tags should be empty since no labels and no category
	if len(signal.Tags) != 0 {
		t.Fatalf("expected 0 tags, got %d: %v", len(signal.Tags), signal.Tags)
	}
	if len(signal.Comments) != 0 {
		t.Fatalf("expected 0 comments, got %d", len(signal.Comments))
	}
}

// TestParseDiscussionToSignal_CommentCap verifies discussion comment truncation.
func TestParseDiscussionToSignal_CommentCap(t *testing.T) {
	t.Parallel()
	disc := &graphQLDiscussionNode{
		ID:        "D_kwCAP",
		Number:    1,
		Title:     "Many discussion comments",
		Body:      "Body",
		URL:       "https://github.com/o/r/discussions/1",
		CreatedAt: t1,
		UpdatedAt: t2,
		Category: &struct {
			Name string `json:"name"`
			Slug string `json:"slug"`
		}{Name: "Q&A", Slug: "qna"},
		Comments: &struct {
			TotalCount int                        `json:"totalCount"`
			Nodes      []graphQLDiscussionComment `json:"nodes"`
		}{
			TotalCount: 4,
			Nodes: []graphQLDiscussionComment{
				{ID: "c4", Body: "Late comment", CreatedAt: t3},
				{ID: "c3", Body: "Middle comment", CreatedAt: t2.Add(2 * time.Hour)},
				{ID: "c2", Body: "Earlier comment", CreatedAt: t2.Add(1 * time.Hour)},
				{ID: "c1", Body: "First comment", CreatedAt: t2},
			},
		},
		UpvoteCount: 3,
	}

	// Cap at 2 comments
	signal := parseDiscussionToSignal(disc, "o", "r", 2, collectedAt)

	if len(signal.Comments) != 2 {
		t.Fatalf("expected 2 comments (capped), got %d", len(signal.Comments))
	}

	// Should be sorted by CreatedAt ascending and capped
	if signal.Comments[0].ID != "c1" || signal.Comments[1].ID != "c2" {
		t.Fatalf("unexpected comment order after cap: IDs %q, %q (expected c1,c2)",
			signal.Comments[0].ID, signal.Comments[1].ID)
	}
}

// ---- Content hash tests ----

// TestContentHash_Stability verifies that identical inputs produce identical hashes.
func TestContentHash_Stability(t *testing.T) {
	t.Parallel()
	hash1 := generateContentHash("Title", "Body", []domain.Comment{
		{Body: "Comment 1"},
		{Body: "Comment 2"},
	})
	hash2 := generateContentHash("Title", "Body", []domain.Comment{
		{Body: "Comment 1"},
		{Body: "Comment 2"},
	})

	if hash1 != hash2 {
		t.Fatal("content hash should be deterministic for identical inputs")
	}
	if hash1 == "" {
		t.Fatal("content hash should not be empty")
	}
}

// TestContentHash_DifferentInputs verifies that different inputs produce different hashes.
func TestContentHash_DifferentInputs(t *testing.T) {
	t.Parallel()
	hash1 := generateContentHash("Title A", "Body A", []domain.Comment{
		{Body: "Comment"},
	})
	hash2 := generateContentHash("Title B", "Body A", []domain.Comment{
		{Body: "Comment"},
	})
	hash3 := generateContentHash("Title A", "Body B", []domain.Comment{
		{Body: "Comment"},
	})
	hash4 := generateContentHash("Title A", "Body A", []domain.Comment{
		{Body: "Different comment"},
	})

	if hash1 == hash2 {
		t.Fatal("different titles should produce different hashes")
	}
	if hash1 == hash3 {
		t.Fatal("different bodies should produce different hashes")
	}
	if hash1 == hash4 {
		t.Fatal("different comments should produce different hashes")
	}
}

// TestContentHash_EmptyParts verifies that empty parts still produce a consistent hash.
func TestContentHash_EmptyParts(t *testing.T) {
	t.Parallel()
	hash1 := generateContentHash("", "", nil)
	hash2 := generateContentHash("", "", nil)

	if hash1 == "" {
		t.Fatal("hash of empty parts should not be empty")
	}
	if hash1 != hash2 {
		t.Fatal("hash of empty parts should be deterministic")
	}
}

// TestContentHash_CommentOrder verifies that different comment ordering
// produces different hashes (because we sort them).
func TestContentHash_CommentOrder(t *testing.T) {
	t.Parallel()
	// Both have same comments, but hash should be stable because
	// comments are sorted by CreatedAt in the mapping functions.
	hashA := generateContentHash("Title", "Body", []domain.Comment{
		{Body: "First", CreatedAt: t1},
		{Body: "Second", CreatedAt: t2},
	})
	hashB := generateContentHash("Title", "Body", []domain.Comment{
		{Body: "First", CreatedAt: t1},
		{Body: "Second", CreatedAt: t2},
	})

	if hashA != hashB {
		t.Fatal("same sorted comments should produce same hash")
	}
}

// ---- Source ID tests ----

// TestIssueSourceID verifies the source ID format for issues.
func TestIssueSourceID(t *testing.T) {
	t.Parallel()
	id := issueSourceID(12345)
	if id != "github_issue:12345" {
		t.Fatalf("expected github_issue:12345, got %q", id)
	}
}

// TestDiscussionSourceID verifies the source ID format for discussions.
func TestDiscussionSourceID(t *testing.T) {
	t.Parallel()
	id := discussionSourceID("D_kwDOABC123")
	if id != "github_discussion:D_kwDOABC123" {
		t.Fatalf("expected github_discussion:D_kwDOABC123, got %q", id)
	}
}

// ---- Extracted helper tests ----

// TestExtractOwnerRepoFromHTML verifies HTML URL parsing for owner/repo extraction.
func TestExtractOwnerRepoFromHTML(t *testing.T) {
	t.Parallel()
	owner, repo := extractOwnerRepoFromHTML("https://github.com/owner/repo/issues/1")
	if owner != "owner" || repo != "repo" {
		t.Fatalf("expected owner/repo, got %s/%s", owner, repo)
	}

	owner, repo = extractOwnerRepoFromHTML("https://github.com/owner/repo/discussions/42")
	if owner != "owner" || repo != "repo" {
		t.Fatalf("expected owner/repo, got %s/%s", owner, repo)
	}

	// Empty
	owner, repo = extractOwnerRepoFromHTML("")
	if owner != "" || repo != "" {
		t.Fatalf("expected empty, got %s/%s", owner, repo)
	}

	// Invalid
	owner, repo = extractOwnerRepoFromHTML("not-a-github-url")
	if owner != "" || repo != "" {
		t.Fatalf("expected empty for invalid URL, got %s/%s", owner, repo)
	}

	// Short URL (no issue/discussion path component)
	owner, repo = extractOwnerRepoFromHTML("https://github.com/owner/repo")
	if owner != "owner" || repo != "repo" {
		t.Fatalf("expected owner/repo, got %s/%s", owner, repo)
	}
}

// TestExtractOwnerRepo verifies repository URL parsing.
func TestExtractOwnerRepo(t *testing.T) {
	t.Parallel()
	owner, repo := extractOwnerRepo("https://api.github.com/repos/owner/repo")
	if owner != "owner" || repo != "repo" {
		t.Fatalf("expected owner/repo, got %s/%s", owner, repo)
	}

	// Empty
	owner, repo = extractOwnerRepo("")
	if owner != "" || repo != "" {
		t.Fatalf("expected empty, got %s/%s", owner, repo)
	}

	// Invalid
	owner, repo = extractOwnerRepo("not-a-url")
	if owner != "" || repo != "" {
		t.Fatalf("expected empty for invalid URL, got %s/%s", owner, repo)
	}
}

// TestExtractLabelNames verifies label name extraction.
func TestExtractLabelNames(t *testing.T) {
	t.Parallel()
	labels := []ghLabel{
		{Name: "bug", Color: "red"},
		{Name: "enhancement", Color: "blue"},
	}
	names := extractLabelNames(labels)
	if len(names) != 2 || names[0] != "bug" || names[1] != "enhancement" {
		t.Fatalf("unexpected label names: %v", names)
	}

	// Empty slice
	names = extractLabelNames(nil)
	if names != nil {
		t.Fatalf("expected nil for empty labels")
	}
}

// ---- Integration-like mapping tests ----

// TestParseIssueToSignal_ContentHashStability verifies that the same issue
// produces the same content hash across multiple parse calls.
func TestParseIssueToSignal_ContentHashStability(t *testing.T) {
	t.Parallel()
	issue := &ghIssue{
		ID:        100,
		Number:    1,
		Title:     "Stable hash test",
		Body:      "Body text",
		HTMLURL:   "https://github.com/o/r/issues/1",
		CreatedAt: t1,
		UpdatedAt: t2,
		Labels:    []ghLabel{{Name: "bug"}},
		Comments:  2,
		Reactions: ghReactions{Plus1: 1},
	}

	comments := []ghIssueComment{
		{ID: 1, Body: "Comment A", CreatedAt: t2},
		{ID: 2, Body: "Comment B", CreatedAt: t3},
	}

	signal1 := parseIssueToSignal(issue, "o", "r", comments, 5, collectedAt)
	signal2 := parseIssueToSignal(issue, "o", "r", comments, 5, collectedAt)

	if signal1.ContentHash != signal2.ContentHash {
		t.Fatal("content hash should be stable across identical issue parse calls")
	}
}

// TestParseDiscussionToSignal_ContentHashStability verifies content hash stability
// for discussions.
func TestParseDiscussionToSignal_ContentHashStability(t *testing.T) {
	t.Parallel()
	disc := &graphQLDiscussionNode{
		ID:        "D_kwTEST",
		Number:    1,
		Title:     "Discussion hash test",
		Body:      "Body",
		URL:       "https://github.com/o/r/discussions/1",
		CreatedAt: t1,
		UpdatedAt: t2,
		Category: &struct {
			Name string `json:"name"`
			Slug string `json:"slug"`
		}{Name: "Ideas", Slug: "ideas"},
		Comments: &struct {
			TotalCount int                        `json:"totalCount"`
			Nodes      []graphQLDiscussionComment `json:"nodes"`
		}{
			TotalCount: 2,
			Nodes: []graphQLDiscussionComment{
				{ID: "c1", Body: "Comment 1", CreatedAt: t2},
			},
		},
		UpvoteCount: 3,
	}

	signal1 := parseDiscussionToSignal(disc, "o", "r", 5, collectedAt)
	signal2 := parseDiscussionToSignal(disc, "o", "r", 5, collectedAt)

	if signal1.ContentHash != signal2.ContentHash {
		t.Fatal("content hash should be stable across identical discussion parse calls")
	}
}
