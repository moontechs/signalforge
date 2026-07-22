package github

import (
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/moontechs/signalforge/internal/domain"
	"github.com/moontechs/signalforge/internal/storage"
)

const (
	sourceName            = "github"
	sourceTypeIssue       = "issue"
	sourceTypeDiscussion  = "discussion"
	metadataState         = "github_state"
	metadataLocked        = "github_locked"
	metadataClosed        = "github_closed"
	metadataClosedAt      = "github_closed_at"
	metadataAuthor        = "github_author"
	metadataAuthorType    = "github_author_type"
	metadataCategory      = "github_category"
	metadataNumber        = "github_number"
	metadataNodeID        = "github_node_id"
	metadataUpvoteCount   = "github_upvote_count"
	metadataAnswerComment = "github_answer_comment_id"
)

// ParseOptions controls GitHub item to RawSignal conversion.
type ParseOptions struct {
	CollectedAt    time.Time
	MaxComments    int
	IncludeAnswers bool
}

// ParseIssue converts a GitHub issue and its comments into a RawSignal.
func ParseIssue(item IssueItem, comments []IssueComment, opts ParseOptions) domain.RawSignal {
	repository := normalizeRepositoryName(item.RepositoryName, item.RepositoryURL)
	sourceID := buildIssueSourceID(item)
	normalizedComments := normalizeIssueComments(comments, opts.MaxComments)
	body := normalizeBody(item.Body)
	metadata := compactMetadata(map[string]string{
		metadataState:      normalizeText(item.State),
		metadataLocked:     strconv.FormatBool(item.Locked),
		metadataAuthor:     normalizeText(item.User.Login),
		metadataAuthorType: normalizeText(item.User.Type),
		metadataNumber:     strconv.Itoa(item.Number),
		metadataNodeID:     normalizeText(item.NodeID),
		metadataClosedAt:   formatOptionalTime(item.ClosedAt),
	})
	if item.ClosedAt != nil {
		metadata[metadataClosed] = "true"
	} else {
		metadata[metadataClosed] = "false"
	}

	return domain.RawSignal{
		ID:           buildSignalID(sourceTypeIssue, sourceID),
		Source:       sourceName,
		SourceID:     sourceID,
		SourceType:   sourceTypeIssue,
		URL:          strings.TrimSpace(item.HTMLURL),
		Title:        normalizeText(item.Title),
		Body:         body,
		Comments:     normalizedComments,
		Community:    repository,
		Repository:   repository,
		Tags:         labelNames(item.Labels),
		Labels:       labelNames(item.Labels),
		CommentCount: item.Comments,
		ReactionCnt:  item.Reactions.TotalCount,
		CreatedAt:    item.CreatedAt.UTC(),
		UpdatedAt:    item.UpdatedAt.UTC(),
		CollectedAt:  normalizeCollectedAt(opts.CollectedAt),
		ContentHash:  buildContentHash(sourceTypeIssue, repository, normalizeText(item.Title), body, normalizedComments),
		Metadata:     metadata,
	}
}

// ParseDiscussion converts a GitHub discussion and its comments into a RawSignal.
func ParseDiscussion(item Discussion, comments []DiscussionComment, opts ParseOptions) domain.RawSignal {
	repository := normalizeText(item.Repository.NameWithOwner)
	sourceID := buildDiscussionSourceID(item)
	normalizedComments, answerID := normalizeDiscussionComments(comments, opts.MaxComments)
	body := normalizeBody(item.Body)
	metadata := compactMetadata(map[string]string{
		metadataLocked:      strconv.FormatBool(item.Locked),
		metadataClosed:      strconv.FormatBool(item.Closed),
		metadataAuthor:      normalizeText(item.Author.Login),
		metadataCategory:    normalizeText(item.Category.Name),
		metadataNumber:      strconv.Itoa(item.Number),
		metadataNodeID:      normalizeText(item.ID),
		metadataUpvoteCount: strconv.Itoa(item.UpvoteCount),
	})
	if answerID != "" {
		metadata[metadataAnswerComment] = answerID
	}

	return domain.RawSignal{
		ID:           buildSignalID(sourceTypeDiscussion, sourceID),
		Source:       sourceName,
		SourceID:     sourceID,
		SourceType:   sourceTypeDiscussion,
		URL:          strings.TrimSpace(item.URL),
		Title:        normalizeText(item.Title),
		Body:         body,
		Comments:     normalizedComments,
		Community:    repository,
		Repository:   repository,
		Category:     normalizeText(item.Category.Name),
		Tags:         labelNames(item.Labels.Nodes),
		Labels:       labelNames(item.Labels.Nodes),
		CommentCount: item.Comments.TotalCount,
		ReactionCnt:  item.Reactions.TotalCount,
		AnswerCount:  countDiscussionAnswers(comments),
		CreatedAt:    item.CreatedAt.UTC(),
		UpdatedAt:    item.UpdatedAt.UTC(),
		CollectedAt:  normalizeCollectedAt(opts.CollectedAt),
		ContentHash:  buildContentHash(sourceTypeDiscussion, repository, normalizeText(item.Title), body, normalizedComments),
		Metadata:     metadata,
	}
}

func normalizeIssueComments(comments []IssueComment, maxComments int) []domain.Comment {
	normalized := make([]domain.Comment, 0, len(comments))
	for _, comment := range comments {
		body := normalizeBody(comment.Body)
		if body == "" {
			continue
		}
		normalized = append(normalized, domain.Comment{
			ID:        buildIssueCommentID(comment),
			Body:      body,
			Score:     comment.Reactions.TotalCount,
			CreatedAt: comment.CreatedAt.UTC(),
		})
		if maxComments > 0 && len(normalized) >= maxComments {
			break
		}
	}
	return normalized
}

func normalizeDiscussionComments(comments []DiscussionComment, maxComments int) ([]domain.Comment, string) {
	normalized := make([]domain.Comment, 0, len(comments))
	answerID := ""
	for _, comment := range comments {
		body := normalizeBody(comment.Body)
		if body == "" {
			continue
		}
		commentID := buildDiscussionCommentID(comment)
		if comment.IsAnswer && answerID == "" {
			answerID = commentID
		}
		normalized = append(normalized, domain.Comment{
			ID:        commentID,
			Body:      body,
			Score:     comment.Reactions.TotalCount,
			CreatedAt: comment.CreatedAt.UTC(),
		})
		if maxComments > 0 && len(normalized) >= maxComments {
			break
		}
	}
	return normalized, answerID
}

func buildContentHash(sourceType, repository, title, body string, comments []domain.Comment) string {
	parts := []string{sourceName, sourceType, repository, title, body}
	for _, comment := range comments {
		parts = append(parts, comment.Body)
	}
	return storage.ContentHash(parts...)
}

func buildSignalID(sourceType, sourceID string) string {
	return fmt.Sprintf("%s_%s", sourceType, storage.ContentHash(sourceName, sourceType, sourceID)[:16])
}

func buildIssueSourceID(item IssueItem) string {
	if nodeID := normalizeText(item.NodeID); nodeID != "" {
		return sourceTypeIssue + ":" + nodeID
	}
	if item.ID != 0 {
		return sourceTypeIssue + ":" + strconv.FormatInt(item.ID, 10)
	}
	return sourceTypeIssue + ":" + strconv.Itoa(item.Number)
}

func buildDiscussionSourceID(item Discussion) string {
	if id := normalizeText(item.ID); id != "" {
		return sourceTypeDiscussion + ":" + id
	}
	return sourceTypeDiscussion + ":" + strconv.Itoa(item.Number)
}

func buildIssueCommentID(comment IssueComment) string {
	if nodeID := normalizeText(comment.NodeID); nodeID != "" {
		return nodeID
	}
	if comment.ID != 0 {
		return strconv.FormatInt(comment.ID, 10)
	}
	return normalizeText(comment.HTMLURL)
}

func buildDiscussionCommentID(comment DiscussionComment) string {
	if id := normalizeText(comment.ID); id != "" {
		return id
	}
	return normalizeText(comment.URL)
}

func normalizeRepositoryName(repoName, repoURL string) string {
	if repo := normalizeText(repoName); repo != "" {
		return repo
	}
	urlPath := strings.Trim(strings.TrimSpace(repoURL), "/")
	if urlPath == "" {
		return ""
	}
	base := path.Base(urlPath)
	dir := path.Base(path.Dir(urlPath))
	if dir == "" || dir == "." || base == "" || base == "." {
		return ""
	}
	return dir + "/" + base
}

func labelNames(labels []Label) []string {
	if len(labels) == 0 {
		return nil
	}
	names := make([]string, 0, len(labels))
	seen := make(map[string]struct{}, len(labels))
	for _, label := range labels {
		name := normalizeText(label.Name)
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	if len(names) == 0 {
		return nil
	}
	return names
}

func normalizeBody(body string) string {
	return normalizeText(body)
}

func normalizeText(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	lines := strings.Split(value, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}
	value = strings.Join(lines, "\n")
	value = strings.TrimSpace(value)

	var b strings.Builder
	lastSpace := false
	for _, r := range value {
		if r == '\n' {
			if b.Len() > 0 && b.String()[b.Len()-1] == '\n' {
				continue
			}
			b.WriteRune(r)
			lastSpace = false
			continue
		}
		if r == ' ' || r == '\t' {
			if lastSpace {
				continue
			}
			b.WriteByte(' ')
			lastSpace = true
			continue
		}
		b.WriteRune(r)
		lastSpace = false
	}
	return strings.TrimSpace(b.String())
}

func compactMetadata(metadata map[string]string) map[string]string {
	if len(metadata) == 0 {
		return nil
	}
	compacted := make(map[string]string, len(metadata))
	for key, value := range metadata {
		if strings.TrimSpace(value) == "" {
			continue
		}
		compacted[key] = value
	}
	if len(compacted) == 0 {
		return nil
	}
	return compacted
}

func formatOptionalTime(ts *time.Time) string {
	if ts == nil || ts.IsZero() {
		return ""
	}
	return ts.UTC().Format(time.RFC3339)
}

func normalizeCollectedAt(ts time.Time) time.Time {
	if ts.IsZero() {
		return time.Now().UTC()
	}
	return ts.UTC()
}

func countDiscussionAnswers(comments []DiscussionComment) int {
	count := 0
	for _, comment := range comments {
		if comment.IsAnswer {
			count++
		}
	}
	return count
}
