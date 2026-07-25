package reddit

import (
	"net/http"
	"sync"
)

type fakeTransport struct {
	mu        sync.Mutex
	responses []*http.Response
	requests  []*http.Request
}

func (f *fakeTransport) Do(r *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, r.Clone(r.Context()))
	if len(f.responses) == 0 {
		return nil, assertionError("unexpected request")
	}
	v := f.responses[0]
	f.responses = f.responses[1:]
	return v, nil
}

type assertionError string

func (e assertionError) Error() string { return string(e) }
