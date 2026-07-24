package reddit

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/moontechs/signalforge/internal/domain"
	"github.com/moontechs/signalforge/internal/storage"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// loadFixture reads and parses a test fixture file as the given type.
func loadFixture(t *testing.T, path string, into any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	if err := json.Unmarshal(data, into); err != nil {
		t.Fatalf("unmarshal fixture %s: %v", path, err)
	}
}

// mustParseTime parses an RFC3339 timestamp or fails the test.
func mustParseTime(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse time %q: %v", s, err)
	}
	return ts
}

// fixturePath returns the absolute path to a testdata fixture.
func fixturePath(name string) string {
	return "../../../testdata/reddit/" + name
}

// readFixture reads the raw bytes of a test fixture file.
func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(fixturePath(name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

// collectedAt is a fixed timestamp used as CollectedAt in tests.
var collectedAt = time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)

// ---------------------------------------------------------------------------
// parsePost
// ---------------------------------------------------------------------------

func TestParsePost_basic(t *testing.T) {
	t.Parallel()

	var listing listingResponse
	loadFixture(t, fixturePath("listing.json"), &listing)

	if len(listing.Data.Children) < 1 {
		t.Fatal("fixture has no children")
	}

	signal := parsePost(listing.Data.Children[0].Data, nil, "new", collectedAt)

	if signal.ID != "reddit:abc123" {
		t.Fatalf("ID = %q, want %q", signal.ID, "reddit:abc123")
	}
	if signal.Source != "reddit" {
		t.Fatalf("Source = %q, want %q", signal.Source, "reddit")
	}
	if signal.SourceID != "abc123" {
		t.Fatalf("SourceID = %q, want %q", signal.SourceID, "abc123")
	}
	if signal.SourceType != "discussion" {
		t.Fatalf("SourceType = %q, want %q", signal.SourceType, "discussion")
	}

	expectedURL := "https://www.reddit.com/r/golang/comments/abc123/test_post_one_learning_go_in_2025/"
	if signal.URL != expectedURL {
		t.Fatalf("URL = %q, want %q", signal.URL, expectedURL)
	}
	if signal.Title != "Test Post One: Learning Go in 2025" {
		t.Fatalf("Title = %q, want %q", signal.Title, "Test Post One: Learning Go in 2025")
	}
	if !strings.Contains(signal.Body, "error handling can be verbose") {
		t.Fatalf("Body should contain expected text, got %q", signal.Body)
	}
	if signal.Community != "golang" {
		t.Fatalf("Community = %q, want %q", signal.Community, "golang")
	}
	if signal.Score != 150 {
		t.Fatalf("Score = %d, want %d", signal.Score, 150)
	}
	if signal.CommentCount != 42 {
		t.Fatalf("CommentCount = %d, want %d", signal.CommentCount, 42)
	}

	expectedCreated := time.Unix(1700000000, 0).UTC()
	if !signal.CreatedAt.Equal(expectedCreated) {
		t.Fatalf("CreatedAt = %v, want %v", signal.CreatedAt, expectedCreated)
	}
	if !signal.CollectedAt.Equal(collectedAt) {
		t.Fatalf("CollectedAt = %v, want %v", signal.CollectedAt, collectedAt)
	}

	// Metadata
	if signal.Metadata[MetaKeyAuthor] != "gopher_user" {
		t.Fatalf("metadata author = %q, want %q", signal.Metadata[MetaKeyAuthor], "gopher_user")
	}
	if signal.Metadata[MetaKeyPostScore] != "150" {
		t.Fatalf("metadata post_score = %q, want %q", signal.Metadata[MetaKeyPostScore], "150")
	}
	if signal.Metadata[MetaKeyCommentCount] != "42" {
		t.Fatalf("metadata comment_count = %q, want %q", signal.Metadata[MetaKeyCommentCount], "42")
	}
	if signal.Metadata[MetaKeySubreddit] != "golang" {
		t.Fatalf("metadata subreddit = %q, want %q", signal.Metadata[MetaKeySubreddit], "golang")
	}
	if signal.Metadata[MetaKeyListingSort] != "new" {
		t.Fatalf("metadata listing_sort = %q, want %q", signal.Metadata[MetaKeyListingSort], "new")
	}

	// ContentHash is non-empty and deterministic
	if signal.ContentHash == "" {
		t.Fatal("ContentHash should be non-empty")
	}
}

func TestParsePost_contentHashStability(t *testing.T) {
	t.Parallel()

	var listing listingResponse
	loadFixture(t, fixturePath("listing.json"), &listing)

	s1 := parsePost(listing.Data.Children[0].Data, nil, "new", collectedAt)
	s2 := parsePost(listing.Data.Children[0].Data, nil, "new", collectedAt)

	if s1.ContentHash != s2.ContentHash {
		t.Fatal("ContentHash should be deterministic")
	}
}

func TestParsePost_contentHashWithComments(t *testing.T) {
	t.Parallel()

	var listing listingResponse
	loadFixture(t, fixturePath("listing.json"), &listing)

	comments := []domain.Comment{
		{ID: "c1", Body: "First comment", Score: 10, CreatedAt: time.Now()},
		{ID: "c2", Body: "Second comment", Score: 5, CreatedAt: time.Now()},
	}

	s1 := parsePost(listing.Data.Children[0].Data, comments, "new", collectedAt)
	s2 := parsePost(listing.Data.Children[0].Data, comments, "new", collectedAt)

	if s1.ContentHash != s2.ContentHash {
		t.Fatal("ContentHash should be deterministic with same comments")
	}

	// Different comments => different hash
	diffComments := []domain.Comment{
		{ID: "c1", Body: "Different body", Score: 10, CreatedAt: time.Now()},
	}
	s3 := parsePost(listing.Data.Children[0].Data, diffComments, "new", collectedAt)
	if s1.ContentHash == s3.ContentHash {
		t.Fatal("ContentHash should differ when comment bodies differ")
	}
}

func TestParsePost_emptySort(t *testing.T) {
	t.Parallel()

	var listing listingResponse
	loadFixture(t, fixturePath("listing.json"), &listing)

	signal := parsePost(listing.Data.Children[0].Data, nil, "", collectedAt)

	// No listing_sort key should be present.
	if _, ok := signal.Metadata[MetaKeyListingSort]; ok {
		t.Fatal("listing_sort should not be present when sort is empty")
	}
}

func TestParsePost_emptySelftext(t *testing.T) {
	t.Parallel()

	var listing listingResponse
	loadFixture(t, fixturePath("listing.json"), &listing)

	// Second post has empty selftext.
	if len(listing.Data.Children) < 2 {
		t.Fatal("fixture has fewer than 2 children")
	}

	signal := parsePost(listing.Data.Children[1].Data, nil, "new", collectedAt)
	if signal.Body != "" {
		t.Fatalf("Body should be empty for link post, got %q", signal.Body)
	}
	if signal.URL != "https://www.reddit.com/r/golang/comments/def456/show_hn_my_new_cli_tool_for_data_processing/" {
		t.Fatalf("URL = %q", signal.URL)
	}
}

// ---------------------------------------------------------------------------
// parsePost with Unicode/escaped content
// ---------------------------------------------------------------------------

func TestParsePost_unicodeAndEscaped(t *testing.T) {
	t.Parallel()

	var listing listingResponse
	loadFixture(t, fixturePath("listing-with-unicode.json"), &listing)

	if len(listing.Data.Children) < 1 {
		t.Fatal("fixture has no children")
	}

	signal := parsePost(listing.Data.Children[0].Data, nil, "new", collectedAt)

	if !strings.Contains(signal.Title, "こんにちは世界") {
		t.Fatalf("Title should contain unicode characters, got %q", signal.Title)
	}
	if !strings.Contains(signal.Title, "🚀") {
		t.Fatalf("Title should contain emoji, got %q", signal.Title)
	}
	if !strings.Contains(signal.Body, "🔥") {
		t.Fatalf("Body should contain emoji, got %q", signal.Body)
	}
	// JSON encoding preserves HTML entities as-is; they are not decoded.
	if !strings.Contains(signal.Body, "&amp;") {
		t.Fatalf("Body should contain &amp; as stored in JSON, got %q", signal.Body)
	}
}

// ---------------------------------------------------------------------------
// FlattenComments
// ---------------------------------------------------------------------------

func TestFlattenComments_basic(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "comment-tree.json")

	comments, err := FlattenComments(raw, 0)
	if err != nil {
		t.Fatalf("FlattenComments: %v", err)
	}

	// Expected comments: c1, c1_1, c1_2, c2 (4 total). "more" placeholder skipped.
	if len(comments) != 4 {
		t.Fatalf("expected 4 comments, got %d", len(comments))
	}

	// Order: top-level comments first (c1, c2), then nested replies (c1_1, c1_2).
	if comments[0].ID != "c1" {
		t.Fatalf("comments[0].ID = %q, want %q", comments[0].ID, "c1")
	}
	if comments[1].ID != "c2" {
		t.Fatalf("comments[1].ID = %q, want %q", comments[1].ID, "c2")
	}
	if comments[2].ID != "c1_1" {
		t.Fatalf("comments[2].ID = %q, want %q", comments[2].ID, "c1_1")
	}
	if comments[3].ID != "c1_2" {
		t.Fatalf("comments[3].ID = %q, want %q", comments[3].ID, "c1_2")
	}

	// Check body content.
	if !strings.Contains(comments[0].Body, "Tour of Go") {
		t.Fatalf("comments[0].Body = %q, want Tour of Go", comments[0].Body)
	}

	// Check scores.
	if comments[0].Score != 25 {
		t.Fatalf("comments[0].Score = %d, want %d", comments[0].Score, 25)
	}
	if comments[2].Score != 12 {
		t.Fatalf("comments[2].Score = %d, want %d", comments[2].Score, 12)
	}

	// Check timestamps.
	expectedC1 := time.Unix(1700000100, 0).UTC()
	if !comments[0].CreatedAt.Equal(expectedC1) {
		t.Fatalf("comments[0].CreatedAt = %v, want %v", comments[0].CreatedAt, expectedC1)
	}
}

func TestFlattenComments_maxComments(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "comment-tree.json")

	// Limit to 2 comments.
	comments, err := FlattenComments(raw, 2)
	if err != nil {
		t.Fatalf("FlattenComments: %v", err)
	}

	if len(comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(comments))
	}

	// First two are c1 and c2 (top-level).
	if comments[0].ID != "c1" {
		t.Fatalf("comments[0].ID = %q, want %q", comments[0].ID, "c1")
	}
	if comments[1].ID != "c2" {
		t.Fatalf("comments[1].ID = %q, want %q", comments[1].ID, "c2")
	}
}

func TestFlattenComments_maxCommentsIncludesNested(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "comment-tree.json")

	// Limit to 3 comments: c1, c2, and the first nested reply (c1_1).
	comments, err := FlattenComments(raw, 3)
	if err != nil {
		t.Fatalf("FlattenComments: %v", err)
	}

	if len(comments) != 3 {
		t.Fatalf("expected 3 comments, got %d", len(comments))
	}

	if comments[0].ID != "c1" {
		t.Fatalf("comments[0].ID = %q, want %q", comments[0].ID, "c1")
	}
	if comments[1].ID != "c2" {
		t.Fatalf("comments[1].ID = %q, want %q", comments[1].ID, "c2")
	}
	if comments[2].ID != "c1_1" {
		t.Fatalf("comments[2].ID = %q, want %q", comments[2].ID, "c1_1")
	}
}

func TestFlattenComments_zeroMax(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "comment-tree.json")

	// 0 max = unlimited.
	comments, err := FlattenComments(raw, 0)
	if err != nil {
		t.Fatalf("FlattenComments: %v", err)
	}

	if len(comments) != 4 {
		t.Fatalf("expected 4 comments (unlimited), got %d", len(comments))
	}
}

func TestFlattenComments_negativeMax(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "comment-tree.json")

	// Negative max = unlimited (same as 0).
	comments, err := FlattenComments(raw, -1)
	if err != nil {
		t.Fatalf("FlattenComments: %v", err)
	}

	if len(comments) != 4 {
		t.Fatalf("expected 4 comments (unlimited), got %d", len(comments))
	}
}

// ---------------------------------------------------------------------------
// Deleted / removed / empty comment filtering
// ---------------------------------------------------------------------------

func TestFlattenComments_skipsDeletedRemovedEmpty(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "comment-tree-deleted.json")

	comments, err := FlattenComments(raw, 0)
	if err != nil {
		t.Fatalf("FlattenComments: %v", err)
	}

	// Only the first comment "d1" should survive; d2 ([deleted]), d3 ([removed]),
	// d4 (empty), and the "more" placeholder should all be skipped.
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	if comments[0].ID != "d1" {
		t.Fatalf("expected comment ID d1, got %s", comments[0].ID)
	}
	if comments[0].Body != "This is a normal comment." {
		t.Fatalf("expected normal comment body, got %q", comments[0].Body)
	}
}

// ---------------------------------------------------------------------------
// Nested comment tree
// ---------------------------------------------------------------------------

func TestFlattenComments_nestedDepth(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "comment-tree-nested.json")

	comments, err := FlattenComments(raw, 0)
	if err != nil {
		t.Fatalf("FlattenComments: %v", err)
	}

	// Expected: n1 (level 1), n1_1 (level 2), n1_1_1 (level 3), n1_2 (level 2)
	if len(comments) != 4 {
		t.Fatalf("expected 4 comments in nested tree, got %d", len(comments))
	}

	// Order check
	if comments[0].ID != "n1" {
		t.Fatalf("comments[0].ID = %q, want %q", comments[0].ID, "n1")
	}
	if comments[1].ID != "n1_1" {
		t.Fatalf("comments[1].ID = %q, want %q", comments[1].ID, "n1_1")
	}
	if comments[2].ID != "n1_1_1" {
		t.Fatalf("comments[2].ID = %q, want %q", comments[2].ID, "n1_1_1")
	}
	if comments[3].ID != "n1_2" {
		t.Fatalf("comments[3].ID = %q, want %q", comments[3].ID, "n1_2")
	}

	// Body content
	if !strings.Contains(comments[0].Body, "Level 1") {
		t.Fatalf("comments[0].Body = %q, want Level 1", comments[0].Body)
	}
	if !strings.Contains(comments[1].Body, "Level 2") {
		t.Fatalf("comments[1].Body = %q, want Level 2", comments[1].Body)
	}
	if !strings.Contains(comments[2].Body, "Level 3") {
		t.Fatalf("comments[2].Body = %q, want Level 3", comments[2].Body)
	}
}

func TestFlattenComments_nestedWithCap(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "comment-tree-nested.json")

	// Cap at 2 comments: n1 and n1_1.
	comments, err := FlattenComments(raw, 2)
	if err != nil {
		t.Fatalf("FlattenComments: %v", err)
	}

	if len(comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(comments))
	}
	if comments[0].ID != "n1" {
		t.Fatalf("comments[0].ID = %q, want %q", comments[0].ID, "n1")
	}
	if comments[1].ID != "n1_1" {
		t.Fatalf("comments[1].ID = %q, want %q", comments[1].ID, "n1_1")
	}
}

// ---------------------------------------------------------------------------
// Edge cases
// ---------------------------------------------------------------------------

func TestFlattenComments_emptyTree(t *testing.T) {
	t.Parallel()

	// Empty array.
	comments, err := FlattenComments([]byte(`[]`), 0)
	if err != nil {
		t.Fatalf("FlattenComments: %v", err)
	}
	if len(comments) != 0 {
		t.Fatalf("expected 0 comments from empty tree, got %d", len(comments))
	}
}

func TestFlattenComments_singleElementTree(t *testing.T) {
	t.Parallel()

	// Only one element (post listing, no comment listing).
	comments, err := FlattenComments([]byte(`[{}]`), 0)
	if err != nil {
		t.Fatalf("FlattenComments: %v", err)
	}
	if len(comments) != 0 {
		t.Fatalf("expected 0 comments from single-element tree, got %d", len(comments))
	}
}

func TestFlattenComments_allMorePlaceholders(t *testing.T) {
	t.Parallel()

	raw := []byte(`[
		{"kind":"Listing","data":{"children":[{"kind":"t3"}]}},
		{"kind":"Listing","data":{"children":[
			{"kind":"more","data":{"count":2,"parent_id":"t3_p1","children":["c1","c2"]}}
		]}}
	]`)

	comments, err := FlattenComments(raw, 0)
	if err != nil {
		t.Fatalf("FlattenComments: %v", err)
	}
	if len(comments) != 0 {
		t.Fatalf("expected 0 comments (all more), got %d", len(comments))
	}
}

// ---------------------------------------------------------------------------
// eligiblePost
// ---------------------------------------------------------------------------

func TestEligiblePost_kind(t *testing.T) {
	t.Parallel()

	// Only "t3" kind should be eligible.
	if eligiblePost(listingChild{Kind: "t3"}, time.Time{}) != true {
		t.Fatal("expected t3 kind to be eligible with zero since")
	}
	if eligiblePost(listingChild{Kind: "t1"}, time.Time{}) != false {
		t.Fatal("expected t1 kind to be ineligible")
	}
	if eligiblePost(listingChild{Kind: "more"}, time.Time{}) != false {
		t.Fatal("expected more kind to be ineligible")
	}
	if eligiblePost(listingChild{Kind: ""}, time.Time{}) != false {
		t.Fatal("expected empty kind to be ineligible")
	}
}

func TestEligiblePost_since(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	// Post created after since should be eligible.
	recent := listingChild{
		Kind: "t3",
		Data: postData{CreatedUTC: float64(now.Add(-1 * time.Hour).Unix())},
	}
	if !eligiblePost(recent, now.Add(-2*time.Hour)) {
		t.Fatal("recent post should be eligible")
	}

	// Post created before since should not be eligible.
	old := listingChild{
		Kind: "t3",
		Data: postData{CreatedUTC: float64(now.Add(-3 * time.Hour).Unix())},
	}
	if eligiblePost(old, now.Add(-2*time.Hour)) {
		t.Fatal("old post should not be eligible")
	}

	// Post created exactly at since should be eligible.
	exact := listingChild{
		Kind: "t3",
		Data: postData{CreatedUTC: float64(now.Add(-2 * time.Hour).Unix())},
	}
	if !eligiblePost(exact, now.Add(-2*time.Hour)) {
		t.Fatal("post at since boundary should be eligible")
	}
}

func TestEligiblePost_zeroSince(t *testing.T) {
	t.Parallel()

	child := listingChild{
		Kind: "t3",
		Data: postData{CreatedUTC: 1000000},
	}
	if !eligiblePost(child, time.Time{}) {
		t.Fatal("post should be eligible when since is zero")
	}
}

// ---------------------------------------------------------------------------
// Integration-style: parsePost + FlattenComments together
// ---------------------------------------------------------------------------

func TestParsePostWithComments(t *testing.T) {
	t.Parallel()

	var listing listingResponse
	loadFixture(t, fixturePath("listing.json"), &listing)

	raw := readFixture(t, "comment-tree.json")

	comments, err := FlattenComments(raw, 10)
	if err != nil {
		t.Fatalf("FlattenComments: %v", err)
	}

	signal := parsePost(listing.Data.Children[0].Data, comments, "new", collectedAt)

	if len(signal.Comments) != 4 {
		t.Fatalf("expected 4 comments on signal, got %d", len(signal.Comments))
	}
	if signal.CommentCount != 42 {
		t.Fatalf("CommentCount = %d, want %d", signal.CommentCount, 42)
	}
	if signal.ContentHash == "" {
		t.Fatal("ContentHash should be non-empty")
	}

	// Verify the content hash includes comment bodies.
	hashOnlyTitle := storage.ContentHash(signal.Title)
	if signal.ContentHash == hashOnlyTitle {
		t.Fatal("ContentHash should incorporate body and comments, not just title")
	}
}

// ---------------------------------------------------------------------------
// Timestamp conversion
// ---------------------------------------------------------------------------

func TestParsePost_timestampConversion(t *testing.T) {
	t.Parallel()

	// Reddit uses UNIX timestamps with potential fractional seconds.
	// Use 0.125 (1/8) which is exactly representable in float64.
	createdUTC := 1700000000.125

	post := postData{
		ID:          "ts1",
		Title:       "Timestamp test",
		Permalink:   "/r/test/comments/ts1/timestamp/",
		Subreddit:   "test",
		Author:      "ts_user",
		Score:       1,
		NumComments: 0,
		CreatedUTC:  createdUTC,
	}

	signal := parsePost(post, nil, "new", collectedAt)

	// 1700000000.125 = 1700000000 sec + 125000000 nsec
	expected := time.Unix(1700000000, 125000000).UTC()
	if !signal.CreatedAt.Equal(expected) {
		t.Fatalf("CreatedAt = %v (unix %d), want %v (unix %d)",
			signal.CreatedAt, signal.CreatedAt.UnixNano(),
			expected, expected.UnixNano())
	}

	// Also test integer timestamp (common case for Reddit).
	intTS := 1700000000.0
	post2 := postData{
		ID:          "ts2",
		Title:       "Integer timestamp test",
		Permalink:   "/r/test/comments/ts2/int/",
		Subreddit:   "test",
		Author:      "ts_user2",
		Score:       1,
		NumComments: 0,
		CreatedUTC:  intTS,
	}
	signal2 := parsePost(post2, nil, "new", collectedAt)
	expected2 := time.Unix(1700000000, 0).UTC()
	if !signal2.CreatedAt.Equal(expected2) {
		t.Fatalf("CreatedAt = %v, want %v", signal2.CreatedAt, expected2)
	}
}

func TestFlattenComments_timestampConversion(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "comment-tree.json")

	comments, err := FlattenComments(raw, 0)
	if err != nil {
		t.Fatalf("FlattenComments: %v", err)
	}

	expectedC1 := time.Unix(1700000100, 0).UTC()
	if !comments[0].CreatedAt.Equal(expectedC1) {
		t.Fatalf("comment c1 CreatedAt = %v, want %v", comments[0].CreatedAt, expectedC1)
	}
}

// ---------------------------------------------------------------------------
// commentBodies helper
// ---------------------------------------------------------------------------

func TestCommentBodies(t *testing.T) {
	t.Parallel()

	comments := []domain.Comment{
		{Body: "first"},
		{Body: "second"},
		{Body: ""},
	}
	bodies := commentBodies(comments)

	if len(bodies) != 3 {
		t.Fatalf("expected 3 bodies, got %d", len(bodies))
	}
	if bodies[0] != "first" {
		t.Fatalf("bodies[0] = %q, want %q", bodies[0], "first")
	}
	if bodies[1] != "second" {
		t.Fatalf("bodies[1] = %q, want %q", bodies[1], "second")
	}
	if bodies[2] != "" {
		t.Fatalf("bodies[2] = %q, want empty", bodies[2])
	}
}

func TestCommentBodies_empty(t *testing.T) {
	t.Parallel()

	bodies := commentBodies(nil)
	if len(bodies) != 0 {
		t.Fatalf("expected 0 bodies from nil, got %d", len(bodies))
	}

	bodies = commentBodies([]domain.Comment{})
	if len(bodies) != 0 {
		t.Fatalf("expected 0 bodies from empty, got %d", len(bodies))
	}
}
