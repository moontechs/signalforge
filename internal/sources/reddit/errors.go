package reddit

import "errors"

var (
	// ErrDisabled indicates Reddit collection is disabled in config.
	ErrDisabled = errors.New("reddit collection is disabled")
	// ErrMissingCredentials indicates REDDIT_CLIENT_ID or REDDIT_CLIENT_SECRET is not set.
	ErrMissingCredentials = errors.New("reddit client credentials are required")
	// ErrInvalidSubreddit indicates an unrecognized or malformed subreddit name.
	ErrInvalidSubreddit = errors.New("invalid subreddit")
	// ErrInvalidSort indicates an unsupported listing sort value.
	ErrInvalidSort = errors.New("invalid reddit sort")
	// ErrInvalidTime indicates an unsupported time filter value.
	ErrInvalidTime = errors.New("invalid reddit time filter")
	// ErrMalformedResponse indicates an unparseable response from the Reddit API.
	ErrMalformedResponse = errors.New("malformed reddit api response")
	// ErrRequestCap indicates the per-run request limit has been reached.
	ErrRequestCap = errors.New("reddit request cap exhausted")
	// ErrTokenAuth indicates OAuth token acquisition or authentication failure.
	ErrTokenAuth = errors.New("reddit token authentication failure")
	// ErrRetriesExhausted indicates all retry attempts were consumed.
	ErrRetriesExhausted = errors.New("reddit retries exhausted")
)
