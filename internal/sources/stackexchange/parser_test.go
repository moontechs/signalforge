package stackexchange

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

// loadFixture loads a JSON file from testdata/stackexchange/ into the target.
func loadFixture(t *testing.T, name string, target any) {
	t.Helper()
	data, err := os.ReadFile("../../../testdata/stackexchange/" + name)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatalf("unmarshal fixture %s: %v", name, err)
	}
}

// ---------------------------------------------------------------------------
// Existing tests (kept for backward compatibility)
// ---------------------------------------------------------------------------

func TestParseQuestionsNormalizesHTMLAndMetadata(t *testing.T) {
	t.Parallel()
	q := questionDTO{QuestionID: 12345, Title: "How &amp; why?", BodyMarkdown: "<p>I need &amp; help.</p><pre><code>secret()</code></pre><p>Next<br>line</p>", Link: "https://stackoverflow.com/q/12345", Score: 7, ViewCount: 42, AnswerCount: 2, CreationDate: 100, LastActivityDate: 200, Tags: []string{"go", "api"}, Owner: ownerDTO{DisplayName: "Ada"}}
	out := parseQuestions("stackoverflow", []questionDTO{q})
	if len(out) != 1 || out[0].ID != "se:stackoverflow:12345" || out[0].Body != "I need & help.\n\nNext\nline" {
		t.Fatalf("unexpected signal: %+v", out)
	}
	if out[0].Metadata[MetaKeyAuthor] != "Ada" || out[0].Metadata[MetaKeyViewCount] != "42" || out[0].Metadata[MetaKeyTags] != "go,api" {
		t.Fatalf("unexpected metadata: %#v", out[0].Metadata)
	}
}

func TestParseQuestionsHashIsDeterministicAndSkips(t *testing.T) {
	t.Parallel()
	q := questionDTO{QuestionID: 1, Title: "x", BodyMarkdown: "<p>body</p>", Tags: []string{"z", "a"}}
	a := parseQuestions("x", []questionDTO{q})[0]
	b := parseQuestions("x", []questionDTO{q})[0]
	if a.ContentHash != b.ContentHash {
		t.Fatal("content hash is not deterministic")
	}
	if got, skipped := parseQuestionsWithStats("x", []questionDTO{q, {QuestionID: 2, BodyMarkdown: "<p> </p>"}}, 0, 0); len(got) != 1 || skipped != 1 {
		t.Fatalf("got %d signals, skipped %d", len(got), skipped)
	}
	if got, skipped := parseQuestionsWithStats("x", []questionDTO{q}, 1, 0); len(got) != 0 || skipped != 1 {
		t.Fatalf("threshold filtering failed: %d/%d", len(got), skipped)
	}
}

// ---------------------------------------------------------------------------
// TestParseQuestion — all fields from a full fixture
// ---------------------------------------------------------------------------

func TestParseQuestion_allFields(t *testing.T) {
	t.Parallel()
	var questions []questionDTO
	loadFixture(t, "questions.json", &questions)

	// Find question 1001 (has full data: tags, owner, accepted answer, etc.)
	var target questionDTO
	for _, q := range questions {
		if q.QuestionID == 1001 {
			target = q
			break
		}
	}
	if target.QuestionID == 0 {
		t.Fatal("question 1001 not found in fixtures")
	}

	var answers []answerDTO
	loadFixture(t, "answers.json", &answers)

	var comments []commentDTO
	loadFixture(t, "comments.json", &comments)

	// Filter answers/comments for question 1001.
	var qAnswers []answerDTO
	for _, a := range answers {
		if a.QuestionID == 1001 {
			qAnswers = append(qAnswers, a)
		}
	}
	var qComments []commentDTO
	for _, c := range comments {
		if c.PostID == 1001 {
			qComments = append(qComments, c)
		}
	}

	collectedAt := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	signal := parseQuestion(&target, qAnswers, qComments, "stackoverflow", collectedAt)

	// Basic fields.
	if signal.ID != "se:1001" {
		t.Fatalf("ID = %q, want se:1001", signal.ID)
	}
	if signal.Source != "stackexchange" {
		t.Fatalf("Source = %q, want stackexchange", signal.Source)
	}
	if signal.SourceID != "1001" {
		t.Fatalf("SourceID = %q, want 1001", signal.SourceID)
	}
	if signal.SourceType != "discussion" {
		t.Fatalf("SourceType = %q, want discussion", signal.SourceType)
	}
	if signal.URL != "https://stackoverflow.com/questions/1001/go-error-handling" {
		t.Fatalf("URL = %q, want expected", signal.URL)
	}
	if signal.Title != "How to handle errors in Go gracefully?" {
		t.Fatalf("Title = %q, want expected", signal.Title)
	}
	if signal.Community != "stackoverflow" {
		t.Fatalf("Community = %q, want stackoverflow", signal.Community)
	}
	if signal.Score != 42 {
		t.Fatalf("Score = %d, want 42", signal.Score)
	}
	if signal.ViewCount != 15000 {
		t.Fatalf("ViewCount = %d, want 15000", signal.ViewCount)
	}
	if signal.AnswerCount != 3 {
		t.Fatalf("AnswerCount = %d, want 3", signal.AnswerCount)
	}

	// Tags.
	if len(signal.Tags) != 3 || signal.Tags[0] != "go" {
		t.Fatalf("Tags = %v, want [go error-handling best-practices]", signal.Tags)
	}

	// Timestamps.
	expectedCreated := time.Unix(1700000000, 0).UTC()
	if !signal.CreatedAt.Equal(expectedCreated) {
		t.Fatalf("CreatedAt = %v, want %v", signal.CreatedAt, expectedCreated)
	}
	expectedUpdated := time.Unix(1700100000, 0).UTC()
	if !signal.UpdatedAt.Equal(expectedUpdated) {
		t.Fatalf("UpdatedAt = %v, want %v", signal.UpdatedAt, expectedUpdated)
	}
	if !signal.CollectedAt.Equal(collectedAt) {
		t.Fatalf("CollectedAt = %v, want %v", signal.CollectedAt, collectedAt)
	}

	// Body should have code blocks removed and HTML cleaned.
	if signal.Body == "" {
		t.Fatal("Body is empty")
	}

	// Metadata.
	if signal.Metadata[MetaKeyAuthor] != "gopher123" {
		t.Fatalf("MetaKeyAuthor = %q, want gopher123", signal.Metadata[MetaKeyAuthor])
	}
	if signal.Metadata[MetaKeyStoryScore] != "42" {
		t.Fatalf("MetaKeyStoryScore = %q, want 42", signal.Metadata[MetaKeyStoryScore])
	}
	if signal.Metadata[MetaKeyViewCount] != "15000" {
		t.Fatalf("MetaKeyViewCount = %q, want 15000", signal.Metadata[MetaKeyViewCount])
	}
	if signal.Metadata[MetaKeyAnswerCount] != "3" {
		t.Fatalf("MetaKeyAnswerCount = %q, want 3", signal.Metadata[MetaKeyAnswerCount])
	}
	if signal.Metadata[MetaKeyTags] != "go,error-handling,best-practices" {
		t.Fatalf("MetaKeyTags = %q, want go,error-handling,best-practices", signal.Metadata[MetaKeyTags])
	}
	if signal.Metadata[MetaKeySiteName] != "stackoverflow" {
		t.Fatalf("MetaKeySiteName = %q, want stackoverflow", signal.Metadata[MetaKeySiteName])
	}
	if signal.Metadata[MetaKeyIsAnswered] != "true" {
		t.Fatalf("MetaKeyIsAnswered = %q, want true", signal.Metadata[MetaKeyIsAnswered])
	}
	if signal.Metadata[MetaKeyAcceptedAnswer] != "2001" {
		t.Fatalf("MetaKeyAcceptedAnswer = %q, want 2001", signal.Metadata[MetaKeyAcceptedAnswer])
	}

	// Comments — should have 2 answers + 2 comments = 4 total, sorted chronologically.
	if len(signal.Comments) != 4 {
		t.Fatalf("len(Comments) = %d, want 4", len(signal.Comments))
	}
	// Verify chronological order.
	for i := 1; i < len(signal.Comments); i++ {
		if signal.Comments[i].CreatedAt.Before(signal.Comments[i-1].CreatedAt) {
			t.Fatalf("comments not sorted: idx %d (%v) before idx %d (%v)", i-1, signal.Comments[i-1].CreatedAt, i, signal.Comments[i].CreatedAt)
		}
	}

	// Check content hash is non-empty.
	if signal.ContentHash == "" {
		t.Fatal("ContentHash is empty")
	}
}

// ---------------------------------------------------------------------------
// TestParseQuestion_noOwner — question without owner data
// ---------------------------------------------------------------------------

func TestParseQuestion_noOwner(t *testing.T) {
	t.Parallel()
	q := questionDTO{
		QuestionID:       2001,
		Title:            "Title",
		BodyMarkdown:     "<p>Body</p>",
		Link:             "https://example.com/q/2001",
		CreationDate:     100,
		LastActivityDate: 200,
		Score:            5,
		ViewCount:        50,
		AnswerCount:      0,
	}
	// Empty owner is fine; display name will be empty string.
	signal := parseQuestion(&q, nil, nil, "serverfault", time.Now())
	if signal.Metadata[MetaKeyAuthor] != "" {
		t.Fatalf("MetaKeyAuthor = %q, want empty", signal.Metadata[MetaKeyAuthor])
	}
	if signal.ContentHash == "" {
		t.Fatal("ContentHash is empty")
	}
}

// ---------------------------------------------------------------------------
// TestParseQuestion_noTags
// ---------------------------------------------------------------------------

func TestParseQuestion_noTags(t *testing.T) {
	t.Parallel()
	var questions []questionDTO
	loadFixture(t, "questions.json", &questions)

	var target questionDTO
	for _, q := range questions {
		if q.QuestionID == 1007 {
			target = q
			break
		}
	}
	if target.QuestionID == 0 {
		t.Fatal("question 1007 not found")
	}

	signal := parseQuestion(&target, nil, nil, "stackoverflow", time.Now())
	if len(signal.Tags) != 0 {
		t.Fatalf("Tags = %v, want empty", signal.Tags)
	}
	if signal.Metadata[MetaKeyTags] != "" {
		t.Fatalf("MetaKeyTags = %q, want empty", signal.Metadata[MetaKeyTags])
	}
	if signal.ContentHash == "" {
		t.Fatal("ContentHash is empty")
	}
}

// ---------------------------------------------------------------------------
// TestParseQuestion_noAcceptedAnswer
// ---------------------------------------------------------------------------

func TestParseQuestion_noAcceptedAnswer(t *testing.T) {
	t.Parallel()
	q := questionDTO{
		QuestionID:       3001,
		Title:            "Question without accepted answer",
		BodyMarkdown:     "<p>Body text</p>",
		Link:             "https://example.com/q/3001",
		CreationDate:     100,
		LastActivityDate: 200,
		Score:            10,
		ViewCount:        100,
		AnswerCount:      2,
		IsAnswered:       false,
	}
	signal := parseQuestion(&q, nil, nil, "superuser", time.Now())
	if _, ok := signal.Metadata[MetaKeyAcceptedAnswer]; ok {
		t.Fatal("MetaKeyAcceptedAnswer should not be set")
	}
	if signal.Metadata[MetaKeyIsAnswered] != "false" {
		t.Fatalf("MetaKeyIsAnswered = %q, want false", signal.Metadata[MetaKeyIsAnswered])
	}
}

// ---------------------------------------------------------------------------
// TestParseQuestion_notAnswered
// ---------------------------------------------------------------------------

func TestParseQuestion_notAnswered(t *testing.T) {
	t.Parallel()
	q := questionDTO{
		QuestionID:       4001,
		Title:            "Unanswered question",
		BodyMarkdown:     "<p>Unanswered body</p>",
		Link:             "https://example.com/q/4001",
		CreationDate:     100,
		LastActivityDate: 200,
		Score:            3,
		ViewCount:        30,
		AnswerCount:      0,
		IsAnswered:       false,
	}
	signal := parseQuestion(&q, nil, nil, "stackoverflow", time.Now())
	if signal.AnswerCount != 0 {
		t.Fatalf("AnswerCount = %d, want 0", signal.AnswerCount)
	}
	if len(signal.Comments) != 0 {
		t.Fatalf("len(Comments) = %d, want 0", len(signal.Comments))
	}
}

// ---------------------------------------------------------------------------
// TestParseAnswer
// ---------------------------------------------------------------------------

func TestParseAnswer(t *testing.T) {
	t.Parallel()
	a := answerDTO{
		AnswerID:     5001,
		QuestionID:   1001,
		BodyMarkdown: "<p>This is an <strong>answer</strong>.</p>",
		CreationDate: 1000,
		Score:        42,
		IsAccepted:   true,
	}
	comment := parseAnswer(&a)
	if comment.ID != "se_answer:5001" {
		t.Fatalf("ID = %q, want se_answer:5001", comment.ID)
	}
	if comment.Body != "This is an answer." {
		t.Fatalf("Body = %q, want 'This is an answer.'", comment.Body)
	}
	if comment.Score != 42 {
		t.Fatalf("Score = %d, want 42", comment.Score)
	}
	expectedTime := time.Unix(1000, 0).UTC()
	if !comment.CreatedAt.Equal(expectedTime) {
		t.Fatalf("CreatedAt = %v, want %v", comment.CreatedAt, expectedTime)
	}
}

// ---------------------------------------------------------------------------
// TestParseComment
// ---------------------------------------------------------------------------

func TestParseComment(t *testing.T) {
	t.Parallel()
	c := commentDTO{
		CommentID:    6001,
		PostID:       1001,
		BodyMarkdown: "<p>A helpful <em>comment</em>.</p>",
		CreationDate: 2000,
		Score:        7,
	}
	comment := parseComment(&c)
	if comment.ID != "se_comment:6001" {
		t.Fatalf("ID = %q, want se_comment:6001", comment.ID)
	}
	if comment.Body != "A helpful comment." {
		t.Fatalf("Body = %q, want 'A helpful comment.'", comment.Body)
	}
	if comment.Score != 7 {
		t.Fatalf("Score = %d, want 7", comment.Score)
	}
	expectedTime := time.Unix(2000, 0).UTC()
	if !comment.CreatedAt.Equal(expectedTime) {
		t.Fatalf("CreatedAt = %v, want %v", comment.CreatedAt, expectedTime)
	}
}

// ---------------------------------------------------------------------------
// TestEligibleQuestion — table-driven
// ---------------------------------------------------------------------------

func TestEligibleQuestion(t *testing.T) {
	t.Parallel()
	sinceTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)  // unix ~1735689600
	beforeTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC) // unix ~1704067200
	afterTime := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)  // unix ~1748736000
	tests := []struct {
		name  string
		item  questionDTO
		scope QuestionScope
		want  bool
	}{
		{
			name:  "meets all criteria",
			item:  questionDTO{QuestionID: 1, Score: 10, ViewCount: 100, CreationDate: 1740000000}, // after sinceTime
			scope: QuestionScope{MinimumScore: 5, MinimumViews: 50, Since: beforeTime},
			want:  true,
		},
		{
			name:  "score too low",
			item:  questionDTO{QuestionID: 2, Score: 3, ViewCount: 100, CreationDate: 1740000000},
			scope: QuestionScope{MinimumScore: 5, MinimumViews: 50, Since: beforeTime},
			want:  false,
		},
		{
			name:  "views too low",
			item:  questionDTO{QuestionID: 3, Score: 10, ViewCount: 10, CreationDate: 1740000000},
			scope: QuestionScope{MinimumScore: 5, MinimumViews: 50, Since: beforeTime},
			want:  false,
		},
		{
			name:  "before since window",
			item:  questionDTO{QuestionID: 4, Score: 10, ViewCount: 100, CreationDate: 1700000000}, // before sinceTime
			scope: QuestionScope{MinimumScore: 5, MinimumViews: 50, Since: sinceTime},
			want:  false,
		},
		{
			name:  "empty scope (no filters)",
			item:  questionDTO{QuestionID: 5, Score: 1, ViewCount: 1, CreationDate: 1700000000},
			scope: QuestionScope{},
			want:  true,
		},
		{
			name:  "zero score/views requirements",
			item:  questionDTO{QuestionID: 6, Score: 0, ViewCount: 0, CreationDate: 1700000000},
			scope: QuestionScope{MinimumScore: 0, MinimumViews: 0},
			want:  true,
		},
		{
			name:  "exactly meets score threshold",
			item:  questionDTO{QuestionID: 7, Score: 5, ViewCount: 50, CreationDate: 1740000000},
			scope: QuestionScope{MinimumScore: 5, MinimumViews: 50},
			want:  true,
		},
		{
			name:  "same time as since window",
			item:  questionDTO{QuestionID: 8, Score: 10, ViewCount: 100, CreationDate: 1740000000},
			scope: QuestionScope{MinimumScore: 5, MinimumViews: 50, Since: time.Unix(1740000000, 0).UTC()},
			want:  true, // same timestamp is not before, so eligible
		},
		{
			name:  "after since window",
			item:  questionDTO{QuestionID: 9, Score: 10, ViewCount: 100, CreationDate: 1748736001}, // after afterTime
			scope: QuestionScope{MinimumScore: 5, MinimumViews: 50, Since: afterTime},
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := eligibleQuestion(&tt.item, tt.scope)
			if got != tt.want {
				t.Fatalf("eligibleQuestion(%+v, %+v) = %v, want %v", tt.item, tt.scope, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestMergeAndSortComments
// ---------------------------------------------------------------------------

func TestMergeAndSortComments(t *testing.T) {
	t.Parallel()
	answers := []answerDTO{
		{AnswerID: 1, BodyMarkdown: "<p>First answer</p>", CreationDate: 300},
		{AnswerID: 2, BodyMarkdown: "<p>Second answer</p>", CreationDate: 100},
	}
	comments := []commentDTO{
		{CommentID: 3, BodyMarkdown: "<p>A comment</p>", CreationDate: 200},
	}

	result := mergeAndSortComments(answers, comments)
	if len(result) != 3 {
		t.Fatalf("len = %d, want 3", len(result))
	}

	// Chronological order: second answer (100), comment (200), first answer (300).
	if result[0].ID != "se_answer:2" {
		t.Fatalf("result[0].ID = %q, want se_answer:2", result[0].ID)
	}
	if result[1].ID != "se_comment:3" {
		t.Fatalf("result[1].ID = %q, want se_comment:3", result[1].ID)
	}
	if result[2].ID != "se_answer:1" {
		t.Fatalf("result[2].ID = %q, want se_answer:1", result[2].ID)
	}
}

func TestMergeAndSortComments_empty(t *testing.T) {
	t.Parallel()
	result := mergeAndSortComments(nil, nil)
	if len(result) != 0 {
		t.Fatalf("len = %d, want 0", len(result))
	}
}

func TestMergeAndSortComments_onlyAnswers(t *testing.T) {
	t.Parallel()
	answers := []answerDTO{
		{AnswerID: 1, BodyMarkdown: "<p>A</p>", CreationDate: 200},
		{AnswerID: 2, BodyMarkdown: "<p>B</p>", CreationDate: 100},
	}
	result := mergeAndSortComments(answers, nil)
	if len(result) != 2 {
		t.Fatalf("len = %d, want 2", len(result))
	}
	if result[0].ID != "se_answer:2" {
		t.Fatalf("result[0].ID = %q, want se_answer:2", result[0].ID)
	}
}

func TestMergeAndSortComments_onlyComments(t *testing.T) {
	t.Parallel()
	comments := []commentDTO{
		{CommentID: 3, BodyMarkdown: "<p>C</p>", CreationDate: 50},
	}
	result := mergeAndSortComments(nil, comments)
	if len(result) != 1 {
		t.Fatalf("len = %d, want 1", len(result))
	}
	if result[0].ID != "se_comment:3" {
		t.Fatalf("result[0].ID = %q, want se_comment:3", result[0].ID)
	}
}

// ---------------------------------------------------------------------------
// TestContentHashDeterminism
// ---------------------------------------------------------------------------

func TestContentHashDeterminism(t *testing.T) {
	t.Parallel()
	q := questionDTO{
		QuestionID:   100,
		Title:        "Test title",
		BodyMarkdown: "<p>Test body</p>",
		CreationDate: 1000,
		Score:        5,
		ViewCount:    50,
		Tags:         []string{"go", "test"},
		Owner:        ownerDTO{DisplayName: "tester"},
	}
	answers := []answerDTO{
		{AnswerID: 1, BodyMarkdown: "<p>Answer 1</p>", CreationDate: 1100},
	}
	comments := []commentDTO{
		{CommentID: 2, BodyMarkdown: "<p>Comment 1</p>", CreationDate: 1050},
	}

	s1 := parseQuestion(&q, answers, comments, "stackoverflow", time.Now())
	s2 := parseQuestion(&q, answers, comments, "stackoverflow", time.Now())

	if s1.ContentHash != s2.ContentHash {
		t.Fatal("content hash is not deterministic")
	}
	if s1.ContentHash == "" {
		t.Fatal("content hash is empty")
	}
}

func TestContentHashDiffersWithDifferentComments(t *testing.T) {
	t.Parallel()
	q := questionDTO{
		QuestionID:   101,
		Title:        "Same title",
		BodyMarkdown: "<p>Same body</p>",
		CreationDate: 1000,
		Score:        5,
		ViewCount:    50,
		Owner:        ownerDTO{DisplayName: "tester"},
	}

	// Different comment content should produce different hashes.
	s1 := parseQuestion(&q, nil, nil, "stackoverflow", time.Now())
	s2 := parseQuestion(&q, nil, []commentDTO{{CommentID: 1, BodyMarkdown: "<p>Extra comment</p>", CreationDate: 1100}}, "stackoverflow", time.Now())

	if s1.ContentHash == s2.ContentHash {
		t.Fatal("content hashes should differ with different comment content")
	}
}

// ---------------------------------------------------------------------------
// TestParseQuestion_fromFixture
// ---------------------------------------------------------------------------

func TestParseQuestion_fromFixture_questions(t *testing.T) {
	t.Parallel()
	var questions []questionDTO
	loadFixture(t, "questions.json", &questions)

	if len(questions) < 3 {
		t.Fatalf("need at least 3 questions in fixture, got %d", len(questions))
	}

	// Verify all questions can be parsed without panic/error.
	for i := range questions {
		signal := parseQuestion(&questions[i], nil, nil, "stackoverflow", time.Now())
		if signal.ID == "" {
			t.Fatalf("empty ID for question %d", questions[i].QuestionID)
		}
		if signal.Title == "" {
			t.Fatalf("empty title for question %d", questions[i].QuestionID)
		}
		// Body may be empty after cleaning, but that's OK for some edge cases.
	}
}

func TestParseQuestion_fromFixture_withAnswersAndComments(t *testing.T) {
	t.Parallel()
	var questions []questionDTO
	loadFixture(t, "questions.json", &questions)
	var answers []answerDTO
	loadFixture(t, "answers.json", &answers)
	var comments []commentDTO
	loadFixture(t, "comments.json", &comments)

	// Build a map from question_id → answers.
	ansByQ := make(map[int][]answerDTO)
	for _, a := range answers {
		ansByQ[a.QuestionID] = append(ansByQ[a.QuestionID], a)
	}
	// Build a map from post_id → comments.
	comByQ := make(map[int][]commentDTO)
	for _, c := range comments {
		comByQ[c.PostID] = append(comByQ[c.PostID], c)
	}

	for i := range questions {
		signal := parseQuestion(&questions[i], ansByQ[questions[i].QuestionID], comByQ[questions[i].QuestionID], "stackoverflow", time.Now())
		if signal.ID == "" {
			t.Fatalf("empty ID for question %d", questions[i].QuestionID)
		}
		// Verify comments/answers count matches.
		expectedComments := len(ansByQ[questions[i].QuestionID]) + len(comByQ[questions[i].QuestionID])
		if len(signal.Comments) != expectedComments {
			t.Fatalf("question %d: len(Comments) = %d, want %d", questions[i].QuestionID, len(signal.Comments), expectedComments)
		}
	}
}

// ---------------------------------------------------------------------------
// TestCleanHTML — edge cases
// ---------------------------------------------------------------------------

func TestCleanHTML(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"plain text", "hello world", "hello world"},
		{"simple paragraph", "<p>Hello</p>", "Hello"},
		{"line break", "<p>Line1<br>Line2</p>", "Line1\nLine2"},
		{"code block stripped", "<p>Text</p><pre><code>code here</code></pre>", "Text"},
		{"nested tags", "<div><p>Nested</p></div>", "Nested"},
		{"html entities", "&amp; &lt; &gt;", "& < >"},
		{"multiple paragraphs", "<p>First</p><p>Second</p>", "First\n\nSecond"},
		{"whitespace normalization", "  hello   world  ", "hello world"},
		{"code block with attributes", "<code class=\"lang-go\">fmt.Println()</code>", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanHTML(tt.input)
			if got != tt.want {
				t.Fatalf("cleanHTML(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
