package classify

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/moontechs/signalforge/internal/domain"
)

// mockResult holds either a response or an error for the mock LLM client.
type mockResult struct {
	resp domain.CompletionResponse
	err  error
}

// mockLLMClient simulates an LLM client for testing. It returns pre-configured
// results in sequence for each Complete call.
type mockLLMClient struct {
	results   []mockResult
	callIndex int
	requests  []domain.CompletionRequest
}

func newMockLLMClient() *mockLLMClient {
	return &mockLLMClient{}
}

func (m *mockLLMClient) addResponse(resp domain.CompletionResponse) {
	m.results = append(m.results, mockResult{resp: resp})
}

func (m *mockLLMClient) addErr(err error) {
	m.results = append(m.results, mockResult{err: err})
}

func (m *mockLLMClient) Complete(
	_ any,
	req domain.CompletionRequest, //nolint:gocritic // Value signature is required by domain.LLMClient.
) (domain.CompletionResponse, error) {
	m.requests = append(m.requests, req)
	idx := m.callIndex
	m.callIndex++
	if idx < len(m.results) {
		res := m.results[idx]
		if res.err != nil {
			return domain.CompletionResponse{}, res.err
		}
		return res.resp, nil
	}
	return domain.CompletionResponse{}, nil
}

// validClassifyResponse returns a complete valid classification JSON response.
func validClassifyResponse() string {
	data := classifyResponse{
		IsProblemSignal:      true,
		Relevance:            0.85,
		Problem:              "Users cannot efficiently track time across multiple projects",
		TargetUser:           "Freelance developers managing multiple client projects",
		Context:              "Working remotely with distributed teams using different tools",
		CurrentWorkaround:    "Manually tracking time in spreadsheets",
		DesiredOutcome:       "Automatic time tracking that integrates with existing project management tools",
		Recurring:            true,
		ProductSolvable:      true,
		IsTemporaryIncident:  false,
		IsSupportQuestion:    false,
		IsExistingBug:        false,
		IsConfigurationIssue: false,
		IsFeatureRequest:     true,
		SeverityHint:         7,
		FrequencyHint:        8,
		PaymentHint:          6,
		FrustrationHint:      9,
		Keywords:             []string{"time tracking", "productivity", "project management"},
		Entities:             []string{"Jira", "Trello", "Slack"},
		Actions:              []string{"track", "integrate", "automate"},
		Constraints:          []string{"must work offline", "supports multiple currencies"},
	}
	b, _ := json.Marshal(data)
	return string(b)
}

// noiseClassifyResponse returns a response where is_problem_signal is false.
func noiseClassifyResponse() string {
	data := classifyResponse{
		IsProblemSignal: false,
		Relevance:       0.0,
	}
	b, _ := json.Marshal(data)
	return string(b)
}

func writePromptFile(t *testing.T, dir string) string {
	t.Helper()
	prompt := `You are a signal classifier. Analyze the following public post and determine if it describes a recurring user problem that could be solved by a product. Return only valid JSON.

## Post

Title: {{.Title}}
Body: {{.Body}}
Comments: {{.Comments}}
Source: {{.Source}}
URL: {{.URL}}

## Decision criteria

Classify this post into the following JSON structure:

{
  "is_problem_signal": false,
  "relevance": 0.0,
  "problem": "",
  "target_user": "",
  "context": "",
  "current_workaround": "",
  "desired_outcome": "",
  "recurring": false,
  "product_solvable": false,
  "is_temporary_incident": false,
  "is_support_question": false,
  "is_existing_bug": false,
  "is_configuration_issue": false,
  "is_feature_request": false,
  "severity_hint": 0,
  "frequency_hint": 0,
  "payment_hint": 0,
  "frustration_hint": 0,
  "keywords": [],
  "entities": [],
  "actions": [],
  "constraints": []
}

### Fields

- is_problem_signal (boolean): true if this post describes a recurring user problem that could be addressed by a product or tool
- relevance (float 0-1): how relevant this post is to product opportunity discovery
- problem (string): concise description of the core problem the user is facing
- target_user (string): description of who experiences this problem
- context (string): the circumstances or environment in which the problem occurs
- current_workaround (string): how users currently deal with this problem, if any
- desired_outcome (string): what the user wants to be able to do instead

### Classification flags (boolean)

- recurring: this problem happens repeatedly, not a one-time event
- product_solvable: a product or tool could realistically solve this
- is_temporary_incident: this is a temporary outage or transient issue
- is_support_question: this is a simple support or how-to question
- is_existing_bug: this describes a known bug in an existing product
- is_configuration_issue: the problem is caused by incorrect setup or configuration
- is_feature_request: this is a request for a feature in an existing product

### Hints (0-10 integer, higher = more)

- severity_hint: how severe/impactful the problem is for the user
- frequency_hint: how often this problem occurs
- payment_hint: how likely the user is to pay for a solution
- frustration_hint: how frustrated the user is about this problem

### Arrays (strings)

- keywords: key terms that describe the problem domain
- entities: specific products, companies, or technologies mentioned
- actions: verbs describing what users are trying to do
- constraints: limitations or requirements mentioned

## Instructions

Return valid JSON matching the schema above. Do not invent facts. If the post does not describe a problem signal, set is_problem_signal to false and relevance to 0. For non-problem signals, all other fields should be empty or zero values as appropriate.`
	path := filepath.Join(dir, "classify_signal.txt")
	if err := os.WriteFile(path, []byte(prompt), 0o644); err != nil {
		t.Fatalf("write prompt file: %v", err)
	}
	return path
}

func newTestSignal(id string) domain.RawSignal {
	return domain.RawSignal{
		ID:       id,
		Source:   "test-source",
		SourceID: "src-" + id,
		URL:      "https://example.com/post/" + id,
		Title:    "Test title for " + id,
		Body:     "This is a test body describing a recurring problem.",
		Comments: []domain.Comment{
			{ID: "c1", Body: "I have this problem too!", Score: 5},
			{ID: "c2", Body: "A workaround is to use tool X.", Score: 3},
		},
		CollectedAt: time.Now(),
	}
}

func newEmptyCommentsSignal(id string) domain.RawSignal {
	return domain.RawSignal{
		ID:          id,
		Source:      "test-source",
		SourceID:    "src-" + id,
		URL:         "https://example.com/post/" + id,
		Title:       "Signal with no comments",
		Body:        "Body text without comments.",
		Comments:    nil,
		CollectedAt: time.Now(),
	}
}

func TestNewClassifier_Defaults(t *testing.T) {
	t.Parallel()

	mock := newMockLLMClient()
	cfg := Config{
		Model:      "test-model",
		PromptPath: "/nonexistent/prompt.txt",
	}

	c := New(mock, cfg)
	if c == nil {
		t.Fatal("New returned nil")
	}
	if c.cfg.BatchSize != 20 {
		t.Errorf("expected default BatchSize 20, got %d", c.cfg.BatchSize)
	}
	if c.cfg.Temperature != 0.1 {
		t.Errorf("expected default Temperature 0.1, got %f", c.cfg.Temperature)
	}
	if c.cfg.MaxTokens != 4000 {
		t.Errorf("expected default MaxTokens 4000, got %d", c.cfg.MaxTokens)
	}
}

func TestNewClassifier_CustomValues(t *testing.T) {
	t.Parallel()

	mock := newMockLLMClient()
	cfg := Config{
		Model:       "custom-model",
		BatchSize:   5,
		Temperature: 0.5,
		MaxTokens:   2048,
		PromptPath:  "/nonexistent/prompt.txt",
	}

	c := New(mock, cfg)
	if c.cfg.BatchSize != 5 {
		t.Errorf("expected BatchSize 5, got %d", c.cfg.BatchSize)
	}
	if c.cfg.Temperature != 0.5 {
		t.Errorf("expected Temperature 0.5, got %f", c.cfg.Temperature)
	}
	if c.cfg.MaxTokens != 2048 {
		t.Errorf("expected MaxTokens 2048, got %d", c.cfg.MaxTokens)
	}
}

func TestFormatComments_Empty(t *testing.T) {
	t.Parallel()

	result := formatComments(nil)
	if result != "none" {
		t.Errorf("expected 'none', got %q", result)
	}

	result = formatComments([]domain.Comment{})
	if result != "none" {
		t.Errorf("expected 'none' for empty slice, got %q", result)
	}
}

func TestFormatComments_Single(t *testing.T) {
	t.Parallel()

	comments := []domain.Comment{
		{ID: "c1", Body: "This is a comment", Score: 10},
	}
	result := formatComments(comments)
	if !strings.Contains(result, "Comment 1") {
		t.Errorf("expected 'Comment 1' in output, got %q", result)
	}
	if !strings.Contains(result, "score: 10") {
		t.Errorf("expected 'score: 10' in output, got %q", result)
	}
	if !strings.Contains(result, "This is a comment") {
		t.Errorf("expected comment body in output, got %q", result)
	}
}

func TestFormatComments_Multiple(t *testing.T) {
	t.Parallel()

	comments := []domain.Comment{
		{ID: "c1", Body: "First comment", Score: 5},
		{ID: "c2", Body: "Second comment", Score: 3},
	}
	result := formatComments(comments)
	if !strings.Contains(result, "---") {
		t.Errorf("expected separator between comments, got %q", result)
	}
	if !strings.Contains(result, "Comment 1") {
		t.Errorf("expected 'Comment 1', got %q", result)
	}
	if !strings.Contains(result, "Comment 2") {
		t.Errorf("expected 'Comment 2', got %q", result)
	}
}

func TestClassify_FullFieldMapping(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	promptPath := writePromptFile(t, dir)

	mock := newMockLLMClient()
	mock.addResponse(domain.CompletionResponse{
		Content: validClassifyResponse(),
		Model:   "test-model-v1",
	})

	cfg := Config{
		Model:      "test-model",
		BatchSize:  20,
		PromptPath: promptPath,
	}
	classifier := New(mock, cfg)
	signal := newTestSignal("sig-1")

	results, failures := classifier.Classify(context.Background(), []domain.RawSignal{signal})
	if len(failures) > 0 {
		t.Fatalf("unexpected failures: %v", failures[0].Err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	ps := results[0]

	// Check derived fields.
	if ps.ID == "" {
		t.Error("expected non-empty ID")
	}
	if !strings.HasPrefix(ps.ID, "ps_") {
		t.Errorf("expected ID to start with 'ps_', got %q", ps.ID)
	}
	if ps.RawSignalID != "sig-1" {
		t.Errorf("expected RawSignalID 'sig-1', got %q", ps.RawSignalID)
	}
	if ps.Source != "test-source" {
		t.Errorf("expected Source 'test-source', got %q", ps.Source)
	}
	if ps.URL != "https://example.com/post/sig-1" {
		t.Errorf("expected URL 'https://example.com/post/sig-1', got %q", ps.URL)
	}
	if ps.ClassificationModel != "test-model-v1" {
		t.Errorf("expected ClassificationModel 'test-model-v1', got %q", ps.ClassificationModel)
	}
	if ps.ClassifiedAt.IsZero() {
		t.Error("expected non-zero ClassifiedAt")
	}

	// Check mapped fields from response.
	if !ps.IsProblemSignal {
		t.Error("expected IsProblemSignal true")
	}
	if ps.Relevance != 0.85 {
		t.Errorf("expected Relevance 0.85, got %f", ps.Relevance)
	}
	if ps.Problem != "Users cannot efficiently track time across multiple projects" {
		t.Errorf("unexpected Problem: %q", ps.Problem)
	}
	if ps.TargetUser != "Freelance developers managing multiple client projects" {
		t.Errorf("unexpected TargetUser: %q", ps.TargetUser)
	}
	if ps.Context != "Working remotely with distributed teams using different tools" {
		t.Errorf("unexpected Context: %q", ps.Context)
	}
	if ps.CurrentWorkaround != "Manually tracking time in spreadsheets" {
		t.Errorf("unexpected CurrentWorkaround: %q", ps.CurrentWorkaround)
	}
	if ps.DesiredOutcome != "Automatic time tracking that integrates with existing project management tools" {
		t.Errorf("unexpected DesiredOutcome: %q", ps.DesiredOutcome)
	}
	if !ps.Recurring {
		t.Error("expected Recurring true")
	}
	if !ps.ProductSolvable {
		t.Error("expected ProductSolvable true")
	}
	if ps.IsTemporaryIncident {
		t.Error("expected IsTemporaryIncident false")
	}
	if !ps.IsFeatureRequest {
		t.Error("expected IsFeatureRequest true")
	}

	// Check hints.
	if ps.SeverityHint != 7 {
		t.Errorf("expected SeverityHint 7, got %f", ps.SeverityHint)
	}
	if ps.FrequencyHint != 8 {
		t.Errorf("expected FrequencyHint 8, got %f", ps.FrequencyHint)
	}
	if ps.PaymentHint != 6 {
		t.Errorf("expected PaymentHint 6, got %f", ps.PaymentHint)
	}
	if ps.FrustrationHint != 9 {
		t.Errorf("expected FrustrationHint 9, got %f", ps.FrustrationHint)
	}

	// Check arrays.
	if len(ps.Keywords) != 3 || ps.Keywords[0] != "time tracking" {
		t.Errorf("unexpected Keywords: %v", ps.Keywords)
	}
	if len(ps.Entities) != 3 || ps.Entities[0] != "Jira" {
		t.Errorf("unexpected Entities: %v", ps.Entities)
	}
	if len(ps.Actions) != 3 || ps.Actions[0] != "track" {
		t.Errorf("unexpected Actions: %v", ps.Actions)
	}
	if len(ps.Constraints) != 2 || ps.Constraints[0] != "must work offline" {
		t.Errorf("unexpected Constraints: %v", ps.Constraints)
	}
}

func TestClassify_NoiseSignal(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	promptPath := writePromptFile(t, dir)

	mock := newMockLLMClient()
	mock.addResponse(domain.CompletionResponse{
		Content: noiseClassifyResponse(),
		Model:   "test-model",
	})

	cfg := Config{
		Model:      "test-model",
		BatchSize:  20,
		PromptPath: promptPath,
	}
	classifier := New(mock, cfg)
	signal := newTestSignal("noise-1")

	results, failures := classifier.Classify(context.Background(), []domain.RawSignal{signal})
	if len(failures) > 0 {
		t.Fatalf("unexpected failures: %v", failures[0].Err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	ps := results[0]
	if ps.IsProblemSignal {
		t.Error("expected IsProblemSignal false for noise")
	}
	if ps.Relevance != 0.0 {
		t.Errorf("expected Relevance 0, got %f", ps.Relevance)
	}
	if ps.Problem != "" {
		t.Errorf("expected empty Problem for noise, got %q", ps.Problem)
	}
	if ps.RawSignalID != "noise-1" {
		t.Errorf("expected RawSignalID 'noise-1', got %q", ps.RawSignalID)
	}
}

func TestClassify_MalformedJSON(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	promptPath := writePromptFile(t, dir)

	mock := newMockLLMClient()
	mock.addResponse(domain.CompletionResponse{
		Content: `{"is_problem_signal": true, "relevance": "not-a-number"`, // invalid JSON + wrong type
		Model:   "test-model",
	})

	cfg := Config{
		Model:      "test-model",
		BatchSize:  20,
		PromptPath: promptPath,
	}
	classifier := New(mock, cfg)
	signal := newTestSignal("bad-json")

	results, failures := classifier.Classify(context.Background(), []domain.RawSignal{signal})
	if len(results) != 0 {
		t.Errorf("expected 0 results for malformed JSON, got %d", len(results))
	}
	if len(failures) != 1 {
		t.Fatalf("expected 1 failure, got %d", len(failures))
	}
	if failures[0].RawSignalID != "bad-json" {
		t.Errorf("expected RawSignalID 'bad-json', got %q", failures[0].RawSignalID)
	}
}

func TestClassify_PartialFailure(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	promptPath := writePromptFile(t, dir)

	mock := newMockLLMClient()
	// Signal 1 succeeds.
	mock.addResponse(domain.CompletionResponse{
		Content: validClassifyResponse(),
		Model:   "test-model",
	})
	// Signal 2 fails (LLM error).
	mock.addErr(assertiveError{msg: "LLM rate limit exceeded"})
	// Signal 3 succeeds.
	mock.addResponse(domain.CompletionResponse{
		Content: validClassifyResponse(),
		Model:   "test-model",
	})

	cfg := Config{
		Model:      "test-model",
		BatchSize:  20,
		PromptPath: promptPath,
	}
	classifier := New(mock, cfg)
	signals := []domain.RawSignal{
		newTestSignal("sig-ok-1"),
		newTestSignal("sig-fail"),
		newTestSignal("sig-ok-2"),
	}

	results, failures := classifier.Classify(context.Background(), signals)
	if len(results) != 2 {
		t.Errorf("expected 2 success results, got %d", len(results))
	}
	if len(failures) != 1 {
		t.Fatalf("expected 1 failure, got %d", len(failures))
	}
	if failures[0].RawSignalID != "sig-fail" {
		t.Errorf("expected failure for 'sig-fail', got %q", failures[0].RawSignalID)
	}

	// Verify the successful results have correct raw signal IDs.
	seen := make(map[string]bool)
	for _, ps := range results {
		seen[ps.RawSignalID] = true
	}
	if !seen["sig-ok-1"] {
		t.Error("missing result for sig-ok-1")
	}
	if !seen["sig-ok-2"] {
		t.Error("missing result for sig-ok-2")
	}
	if seen["sig-fail"] {
		t.Error("unexpected result for sig-fail (should have failed)")
	}
}

func TestClassify_BatchProcessing(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	promptPath := writePromptFile(t, dir)

	mock := newMockLLMClient()
	// Add responses for 4 signals.
	for i := 0; i < 4; i++ {
		mock.addResponse(domain.CompletionResponse{
			Content: validClassifyResponse(),
			Model:   "test-model",
		})
	}

	cfg := Config{
		Model:      "test-model",
		BatchSize:  2, // Process 2 at a time.
		PromptPath: promptPath,
	}
	classifier := New(mock, cfg)
	signals := []domain.RawSignal{
		newTestSignal("sig-1"),
		newTestSignal("sig-2"),
		newTestSignal("sig-3"),
		newTestSignal("sig-4"),
	}

	results, failures := classifier.Classify(context.Background(), signals)
	if len(failures) > 0 {
		t.Fatalf("unexpected failures: %v", failures[0].Err)
	}
	if len(results) != 4 {
		t.Fatalf("expected 4 results, got %d", len(results))
	}

	// Check that the prompt was rendered for each signal.
	if len(mock.requests) != 4 {
		t.Fatalf("expected 4 LLM requests, got %d", len(mock.requests))
	}

	// Each request should have the Model field set.
	for i, req := range mock.requests {
		if req.Model != "test-model" {
			t.Errorf("request %d: expected model 'test-model', got %q", i, req.Model)
		}
		if req.Prompt == "" {
			t.Errorf("request %d: expected non-empty prompt", i)
		}
	}
}

func TestClassify_PromptFileMissing(t *testing.T) {
	t.Parallel()

	mock := newMockLLMClient()
	cfg := Config{
		Model:      "test-model",
		BatchSize:  20,
		PromptPath: "/nonexistent/prompt.txt",
	}
	classifier := New(mock, cfg)
	signal := newTestSignal("sig-1")

	results, failures := classifier.Classify(context.Background(), []domain.RawSignal{signal})
	if len(results) != 0 {
		t.Errorf("expected 0 results for missing prompt, got %d", len(results))
	}
	if len(failures) != 1 {
		t.Fatalf("expected 1 failure, got %d", len(failures))
	}
	if !strings.Contains(failures[0].Err.Error(), "read prompt file") {
		t.Errorf("expected 'read prompt file' error, got %v", failures[0].Err)
	}
}

func TestClassify_AllFailures(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	promptPath := writePromptFile(t, dir)

	mock := newMockLLMClient()
	mock.addErr(assertiveError{msg: "model unavailable"})
	mock.addErr(assertiveError{msg: "rate limited"})
	mock.addErr(assertiveError{msg: "timeout"})

	cfg := Config{
		Model:      "test-model",
		BatchSize:  20,
		PromptPath: promptPath,
	}
	classifier := New(mock, cfg)
	signals := []domain.RawSignal{
		newTestSignal("sig-a"),
		newTestSignal("sig-b"),
		newTestSignal("sig-c"),
	}

	results, failures := classifier.Classify(context.Background(), signals)
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
	if len(failures) != 3 {
		t.Fatalf("expected 3 failures, got %d", len(failures))
	}
}

func TestClassify_EmptySignals(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	promptPath := writePromptFile(t, dir)

	mock := newMockLLMClient()
	cfg := Config{
		Model:      "test-model",
		BatchSize:  20,
		PromptPath: promptPath,
	}
	classifier := New(mock, cfg)

	results, failures := classifier.Classify(context.Background(), nil)
	if len(results) != 0 {
		t.Errorf("expected 0 results for nil input, got %d", len(results))
	}
	if len(failures) != 0 {
		t.Errorf("expected 0 failures for nil input, got %d", len(failures))
	}

	results, failures = classifier.Classify(context.Background(), []domain.RawSignal{})
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty input, got %d", len(results))
	}
	if len(failures) != 0 {
		t.Errorf("expected 0 failures for empty input, got %d", len(failures))
	}
}

func TestClassify_ContextCancellation(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	promptPath := writePromptFile(t, dir)

	mock := newMockLLMClient()
	// The mock returns normally, but we cancel the context before calling.
	mock.addResponse(domain.CompletionResponse{
		Content: validClassifyResponse(),
		Model:   "test-model",
	})

	cfg := Config{
		Model:      "test-model",
		BatchSize:  20,
		PromptPath: promptPath,
	}
	classifier := New(mock, cfg)
	signal := newTestSignal("sig-cancel")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	results, failures := classifier.Classify(ctx, []domain.RawSignal{signal})
	// The behavior depends on whether the mock checks ctx or not.
	// Our mock doesn't check ctx, so it will return success.
	// This test at minimum verifies no panic.
	_ = results
	_ = failures
}

func TestClassify_PromptRenderingWithSignal(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	promptPath := writePromptFile(t, dir)

	mock := newMockLLMClient()
	mock.addResponse(domain.CompletionResponse{
		Content: validClassifyResponse(),
		Model:   "test-model",
	})

	cfg := Config{
		Model:      "test-model",
		BatchSize:  20,
		PromptPath: promptPath,
	}
	classifier := New(mock, cfg)
	signal := newTestSignal("render-test")

	_, failures := classifier.Classify(context.Background(), []domain.RawSignal{signal})
	if len(failures) > 0 {
		t.Fatalf("unexpected failures: %v", failures[0].Err)
	}

	// Verify the rendered prompt contains signal data.
	if len(mock.requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(mock.requests))
	}
	prompt := mock.requests[0].Prompt

	if !strings.Contains(prompt, "Test title for render-test") {
		t.Errorf("expected signal title in prompt, got: %s", prompt)
	}
	if !strings.Contains(prompt, "This is a test body describing a recurring problem.") {
		t.Errorf("expected signal body in prompt, got: %s", prompt)
	}
	if !strings.Contains(prompt, "Comment 1") {
		t.Errorf("expected formatted comments in prompt, got: %s", prompt)
	}
	if !strings.Contains(prompt, "test-source") {
		t.Errorf("expected source in prompt, got: %s", prompt)
	}
	if !strings.Contains(prompt, "https://example.com/post/render-test") {
		t.Errorf("expected URL in prompt, got: %s", prompt)
	}
}

func TestClassify_PromptRenderingNoComments(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	promptPath := writePromptFile(t, dir)

	mock := newMockLLMClient()
	mock.addResponse(domain.CompletionResponse{
		Content: validClassifyResponse(),
		Model:   "test-model",
	})

	cfg := Config{
		Model:      "test-model",
		BatchSize:  20,
		PromptPath: promptPath,
	}
	classifier := New(mock, cfg)
	signal := newEmptyCommentsSignal("no-comments")

	_, failures := classifier.Classify(context.Background(), []domain.RawSignal{signal})
	if len(failures) > 0 {
		t.Fatalf("unexpected failures: %v", failures[0].Err)
	}

	if len(mock.requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(mock.requests))
	}
	prompt := mock.requests[0].Prompt

	if !strings.Contains(prompt, "Comments: none") {
		t.Errorf("expected 'Comments: none' in prompt for signal with no comments, got: %s", prompt)
	}
}

func TestClassify_ModelFromConfig(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	promptPath := writePromptFile(t, dir)

	mock := newMockLLMClient()
	mock.addResponse(domain.CompletionResponse{
		Content: validClassifyResponse(),
		Model:   "test-model-v2",
	})

	cfg := Config{
		Model:      "custom-model-name",
		BatchSize:  20,
		PromptPath: promptPath,
	}
	classifier := New(mock, cfg)
	signal := newTestSignal("model-test")

	_, failures := classifier.Classify(context.Background(), []domain.RawSignal{signal})
	if len(failures) > 0 {
		t.Fatalf("unexpected failures: %v", failures[0].Err)
	}

	if len(mock.requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(mock.requests))
	}
	if mock.requests[0].Model != "custom-model-name" {
		t.Errorf("expected model 'custom-model-name', got %q", mock.requests[0].Model)
	}
}

// assertiveError is a simple error implementation for testing.
type assertiveError struct{ msg string }

func (e assertiveError) Error() string { return e.msg }
