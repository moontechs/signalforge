package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultBaseURL     = "https://api.github.com"
	defaultGraphQLURL  = "https://api.github.com/graphql"
	defaultTimeout     = 30 * time.Second
	defaultBackoffBase = 200 * time.Millisecond
	defaultUserAgent   = "signalforge-github-collector"
)

type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// ClientConfig controls GitHub API client behavior.
type ClientConfig struct {
	BaseURL     string
	GraphQLURL  string
	Token       string
	HTTPClient  httpDoer
	Timeout     time.Duration
	MaxRetries  int
	MaxRequests int
	UserAgent   string
	BackoffBase time.Duration
	Sleep       func(time.Duration)
	OnRequest   func(RequestEvent)
}

// RequestEvent reports request attempts for stats or logging.
type RequestEvent struct {
	Operation    string
	Protocol     string
	Attempt      int
	RequestCount int
	URL          string
}

// ClientStats reports client request counts.
type ClientStats struct {
	Requests        int
	RESTRequests    int
	GraphQLRequests int
	Retries         int
}

// SearchIssuesParams defines the supported REST issue-search inputs.
type SearchIssuesParams struct {
	Query   string
	Sort    string
	Order   string
	Page    int
	PerPage int
}

// IssueCommentsParams defines the supported REST issue-comment inputs.
type IssueCommentsParams struct {
	Owner     string
	Repo      string
	IssueNum  int
	Page      int
	PerPage   int
	Sort      string
	Direction string
}

// DiscussionSearchParams defines the supported GraphQL discussion-search inputs.
type DiscussionSearchParams struct {
	Query string
	First int
	After string
}

// DiscussionCommentsParams defines the supported GraphQL discussion-comment inputs.
type DiscussionCommentsParams struct {
	DiscussionID string
	First        int
	After        string
}

// SearchIssuesPage is a parsed issue-search page plus pagination state.
type SearchIssuesPage struct {
	Response SearchIssuesResponse
	HasNext  bool
}

// IssueCommentsPage is a parsed issue-comment page plus pagination state.
type IssueCommentsPage struct {
	Comments IssueCommentsResponse
	HasNext  bool
}

// DiscussionSearchPage is a parsed discussion-search page.
type DiscussionSearchPage struct {
	Response GraphQLResponse[DiscussionsQueryData]
}

// DiscussionCommentsQueryData is the GraphQL discussion comment query response.
type DiscussionCommentsQueryData struct {
	Node DiscussionCommentsNode `json:"node"`
}

// DiscussionCommentsNode wraps paginated discussion comments for a single discussion.
type DiscussionCommentsNode struct {
	Comments DiscussionCommentConnection `json:"comments"`
}

// DiscussionCommentsPage is a parsed discussion-comment page.
type DiscussionCommentsPage struct {
	Response GraphQLResponse[DiscussionCommentsQueryData]
}

// Client implements the GitHub REST and GraphQL transport.
type Client struct {
	baseURL     string
	graphQLURL  string
	token       string
	httpClient  httpDoer
	maxRetries  int
	maxRequests int
	userAgent   string
	backoffBase time.Duration
	sleep       func(time.Duration)
	onRequest   func(RequestEvent)

	mu    sync.Mutex
	stats ClientStats
}

// NewClient constructs a GitHub API client with retries and request accounting.
func NewClient(cfg ClientConfig) (*Client, error) {
	token := strings.TrimSpace(cfg.Token)
	if token == "" {
		token = strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
	}
	if token == "" {
		return nil, fmt.Errorf("github token is required")
	}

	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	graphQLURL := strings.TrimSpace(cfg.GraphQLURL)
	if graphQLURL == "" {
		graphQLURL = defaultGraphQLURL
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeout}
	}

	userAgent := strings.TrimSpace(cfg.UserAgent)
	if userAgent == "" {
		userAgent = defaultUserAgent
	}

	backoffBase := cfg.BackoffBase
	if backoffBase <= 0 {
		backoffBase = defaultBackoffBase
	}

	sleep := cfg.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}

	return &Client{
		baseURL:     baseURL,
		graphQLURL:  graphQLURL,
		token:       token,
		httpClient:  httpClient,
		maxRetries:  max(0, cfg.MaxRetries),
		maxRequests: cfg.MaxRequests,
		userAgent:   userAgent,
		backoffBase: backoffBase,
		sleep:       sleep,
		onRequest:   cfg.OnRequest,
	}, nil
}

// SearchIssues searches GitHub issues and pull-request issues via REST.
func (c *Client) SearchIssues(ctx context.Context, params SearchIssuesParams) (SearchIssuesPage, error) {
	values := url.Values{}
	values.Set("q", strings.TrimSpace(params.Query))
	if params.Sort != "" {
		values.Set("sort", params.Sort)
	}
	if params.Order != "" {
		values.Set("order", params.Order)
	}
	if params.Page > 0 {
		values.Set("page", strconv.Itoa(params.Page))
	}
	if params.PerPage > 0 {
		values.Set("per_page", strconv.Itoa(params.PerPage))
	}

	req, err := c.newRESTRequest(ctx, http.MethodGet, "/search/issues", values, nil)
	if err != nil {
		return SearchIssuesPage{}, err
	}

	var payload SearchIssuesResponse
	resp, err := c.doJSON(req, "search issues", "rest", &payload)
	if err != nil {
		return SearchIssuesPage{}, err
	}

	return SearchIssuesPage{
		Response: payload,
		HasNext:  hasNextPage(resp.Header.Get("Link")),
	}, nil
}

// ListIssueComments fetches issue comments over REST.
func (c *Client) ListIssueComments(ctx context.Context, params IssueCommentsParams) (IssueCommentsPage, error) {
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/comments", params.Owner, params.Repo, params.IssueNum)
	values := url.Values{}
	if params.Page > 0 {
		values.Set("page", strconv.Itoa(params.Page))
	}
	if params.PerPage > 0 {
		values.Set("per_page", strconv.Itoa(params.PerPage))
	}
	if params.Sort != "" {
		values.Set("sort", params.Sort)
	}
	if params.Direction != "" {
		values.Set("direction", params.Direction)
	}

	req, err := c.newRESTRequest(ctx, http.MethodGet, path, values, nil)
	if err != nil {
		return IssueCommentsPage{}, err
	}

	var payload IssueCommentsResponse
	resp, err := c.doJSON(req, "list issue comments", "rest", &payload)
	if err != nil {
		return IssueCommentsPage{}, err
	}

	return IssueCommentsPage{
		Comments: payload,
		HasNext:  hasNextPage(resp.Header.Get("Link")),
	}, nil
}

// SearchDiscussions searches GitHub discussions through GraphQL.
func (c *Client) SearchDiscussions(ctx context.Context, params DiscussionSearchParams) (DiscussionSearchPage, error) {
	query := `
query SearchDiscussions($query: String!, $first: Int!, $after: String) {
  search(type: DISCUSSION, query: $query, first: $first, after: $after) {
    pageInfo {
      endCursor
      hasNextPage
    }
    nodes {
      ... on Discussion {
        id
        number
        title
        body
        url
        locked
        closed
        createdAt
        updatedAt
        repository {
          nameWithOwner
        }
        category {
          name
        }
        labels(first: 20) {
          nodes {
            name
          }
        }
        comments(first: 20) {
          totalCount
          pageInfo {
            endCursor
            hasNextPage
          }
          nodes {
            id
            body
            url
            createdAt
            updatedAt
            replyCount
            isAnswer
            author {
              login
            }
            reactions {
              totalCount
            }
          }
        }
        reactions {
          totalCount
        }
        upvoteCount
        author {
          login
        }
      }
    }
  }
}`

	variables := map[string]any{
		"query": params.Query,
		"first": max(1, params.First),
		"after": nil,
	}
	if params.After != "" {
		variables["after"] = params.After
	}

	req, err := c.newGraphQLRequest(ctx, query, variables)
	if err != nil {
		return DiscussionSearchPage{}, err
	}

	var payload GraphQLResponse[DiscussionsQueryData]
	if _, err := c.doJSON(req, "search discussions", "graphql", &payload); err != nil {
		return DiscussionSearchPage{}, err
	}
	if err := graphqlErrors("search discussions", payload.Errors); err != nil {
		return DiscussionSearchPage{}, err
	}

	return DiscussionSearchPage{Response: payload}, nil
}

// ListDiscussionComments fetches more comments for a discussion through GraphQL.
func (c *Client) ListDiscussionComments(ctx context.Context, params DiscussionCommentsParams) (DiscussionCommentsPage, error) {
	query := `
query DiscussionComments($id: ID!, $first: Int!, $after: String) {
  node(id: $id) {
    ... on Discussion {
      comments(first: $first, after: $after) {
        totalCount
        pageInfo {
          endCursor
          hasNextPage
        }
        nodes {
          id
          body
          url
          createdAt
          updatedAt
          replyCount
          isAnswer
          author {
            login
          }
          reactions {
            totalCount
          }
        }
      }
    }
  }
}`

	variables := map[string]any{
		"id":    params.DiscussionID,
		"first": max(1, params.First),
		"after": nil,
	}
	if params.After != "" {
		variables["after"] = params.After
	}

	req, err := c.newGraphQLRequest(ctx, query, variables)
	if err != nil {
		return DiscussionCommentsPage{}, err
	}

	var payload GraphQLResponse[DiscussionCommentsQueryData]
	if _, err := c.doJSON(req, "list discussion comments", "graphql", &payload); err != nil {
		return DiscussionCommentsPage{}, err
	}
	if err := graphqlErrors("list discussion comments", payload.Errors); err != nil {
		return DiscussionCommentsPage{}, err
	}

	return DiscussionCommentsPage{Response: payload}, nil
}

// Stats returns a snapshot of client request counters.
func (c *Client) Stats() ClientStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stats
}

func (c *Client) newRESTRequest(ctx context.Context, method, path string, values url.Values, body io.Reader) (*http.Request, error) {
	endpoint := c.baseURL + path
	if len(values) > 0 {
		endpoint += "?" + values.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("build github request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	return req, nil
}

func (c *Client) newGraphQLRequest(ctx context.Context, query string, variables map[string]any) (*http.Request, error) {
	payload := map[string]any{
		"query":     strings.TrimSpace(query),
		"variables": variables,
	}
	buf := bytes.NewBuffer(nil)
	if err := json.NewEncoder(buf).Encode(payload); err != nil {
		return nil, fmt.Errorf("encode graphql request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.graphQLURL, buf)
	if err != nil {
		return nil, fmt.Errorf("build github graphql request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	return req, nil
}

func (c *Client) doJSON(req *http.Request, operation, protocol string, into any) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		resp, err := c.do(req, operation, protocol, attempt)
		if err == nil {
			defer resp.Body.Close()
			if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
				return nil, &MalformedResponseError{Operation: operation, Err: err}
			}
			return resp, nil
		}

		lastErr = err
		if !isRetryable(err) || attempt == c.maxRetries {
			return nil, lastErr
		}

		c.recordRetry()
		c.sleep(c.backoffBase * time.Duration(1<<attempt))
	}
	return nil, lastErr
}

func (c *Client) do(req *http.Request, operation, protocol string, attempt int) (*http.Response, error) {
	requestCount, err := c.recordRequest(protocol, operation, req.URL.String(), attempt+1)
	if err != nil {
		return nil, err
	}

	if c.onRequest != nil {
		c.onRequest(RequestEvent{
			Operation:    operation,
			Protocol:     protocol,
			Attempt:      attempt + 1,
			RequestCount: requestCount,
			URL:          req.URL.String(),
		})
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if req.Context().Err() != nil {
			return nil, req.Context().Err()
		}
		return nil, &APIError{
			Operation: operation,
			Message:   err.Error(),
			Retryable: true,
		}
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp, nil
	}

	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if readErr != nil {
		return nil, &MalformedResponseError{Operation: operation, Err: readErr}
	}

	return nil, classifyAPIError(operation, resp, body)
}

func (c *Client) recordRequest(protocol, operation, requestURL string, attempt int) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.maxRequests > 0 && c.stats.Requests >= c.maxRequests {
		return c.stats.Requests, fmt.Errorf("%w for %s", ErrRequestLimitExceeded, operation)
	}

	c.stats.Requests++
	if protocol == "graphql" {
		c.stats.GraphQLRequests++
	} else {
		c.stats.RESTRequests++
	}

	return c.stats.Requests, nil
}

func (c *Client) recordRetry() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stats.Retries++
}

func hasNextPage(linkHeader string) bool {
	for _, part := range strings.Split(linkHeader, ",") {
		if strings.Contains(part, `rel="next"`) {
			return true
		}
	}
	return false
}

func graphqlErrors(operation string, errs []GraphQLError) error {
	if len(errs) == 0 {
		return nil
	}
	message := strings.TrimSpace(errs[0].Message)
	if message == "" {
		message = "graphql query failed"
	}
	return &APIError{
		Operation: operation,
		Message:   message,
		Retryable: false,
	}
}

func classifyAPIError(operation string, resp *http.Response, body []byte) error {
	message := extractAPIMessage(body)
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		if remaining := strings.TrimSpace(resp.Header.Get("X-RateLimit-Remaining")); remaining == "0" {
			return &RateLimitError{
				StatusCode: resp.StatusCode,
				Message:    message,
				ResetAt:    parseRateLimitReset(resp.Header.Get("X-RateLimit-Reset")),
			}
		}
		if resp.StatusCode == http.StatusUnauthorized {
			return &AuthenticationError{StatusCode: resp.StatusCode, Message: message}
		}
		return &APIError{
			Operation:  operation,
			StatusCode: resp.StatusCode,
			Message:    message,
			Retryable:  false,
		}
	case http.StatusTooManyRequests:
		return &RateLimitError{
			StatusCode: resp.StatusCode,
			Message:    message,
			ResetAt:    parseRateLimitReset(resp.Header.Get("X-RateLimit-Reset")),
		}
	}

	retryable := resp.StatusCode >= 500
	return &APIError{
		Operation:  operation,
		StatusCode: resp.StatusCode,
		Message:    message,
		Retryable:  retryable,
	}
}

func extractAPIMessage(body []byte) string {
	var payload struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &payload); err == nil && strings.TrimSpace(payload.Message) != "" {
		return payload.Message
	}

	text := strings.TrimSpace(string(body))
	if text == "" {
		return "github API request failed"
	}
	return text
}

func parseRateLimitReset(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	epoch, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(epoch, 0).UTC()
}

func isRetryable(err error) bool {
	var rateLimitErr *RateLimitError
	if errors.As(err, &rateLimitErr) {
		return false
	}

	var authErr *AuthenticationError
	if errors.As(err, &authErr) {
		return false
	}

	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Retryable
	}

	return false
}
