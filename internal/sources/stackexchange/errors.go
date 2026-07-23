package stackexchange

import (
	"errors"
	"fmt"
)

var (
	ErrDisabled          = errors.New("stack exchange collection is disabled")
	ErrNoSitesConfigured = errors.New("no stack exchange sites configured")
	ErrRequestCap        = errors.New("stack exchange request cap exhausted")
	ErrQuotaExhausted    = errors.New("stack exchange API quota exhausted")
	ErrMalformedResponse = errors.New("malformed stack exchange response")
	ErrRetriesExhausted  = errors.New("stack exchange retries exhausted")
	ErrBackoffCancelled  = errors.New("stack exchange backoff cancelled")
	ErrAPIError          = errors.New("stack exchange API error")
)

// APIError identifies an error returned in the API response envelope.
type APIError struct {
	ID            int
	Name, Message string
	Cause         error
}

func (e *APIError) Error() string {
	return fmt.Sprintf("stack exchange API error %d (%s): %s", e.ID, e.Name, e.Message)
}
func (e *APIError) Unwrap() error {
	if e.Cause != nil {
		return e.Cause
	}
	return ErrAPIError
}
func (e *APIError) Is(target error) bool {
	return target == ErrAPIError || (e.Cause != nil && errors.Is(e.Cause, target))
}
