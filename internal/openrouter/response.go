package openrouter

// ChatCompletionResponse is an OpenAI-compatible chat completion response.
type ChatCompletionResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   *Usage   `json:"usage,omitempty"`
}

// Choice represents a single completion choice.
type Choice struct {
	Index   int     `json:"index"`
	Message Message `json:"message"`
}

// Usage holds token usage statistics.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// APIError represents an error response from the OpenRouter API.
type APIError struct {
	Err APIErrorDetail `json:"error"`
}

// APIErrorDetail holds the details of an API error.
type APIErrorDetail struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code,omitempty"`
}

// Error returns the error message.
func (e *APIError) Error() string {
	return e.Err.Message
}
