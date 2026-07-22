package github

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSearchIssuesResponseUnmarshal(t *testing.T) {
	payload := []byte(`{
		"total_count": 1,
		"items": [
			{
				"id": 101,
				"number": 7,
				"node_id": "I_kwDOAA",
				"html_url": "https://github.com/openai/codex/issues/7",
				"title": "Collector request",
				"body": "Need better retries",
				"state": "open",
				"locked": false,
				"comments": 3,
				"created_at": "2026-07-20T10:00:00Z",
				"updated_at": "2026-07-21T10:00:00Z",
				"repository_url": "https://api.github.com/repos/openai/codex",
				"labels": [{"name": "bug"}, {"name": "collector"}],
				"user": {"login": "alice", "type": "User"},
				"reactions": {"total_count": 11}
			}
		]
	}`)

	var resp SearchIssuesResponse
	if err := json.Unmarshal(payload, &resp); err != nil {
		t.Fatalf("unmarshal issues response: %v", err)
	}

	if resp.TotalCount != 1 {
		t.Fatalf("expected total count 1, got %d", resp.TotalCount)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(resp.Items))
	}
	issue := resp.Items[0]
	if issue.Title != "Collector request" {
		t.Fatalf("unexpected title %q", issue.Title)
	}
	if issue.Reactions.TotalCount != 11 {
		t.Fatalf("expected reactions 11, got %d", issue.Reactions.TotalCount)
	}
	if issue.CreatedAt != time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC) {
		t.Fatalf("unexpected created at %v", issue.CreatedAt)
	}
}

func TestGraphQLDiscussionResponseUnmarshal(t *testing.T) {
	payload := []byte(`{
		"data": {
			"search": {
				"pageInfo": {
					"endCursor": "cursor-1",
					"hasNextPage": true
				},
				"nodes": [
					{
						"id": "D_kwDOAA",
						"number": 12,
						"title": "Need better discovery",
						"body": "The collector misses discussions.",
						"url": "https://github.com/openai/codex/discussions/12",
						"locked": false,
						"closed": false,
						"createdAt": "2026-07-19T08:00:00Z",
						"updatedAt": "2026-07-21T08:30:00Z",
						"repository": {"nameWithOwner": "openai/codex"},
						"category": {"name": "Ideas"},
						"labels": {"nodes": [{"name": "feedback"}]},
						"comments": {
							"totalCount": 1,
							"pageInfo": {"endCursor": null, "hasNextPage": false},
							"nodes": [
								{
									"id": "DC_kwDOAA",
									"body": "Same problem here",
									"url": "https://github.com/openai/codex/discussions/12#discussioncomment-1",
									"createdAt": "2026-07-20T08:00:00Z",
									"updatedAt": "2026-07-20T08:10:00Z",
									"replyCount": 0,
									"isAnswer": false,
									"author": {"login": "bob"},
									"reactions": {"totalCount": 2}
								}
							]
						},
						"reactions": {"totalCount": 5},
						"upvoteCount": 3,
						"author": {"login": "maintainer"}
					}
				]
			}
		}
	}`)

	var resp GraphQLResponse[DiscussionsQueryData]
	if err := json.Unmarshal(payload, &resp); err != nil {
		t.Fatalf("unmarshal graphql response: %v", err)
	}

	if resp.Data.Search.PageInfo.EndCursor != "cursor-1" {
		t.Fatalf("unexpected end cursor %q", resp.Data.Search.PageInfo.EndCursor)
	}
	if len(resp.Data.Search.Nodes) != 1 {
		t.Fatalf("expected 1 discussion, got %d", len(resp.Data.Search.Nodes))
	}
	discussion := resp.Data.Search.Nodes[0]
	if discussion.Repository.NameWithOwner != "openai/codex" {
		t.Fatalf("unexpected repository %q", discussion.Repository.NameWithOwner)
	}
	if discussion.Comments.TotalCount != 1 {
		t.Fatalf("expected 1 comment, got %d", discussion.Comments.TotalCount)
	}
	if len(discussion.Comments.Nodes) != 1 {
		t.Fatalf("expected 1 comment node, got %d", len(discussion.Comments.Nodes))
	}
	if discussion.Comments.Nodes[0].Reactions.TotalCount != 2 {
		t.Fatalf("expected comment reactions 2, got %d", discussion.Comments.Nodes[0].Reactions.TotalCount)
	}
}
