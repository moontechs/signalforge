package stackexchange

import (
	"io"
	"net/http"
	"strings"
	"sync"
)

// fakeResponse describes a single canned HTTP response.
type fakeResponse struct {
	statusCode int
	headers    map[string]string
	body       string
}

// fakeTransport is a test HTTP transport that returns canned responses.
// It supports sequential responses per URL (consumed in order), and
// records every request for later inspection.
// All methods are safe for concurrent use.
type fakeTransport struct {
	mu        sync.Mutex
	responses map[string][]fakeResponse // URL -> ordered responses.
	callCount map[string]int
	calls     []*http.Request
}

// newFakeTransport creates a ready-to-use fake transport.
func newFakeTransport() *fakeTransport {
	return &fakeTransport{
		responses: make(map[string][]fakeResponse),
		callCount: make(map[string]int),
	}
}

// addResponse registers a single response for a URL pattern.
// If the URL ends with '*', all URLs with that prefix will match.
func (f *fakeTransport) addResponse(url string, resp fakeResponse) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.responses[url] = append(f.responses[url], resp)
}

// addSequentialResponses registers ordered responses for the same URL.
func (f *fakeTransport) addSequentialResponses(url string, resp ...fakeResponse) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.responses[url] = append(f.responses[url], resp...)
}

// findResponse locates the next response for a URL, trying exact match
// first and falling back to prefix (wildcard) patterns.
// Must be called with f.mu held.
func (f *fakeTransport) findResponseLocked(urlStr string) (fakeResponse, bool) {
	if resp, ok := f.nextResponseLocked(urlStr); ok {
		return resp, true
	}
	for pattern := range f.responses {
		if strings.HasSuffix(pattern, "*") {
			prefix := strings.TrimSuffix(pattern, "*")
			if strings.HasPrefix(urlStr, prefix) {
				if resp, ok := f.nextResponseLocked(pattern); ok {
					return resp, true
				}
			}
		}
	}
	return fakeResponse{}, false
}

// nextResponseLocked consumes the next response from the FIFO queue for a key.
// Once all responses for a key are exhausted, the last response is
// returned on every subsequent call.
// Must be called with f.mu held.
func (f *fakeTransport) nextResponseLocked(key string) (fakeResponse, bool) {
	responses, ok := f.responses[key]
	if !ok || len(responses) == 0 {
		return fakeResponse{}, false
	}
	count := f.callCount[key]
	f.callCount[key]++
	if count >= len(responses) {
		return responses[len(responses)-1], true
	}
	return responses[count], true
}

// Do implements the http.RoundTripper interface.
// It respects context cancellation: if the request's context is done before
// the response is returned, it returns the context error.
func (f *fakeTransport) Do(req *http.Request) (*http.Response, error) {
	select {
	case <-req.Context().Done():
		return nil, req.Context().Err()
	default:
	}

	f.mu.Lock()
	f.calls = append(f.calls, req)
	resp, ok := f.findResponseLocked(req.URL.String())
	f.mu.Unlock()

	if !ok {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	}

	header := make(http.Header)
	for k, v := range resp.headers {
		header.Set(k, v)
	}

	return &http.Response{
		StatusCode: resp.statusCode,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(resp.body)),
	}, nil
}

// resetCallCount resets all call counters and recorded calls.
func (f *fakeTransport) resetCallCount() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.callCount = make(map[string]int)
	f.calls = nil
}

// testClient creates a client backed by a fake transport for testing.
// If fake is nil, a new one is created.
func testClient(fake *fakeTransport) *client {
	if fake == nil {
		fake = newFakeTransport()
	}
	return newClient(fake, &ConfigValues{})
}
