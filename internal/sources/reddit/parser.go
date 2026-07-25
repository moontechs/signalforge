package reddit

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/moontechs/signalforge/internal/domain"
	"github.com/moontechs/signalforge/internal/storage"
)

const maxCommentDepth = 50

func eligiblePost(p *postResponse, since time.Time) bool {
	if p.ID == "" || p.Removed || p.RemovedByCategory != "" || (!since.IsZero() && postTime(p).Before(since)) {
		return false
	}
	title := strings.TrimSpace(p.Title)
	body := strings.TrimSpace(p.Selftext)
	return !unavailableText(title) && !unavailableText(body) && (title != "" || body != "")
}

func unavailableText(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "[deleted]", "[removed]":
		return true
	default:
		return false
	}
}

func parsePost(p *postResponse, collectedAt time.Time, maxComments int, comments *listingResponse) domain.RawSignal {
	community := p.Subreddit
	url := p.Permalink
	if strings.HasPrefix(url, "/") {
		url = "https://www.reddit.com" + url
	}
	if url == "" {
		url = fmt.Sprintf("https://www.reddit.com/r/%s/comments/%s", community, p.ID)
	}
	cs := flattenComments(comments, maxComments)
	parts := []string{p.Title, p.Selftext}
	for _, c := range cs {
		parts = append(parts, c.Body)
	}
	return domain.RawSignal{ID: SignalIDPrefix + ":" + p.ID, Source: SourceName, SourceID: p.ID, SourceType: SourceType, URL: url, Title: p.Title, Body: p.Selftext, Comments: cs, Community: community, Score: p.Score, CommentCount: p.NumComments, CreatedAt: postTime(p), CollectedAt: collectedAt, ContentHash: storage.ContentHash(parts...), Metadata: map[string]string{MetaKeyAuthor: p.Author, MetaKeySubreddit: community, MetaKeyPostScore: strconv.Itoa(p.Score), MetaKeyCommentCount: strconv.Itoa(p.NumComments)}}
}

func postTime(p *postResponse) time.Time { return time.Unix(int64(p.CreatedUTC), 0).UTC() }

type commentQueueItem struct {
	c     *postResponse
	depth int
}

func flattenComments(list *listingResponse, limit int) []domain.Comment {
	if list == nil || limit == 0 {
		return nil
	}
	queue := make([]commentQueueItem, 0, len(list.Data.Children))
	for index := range list.Data.Children {
		child := &list.Data.Children[index]
		if child.Kind == "t1" {
			queue = append(queue, commentQueueItem{c: &child.Data, depth: 0})
		}
	}
	result := make([]domain.Comment, 0)
	for len(queue) > 0 && (limit < 0 || len(result) < limit) {
		item := queue[0]
		queue = queue[1:]
		if item.c.ID != "" && !item.c.Removed && item.c.RemovedByCategory == "" && strings.TrimSpace(item.c.Body) != "" && !unavailableText(item.c.Body) {
			result = append(result, domain.Comment{ID: item.c.ID, Body: item.c.Body, Score: item.c.Score, CreatedAt: time.Unix(int64(item.c.CreatedUTC), 0).UTC()})
		}
		if item.depth < maxCommentDepth && item.c.Replies.Listing != nil {
			for index := range item.c.Replies.Listing.Data.Children {
				child := &item.c.Replies.Listing.Data.Children[index]
				if child.Kind == "t1" {
					queue = append(queue, commentQueueItem{c: &child.Data, depth: item.depth + 1})
				}
			}
		}
	}
	return result
}

func sortSignalsNewestFirst(signals []domain.RawSignal) {
	sort.SliceStable(signals, func(i, j int) bool { return signals[i].CreatedAt.After(signals[j].CreatedAt) })
}
