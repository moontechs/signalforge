package reddit

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/moontechs/signalforge/internal/storage"
)

func TestParsePostAndComments(t *testing.T) {
	postBody, err := os.ReadFile("../../../testdata/reddit/posts.json")
	if err != nil {
		t.Fatal(err)
	}
	var posts listingResponse
	if err := json.Unmarshal(postBody, &posts); err != nil {
		t.Fatal(err)
	}
	commentBody, err := os.ReadFile("../../../testdata/reddit/comments.json")
	if err != nil {
		t.Fatal(err)
	}
	var commentListings []listingResponse
	if err := json.Unmarshal(commentBody, &commentListings); err != nil {
		t.Fatal(err)
	}

	collectedAt := time.Unix(1700000100, 0).UTC()
	signal := parsePost(&posts.Data.Children[0].Data, collectedAt, 10, &commentListings[1])
	if signal.ID != "rd:abc123" || signal.URL != "https://www.reddit.com/r/smallbusiness/comments/abc123/problem/" {
		t.Fatalf("unexpected signal identity: %+v", signal)
	}
	if len(signal.Comments) != 3 || signal.Comments[0].ID != "c1" || signal.Comments[1].ID != "c2" || signal.Comments[2].ID != "c3" {
		t.Fatalf("unexpected flattened comments: %+v", signal.Comments)
	}
	if signal.CreatedAt.Location() != time.UTC || signal.CollectedAt != collectedAt {
		t.Fatalf("unexpected timestamps: created=%v collected=%v", signal.CreatedAt, signal.CollectedAt)
	}
	expectedHash := storage.ContentHash("A recurring problem", "Need a better tool", "first", "nested", "child survives")
	if signal.ContentHash != expectedHash {
		t.Fatalf("content hash = %q, want %q", signal.ContentHash, expectedHash)
	}
}

func TestEligiblePost(t *testing.T) {
	base := postResponse{ID: "post", Title: "title", CreatedUTC: 100}
	tests := []struct {
		name string
		post postResponse
		want bool
	}{
		{name: "eligible", post: base, want: true},
		{name: "old", post: postResponse{ID: "post", Title: "title", CreatedUTC: 98}},
		{name: "missing ID", post: postResponse{Title: "title", CreatedUTC: 100}},
		{name: "empty content", post: postResponse{ID: "post", CreatedUTC: 100}},
		{name: "removed flag", post: postResponse{ID: "post", Title: "title", Removed: true, CreatedUTC: 100}},
		{name: "removed category", post: postResponse{ID: "post", Title: "title", RemovedByCategory: "moderator", CreatedUTC: 100}},
		{name: "deleted marker", post: postResponse{ID: "post", Title: "title", Selftext: "[deleted]", CreatedUTC: 100}},
		{name: "removed marker", post: postResponse{ID: "post", Title: "[removed]", CreatedUTC: 100}},
	}
	since := time.Unix(99, 0)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := eligiblePost(&test.post, since); got != test.want {
				t.Fatalf("eligiblePost() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestFlattenCommentsLimitsAndDepth(t *testing.T) {
	list := &listingResponse{Data: listingData{Children: []listingChild{
		{Kind: "t1", Data: postResponse{ID: "1", Body: "one"}},
		{Kind: "t1", Data: postResponse{ID: "2", Body: "two"}},
		{Kind: "t1", Data: postResponse{ID: "3", Body: "three"}},
		{Kind: "more", Data: postResponse{ID: "ignored", Body: "ignored"}},
	}}}
	if got := flattenComments(list, 0); got != nil {
		t.Fatalf("zero limit returned comments: %+v", got)
	}
	if got := flattenComments(list, 1); len(got) != 1 || got[0].ID != "1" {
		t.Fatalf("limit one returned: %+v", got)
	}
	if got := flattenComments(list, -1); len(got) != 3 {
		t.Fatalf("unlimited returned %d comments", len(got))
	}

	var replies *listingResponse
	for depth := maxCommentDepth + 1; depth >= 0; depth-- {
		replies = &listingResponse{Data: listingData{Children: []listingChild{{
			Kind: "t1",
			Data: postResponse{
				ID:      string(rune('a' + depth)),
				Body:    "body",
				Replies: listingReplies{Listing: replies},
			},
		}}}}
	}
	got := flattenComments(replies, -1)
	if len(got) != maxCommentDepth+1 {
		t.Fatalf("depth limit returned %d comments, want %d", len(got), maxCommentDepth+1)
	}
}

func TestParsePostFallbackURL(t *testing.T) {
	post := postResponse{ID: "x", Title: "title", Subreddit: "go", CreatedUTC: 100}
	signal := parsePost(&post, time.Time{}, 0, nil)
	if signal.URL != "https://www.reddit.com/r/go/comments/x" {
		t.Fatalf("fallback URL = %q", signal.URL)
	}
}
