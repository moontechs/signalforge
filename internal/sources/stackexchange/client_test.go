package stackexchange

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type testTransport func(*http.Request) (*http.Response, error)

func (f testTransport) Do(r *http.Request) (*http.Response, error) { return f(r) }

func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func TestGetQuestionsBuildsRequest(t *testing.T) {
	tr := testTransport(func(r *http.Request) (*http.Response, error) {
		q := r.URL.Query()
		for k, want := range map[string]string{"site": "stackoverflow", "fromdate": "10", "todate": "20", "page": "2", "pagesize": "25", "filter": "x", "order": "desc", "sort": "creation", "key": "secret"} {
			if q.Get(k) != want {
				t.Errorf("%s = %q, want %q", k, q.Get(k), want)
			}
		}
		return response(200, `{"items":[],"quota_remaining":99}`), nil
	})
	c := newClient(tr, ConfigValues{BaseURL: "https://example.test/2.3", APIKey: "secret"})
	if _, err := c.getQuestions(context.Background(), "stackoverflow", 10, 20, 2, 25, "x"); err != nil {
		t.Fatal(err)
	}
}

func TestGetQuestionsAPIErrorAndQuota(t *testing.T) {
	api := newClient(testTransport(func(*http.Request) (*http.Response, error) {
		return response(200, `{"error_id":502,"error_name":"bad_parameter","error_message":"nope"}`), nil
	}), ConfigValues{})
	if _, err := api.getQuestions(context.Background(), "x", 0, 1, 1, 1, "x"); !errors.Is(err, ErrAPIError) {
		t.Fatalf("error = %v", err)
	}
	quota := newClient(testTransport(func(*http.Request) (*http.Response, error) {
		return response(200, `{"items":[{"question_id":1}],"quota_remaining":0}`), nil
	}), ConfigValues{})
	out, err := quota.getQuestions(context.Background(), "x", 0, 1, 1, 1, "x")
	if !errors.Is(err, ErrQuotaExhausted) || len(out.Items) != 1 {
		t.Fatalf("out=%v err=%v", out, err)
	}
}

func TestGetQuestionsRetriesTransientStatus(t *testing.T) {
	attempts := 0
	c := newClient(testTransport(func(*http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return response(503, "retry"), nil
		}
		return response(200, `{"quota_remaining":1}`), nil
	}), ConfigValues{})
	c.backoff = func(int) time.Duration { return 0 }
	if _, err := c.getQuestions(context.Background(), "x", 0, 1, 1, 1, "x"); err != nil || attempts != 2 {
		t.Fatalf("attempts=%d err=%v", attempts, err)
	}
}
