// Package openrouter provides an LLM client for the OpenRouter API.
package openrouter

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/moontechs/signalforge/internal/config"
	"github.com/moontechs/signalforge/internal/domain"
)

// Stats holds request statistics for the OpenRouter client.
type Stats struct {
	// Attempts is the number of LLM completion requests made.
	Attempts int
}

// Client communicates with the OpenRouter API using the OpenAI-compatible
// chat completions endpoint. It supports model fallback, retry with
// exponential backoff, JSON validation, and single-attempt repair.
type Client struct {
	httpClient *http.Client
	cfg        config.OpenRouterConfig
	apiKey     string
	stats      Stats
}

// New creates a new OpenRouter client. It returns ErrNoAPIKey if apiKey
// is empty. The client uses cfg.RequestTimeoutSeconds for its HTTP timeout.
func New(cfg *config.OpenRouterConfig, apiKey string) (*Client, error) {
	if apiKey == "" {
		return nil, ErrNoAPIKey
	}
	if cfg == nil {
		return nil, errors.New("openrouter config is nil")
	}

	timeout := time.Duration(cfg.RequestTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 120 * time.Second
	}

	return &Client{
		httpClient: &http.Client{
			Timeout: timeout,
		},
		cfg:    *cfg,
		apiKey: apiKey,
	}, nil
}

// Stats returns the current request statistics.
func (c *Client) Stats() Stats {
	return c.stats
}

// Complete implements domain.LLMClient. It resolves the model chain,
// builds chat messages from the request, and tries each model in order
// until one succeeds or all models are exhausted.
func (c *Client) Complete(
	ctx any,
	req domain.CompletionRequest, //nolint:gocritic // Value signature is required by domain.LLMClient.
) (domain.CompletionResponse, error) {
	requestCtx, ok := ctx.(context.Context)
	if !ok {
		requestCtx = context.Background()
	}

	// Build chat messages.
	messages := make([]Message, 0, 2)
	if req.System != "" {
		messages = append(messages, Message{Role: "system", Content: req.System})
	}
	messages = append(messages, Message{Role: "user", Content: req.Prompt})

	// Resolve model chain: req.Model -> cfg.Model -> cfg.FallbackModels.
	models := resolveModels(req.Model, c.cfg.Model, c.cfg.FallbackModels)
	if len(models) == 0 {
		return domain.CompletionResponse{}, ErrNoModel
	}

	// Determine temperature.
	temp := req.Temperature
	if temp == 0 {
		temp = c.cfg.ClassificationTemp
	}

	// Determine max tokens.
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = c.cfg.MaxOutputTokens
	}

	// Try each model in order.
	var lastErr error
	for _, model := range models {
		resp, err := c.tryModel(requestCtx, model, messages, temp, maxTokens, req.Schema)
		if err == nil {
			return resp, nil
		}
		// Terminal errors that apply across all models — stop immediately.
		if errors.Is(err, ErrNoAPIKey) {
			return domain.CompletionResponse{}, err
		}
		lastErr = err
	}

	if lastErr == nil {
		lastErr = ErrAllModelsFailed
	}
	return domain.CompletionResponse{}, fmt.Errorf("%w: %w", ErrAllModelsFailed, lastErr)
}

// resolveModels builds an ordered list of model strings, removing empty
// strings and duplicates. The order is: reqModel, cfgModel, fallbacks.
func resolveModels(reqModel, cfgModel string, fallbacks []string) []string {
	seen := make(map[string]bool)
	var models []string

	add := func(m string) {
		m = strings.TrimSpace(m)
		if m != "" && !seen[m] {
			seen[m] = true
			models = append(models, m)
		}
	}

	add(reqModel)
	add(cfgModel)
	for _, fb := range fallbacks {
		add(fb)
	}

	return models
}
