package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// repairJSON attempts one repair of invalid JSON by sending it back to the
// LLM with instructions to fix it. It uses cfg.RepairTemp (typically 0) and
// the same model. The repaired output is validated once. If it is still
// invalid, ErrRepairFailed is returned with no further retries.
func (c *Client) repairJSON(ctx context.Context, model, invalidContent string) ([]byte, error) {
	prompt := fmt.Sprintf(`The following JSON is invalid. Please fix it and return only valid JSON.

Invalid JSON:
%s

Return only the corrected JSON, nothing else.`, invalidContent)

	reqBody := ChatCompletionRequest{
		Model: model,
		Messages: []Message{
			{Role: "user", Content: prompt},
		},
		Temperature: c.cfg.RepairTemp,
		MaxTokens:   c.cfg.MaxOutputTokens,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal repair request: %w", err)
	}

	baseURL := strings.TrimRight(c.cfg.BaseURL, "/")
	url := baseURL + "/chat/completions"

	req, rErr := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if rErr != nil {
		return nil, fmt.Errorf("create repair request: %w", rErr)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, rErr := c.httpClient.Do(req)
	if rErr != nil {
		return nil, fmt.Errorf("repair request failed: %w", rErr)
	}

	body, rErr := io.ReadAll(resp.Body)
	resp.Body.Close()
	if rErr != nil {
		return nil, fmt.Errorf("read repair response: %w", rErr)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: repair request status %d", ErrRepairFailed, resp.StatusCode)
	}

	var chatResp ChatCompletionResponse
	if pErr := json.Unmarshal(body, &chatResp); pErr != nil {
		return nil, fmt.Errorf("%w: parse repair response: %s", ErrRepairFailed, pErr)
	}

	if len(chatResp.Choices) == 0 {
		return nil, fmt.Errorf("%w: empty repair response", ErrRepairFailed)
	}

	repairedContent := chatResp.Choices[0].Message.Content

	// Validate repaired output exactly once.
	if !json.Valid([]byte(repairedContent)) {
		return nil, ErrRepairFailed
	}

	return []byte(repairedContent), nil
}
