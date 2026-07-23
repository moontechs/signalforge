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
func parseQuestionsWithStats(site string, questions []questionDTO, minimumScore, minimumViews int) ([]domain.RawSignal, int) {
	collectedAt := time.Now().UTC()
	out := make([]domain.RawSignal, 0, len(questions))
	skipped := 0
	for _, q := range questions {
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
			Metadata: map[string]string{MetaKeyStoryScore: strconv.Itoa(q.Score), MetaKeyAuthor: q.Owner.DisplayName,
				MetaKeyViewCount: strconv.Itoa(q.ViewCount), MetaKeyTags: strings.Join(tags, ","), MetaKeySiteName: site},
		}
		s.ContentHash = storage.ContentHash(s.Title, s.Body, strings.Join(sortedTags, ","))
		out = append(out, s)
	}
	return out, skipped
}
