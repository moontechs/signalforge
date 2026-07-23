package stackexchange

import "testing"

func TestParseQuestionsNormalizesHTMLAndMetadata(t *testing.T) {
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
