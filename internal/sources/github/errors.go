package github

import (
	"errors"
	"fmt"
	"time"
)

var ErrRequestLimitExceeded = errors.New("github request limit exceeded")

// AuthenticationError indicates the GitHub token was rejected.
type AuthenticationError struct {
	StatusCode int
	Message    string
}

func (e *AuthenticationError) Error() string {
	return fmt.Sprintf("github authentication failed (status %d): %s", e.StatusCode, e.Message)
}

// RateLimitError indicates GitHub rejected a request due to rate limiting.
type RateLimitError struct {
	StatusCode int
	Message    string
	ResetAt    time.Time
}

func (e *RateLimitError) Error() string {
	if e.ResetAt.IsZero() {
		return fmt.Sprintf("github rate limit exceeded (status %d): %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf(
		"github rate limit exceeded (status %d, resets at %s): %s",
		e.StatusCode,
		e.ResetAt.UTC().Format(time.RFC3339),
		e.Message,
	)
}

// MalformedResponseError indicates GitHub returned an unreadable response body.
type MalformedResponseError struct {
	Operation string
	Err       error
}

func (e *MalformedResponseError) Error() string {
	return fmt.Sprintf("github %s returned malformed response: %v", e.Operation, e.Err)
}

func (e *MalformedResponseError) Unwrap() error {
	return e.Err
}

// APIError indicates a non-auth, non-rate-limit API failure.
type APIError struct {
	Operation  string
	StatusCode int
	Message    string
	Retryable  bool
}

func (e *APIError) Error() string {
	return fmt.Sprintf("github %s failed with status %d: %s", e.Operation, e.StatusCode, e.Message)
}
