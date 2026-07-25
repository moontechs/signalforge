package reddit

import "errors"

var (
	ErrDisabled          = errors.New("reddit collection is disabled")
	ErrInvalidSubreddit  = errors.New("invalid subreddit")
	ErrRequestCap        = errors.New("reddit request cap exhausted")
	ErrMalformedResponse = errors.New("malformed reddit response")
	ErrAuthFailed        = errors.New("reddit authentication failed")
	ErrRateLimited       = errors.New("reddit rate limited")
	ErrRetriesExhausted  = errors.New("reddit retries exhausted")
)
