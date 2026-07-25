package openrouter

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
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

	endpoint := strings.TrimRight(c.cfg.BaseURL, "/") + "/chat/completions"
	maxRetries := c.cfg.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}

	var lastErr error
	var delay time.Duration
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			if err := waitForRetry(ctx, delay); err != nil {
				return domain.CompletionResponse{}, err
			}
		}

		response, retry, retryAfter, err := c.modelAttempt(ctx, endpoint, bodyBytes, model, schema)
		if err == nil {
			return response, nil
		}
		if !retry {
			return domain.CompletionResponse{}, err
		}
		lastErr = err
		if retryAfter != nil {
			delay = *retryAfter
		} else {
			delay = retryBackoff(attempt + 1)
		}
	}

	if lastErr == nil {
		lastErr = ErrAllModelsFailed
	}
	return domain.CompletionResponse{}, fmt.Errorf("%w: %w", ErrAllModelsFailed, lastErr)
}

func (c *Client) modelAttempt(
	ctx context.Context,
	endpoint string,
	bodyBytes []byte,
	model string,
	schema any,
) (domain.CompletionResponse, bool, *time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return domain.CompletionResponse{}, false, nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return domain.CompletionResponse{}, true, nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return domain.CompletionResponse{}, true, nil, fmt.Errorf("read response: %w", err)
	}

	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		delay := parseRetryAfter(resp.Header.Get("Retry-After"))
		return domain.CompletionResponse{}, true, &delay, fmt.Errorf("%w: status %d", ErrRateLimited, resp.StatusCode)
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		return domain.CompletionResponse{}, false, nil, apiStatusError(resp.StatusCode, body)
	case resp.StatusCode >= 500:
		return domain.CompletionResponse{}, true, nil,
			fmt.Errorf("server error (status %d): %s", resp.StatusCode, truncateBody(string(body)))
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		return domain.CompletionResponse{}, true, nil,
			fmt.Errorf("unexpected status %d: %s", resp.StatusCode, truncateBody(string(body)))
	default:
		response, retry, err := c.parseModelResponse(ctx, model, body, schema)
		return response, retry, nil, err
	}
}

func apiStatusError(status int, body []byte) error {
	var apiErr APIError
	if err := json.Unmarshal(body, &apiErr); err == nil && apiErr.Err.Message != "" {
		return fmt.Errorf("api error (status %d): %s", status, apiErr.Err.Message)
	}
	return fmt.Errorf("api error (status %d): %s", status, truncateBody(string(body)))
}

func (c *Client) parseModelResponse(
	ctx context.Context,
	model string,
	body []byte,
	schema any,
) (domain.CompletionResponse, bool, error) {
	var chatResp ChatCompletionResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return domain.CompletionResponse{}, true, fmt.Errorf("%w: %w", ErrInvalidResponse, err)
	}
	if len(chatResp.Choices) == 0 {
		return domain.CompletionResponse{}, true, fmt.Errorf("%w: empty choices", ErrInvalidResponse)
	}

	content := chatResp.Choices[0].Message.Content
	validated, err := c.validateResponse(content, schema)
	if err != nil {
		repaired, repairErr := c.repairJSON(ctx, model, content)
		if repairErr != nil {
			return domain.CompletionResponse{}, false, repairErr
		}
		if _, validationErr := c.validateResponse(string(repaired), schema); validationErr != nil {
			return domain.CompletionResponse{}, false, ErrRepairFailed
		}
		content = string(repaired)
	} else {
		content = string(validated)
	}

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
	}, false, nil
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func retryBackoff(attempt int) time.Duration {
	const maximum = 30 * time.Second
	if attempt >= 5 {
		return maximum
	}
	delay := (time.Second << attempt) + randomJitter()
	return min(delay, maximum)
}

func randomJitter() time.Duration {
	value, err := cryptorand.Int(cryptorand.Reader, big.NewInt(1000))
	if err != nil {
		return 0
	}
	return time.Duration(value.Int64()) * time.Millisecond
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
