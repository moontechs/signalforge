package hackernews

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/moontechs/signalforge/internal/domain"
	"github.com/moontechs/signalforge/internal/storage"
)

// parseStory converts a valid HN story item and its flattened comments into a
// domain.RawSignal.
func parseStory(item *itemResponse, comments []domain.Comment, category string, collectedAt time.Time) domain.RawSignal {
	body := item.Text
	if body == "" {
		body = item.URL
	}
	url := item.URL
	if url == "" {
		url = fmt.Sprintf("https://news.ycombinator.com/item?id=%d", item.ID)
	}

	s := domain.RawSignal{
		ID:           fmt.Sprintf("%s:%d", SignalIDPrefix, item.ID),
		Source:       SourceName,
		SourceID:     strconv.Itoa(item.ID),
		SourceType:   SourceType,
		URL:          url,
		Title:        item.Title,
		Body:         body,
		Comments:     comments,
		Community:    "hackernews",
		Category:     category,
		Score:        item.Score,
		CommentCount: item.Descendants,
		CreatedAt:    time.Unix(item.Time, 0).UTC(),
		CollectedAt:  collectedAt,
		Metadata: map[string]string{
			MetaKeyAuthor:     item.By,
			MetaKeyStoryScore: strconv.Itoa(item.Score),
			MetaKeyCommentCnt: strconv.Itoa(item.Descendants),
		},
	}
	parts := append([]string{s.Title, s.Body}, commentBodies(comments)...)
	s.ContentHash = storage.ContentHash(parts...)
	return s
}

func commentBodies(comments []domain.Comment) []string {
	parts := make([]string, len(comments))
	for i := range comments {
		parts[i] = comments[i].Body
	}
	return parts
}

// commentRef is a BFS queue entry for flattening comment trees.
type commentRef struct {
	id    int
	depth int
}

// flattenComments traverses a story's kid comments in BFS order (max depth 50)
// and returns a flat list of domain.Comment values. maxComments caps the total
// number of flattened comments (0 = unlimited).
func flattenComments(root *itemResponse, c *client, maxComments int, ctx context.Context) ([]domain.Comment, error) {
	queue := make([]commentRef, 0, len(root.Kids))
	for _, id := range root.Kids {
		queue = append(queue, commentRef{id: id, depth: 1})
	}

	var out []domain.Comment
	for len(queue) > 0 && (maxComments <= 0 || len(out) < maxComments) {
		ref := queue[0]
		queue = queue[1:]

		if ref.depth > 50 {
			continue
		}

		item, err := c.item(ctx, ref.id)
		if err != nil {
			return out, fmt.Errorf("comment %d: %w", ref.id, err)
		}
		if item == nil || item.Deleted || item.Dead || item.Type != "comment" {
			continue
		}

		out = append(out, domain.Comment{
			ID:        strconv.Itoa(item.ID),
			Body:      item.Text,
			Score:     item.Score,
			CreatedAt: time.Unix(item.Time, 0).UTC(),
		})

		if ref.depth < 50 {
			for _, kid := range item.Kids {
				queue = append(queue, commentRef{id: kid, depth: ref.depth + 1})
			}
		}
	}
	return out, nil
}

// eligibleStory checks whether an item should be included as a top-level signal.
// It filters by type, dead/deleted status, minimum score, and the since window.
func eligibleStory(item *itemResponse, since time.Time, minimumScore int) bool {
	if item == nil || item.Type != "story" || item.Dead || item.Deleted {
		return false
	}
	if item.Score < minimumScore {
		return false
	}
	return since.IsZero() || !time.Unix(item.Time, 0).Before(since)
}

// MetaKeyCommentCnt is the metadata key for the original comment count.
const MetaKeyCommentCnt = "comment_count"