//nolint:gocritic // pre-existing code; struct copy patterns intentional for this package
package reddit

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/moontechs/signalforge/internal/domain"
	"github.com/moontechs/signalforge/internal/storage"
)

// ---------------------------------------------------------------------------
// Local types for correctly decoding Reddit comment trees
//
// The existing listingChild type uses postData for its Data field, but
// comment tree entries (kind "t1") need commentData to capture body,
// parent_id, and nested replies. These local types mirror the API response
// structure so we can re-decode the comment listing portion accurately.
// ---------------------------------------------------------------------------

// commentTreeListing is used to decode the comment listing portion of a
// Reddit comment tree response (/comments/{id}.json, index 1).
type commentTreeListing struct {
	Kind string              `json:"kind"`
	Data commentTreeListData `json:"data"`
}

type commentTreeListData struct {
	Children []commentTreeEntry `json:"children"`
}

// commentTreeEntry represents a single entry in the top-level comment listing.
// Its Data field uses commentData so body, parent_id, and nested replies are
// properly captured.
type commentTreeEntry struct {
	Kind string      `json:"kind"`
	Data commentData `json:"data"`
}

// ---------------------------------------------------------------------------
// Post parsing
// ---------------------------------------------------------------------------

// parsePost converts a Reddit post (from a subreddit listing child) and its
// flattened comments into a domain.RawSignal. The sort parameter, if non-empty,
// is stored in metadata for downstream consumers.
//
//nolint:gocritic // postData passed by value intentionally
func parsePost(post postData, comments []domain.Comment, sort string, collectedAt time.Time) domain.RawSignal {
	url := "https://www.reddit.com" + post.Permalink
	body := strings.TrimSpace(post.Selftext)

	metadata := map[string]string{
		MetaKeyAuthor:       post.Author,
		MetaKeyPostScore:    strconv.Itoa(post.Score),
		MetaKeyCommentCount: strconv.Itoa(post.NumComments),
		MetaKeySubreddit:    post.Subreddit,
	}
	if sort != "" {
		metadata[MetaKeyListingSort] = sort
	}

	s := domain.RawSignal{
		ID:           fmt.Sprintf("%s:%s", SignalIDPrefix, post.ID),
		Source:       SourceName,
		SourceID:     post.ID,
		SourceType:   SourceType,
		URL:          url,
		Title:        post.Title,
		Body:         body,
		Comments:     comments,
		Community:    post.Subreddit,
		Score:        post.Score,
		CommentCount: post.NumComments,
		CreatedAt:    unixTimestamp(post.CreatedUTC),
		CollectedAt:  collectedAt,
		Metadata:     metadata,
	}

	parts := append([]string{s.Title, s.Body}, commentBodies(comments)...)
	s.ContentHash = storage.ContentHash(parts...)
	return s
}

// commentBodies extracts the Body field from a slice of domain.Comment.
func commentBodies(comments []domain.Comment) []string {
	parts := make([]string, len(comments))
	for i := range comments {
		parts[i] = comments[i].Body
	}
	return parts
}

// ---------------------------------------------------------------------------
// Comment tree flattening
// ---------------------------------------------------------------------------

// FlattenComments extracts comments from a raw Reddit comment tree JSON
// response body (/comments/{id}.json), skipping deleted/removed/empty bodies
// and "more" placeholders. maxComments caps the total returned (0 = unlimited).
// Comments are returned in deterministic BFS order, top-level first, then
// nested replies depth-first per parent.
//
// The raw JSON is required because the existing listingChild type uses postData
// for its Data field, which silently drops comment-specific fields (body,
// parent_id, replies) during unmarshal. This function decodes directly into
// the proper types to preserve all fields.
func FlattenComments(rawJSON []byte, maxComments int) ([]domain.Comment, error) {
	// Parse the two-element array: [postListing, commentListing].
	var response []json.RawMessage
	if err := json.Unmarshal(rawJSON, &response); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMalformedResponse, err)
	}
	if len(response) < 2 {
		return nil, nil
	}

	var listing commentTreeListing
	if err := json.Unmarshal(response[1], &listing); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMalformedResponse, err)
	}

	return flattenEntries(listing.Data.Children, maxComments), nil
}

// flattenEntries processes a slice of top-level comment tree entries and
// recursively collects nested replies into a flat []domain.Comment.
//
// Top-level comments are collected first (in order), then their nested
// replies are flattened depth-first per parent. This produces a
// deterministic BFS-like order: all top-level, then immediate replies,
// then deeper nesting.
func flattenEntries(entries []commentTreeEntry, maxComments int) []domain.Comment {
	// First pass: collect top-level comments (skip "more", deleted, etc.).
	var topLevel []commentTreeEntry
	for _, entry := range entries {
		if entry.Kind == "t1" {
			topLevel = append(topLevel, entry)
		}
	}

	out := make([]domain.Comment, 0, len(topLevel))
	for _, entry := range topLevel {
		if maxComments > 0 && len(out) >= maxComments {
			break
		}
		comment := buildComment(entry.Data)
		if comment == nil {
			continue
		}
		out = append(out, *comment)
	}

	// Second pass: flatten nested replies for all top-level entries.
	for _, entry := range topLevel {
		if maxComments > 0 && len(out) >= maxComments {
			break
		}
		if entry.Data.Replies != nil {
			nested := flattenReplyChildren(entry.Data.Replies.Data.Children, maxComments)
			out = appendNested(out, nested, maxComments)
		}
	}
	return out
}

// flattenReplyChildren processes nested reply children (inside a repliesWrapper)
// and returns a flat []domain.Comment.
func flattenReplyChildren(children []replyChild, maxComments int) []domain.Comment {
	out := make([]domain.Comment, 0, len(children))
	for _, child := range children {
		if maxComments > 0 && len(out) >= maxComments {
			break
		}
		if child.Kind != "t1" {
			continue
		}
		comment := buildComment(child.Data)
		if comment == nil {
			continue
		}
		out = append(out, *comment)

		if child.Data.Replies != nil {
			nested := flattenReplyChildren(child.Data.Replies.Data.Children, maxComments)
			out = appendNested(out, nested, maxComments)
		}
	}
	return out
}

// buildComment constructs a domain.Comment from commentData, returning nil
// if the comment should be skipped (deleted, removed, empty body).
func buildComment(data commentData) *domain.Comment {
	body := strings.TrimSpace(data.Body)
	if body == "" || body == "[deleted]" || body == "[removed]" {
		return nil
	}
	return &domain.Comment{
		ID:        data.ID,
		Body:      body,
		Score:     data.Score,
		CreatedAt: unixTimestamp(data.CreatedUTC),
	}
}

// unixTimestamp converts a Unix timestamp with optional fractional seconds
// (as returned by the Reddit API) to time.Time.
//
// We convert via string formatting to avoid float64 precision issues with
// values like 1700000000.123456 which cannot be exactly represented.
func unixTimestamp(ts float64) time.Time {
	// Format with 9 decimal digits (nanosecond precision), then split.
	s := strconv.FormatFloat(ts, 'f', 9, 64)
	if dot := strings.IndexByte(s, '.'); dot >= 0 {
		sec, _ := strconv.ParseInt(s[:dot], 10, 64)
		// Truncate or pad the fractional part to exactly 9 digits.
		frac := s[dot+1:]
		if len(frac) > 9 {
			frac = frac[:9]
		} else {
			frac += strings.Repeat("0", 9-len(frac))
		}
		nsec, _ := strconv.ParseInt(frac, 10, 64)
		return time.Unix(sec, nsec).UTC()
	}
	return time.Unix(int64(ts), 0).UTC()
}

// appendNested appends nested comments to out, respecting maxComments.
func appendNested(out, nested []domain.Comment, maxComments int) []domain.Comment {
	if maxComments <= 0 {
		return append(out, nested...)
	}
	remaining := maxComments - len(out)
	if remaining <= 0 {
		return out
	}
	if len(nested) > remaining {
		nested = nested[:remaining]
	}
	return append(out, nested...)
}

// eligiblePost checks whether a listing child should be included as a
// top-level signal. It requires kind "t3" and the given since window.
func eligiblePost(child listingChild, since time.Time) bool {
	if child.Kind != "t3" {
		return false
	}
	return since.IsZero() || !unixTimestamp(child.Data.CreatedUTC).Before(since)
}
