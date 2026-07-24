package openrouter

// ChatCompletionRequest is an OpenAI-compatible chat completion request.
// See https://platform.openai.com/docs/api-reference/chat/create
type ChatCompletionRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Stream      bool      `json:"stream,omitempty"`
}

// Message represents a single message in a chat conversation.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ResponseFormat describes the expected response format.
type ResponseFormat struct {
	Type string `json:"type"`
}
