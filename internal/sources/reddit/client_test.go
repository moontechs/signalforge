package reddit

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func preauthenticatedClient(t transport, maxRequests int) *client {
	c := newClient(t, &ConfigValues{MaxRequests: maxRequests})
	c.apiURL = "https://api.test"
	c.token = "tok"
	c.tokenExpires = time.Now().Add(time.Hour)
	c.retryMax = 1
	c.backoff = func(int) time.Duration { return 0 }
	return c
}

func TestClientAuthAndListing(t *testing.T) {
	fake := &fakeTransport{responses: []*http.Response{
		response(http.StatusOK, `{"access_token":"tok","token_type":"bearer","expires_in":3600}`),
		response(http.StatusOK, `{"data":{"after":"x","children":[]}}`),
		response(http.StatusOK, `{"data":{"after":"","children":[]}}`),
	}}
	c := newClient(fake, &ConfigValues{MaxRequests: 5})
	c.clientID = "id"
	c.clientSecret = "secret"
	c.tokenURL = "https://token.test"
	c.apiURL = "https://api.test"
	c.backoff = func(int) time.Duration { return 0 }
	scope := deriveScope(&ConfigValues{MaxPostsPerRun: 10}, time.Time{})

	listing, err := c.listing(context.Background(), "go", &scope, "")
	if err != nil {
		t.Fatal(err)
	}
	if listing.Data.After != "x" {
		t.Fatalf("after = %q", listing.Data.After)
	}
	if _, err := c.listing(context.Background(), "go", &scope, "x"); err != nil {
		t.Fatal(err)
	}
	if len(fake.requests) != 3 {
		t.Fatalf("requests = %d, want 3 (one token and two listings)", len(fake.requests))
	}

	authReq := fake.requests[0]
	if authReq.Method != http.MethodPost || authReq.URL.String() != "https://token.test" {
		t.Fatalf("unexpected auth request: %s %s", authReq.Method, authReq.URL)
	}
	id, secret, ok := authReq.BasicAuth()
	if !ok || id != "id" || secret != "secret" {
		t.Fatalf("unexpected basic auth: id=%q secret=%q ok=%v", id, secret, ok)
	}
	if authReq.Header.Get("Content-Type") != "application/x-www-form-urlencoded" || authReq.Header.Get("User-Agent") == "" {
		t.Fatalf("unexpected auth headers: %v", authReq.Header)
	}
	form, err := io.ReadAll(authReq.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(form) != "grant_type=client_credentials" {
		t.Fatalf("auth form = %q", form)
	}

	listingReq := fake.requests[1]
	if listingReq.Method != http.MethodGet || listingReq.URL.String() != "https://api.test/r/go/new.json?limit=10&t=all" {
		t.Fatalf("unexpected listing request: %s %s", listingReq.Method, listingReq.URL)
	}
	if listingReq.Header.Get("Authorization") != "Bearer tok" || listingReq.Header.Get("User-Agent") == "" {
		t.Fatalf("unexpected listing headers: %v", listingReq.Header)
	}
	if c.Stats().Requests != 3 {
		t.Fatalf("stats = %+v", c.Stats())
	}
}

func TestClientCommentsDecodesRealResponse(t *testing.T) {
	body, err := os.ReadFile("../../../testdata/reddit/comments.json")
	if err != nil {
		t.Fatal(err)
	}
	var requestedURL string
	c := preauthenticatedClient(transportFunc(func(req *http.Request) (*http.Response, error) {
		requestedURL = req.URL.String()
		return response(http.StatusOK, string(body)), nil
	}), 5)

	listings, err := c.comments(context.Background(), "smallbusiness", "abc123", 20)
	if err != nil {
		t.Fatal(err)
	}
	if requestedURL != "https://api.test/r/smallbusiness/comments/abc123.json?limit=20" {
		t.Fatalf("comments URL = %q", requestedURL)
	}
	if len(listings) != 2 {
		t.Fatalf("comment listings = %d", len(listings))
	}
	comments := flattenComments(&listings[1], 20)
	if len(comments) != 3 || comments[0].ID != "c1" || comments[1].ID != "c2" || comments[2].ID != "c3" {
		t.Fatalf("decoded comments = %+v", comments)
	}
}

func TestClientRetriesOAuthWithFreshBody(t *testing.T) {
	var (
		mu          sync.Mutex
		tokenBodies []string
		tokenCalls  int
	)
	transport := transportFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "token.test" {
			body, err := io.ReadAll(req.Body)
			if err != nil {
				return nil, err
			}
			mu.Lock()
			tokenBodies = append(tokenBodies, string(body))
			tokenCalls++
			call := tokenCalls
			mu.Unlock()
			if call == 1 {
				return response(http.StatusInternalServerError, `{}`), nil
			}
			return response(http.StatusOK, `{"access_token":"tok","expires_in":3600}`), nil
		}
		return response(http.StatusOK, `{"data":{"children":[]}}`), nil
	})
	c := newClient(transport, &ConfigValues{MaxRequests: 5})
	c.clientID = "id"
	c.clientSecret = "secret"
	c.tokenURL = "https://token.test"
	c.apiURL = "https://api.test"
	c.retryMax = 1
	c.backoff = func(int) time.Duration { return 0 }

	scope := deriveScope(&ConfigValues{MaxPostsPerRun: 1}, time.Time{})
	if _, err := c.listing(context.Background(), "go", &scope, ""); err != nil {
		t.Fatal(err)
	}
	if tokenCalls != 2 || len(tokenBodies) != 2 {
		t.Fatalf("token attempts = %d, bodies = %v", tokenCalls, tokenBodies)
	}
	for _, body := range tokenBodies {
		if body != "grant_type=client_credentials" {
			t.Fatalf("retried token body = %q", body)
		}
	}
}

func TestClientRefreshesExpiringToken(t *testing.T) {
	var authCalls atomic.Int32
	c := newClient(transportFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "token.test" {
			authCalls.Add(1)
			return response(http.StatusOK, `{"access_token":"fresh","expires_in":3600}`), nil
		}
		if req.Header.Get("Authorization") != "Bearer fresh" {
			return nil, fmt.Errorf("authorization = %q", req.Header.Get("Authorization"))
		}
		return response(http.StatusOK, `{"data":{"children":[]}}`), nil
	}), &ConfigValues{MaxRequests: 5})
	c.clientID = "id"
	c.clientSecret = "secret"
	c.tokenURL = "https://token.test"
	c.apiURL = "https://api.test"
	c.token = "expiring"
	c.tokenExpires = time.Now().Add(10 * time.Second)
	c.retryMax = 0

	scope := deriveScope(&ConfigValues{MaxPostsPerRun: 1}, time.Time{})
	if _, err := c.listing(context.Background(), "go", &scope, ""); err != nil {
		t.Fatal(err)
	}
	if authCalls.Load() != 1 {
		t.Fatalf("authentication calls = %d", authCalls.Load())
	}
}

func TestClientAuthenticationErrorPreservesTypedCauses(t *testing.T) {
	c := newClient(transportFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusTooManyRequests, `{}`), nil
	}), &ConfigValues{MaxRequests: 5})
	c.clientID = "id"
	c.clientSecret = "secret"
	c.tokenURL = "https://token.test"
	c.retryMax = 0

	scope := deriveScope(&ConfigValues{MaxPostsPerRun: 1}, time.Time{})
	_, err := c.listing(context.Background(), "go", &scope, "")
	for _, want := range []error{ErrAuthFailed, ErrRateLimited, ErrRetriesExhausted} {
		if !errors.Is(err, want) {
			t.Fatalf("error = %v, want typed cause %v", err, want)
		}
	}
}

func TestClientErrorClassification(t *testing.T) {
	tests := []struct {
		name       string
		transport  transport
		want       error
		wantSecond error
		attempts   int
	}{
		{
			name: "rate limited",
			transport: transportFunc(func(*http.Request) (*http.Response, error) {
				return response(http.StatusTooManyRequests, `{}`), nil
			}),
			want:       ErrRateLimited,
			wantSecond: ErrRetriesExhausted,
			attempts:   2,
		},
		{
			name: "server retries exhausted",
			transport: transportFunc(func(*http.Request) (*http.Response, error) {
				return response(http.StatusInternalServerError, `{}`), nil
			}),
			want:     ErrRetriesExhausted,
			attempts: 2,
		},
		{
			name: "transport retries exhausted",
			transport: transportFunc(func(*http.Request) (*http.Response, error) {
				return nil, errors.New("network down")
			}),
			want:     ErrRetriesExhausted,
			attempts: 2,
		},
		{
			name: "malformed JSON",
			transport: transportFunc(func(*http.Request) (*http.Response, error) {
				return response(http.StatusOK, `{`), nil
			}),
			want:     ErrMalformedResponse,
			attempts: 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var attempts atomic.Int32
			original := test.transport
			c := preauthenticatedClient(transportFunc(func(req *http.Request) (*http.Response, error) {
				attempts.Add(1)
				return original.Do(req)
			}), 10)
			var out listingResponse
			err := c.get(context.Background(), "/test", time.Minute, &out)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
			if test.wantSecond != nil && !errors.Is(err, test.wantSecond) {
				t.Fatalf("error = %v, also want %v", err, test.wantSecond)
			}
			if int(attempts.Load()) != test.attempts {
				t.Fatalf("attempts = %d, want %d", attempts.Load(), test.attempts)
			}
		})
	}
}

func TestClientDoesNotRetryNonRetryableStatus(t *testing.T) {
	var attempts atomic.Int32
	c := preauthenticatedClient(transportFunc(func(*http.Request) (*http.Response, error) {
		attempts.Add(1)
		return response(http.StatusBadRequest, `{}`), nil
	}), 10)
	var out listingResponse
	err := c.get(context.Background(), "/bad", time.Minute, &out)
	if err == nil || errors.Is(err, ErrRetriesExhausted) || attempts.Load() != 1 {
		t.Fatalf("error = %v, attempts = %d", err, attempts.Load())
	}
}

func TestClientCancellationDuringBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c := preauthenticatedClient(transportFunc(func(*http.Request) (*http.Response, error) {
		cancel()
		return response(http.StatusInternalServerError, `{}`), nil
	}), 10)
	c.backoff = func(int) time.Duration { return time.Hour }
	var out listingResponse
	err := c.get(ctx, "/cancel", time.Minute, &out)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context cancellation", err)
	}
}

func TestClientConcurrentRequestCapAndPerRunReset(t *testing.T) {
	var calls atomic.Int32
	c := preauthenticatedClient(transportFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return response(http.StatusOK, `{"data":{"children":[]}}`), nil
	}), 3)
	c.retryMax = 0
	c.beginRun()

	const goroutines = 20
	errs := make(chan error, goroutines)
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			var out listingResponse
			errs <- c.get(context.Background(), fmt.Sprintf("/request/%d", index), time.Minute, &out)
		}(i)
	}
	wg.Wait()
	close(errs)

	capErrors := 0
	for err := range errs {
		if errors.Is(err, ErrRequestCap) {
			capErrors++
		}
	}
	if calls.Load() != 3 || c.Stats().Requests != 3 || capErrors != goroutines-3 {
		t.Fatalf("calls=%d stats=%+v cap errors=%d", calls.Load(), c.Stats(), capErrors)
	}

	c.beginRun()
	var out listingResponse
	if err := c.get(context.Background(), "/next-run", time.Minute, &out); err != nil {
		t.Fatalf("new run remained exhausted: %v", err)
	}
	if calls.Load() != 4 || c.Stats().Requests != 4 {
		t.Fatalf("calls=%d stats=%+v after reset", calls.Load(), c.Stats())
	}
}

func TestClientRejectsOversizedResponse(t *testing.T) {
	c := preauthenticatedClient(transportFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusOK, strings.Repeat("x", 10*1024*1024+1)), nil
	}), 2)
	var out listingResponse
	err := c.get(context.Background(), "/large", time.Minute, &out)
	if err == nil || !strings.Contains(err.Error(), "exceeds 10 MiB") {
		t.Fatalf("error = %v", err)
	}
}
