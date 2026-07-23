package stackexchange

import (
	"fmt"
	"html"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/moontechs/signalforge/internal/domain"
	"github.com/moontechs/signalforge/internal/storage"
)

var (
	codeBlockRE = regexp.MustCompile(`(?is)<code\b[^>]*>.*?</code\s*>`)
	tagRE       = regexp.MustCompile(`(?s)<[^>]+>`)
	spaceRE     = regexp.MustCompile(`[ \t\r\n]+`)
)

// parseQuestion converts a single question with its answers and comments into
// a domain.RawSignal. It merges answers and comments, sorts them
// chronologically, and populates all metadata fields.
func parseQuestion(item *questionDTO, answers []answerDTO, comments []commentDTO, site string, collectedAt time.Time) domain.RawSignal {
	body := cleanHTML(item.BodyMarkdown)
	title := cleanHTML(item.Title)
	tags := append([]string(nil), item.Tags...)

	// Merge and sort answers + comments.
	merged := mergeAndSortComments(answers, comments)

	// Build metadata.
	meta := map[string]string{
		MetaKeyAuthor:      item.Owner.DisplayName,
		MetaKeyStoryScore:  strconv.Itoa(item.Score),
		MetaKeyViewCount:   strconv.Itoa(item.ViewCount),
		MetaKeyAnswerCount: strconv.Itoa(item.AnswerCount),
		MetaKeyTags:        strings.Join(tags, ","),
		MetaKeySiteName:    site,
		MetaKeyIsAnswered:  strconv.FormatBool(item.IsAnswered),
	}
	if item.AcceptedAnswerID != nil {
		meta[MetaKeyAcceptedAnswer] = strconv.Itoa(*item.AcceptedAnswerID)
	}

	// Content hash from title, body, and all comment bodies.
	contentParts := []string{title, body}
	for _, c := range merged {
		contentParts = append(contentParts, c.Body)
	}

	s := domain.RawSignal{
		ID:          fmt.Sprintf("%s:%d", SignalIDPrefix, item.QuestionID),
		Source:      SourceName,
		SourceID:    strconv.Itoa(item.QuestionID),
		SourceType:  SourceType,
		URL:         item.Link,
		Title:       title,
		Body:        body,
		Comments:    merged,
		Community:   site,
		Tags:        tags,
		Score:       item.Score,
		ViewCount:   item.ViewCount,
		AnswerCount: item.AnswerCount,
		CreatedAt:   time.Unix(item.CreationDate, 0).UTC(),
		UpdatedAt:   time.Unix(item.LastActivityDate, 0).UTC(),
		CollectedAt: collectedAt,
		ContentHash: storage.ContentHash(contentParts...),
		Metadata:    meta,
	}
	return s
}

// parseAnswer converts an answer DTO into a domain.Comment with a
// "se_answer:{id}" ID.
func parseAnswer(item *answerDTO) domain.Comment {
	return domain.Comment{
		ID:        fmt.Sprintf("se_answer:%d", item.AnswerID),
		Body:      cleanHTML(item.BodyMarkdown),
		Score:     item.Score,
		CreatedAt: time.Unix(item.CreationDate, 0).UTC(),
	}
}

// parseComment converts a comment DTO into a domain.Comment with a
// "se_comment:{id}" ID.
func parseComment(item *commentDTO) domain.Comment {
	return domain.Comment{
		ID:        fmt.Sprintf("se_comment:%d", item.CommentID),
		Body:      cleanHTML(item.BodyMarkdown),
		Score:     item.Score,
		CreatedAt: time.Unix(item.CreationDate, 0).UTC(),
	}
}

// eligibleQuestion checks whether a question meets the scope's eligibility
// criteria: minimum score, minimum views, and the since window.
func eligibleQuestion(item *questionDTO, scope QuestionScope) bool {
	if scope.MinimumScore > 0 && item.Score < scope.MinimumScore {
		return false
	}
	if scope.MinimumViews > 0 && item.ViewCount < scope.MinimumViews {
		return false
	}
	if !scope.Since.IsZero() {
		created := time.Unix(item.CreationDate, 0).UTC()
		if created.Before(scope.Since) {
			return false
		}
	}
	return true
}

// mergeAndSortComments merges answer and comment slices into a single
// chronological list (oldest first). Answers are ordered by creation date,
// then comments, and the combined result is sorted by CreatedAt ascending.
func mergeAndSortComments(answers []answerDTO, comments []commentDTO) []domain.Comment {
	out := make([]domain.Comment, 0, len(answers)+len(comments))
	for i := range answers {
		out = append(out, parseAnswer(&answers[i]))
	}
	for i := range comments {
		out = append(out, parseComment(&comments[i]))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

// cleanHTML turns the API's HTML body into readable plain text. Code blocks
// are omitted because they tend to dominate problem text without adding much
// signal for discovery.
func cleanHTML(value string) string {
	value = codeBlockRE.ReplaceAllString(value, "")
	value = regexp.MustCompile(`(?i)</p\s*>`).ReplaceAllString(value, "\n\n")
	value = regexp.MustCompile(`(?i)<br\s*/?>`).ReplaceAllString(value, "\n")
	value = tagRE.ReplaceAllString(value, "")
	value = html.UnescapeString(value)
	lines := strings.Split(value, "\n")
	for i := range lines {
		lines[i] = strings.TrimSpace(spaceRE.ReplaceAllString(lines[i], " "))
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// parseQuestions parses questions and skips records with no usable body.
func parseQuestions(site string, questions []questionDTO) []domain.RawSignal {
	signals, _ := parseQuestionsWithStats(site, questions, 0, 0)
	return signals
}

// parseQuestionsWithStats additionally applies score/view thresholds and
// returns the number of skipped questions.
func parseQuestionsWithStats(site string, questions []questionDTO, minimumScore, minimumViews int) (signals []domain.RawSignal, skipped int) {
	collectedAt := time.Now().UTC()
	out := make([]domain.RawSignal, 0, len(questions))
	for i := range questions {
		q := &questions[i]
		body := cleanHTML(q.BodyMarkdown)
		if body == "" || q.Score < minimumScore || q.ViewCount < minimumViews {
			skipped++
			continue
		}
		tags := append([]string(nil), q.Tags...)
		sortedTags := append([]string(nil), tags...)
		sort.Strings(sortedTags)
		s := domain.RawSignal{
			ID: fmt.Sprintf("%s:%s:%d", SignalIDPrefix, site, q.QuestionID), Source: SourceName,
			SourceID: fmt.Sprintf("%s:%d", site, q.QuestionID), SourceType: SourceType, URL: q.Link,
			Title: cleanHTML(q.Title), Body: body, Community: site, Tags: tags, Score: q.Score,
			ViewCount: q.ViewCount, AnswerCount: q.AnswerCount, CreatedAt: time.Unix(q.CreationDate, 0).UTC(),
			UpdatedAt: time.Unix(q.LastActivityDate, 0).UTC(), CollectedAt: collectedAt,
			Metadata: map[string]string{
				MetaKeyStoryScore: strconv.Itoa(q.Score), MetaKeyAuthor: q.Owner.DisplayName,
				MetaKeyViewCount: strconv.Itoa(q.ViewCount), MetaKeyTags: strings.Join(tags, ","), MetaKeySiteName: site,
			},
		}
		s.ContentHash = storage.ContentHash(s.Title, s.Body, strings.Join(sortedTags, ","))
		out = append(out, s)
	}
	return out, skipped
}
