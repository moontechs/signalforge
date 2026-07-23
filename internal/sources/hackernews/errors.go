package hackernews

import "errors"

var (
	// ErrDisabled indicates Hacker News collection is disabled in config.
	ErrDisabled = errors.New("hacker news collection is disabled")
	// ErrInvalidFeed indicates an unrecognized feed name.
	ErrInvalidFeed = errors.New("invalid hacker news feed")
	// ErrRequestCap indicates the per-run request limit has been reached.
	ErrRequestCap = errors.New("hacker news request cap exhausted")
	// ErrMalformedResponse indicates an unparseable response from the Firebase API.
	ErrMalformedResponse = errors.New("malformed hacker news response")
	// ErrRetriesExhausted indicates all retry attempts were consumed.
	ErrRetriesExhausted = errors.New("hacker news retries exhausted")
)