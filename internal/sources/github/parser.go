package github

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/moontechs/signalforge/internal/domain"
	"github.com/moontechs/signalforge/internal/storage"
)

const (
	// Source identifiers for normalization.
	sourceIDIssue      = "github_issue"
	sourceIDDiscussion = "github_discussion"
	sourceName         = "github"
)

// ---- Mapping functions ----.

// parseIssueToSignal converts a GitHub issue into a domain.RawSignal.
// It expects the issue's comments to be pre-fetched and passed separately.
func parseIssueToSignal(issue *ghIssue, owner, repo string, comments []ghIssueComment, maxComments int, collectedAt time.Time) domain.RawSignal {
	labelNames := extractLabelNames(issue.Labels)
	repoFull := owner + "/" + repo

	signal := domain.RawSignal{
		ID:           issueSourceID(issue.ID),
		Source:       sourceName,
		SourceID:     strconv.FormatInt(issue.ID, 10),
		SourceType:   sourceIDIssue,
		URL:          issue.HTMLURL,
		Title:        issue.Title,
		Body:         issue.Body,
		Community:    "github",
		Repository:   repoFull,
		Labels:       labelNames,
		Tags:         labelNames,
		CommentCount: issue.Comments,
		ReactionCnt:  issue.Reactions.Total(),
		CreatedAt:    issue.CreatedAt,
		UpdatedAt:    issue.UpdatedAt,
		CollectedAt:  collectedAt,
		Score:        int(issue.Reactions.Total()),
	}

	// Map and cap comments.
	signal.Comments = mapIssueComments(comments, maxComments)

	// Generate content hash from title, body, and comment bodies.
	signal.ContentHash = generateContentHash(signal.Title, signal.Body, signal.Comments)

	return signal
}

// parseDiscussionToSignal converts a GitHub discussion node into a domain.RawSignal.
func parseDiscussionToSignal(disc *graphQLDiscussionNode, owner, repo string, maxComments int, collectedAt time.Time) domain.RawSignal {
	labelNames := getDiscussionLabelNames(disc)
	catName := getDiscussionCategory(disc)
	repoFull := owner + "/" + repo

	commentCount := 0
	hasComments := disc.Comments != nil
	if hasComments {
		commentCount = disc.Comments.TotalCount
	}

	signal := domain.RawSignal{
		ID:           discussionSourceID(disc.ID),
		Source:       sourceName,
		SourceID:     disc.ID,
		SourceType:   sourceIDDiscussion,
		URL:          disc.URL,
		Title:        disc.Title,
		Body:         disc.Body,
		Community:    "github",
		Repository:   repoFull,
		Category:     catName,
		Labels:       labelNames,
		Tags:         appendTags(labelNames, catName),
		CommentCount: commentCount,
		ReactionCnt:  disc.UpvoteCount,
		CreatedAt:    disc.CreatedAt,
		UpdatedAt:    disc.UpdatedAt,
		CollectedAt:  collectedAt,
		Score:        disc.UpvoteCount,
	}

	// Map and cap comments.
	if hasComments {
		signal.Comments = mapDiscussionComments(disc.Comments.Nodes, maxComments)
	}

	// Generate content hash.
	signal.ContentHash = generateContentHash(signal.Title, signal.Body, signal.Comments)

	return signal
}

// ---- Source ID normalization ----.

// issueSourceID generates a normalized unique ID for a GitHub issue signal.
func issueSourceID(issueID int64) string {
	return fmt.Sprintf("github_issue:%d", issueID)
}

// discussionSourceID generates a normalized unique ID for a GitHub discussion signal.
func discussionSourceID(discID string) string {
	return "github_discussion:" + discID
}

// ---- Content hash generation ----.

// generateContentHash creates a deterministic SHA-256 hash from the signal's
// title, body, and comment bodies. This is used for deduplication.
func generateContentHash(title, body string, comments []domain.Comment) string {
	parts := []string{title, body}
	for _, c := range comments {
		parts = append(parts, c.Body)
	}
	return storage.ContentHash(parts...)
}

// ---- Label helpers ----.

// extractLabelNames extracts label name strings from a slice of ghLabel.
func extractLabelNames(labels []ghLabel) []string {
	if len(labels) == 0 {
		return nil
	}
	names := make([]string, len(labels))
	for i, l := range labels {
		names[i] = l.Name
	}
	return names
}

// ---- Comment mapping ----.

// mapIssueComments converts ghIssueComment slice to domain.Comment slice,
// capped at maxComments and sorted by CreatedAt ascending.
func mapIssueComments(comments []ghIssueComment, maxComments int) []domain.Comment {
	if len(comments) == 0 {
		return nil
	}

	// Sort by CreatedAt ascending for deterministic ordering.
	sorted := make([]ghIssueComment, len(comments))
	copy(sorted, comments)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].CreatedAt.Before(sorted[j].CreatedAt)
	})

	if maxComments > 0 && len(sorted) > maxComments {
		sorted = sorted[:maxComments]
	}

	result := make([]domain.Comment, len(sorted))
	for i, c := range sorted {
		result[i] = domain.Comment{
			ID:        strconv.FormatInt(c.ID, 10),
			Body:      c.Body,
			CreatedAt: c.CreatedAt,
		}
	}

	return result
}

// mapDiscussionComments converts graphQLDiscussionComment slice to domain.Comment slice,
// capped at maxComments and sorted by CreatedAt ascending.
func mapDiscussionComments(comments []graphQLDiscussionComment, maxComments int) []domain.Comment {
	if len(comments) == 0 {
		return nil
	}

	// Sort by CreatedAt ascending for deterministic ordering.
	sorted := make([]graphQLDiscussionComment, len(comments))
	copy(sorted, comments)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].CreatedAt.Before(sorted[j].CreatedAt)
	})

	if maxComments > 0 && len(sorted) > maxComments {
		sorted = sorted[:maxComments]
	}

	result := make([]domain.Comment, len(sorted))
	for i, c := range sorted {
		result[i] = domain.Comment{
			ID:        c.ID,
			Body:      c.Body,
			CreatedAt: c.CreatedAt,
		}
	}

	return result
}

// ---- Helpers ----.

// extractOwnerRepo extracts owner and repo name from a GitHub repository API URL.
// URL format: https://api.github.com/repos/owner/repo
func extractOwnerRepo(repoURL string) (string, string) {
	if repoURL == "" {
		return "", ""
	}
	// Trim the base URL prefix.
	trimmed := strings.TrimPrefix(repoURL, "https://api.github.com/repos/")
	trimmed = strings.TrimPrefix(trimmed, "/repos/")
	parts := strings.SplitN(trimmed, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", ""
	}
	return parts[0], parts[1]
}

// extractOwnerRepoFromHTML extracts owner and repo name from a GitHub HTML URL.
// Handles formats like:
//   - https://github.com/owner/repo/issues/1
//   - https://github.com/owner/repo/discussions/1
func extractOwnerRepoFromHTML(url string) (string, string) {
	if url == "" {
		return "", ""
	}
	// Trim the https://github.com/ prefix.
	trimmed := strings.TrimPrefix(url, "https://github.com/")
	trimmed = strings.TrimPrefix(trimmed, "http://github.com/")
	// Split by / to get [owner, repo, ...].
	parts := strings.SplitN(trimmed, "/", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", ""
	}
	return parts[0], parts[1]
}

// AppendTags appends category to labels only if category is non-empty.
func appendTags(labels []string, category string) []string {
	if category != "" {
		return append(labels, category)
	}
	return labels
}
