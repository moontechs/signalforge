// Package openrouter provides an LLM client for OpenRouter API.
package openrouter

import "errors"

var (
	// ErrNoAPIKey is returned when no API key is configured.
	ErrNoAPIKey = errors.New("OPENROUTER_API_KEY is not set")

	// ErrNoModel is returned when no model is configured or available.
	ErrNoModel = errors.New("no model configured or available")

	// ErrRateLimited is returned when the API rate limit is exceeded.
	ErrRateLimited = errors.New("rate limited by upstream API")

	// ErrInvalidResponse is returned when the API response is invalid or
	// cannot be parsed.
	ErrInvalidResponse = errors.New("invalid API response")

	// ErrRepairFailed is returned when a JSON repair attempt fails.
	ErrRepairFailed = errors.New("failed to repair invalid JSON response")

	// ErrAllModelsFailed is returned when all models in the resolution chain
	// have been exhausted without a successful response.
	ErrAllModelsFailed = errors.New("all models failed to produce a valid response")
)
