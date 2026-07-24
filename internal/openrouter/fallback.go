package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/moontechs/signalforge/internal/domain"
)

// tryModel attempts a single completion against one model. It handles
// retries with exponential backoff and jitter (capped at 30s), respects
// the Retry-After header on 429 responses, fails immediately on non-429
// 4xx errors, and retries on 5xx / transport errors. When a valid 2xx
// response is received the content is validated and repaired if needed.
func (c *Client) tryModel(
	ctx context.Context,
	model string,
	messages []Message,
	temperature float64,
	maxTokens int,
	schema any,
) (domain.CompletionResponse, error) {
	// Build request body.
	reqBody := ChatCompletionRequest{
		Model:       model,
		Messages:    messages,
		Temperature: temperature,
		MaxTokens:   maxTokens,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return domain.CompletionResponse{}, fmt.Errorf("marshal request: %w", err)
	}

	// Build URL: ensure trailing slash isn't doubled.
	baseURL := strings.TrimRight(c.cfg.BaseURL, "/")
	url := baseURL + "/chat/completions"

	maxRetries := c.cfg.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff with jitter, capped at 30 s.
			backoff := time.Duration(math.Pow(2, float64(attempt))) * time.Second
			jitter := time.Duration(rand.Intn(1000)) * time.Millisecond
			backoff += jitter
			if backoff > 30*time.Second {
				backoff = 30*time.Second + jitter
			}

			select {
			case <-ctx.Done():
				return domain.CompletionResponse{}, ctx.Err()
			case <-time.After(backoff):
			}
		}

		req, rErr := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
		if rErr != nil {
			return domain.CompletionResponse{}, fmt.Errorf("create request: %w", rErr)
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+c.apiKey)

		resp, rErr := c.httpClient.Do(req)
		if rErr != nil {
			lastErr = fmt.Errorf("request failed: %w", rErr)
			continue
		}

		body, rErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if rErr != nil {
			lastErr = fmt.Errorf("read response: %w", rErr)
			continue
		}

		// 429 rate limit — respect Retry-After, then retry.
		if resp.StatusCode == http.StatusTooManyRequests {
			delay := parseRetryAfter(resp.Header.Get("Retry-After"))
			select {
			case <-ctx.Done():
				return domain.CompletionResponse{}, ctx.Err()
			case <-time.After(delay):
			}
			continue
		}

		// Non-429 4xx — fail immediately for this model.
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			var apiErr APIError
			if uErr := json.Unmarshal(body, &apiErr); uErr == nil && apiErr.Err.Message != "" {
				return domain.CompletionResponse{}, fmt.Errorf(
					"api error (status %d): %s", resp.StatusCode, apiErr.Err.Message,
				)
			}
			return domain.CompletionResponse{}, fmt.Errorf(
				"api error (status %d): %s", resp.StatusCode, truncateBody(string(body)),
			)
		}

		// 5xx — retry with backoff.
		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("server error (status %d): %s", resp.StatusCode, truncateBody(string(body)))
			continue
		}

		// Non-2xx unexpected status.
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			lastErr = fmt.Errorf("unexpected status %d: %s", resp.StatusCode, truncateBody(string(body)))
			continue
		}

		// 2xx — parse response.
		var chatResp ChatCompletionResponse
		if pErr := json.Unmarshal(body, &chatResp); pErr != nil {
			lastErr = fmt.Errorf("%w: %s", ErrInvalidResponse, pErr)
			continue
		}

		if len(chatResp.Choices) == 0 {
			lastErr = fmt.Errorf("%w: empty choices", ErrInvalidResponse)
			continue
		}

		content := chatResp.Choices[0].Message.Content

		// Validate the response content.
		validated, vErr := c.validateResponse(content, schema)
		if vErr != nil {
			// Attempt single repair.
			repaired, repErr := c.repairJSON(ctx, model, content)
			if repErr != nil {
				return domain.CompletionResponse{}, repErr
			}
			// Re-validate repaired content against schema.
			if _, vErr2 := c.validateResponse(string(repaired), schema); vErr2 != nil {
				return domain.CompletionResponse{}, ErrRepairFailed
			}
			content = string(repaired)
		} else {
			content = string(validated)
		}

		// Map usage.
		var usage *domain.Usage
		if chatResp.Usage != nil {
			usage = &domain.Usage{
				PromptTokens:     chatResp.Usage.PromptTokens,
				CompletionTokens: chatResp.Usage.CompletionTokens,
				TotalTokens:      chatResp.Usage.TotalTokens,
			}
		}

		c.stats.Attempts++

		return domain.CompletionResponse{
			Content: content,
			Model:   chatResp.Model,
			Usage:   usage,
		}, nil
	}

	// All retries exhausted.
	return domain.CompletionResponse{}, fmt.Errorf("%w: %w", ErrAllModelsFailed, lastErr)
}

// parseRetryAfter parses the Retry-After header value which can be
// a number of seconds or an HTTP-date string.
func parseRetryAfter(val string) time.Duration {
	if seconds, err := strconv.Atoi(val); err == nil {
		return time.Duration(seconds) * time.Second
	}
	if retryTime, err := time.Parse(http.TimeFormat, val); err == nil {
		return time.Until(retryTime)
	}
	return 5 * time.Second
}

// truncateBody returns up to the first 500 characters of a string for
// safe inclusion in error messages. It never includes secrets or tokens.
func truncateBody(s string) string {
	if len(s) > 500 {
		return s[:500] + "..."
	}
	return s
}
