package github

import (
	"errors"
	"fmt"
	"time"
)

// Sentinel errors for common failure modes.
var (
	ErrAuthentication = errors.New("authentication failed: invalid or missing token")
	ErrNotEnabled     = errors.New("github source is not enabled")
	ErrNoResults      = errors.New("no results returned from GitHub API")
)

// RateLimitError indicates a GitHub API rate limit was reached.
type RateLimitError struct {
	IsPrimary   bool          // REST API (5000 req/h) or GraphQL point limit.
	IsSecondary bool          // Abuse detection / secondary rate limit.
	RetryAfter  time.Duration // Suggested wait time from Retry-After header.
	Limit       int
	Remaining   int
	Reset       time.Time // When the rate limit window resets.
}

func (e *RateLimitError) Error() string {
	if e.IsSecondary {
		return fmt.Sprintf("secondary rate limit reached: retry after %s", e.RetryAfter)
	}
	return fmt.Sprintf("primary rate limit reached: %d/%d remaining, resets at %s",
		e.Remaining, e.Limit, e.Reset.Format(time.RFC3339))
}

// IsRateLimit checks if an error is a RateLimitError.
func IsRateLimit(err error) bool {
	var rle *RateLimitError
	return errors.As(err, &rle)
}

// IsPrimaryRateLimit checks if an error is a primary rate limit error.
func IsPrimaryRateLimit(err error) bool {
	var rle *RateLimitError
	if errors.As(err, &rle) {
		return rle.IsPrimary
	}
	return false
}

// IsSecondaryRateLimit checks if an error is a secondary rate limit error.
func IsSecondaryRateLimit(err error) bool {
	var rle *RateLimitError
	if errors.As(err, &rle) {
		return rle.IsSecondary
	}
	return false
}

// RetryExhaustionError indicates that all retry attempts were exhausted.
type RetryExhaustionError struct {
	Wrapped  error
	Attempts int
}

func (e *RetryExhaustionError) Error() string {
	return fmt.Sprintf("retry exhausted after %d attempts: %v", e.Attempts, e.Wrapped)
}

func (e *RetryExhaustionError) Unwrap() error {
	return e.Wrapped
}

// MalformedResponseError indicates an unexpected or unparseable upstream response.
type MalformedResponseError struct {
	Wrapped error
	Body    string // truncated response body for debugging.
}

func (e *MalformedResponseError) Error() string {
	return fmt.Sprintf("malformed upstream response: %v", e.Wrapped)
}

func (e *MalformedResponseError) Unwrap() error {
	return e.Wrapped
}

// RequestLimitError indicates the configured request cap was reached.
type RequestLimitError struct {
	Limit int
}

func (e *RequestLimitError) Error() string {
	return fmt.Sprintf("request limit reached: %d max requests per run", e.Limit)
}
