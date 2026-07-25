package openrouter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/moontechs/signalforge/internal/config"
	"github.com/moontechs/signalforge/internal/domain"
)

// testRequest represents a recorded HTTP request for test assertions.
type testRequest struct {
	Method string
	Path   string
	Body   map[string]any
	Header map[string]string
}

// newTestServer creates an httptest.Server that records requests and
// responds with the given body.
func newTestServer(t *testing.T, body string) (*httptest.Server, *[]testRequest) {
	t.Helper()
	var requests []testRequest
	var mu sync.Mutex

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Record request.
		record := testRequest{
			Method: r.Method,
			Path:   r.URL.Path,
			Header: make(map[string]string),
		}
		record.Header["Authorization"] = r.Header.Get("Authorization")
		record.Header["Content-Type"] = r.Header.Get("Content-Type")

		if r.Body != nil {
			var bodyMap map[string]any
			if err := json.NewDecoder(r.Body).Decode(&bodyMap); err == nil {
				record.Body = bodyMap
			}
		}

		mu.Lock()
		requests = append(requests, record)
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, body)
	}))

	t.Cleanup(ts.Close)
	return ts, &requests
}

// TestNew validates the constructor.
func TestNew(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		apiKey  string
		wantErr error
	}{
		{"empty api key returns ErrNoAPIKey", "", ErrNoAPIKey},
		{"valid api key succeeds", "sk-valid-key", nil},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := config.OpenRouterConfig{
				BaseURL:               "https://openrouter.ai/api/v1",
				RequestTimeoutSeconds: 30,
			}
			client, err := New(&cfg, tt.apiKey)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("New() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil && client == nil {
				t.Error("New() returned nil client")
			}
		})
	}
}

// TestStats verifies Stats tracking.
func TestStats(t *testing.T) {
	t.Parallel()

	ts, _ := newTestServer(t, `{"id":"1","object":"chat.completion","created":123,"model":"test-model","choices":[{"index":0,"message":{"role":"assistant","content":"{\"is_problem_signal\":false}"}}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`)
	cfg := config.OpenRouterConfig{
		BaseURL:               ts.URL,
		Model:                 "test-model",
		RequestTimeoutSeconds: 30,
		MaxRetries:            0,
		MaxOutputTokens:       100,
		ClassificationTemp:    0.1,
	}

	client, err := New(&cfg, "sk-test")
	if err != nil {
		t.Fatal(err)
	}

	// Initial stats should be zero.
	if s := client.Stats(); s.Attempts != 0 {
		t.Errorf("Stats() attempts = %d, want 0", s.Attempts)
	}

	// Make a request.
	_, err = client.Complete(context.Background(), domain.CompletionRequest{
		Prompt: "test",
	})
	if err != nil {
		t.Fatal(err)
	}

	if s := client.Stats(); s.Attempts != 1 {
		t.Errorf("Stats() attempts = %d, want 1", s.Attempts)
	}
}

// TestCompleteSuccess verifies a successful completion round-trip.
func TestCompleteSuccess(t *testing.T) {
	t.Parallel()

	ts, requests := newTestServer(t, `{
		"id":"chatcmpl-123",
		"object":"chat.completion",
		"created":1234567890,
		"model":"gpt-4",
		"choices":[{
			"index":0,
			"message":{"role":"assistant","content":"{\"is_problem_signal\":false}"}
		}],
		"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}
	}`)

	cfg := config.OpenRouterConfig{
		BaseURL:               ts.URL,
		Model:                 "gpt-4",
		RequestTimeoutSeconds: 30,
		MaxRetries:            0,
		MaxOutputTokens:       100,
		ClassificationTemp:    0.1,
	}

	client, err := New(&cfg, "sk-test")
	if err != nil {
		t.Fatal(err)
	}

	resp, err := client.Complete(context.Background(), domain.CompletionRequest{
		System: "You are a classifier.",
		Prompt: "Classify this post.",
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	if resp.Content != `{"is_problem_signal":false}` {
		t.Errorf("Content = %q, want %q", resp.Content, `{"is_problem_signal":false}`)
	}
	if resp.Model != "gpt-4" {
		t.Errorf("Model = %q, want %q", resp.Model, "gpt-4")
	}
	if resp.Usage == nil {
		t.Fatal("Usage is nil")
	}
	if resp.Usage.TotalTokens != 15 {
		t.Errorf("TotalTokens = %d, want 15", resp.Usage.TotalTokens)
	}

	// Verify request details.
	if len(*requests) != 1 {
		t.Fatalf("got %d requests, want 1", len(*requests))
	}
	req := (*requests)[0]

	if req.Method != http.MethodPost {
		t.Errorf("Method = %s, want POST", req.Method)
	}
	if req.Path != "/chat/completions" {
		t.Errorf("Path = %s, want /chat/completions", req.Path)
	}
	if req.Header["Authorization"] != "Bearer sk-test" {
		t.Errorf("Authorization = %q, want %q", req.Header["Authorization"], "Bearer sk-test")
	}

	// Verify model in body.
	if model, ok := req.Body["model"]; !ok || model != "gpt-4" {
		t.Errorf("Body model = %v, want %q", model, "gpt-4")
	}
}

// TestFreeModelSuffix verifies the :free suffix is preserved.
func TestFreeModelSuffix(t *testing.T) {
	t.Parallel()

	ts, requests := newTestServer(t, `{
		"id":"1","object":"chat.completion","created":123,"model":"mistral:free",
		"choices":[{"index":0,"message":{"role":"assistant","content":"{}"}}],
		"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}
	}`)

	cfg := config.OpenRouterConfig{
		BaseURL:               ts.URL,
		Model:                 "",
		RequestTimeoutSeconds: 30,
		MaxRetries:            0,
		MaxOutputTokens:       100,
		ClassificationTemp:    0.1,
	}

	client, err := New(&cfg, "sk-test")
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.Complete(context.Background(), domain.CompletionRequest{
		Model:  "mistral:free",
		Prompt: "test",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(*requests) != 1 {
		t.Fatalf("got %d requests, want 1", len(*requests))
	}

	model, ok := (*requests)[0].Body["model"]
	if !ok || model != "mistral:free" {
		t.Errorf("Body model = %v, want %q", model, "mistral:free")
	}
}

// TestModelFallback verifies the model resolution chain.
func TestModelFallback(t *testing.T) {
	t.Parallel()

	callCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")

		// First and second models fail with 4xx.
		if callCount <= 2 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprint(w, `{"error":{"message":"bad request","type":"invalid"}}`)
			return
		}

		// Third model succeeds.
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{
			"id":"1","object":"chat.completion","created":123,"model":"gpt-3.5-turbo",
			"choices":[{"index":0,"message":{"role":"assistant","content":"{}"}}],
			"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}
		}`)
	}))
	t.Cleanup(ts.Close)

	cfg := config.OpenRouterConfig{
		BaseURL:               ts.URL,
		Model:                 "gpt-4",
		FallbackModels:        []string{"claude-3", "gpt-3.5-turbo"},
		RequestTimeoutSeconds: 30,
		MaxRetries:            0,
		MaxOutputTokens:       100,
		ClassificationTemp:    0.1,
	}

	client, err := New(&cfg, "sk-test")
	if err != nil {
		t.Fatal(err)
	}

	resp, err := client.Complete(context.Background(), domain.CompletionRequest{
		Prompt: "test",
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	if resp.Model != "gpt-3.5-turbo" {
		t.Errorf("Model = %q, want %q", resp.Model, "gpt-3.5-turbo")
	}
	if callCount != 3 {
		t.Errorf("callCount = %d, want 3", callCount)
	}
}

// TestAllModelsFail verifies ErrAllModelsFailed when no model works.
func TestAllModelsFail(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprint(w, `{"error":{"message":"bad request","type":"invalid"}}`)
	}))
	t.Cleanup(ts.Close)

	cfg := config.OpenRouterConfig{
		BaseURL:               ts.URL,
		Model:                 "gpt-4",
		FallbackModels:        []string{"claude-3"},
		RequestTimeoutSeconds: 30,
		MaxRetries:            0,
		MaxOutputTokens:       100,
		ClassificationTemp:    0.1,
	}

	client, err := New(&cfg, "sk-test")
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.Complete(context.Background(), domain.CompletionRequest{
		Prompt: "test",
	})
	if !errors.Is(err, ErrAllModelsFailed) {
		t.Errorf("Complete() error = %v, want ErrAllModelsFailed", err)
	}
}

// TestContextCancellation verifies that a cancelled context stops the request.
func TestContextCancellation(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		// Wait for context cancellation.
		<-r.Context().Done()
	}))
	t.Cleanup(ts.Close)

	cfg := config.OpenRouterConfig{
		BaseURL:               ts.URL,
		Model:                 "test-model",
		RequestTimeoutSeconds: 5,
		MaxRetries:            0,
		MaxOutputTokens:       100,
		ClassificationTemp:    0.1,
	}

	client, err := New(&cfg, "sk-test")
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	_, err = client.Complete(ctx, domain.CompletionRequest{
		Prompt: "test",
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Complete() error = %v, want context.Canceled", err)
	}
}

// TestMalformedJSONResponse verifies that malformed JSON is detected.
func TestMalformedJSONResponse(t *testing.T) {
	t.Parallel()

	ts, _ := newTestServer(t, `{invalid json`)
	cfg := config.OpenRouterConfig{
		BaseURL:               ts.URL,
		Model:                 "test-model",
		RequestTimeoutSeconds: 30,
		MaxRetries:            0,
		MaxOutputTokens:       100,
		ClassificationTemp:    0.1,
	}

	client, err := New(&cfg, "sk-test")
	if err != nil {
		t.Fatal(err)
	}

	// The response body itself is invalid JSON, so ChatCompletionResponse
	// unmarshal will fail.
	_, err = client.Complete(context.Background(), domain.CompletionRequest{
		Prompt: "test",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// TestSchemaValidation verifies the schema validation with hints and ranges.
func TestSchemaValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		response string
		wantErr  bool
	}{
		{
			name: "valid problem signal",
			response: `{
				"is_problem_signal":true,
				"relevance":0.8,
				"problem":"Cannot manage secrets easily",
				"severity_hint":7,
				"frequency_hint":8,
				"payment_hint":6,
				"frustration_hint":9
			}`,
			wantErr: false,
		},
		{
			name: "relevance out of range",
			response: `{
				"is_problem_signal":true,
				"relevance":1.5
			}`,
			wantErr: true,
		},
		{
			name: "severity_hint out of range",
			response: `{
				"is_problem_signal":true,
				"relevance":0.5,
				"severity_hint":11
			}`,
			wantErr: true,
		},
		{
			name: "hint negative",
			response: `{
				"is_problem_signal":true,
				"relevance":0.5,
				"frequency_hint":-1
			}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ts, _ := newTestServer(t, fmt.Sprintf(`{
				"id":"1","object":"chat.completion","created":123,"model":"test",
				"choices":[{"index":0,"message":{"role":"assistant","content":%s}}],
				"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}
			}`, jsonMarshalString(t, tt.response)))

			cfg := config.OpenRouterConfig{
				BaseURL:               ts.URL,
				Model:                 "test-model",
				RequestTimeoutSeconds: 30,
				MaxRetries:            0,
				MaxOutputTokens:       100,
				ClassificationTemp:    0.1,
			}

			client, err := New(&cfg, "sk-test")
			if err != nil {
				t.Fatal(err)
			}

			var schema domain.ProblemSignal
			_, err = client.Complete(context.Background(), domain.CompletionRequest{
				Prompt: "test",
				Schema: &schema,
			})
			if (err != nil) != tt.wantErr {
				t.Errorf("Complete() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

// TestAPIKeyNotLeaked verifies API key is never in error messages.
func TestAPIKeyNotLeaked(t *testing.T) {
	t.Parallel()

	ts, _ := newTestServer(t, `{"id":"1","object":"chat.completion","created":123,"model":"test","choices":[{"index":0,"message":{"role":"assistant","content":"ok"}}],"usage":{}}`)
	cfg := config.OpenRouterConfig{
		BaseURL:               ts.URL,
		Model:                 "test-model",
		RequestTimeoutSeconds: 30,
		MaxRetries:            0,
		MaxOutputTokens:       100,
		ClassificationTemp:    0.1,
	}

	// Cause a 4xx error to check error messages.
	ts2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprint(w, `{"error":{"message":"invalid_api_key","type":"auth_error"}}`)
	}))
	t.Cleanup(ts2.Close)

	client2, err := New(&cfg, "sk-secret-key-value")
	if err != nil {
		t.Fatal(err)
	}
	client2.cfg.BaseURL = ts2.URL

	_, err = client2.Complete(context.Background(), domain.CompletionRequest{
		Prompt: "test",
	})
	if err != nil {
		errMsg := err.Error()
		if contains(errMsg, "sk-secret-key-value") {
			t.Errorf("error message leaks API key: %s", errMsg)
		}
	}
}

// TestNoModel verifies ErrNoModel when no model is configured.
func TestNoModel(t *testing.T) {
	t.Parallel()

	cfg := config.OpenRouterConfig{
		BaseURL:               "https://openrouter.ai/api/v1",
		Model:                 "",
		FallbackModels:        nil,
		RequestTimeoutSeconds: 30,
	}

	client, err := New(&cfg, "sk-test")
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.Complete(context.Background(), domain.CompletionRequest{
		Prompt: "test",
	})
	if !errors.Is(err, ErrNoModel) {
		t.Errorf("Complete() error = %v, want ErrNoModel", err)
	}
}

// TestResolveModels verifies the model resolution order and dedup.
func TestResolveModels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		reqModel  string
		cfgModel  string
		fallbacks []string
		want      []string
	}{
		{
			name:      "req model only",
			reqModel:  "gpt-4",
			cfgModel:  "",
			fallbacks: nil,
			want:      []string{"gpt-4"},
		},
		{
			name:      "cfg model fallback",
			reqModel:  "",
			cfgModel:  "gpt-4",
			fallbacks: []string{"claude-3"},
			want:      []string{"gpt-4", "claude-3"},
		},
		{
			name:      "dedup identical models",
			reqModel:  "gpt-4",
			cfgModel:  "gpt-4",
			fallbacks: []string{"gpt-4"},
			want:      []string{"gpt-4"},
		},
		{
			name:      "empty strings removed",
			reqModel:  "  ",
			cfgModel:  "gpt-4",
			fallbacks: []string{"", "claude-3"},
			want:      []string{"gpt-4", "claude-3"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := resolveModels(tt.reqModel, tt.cfgModel, tt.fallbacks)
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("got[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// TestParseRetryAfter verifies Retry-After header parsing.
func TestParseRetryAfter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		header string
		min    time.Duration
		max    time.Duration
	}{
		{"5", 5 * time.Second, 6 * time.Second},
		{"120", 120 * time.Second, 121 * time.Second},
		{"invalid", 5 * time.Second, 6 * time.Second}, // fallback to 5s
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.header, func(t *testing.T) {
			t.Parallel()
			got := parseRetryAfter(tt.header)
			if got < tt.min || got > tt.max {
				t.Errorf("parseRetryAfter(%q) = %v, want between %v and %v", tt.header, got, tt.min, tt.max)
			}
		})
	}
}

// TestValidateResponseJSON verifies JSON validity checks.
func TestValidateResponseJSON(t *testing.T) {
	t.Parallel()

	client := &Client{} // minimal client for method access

	tests := []struct {
		name    string
		content string
		schema  any
		wantErr bool
	}{
		{"valid json, no schema", `{"key":"value"}`, nil, false},
		{"invalid json", `{bad`, nil, true},
		{"valid with schema", `{"relevance":0.5}`, &domain.ProblemSignal{}, false},
		{"relevance out of range", `{"relevance":5.0}`, &domain.ProblemSignal{}, true},
		{"severity hint out of range", `{"severity_hint":15}`, &domain.ProblemSignal{}, true},
		{"valid hints", `{"severity_hint":5,"frequency_hint":3,"payment_hint":7,"frustration_hint":9}`, &domain.ProblemSignal{}, false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := client.validateResponse(tt.content, tt.schema)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateResponse() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

// jsonMarshalString is a helper that JSON-marshals a string value.
func jsonMarshalString(t *testing.T, s string) string {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Test429RetryWithRetryAfter verifies that a 429 response with Retry-After
// causes a retry and eventually succeeds.
func Test429RetryWithRetryAfter(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	callCount := 0

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		callCount++
		count := callCount
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")

		if count == 1 {
			// First call: 429 with Retry-After: 0 (no real wait).
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = fmt.Fprint(w, `{"error":{"message":"rate limited","type":"rate_limit"}}`)
			return
		}

		// Second call: success.
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{
			"id":"1","object":"chat.completion","created":123,"model":"test-model",
			"choices":[{"index":0,"message":{"role":"assistant","content":"{\"is_problem_signal\":false}"}}],
			"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}
		}`)
	}))
	t.Cleanup(ts.Close)

	cfg := config.OpenRouterConfig{
		BaseURL:               ts.URL,
		Model:                 "test-model",
		RequestTimeoutSeconds: 30,
		MaxRetries:            3,
		MaxOutputTokens:       100,
		ClassificationTemp:    0.1,
	}

	client, err := New(&cfg, "sk-test")
	if err != nil {
		t.Fatal(err)
	}

	resp, err := client.Complete(context.Background(), domain.CompletionRequest{
		Prompt: "test",
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if resp.Content != `{"is_problem_signal":false}` {
		t.Errorf("Content = %q, want %q", resp.Content, `{"is_problem_signal":false}`)
	}

	mu.Lock()
	if callCount != 2 {
		t.Errorf("callCount = %d, want 2 (one 429, one success)", callCount)
	}
	mu.Unlock()
}

// Test5xxFallback verifies that 5xx errors cause retries within a model, and
// if all retries are exhausted, the client falls back to the next model.
func Test5xxFallback(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	// Track calls per model path (we can distinguish by reading the body).
	callCount := 0

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		callCount++
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")

		// Always return 500 for all models — all retries + fallbacks exhausted.
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, `{"error":{"message":"internal error","type":"server_error"}}`)
	}))
	t.Cleanup(ts.Close)

	cfg := config.OpenRouterConfig{
		BaseURL:               ts.URL,
		Model:                 "gpt-4",
		FallbackModels:        []string{"claude-3"},
		RequestTimeoutSeconds: 30,
		MaxRetries:            2, // 3 attempts per model (0,1,2)
		MaxOutputTokens:       100,
		ClassificationTemp:    0.1,
	}

	client, err := New(&cfg, "sk-test")
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.Complete(context.Background(), domain.CompletionRequest{
		Prompt: "test",
	})
	if !errors.Is(err, ErrAllModelsFailed) {
		t.Errorf("Complete() error = %v, want ErrAllModelsFailed", err)
	}

	// Two models × (MaxRetries+1) attempts each = 2 * 3 = 6
	mu.Lock()
	if callCount != 6 {
		t.Errorf("callCount = %d, want 6 (2 models × 3 attempts)", callCount)
	}
	mu.Unlock()
}

// TestRepair verifies the JSON repair flow:
//   - Valid JSON passes through without repair.
//   - Malformed JSON gets repaired once when the repair request succeeds.
//   - When repair also returns invalid JSON, the client returns ErrRepairFailed.
func TestRepair(t *testing.T) {
	t.Parallel()

	t.Run("valid JSON passes without repair", func(t *testing.T) {
		t.Parallel()

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, `{
				"id":"1","object":"chat.completion","created":123,"model":"test-model",
				"choices":[{"index":0,"message":{"role":"assistant","content":"{\"is_problem_signal\":false}"}}],
				"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}
			}`)
		}))
		t.Cleanup(ts.Close)

		cfg := config.OpenRouterConfig{
			BaseURL:               ts.URL,
			Model:                 "test-model",
			RequestTimeoutSeconds: 30,
			MaxRetries:            0,
			MaxOutputTokens:       100,
			ClassificationTemp:    0.1,
		}

		client, err := New(&cfg, "sk-test")
		if err != nil {
			t.Fatal(err)
		}

		resp, err := client.Complete(context.Background(), domain.CompletionRequest{
			Prompt: "test",
		})
		if err != nil {
			t.Fatalf("Complete() error = %v", err)
		}
		if resp.Content != `{"is_problem_signal":false}` {
			t.Errorf("Content = %q, want %q", resp.Content, `{"is_problem_signal":false}`)
		}
	})

	t.Run("malformed JSON gets repaired once", func(t *testing.T) {
		t.Parallel()

		var repairRequested bool
		var mu sync.Mutex

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")

			// Detect repair request by looking for "fix it" in the body.
			buf := make([]byte, 1024)
			n, _ := r.Body.Read(buf)
			bodyStr := string(buf[:n])
			r.Body.Close()

			if contains(bodyStr, "fix it") || contains(bodyStr, "corrected JSON") || contains(bodyStr, "Fix the following") {
				// This is a repair request — return valid JSON.
				mu.Lock()
				repairRequested = true
				mu.Unlock()
				w.WriteHeader(http.StatusOK)
				_, _ = fmt.Fprint(w, `{
					"id":"2","object":"chat.completion","created":456,"model":"test-model",
					"choices":[{"index":0,"message":{"role":"assistant","content":"{\"is_problem_signal\":true,\"relevance\":0.9}"}}],
					"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}
				}`)
				return
			}

			// Original request — return content that fails schema validation
			// (relevance out of range triggers a validation error, which triggers repair).
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, `{
				"id":"1","object":"chat.completion","created":123,"model":"test-model",
				"choices":[{"index":0,"message":{"role":"assistant","content":"{\"is_problem_signal\":true,\"relevance\":1.5}"}}],
				"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}
			}`)
		}))
		t.Cleanup(ts.Close)

		cfg := config.OpenRouterConfig{
			BaseURL:               ts.URL,
			Model:                 "test-model",
			RequestTimeoutSeconds: 30,
			MaxRetries:            0,
			MaxOutputTokens:       100,
			ClassificationTemp:    0.1,
		}

		client, err := New(&cfg, "sk-test")
		if err != nil {
			t.Fatal(err)
		}

		var schema domain.ProblemSignal
		resp, err := client.Complete(context.Background(), domain.CompletionRequest{
			Prompt: "test",
			Schema: &schema,
		})
		if err != nil {
			t.Fatalf("Complete() error = %v", err)
		}

		mu.Lock()
		if !repairRequested {
			t.Error("repair was not requested")
		}
		mu.Unlock()

		// Content should be the repaired (valid) JSON.
		if resp.Content != `{"is_problem_signal":true,"relevance":0.9}` {
			t.Errorf("Content = %q, want %q", resp.Content, `{"is_problem_signal":true,"relevance":0.9}`)
		}
	})

	t.Run("repair also returns invalid JSON, fails with ErrRepairFailed", func(t *testing.T) {
		t.Parallel()

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")

			buf := make([]byte, 1024)
			n, _ := r.Body.Read(buf)
			bodyStr := string(buf[:n])
			r.Body.Close()

			if contains(bodyStr, "fix it") || contains(bodyStr, "corrected JSON") || contains(bodyStr, "Fix the following") {
				// Repair also returns invalid JSON (relevance still out of range).
				w.WriteHeader(http.StatusOK)
				_, _ = fmt.Fprint(w, `{
					"id":"2","object":"chat.completion","created":456,"model":"test-model",
					"choices":[{"index":0,"message":{"role":"assistant","content":"{\"is_problem_signal\":true,\"relevance\":2.0}"}}],
					"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}
				}`)
				return
			}

			// Original request — return content that fails validation.
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, `{
				"id":"1","object":"chat.completion","created":123,"model":"test-model",
				"choices":[{"index":0,"message":{"role":"assistant","content":"{\"is_problem_signal\":true,\"relevance\":1.5}"}}],
				"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}
			}`)
		}))
		t.Cleanup(ts.Close)

		cfg := config.OpenRouterConfig{
			BaseURL:               ts.URL,
			Model:                 "test-model",
			RequestTimeoutSeconds: 30,
			MaxRetries:            0,
			MaxOutputTokens:       100,
			ClassificationTemp:    0.1,
		}

		client, err := New(&cfg, "sk-test")
		if err != nil {
			t.Fatal(err)
		}

		var schema domain.ProblemSignal
		_, err = client.Complete(context.Background(), domain.CompletionRequest{
			Prompt: "test",
			Schema: &schema,
		})
		if !errors.Is(err, ErrRepairFailed) {
			t.Errorf("Complete() error = %v, want ErrRepairFailed", err)
		}
	})
}

// contains is a simple string contains check.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

// searchString finds substr in s.
func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
