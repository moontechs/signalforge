package github

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/moontechs/signalforge/internal/storage"
)

func TestParseIssueFixture(t *testing.T) {
	t.Parallel()

	var payload SearchIssuesResponse
	readFixture(t, "search_issues.json", &payload)

	var comments IssueCommentsResponse
	readFixture(t, "issue_comments.json", &comments)

	collectedAt := time.Date(2026, 7, 22, 9, 0, 0, 0, time.UTC)
	signal := ParseIssue(payload.Items[0], comments, ParseOptions{
		CollectedAt: collectedAt,
		MaxComments: 1,
	})

	if signal.Source != sourceName {
		t.Fatalf("Source = %q, want %q", signal.Source, sourceName)
	}
	if signal.SourceType != sourceTypeIssue {
		t.Fatalf("SourceType = %q, want %q", signal.SourceType, sourceTypeIssue)
	}
	if signal.SourceID != "issue:I_kwDOAA" {
		t.Fatalf("SourceID = %q", signal.SourceID)
	}
	if signal.Title != "Collector request" {
		t.Fatalf("Title = %q", signal.Title)
	}
	if signal.Body != "Need better retries" {
		t.Fatalf("Body = %q", signal.Body)
	}
	if signal.Repository != "openai/codex" || signal.Community != "openai/codex" {
		t.Fatalf("Repository/Community = %q/%q", signal.Repository, signal.Community)
	}
	if signal.CommentCount != 3 {
		t.Fatalf("CommentCount = %d, want 3", signal.CommentCount)
	}
	if signal.ReactionCnt != 11 {
		t.Fatalf("ReactionCnt = %d, want 11", signal.ReactionCnt)
	}
	if !signal.CollectedAt.Equal(collectedAt) {
		t.Fatalf("CollectedAt = %s, want %s", signal.CollectedAt, collectedAt)
	}
	if len(signal.Comments) != 1 {
		t.Fatalf("len(Comments) = %d, want 1", len(signal.Comments))
	}
	if signal.Comments[0].ID != "IC_kwDOAA1" {
		t.Fatalf("Comments[0].ID = %q", signal.Comments[0].ID)
	}
	if signal.Comments[0].Body != "This hits us too." {
		t.Fatalf("Comments[0].Body = %q", signal.Comments[0].Body)
	}
	if signal.Comments[0].Score != 2 {
		t.Fatalf("Comments[0].Score = %d, want 2", signal.Comments[0].Score)
	}
	if signal.Metadata[metadataState] != "open" {
		t.Fatalf("state metadata = %q", signal.Metadata[metadataState])
	}
	if signal.Metadata[metadataLocked] != "false" {
		t.Fatalf("locked metadata = %q", signal.Metadata[metadataLocked])
	}
	if signal.Metadata[metadataClosed] != "false" {
		t.Fatalf("closed metadata = %q", signal.Metadata[metadataClosed])
	}
	if signal.Metadata[metadataAuthor] != "alice" {
		t.Fatalf("author metadata = %q", signal.Metadata[metadataAuthor])
	}

	wantHash := storage.ContentHash(
		sourceName,
		sourceTypeIssue,
		"openai/codex",
		"Collector request",
		"Need better retries",
		"This hits us too.",
	)
	if signal.ContentHash != wantHash {
		t.Fatalf("ContentHash = %q, want %q", signal.ContentHash, wantHash)
	}
}

func TestParseDiscussionFixture(t *testing.T) {
	t.Parallel()

	var payload GraphQLResponse[DiscussionsQueryData]
	readFixture(t, "search_discussions.json", &payload)

	var commentsPayload GraphQLResponse[DiscussionCommentsQueryData]
	readFixture(t, "discussion_comments.json", &commentsPayload)

	discussion := payload.Data.Search.Nodes[0]
	allComments := append([]DiscussionComment{}, discussion.Comments.Nodes...)
	allComments = append(allComments, commentsPayload.Data.Node.Comments.Nodes...)

	collectedAt := time.Date(2026, 7, 22, 9, 30, 0, 0, time.UTC)
	signal := ParseDiscussion(discussion, allComments, ParseOptions{
		CollectedAt: collectedAt,
		MaxComments: 5,
	})

	if signal.Source != sourceName {
		t.Fatalf("Source = %q, want %q", signal.Source, sourceName)
	}
	if signal.SourceType != sourceTypeDiscussion {
		t.Fatalf("SourceType = %q, want %q", signal.SourceType, sourceTypeDiscussion)
	}
	if signal.SourceID != "discussion:D_kwDOAA" {
		t.Fatalf("SourceID = %q", signal.SourceID)
	}
	if signal.Category != "Ideas" {
		t.Fatalf("Category = %q", signal.Category)
	}
	if signal.CommentCount != 1 {
		t.Fatalf("CommentCount = %d, want 1", signal.CommentCount)
	}
	if signal.AnswerCount != 1 {
		t.Fatalf("AnswerCount = %d, want 1", signal.AnswerCount)
	}
	if signal.ReactionCnt != 5 {
		t.Fatalf("ReactionCnt = %d, want 5", signal.ReactionCnt)
	}
	if len(signal.Comments) != 2 {
		t.Fatalf("len(Comments) = %d, want 2", len(signal.Comments))
	}
	if signal.Comments[1].ID != "DC_kwDOAB" {
		t.Fatalf("Comments[1].ID = %q", signal.Comments[1].ID)
	}
	if signal.Metadata[metadataCategory] != "Ideas" {
		t.Fatalf("category metadata = %q", signal.Metadata[metadataCategory])
	}
	if signal.Metadata[metadataUpvoteCount] != "3" {
		t.Fatalf("upvote metadata = %q", signal.Metadata[metadataUpvoteCount])
	}
	if signal.Metadata[metadataAnswerComment] != "DC_kwDOAB" {
		t.Fatalf("answer comment metadata = %q", signal.Metadata[metadataAnswerComment])
	}

	wantHash := storage.ContentHash(
		sourceName,
		sourceTypeDiscussion,
		"openai/codex",
		"Need better discovery",
		"The collector misses discussions.",
		"Same problem here",
		"Adding another detail here.",
	)
	if signal.ContentHash != wantHash {
		t.Fatalf("ContentHash = %q, want %q", signal.ContentHash, wantHash)
	}
}

func TestParseIssueHandlesPartialDataAndSkipsEmptyComments(t *testing.T) {
	t.Parallel()

	closedAt := time.Date(2026, 7, 21, 18, 0, 0, 0, time.UTC)
	signal := ParseIssue(IssueItem{
		ID:            42,
		Number:        9,
		HTMLURL:       "https://github.com/acme/api/issues/9",
		Title:         "  Need   export   support  ",
		Body:          "",
		State:         "closed",
		Locked:        true,
		Comments:      2,
		CreatedAt:     time.Date(2026, 7, 20, 8, 0, 0, 0, time.UTC),
		UpdatedAt:     time.Date(2026, 7, 21, 9, 0, 0, 0, time.UTC),
		ClosedAt:      &closedAt,
		RepositoryURL: "https://api.github.com/repos/acme/api",
		User: User{
			Login: "ops-bot",
			Type:  "Bot",
		},
		Labels: []Label{{Name: " export "}, {Name: "export"}, {Name: ""}},
	}, []IssueComment{
		{ID: 1, Body: "   ", CreatedAt: time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)},
		{ID: 2, NodeID: "IC_2", Body: " first useful comment ", CreatedAt: time.Date(2026, 7, 20, 11, 0, 0, 0, time.UTC)},
	}, ParseOptions{CollectedAt: time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC), MaxComments: 10})

	if signal.Repository != "acme/api" {
		t.Fatalf("Repository = %q", signal.Repository)
	}
	if signal.Body != "" {
		t.Fatalf("Body = %q, want empty", signal.Body)
	}
	if len(signal.Comments) != 1 {
		t.Fatalf("len(Comments) = %d, want 1", len(signal.Comments))
	}
	if len(signal.Labels) != 1 || signal.Labels[0] != "export" {
		t.Fatalf("Labels = %#v", signal.Labels)
	}
	if signal.Metadata[metadataClosed] != "true" {
		t.Fatalf("closed metadata = %q", signal.Metadata[metadataClosed])
	}
	if signal.Metadata[metadataClosedAt] != closedAt.Format(time.RFC3339) {
		t.Fatalf("closed_at metadata = %q", signal.Metadata[metadataClosedAt])
	}
}

func TestParseDiscussionHandlesMissingFields(t *testing.T) {
	t.Parallel()

	signal := ParseDiscussion(Discussion{
		Number:    4,
		Title:     "  Missing body  ",
		Body:      " \n ",
		URL:       "",
		Locked:    true,
		Closed:    true,
		CreatedAt: time.Date(2026, 7, 19, 7, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 7, 19, 9, 0, 0, 0, time.UTC),
		Repository: Repository{
			NameWithOwner: "",
		},
		Category: DiscussionCategory{Name: " Q&A "},
		Comments: DiscussionCommentConnection{TotalCount: 3},
		Author:   Actor{Login: "maintainer"},
	}, []DiscussionComment{
		{ID: "DC_1", Body: "", IsAnswer: true},
		{ID: "DC_2", Body: " useful answer ", IsAnswer: true, Reactions: ReactionConnection{TotalCount: 4}, CreatedAt: time.Date(2026, 7, 19, 10, 0, 0, 0, time.UTC)},
	}, ParseOptions{MaxComments: 1})

	if signal.SourceID != "discussion:4" {
		t.Fatalf("SourceID = %q", signal.SourceID)
	}
	if signal.Body != "" {
		t.Fatalf("Body = %q, want empty", signal.Body)
	}
	if signal.AnswerCount != 2 {
		t.Fatalf("AnswerCount = %d, want 2", signal.AnswerCount)
	}
	if len(signal.Comments) != 1 {
		t.Fatalf("len(Comments) = %d, want 1", len(signal.Comments))
	}
	if signal.Metadata[metadataAnswerComment] != "DC_2" {
		t.Fatalf("answer comment metadata = %q", signal.Metadata[metadataAnswerComment])
	}
	if signal.Metadata[metadataCategory] != "Q&A" {
		t.Fatalf("category metadata = %q", signal.Metadata[metadataCategory])
	}
}

func readFixture(t *testing.T, name string, dest any) {
	t.Helper()

	path := filepath.Join("..", "..", "..", "testdata", "github", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	if err := json.Unmarshal(data, dest); err != nil {
		t.Fatalf("unmarshal fixture %s: %v", name, err)
	}
}
