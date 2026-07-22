package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// ---- Upstream API types (REST) ----

type ghIssue struct {
	ID        int64        `json:"id"`
	Number    int          `json:"number"`
	Title     string       `json:"title"`
	Body      string       `json:"body"`
	HTMLURL   string       `json:"html_url"`
	State     string       `json:"state"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
	Labels    []ghLabel    `json:"labels"`
	User      ghUser       `json:"user"`
	Comments  int          `json:"comments"`
	Reactions ghReactions  `json:"reactions"`
	Score     float64      `json:"score,omitempty"`
	RepoURL   string       `json:"repository_url,omitempty"`
	PullRequest json.RawMessage `json:"pull_request,omitempty"`
}

type ghLabel struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

type ghUser struct {
	Login string `json:"login"`
	ID    int64  `json:"id"`
}

type ghReactions struct {
	Plus1    int `json:"+1"`
	Minus1   int `json:"-1"`
	Laugh    int `json:"laugh"`
	Hooray   int `json:"hooray"`
	Confused int `json:"confused"`
	Heart    int `json:"heart"`
	Rocket   int `json:"rocket"`
	Eyes     int `json:"eyes"`
}

func (r ghReactions) Total() int {
	return r.Plus1 + r.Minus1 + r.Laugh + r.Hooray + r.Confused + r.Heart + r.Rocket + r.Eyes
}

type ghIssueComment struct {
	ID        int64     `json:"id"`
	Body      string    `json:"body"`
	User      ghUser    `json:"user"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ghSearchResponse struct {
	TotalCount int       `json:"total_count"`
	Items      []ghIssue `json:"items"`
}

// ---- Fetching issues ----

// fetchIssues fetches issues from GitHub using the appropriate strategy
// (search API or per-repo listing) based on the scope.
func fetchIssues(ctx context.Context, c *githubClient, scope collectionScope) ([]ghIssue, error) {
	if !scope.searchIssues {
		return nil, nil
	}

	if scope.strategy == strategyPerRepo {
		return fetchIssuesPerRepoStrategy(ctx, c, scope)
	}
	return fetchIssuesSearchStrategy(ctx, c, scope)
}

// fetchIssuesPerRepoStrategy fetches issues from each configured repository.
func fetchIssuesPerRepoStrategy(ctx context.Context, c *githubClient, scope collectionScope) ([]ghIssue, error) {
	var allIssues []ghIssue
	maxItems := scope.maxItems

	for _, repo := range scope.repos {
		if maxItems > 0 && len(allIssues) >= maxItems {
			break
		}

		owner, repoName, err := parseRepo(repo)
		if err != nil {
			continue // skip invalid repos
		}

		issues, err := listRepoIssues(ctx, c, owner, repoName, scope.since, maxItems)
		if err != nil {
			// Partial failure: continue with other repos
			continue
		}

		allIssues = append(allIssues, issues...)
		if maxItems > 0 && len(allIssues) >= maxItems {
			allIssues = allIssues[:maxItems]
			break
		}
	}

	return allIssues, nil
}

// fetchIssuesSearchStrategy fetches issues using the GitHub search API.
func fetchIssuesSearchStrategy(ctx context.Context, c *githubClient, scope collectionScope) ([]ghIssue, error) {
	query := buildSearchQuery(scope)
	if query == "" {
		return nil, nil
	}

	var allIssues []ghIssue
	page := 1

	for {
		count := scope.maxItems
		if count > 0 {
			remaining := count - len(allIssues)
			if remaining <= 0 {
				break
			}
			if remaining > 100 {
				remaining = 100
			}
		}

		perPage := 100
		if scope.maxItems > 0 && scope.maxItems-len(allIssues) < 100 {
			perPage = scope.maxItems - len(allIssues)
		}
		if perPage <= 0 {
			break
		}

		path := fmt.Sprintf("/search/issues?q=%s&sort=updated&direction=asc&per_page=%d&page=%d",
			url.QueryEscape(query), perPage, page)

		cacheKey := fmt.Sprintf("REST:GET:%s", path)

		var searchResp ghSearchResponse
		_, err := c.doJSONRequest(ctx, requestOptions{
			Method:   "GET",
			Path:     path,
			CacheKey: cacheKey,
		}, &searchResp)
		if err != nil {
			return nil, fmt.Errorf("search issues: %w", err)
		}

		allIssues = append(allIssues, searchResp.Items...)

		// Check if there are more pages
		if len(searchResp.Items) < perPage || (scope.maxItems > 0 && len(allIssues) >= scope.maxItems) {
			break
		}

		page++
	}

	if scope.maxItems > 0 && len(allIssues) > scope.maxItems {
		allIssues = allIssues[:scope.maxItems]
	}

	return allIssues, nil
}

// listRepoIssues fetches issues for a single repository using the per-repo endpoint.
func listRepoIssues(ctx context.Context, c *githubClient, owner, repo, since string, maxItems int) ([]ghIssue, error) {
	var allIssues []ghIssue
	page := 1

	for {
		u := fmt.Sprintf("/repos/%s/%s/issues?state=open&sort=updated&direction=asc&per_page=100&page=%d",
			owner, repo, page)
		if since != "" {
			u += "&since=" + url.QueryEscape(since)
		}

		perPage := 100
		if maxItems > 0 && maxItems-len(allIssues) < 100 && maxItems-len(allIssues) > 0 {
			perPage = maxItems - len(allIssues)
			u = fmt.Sprintf("/repos/%s/%s/issues?state=open&sort=updated&direction=asc&per_page=%d&page=%d",
				owner, repo, perPage, page)
			if since != "" {
				u += "&since=" + url.QueryEscape(since)
			}
		}

		cacheKey := fmt.Sprintf("REST:GET:%s", u)

		var issues []ghIssue
		// The per-repo endpoint returns an array, not a search wrapper
		resp, err := c.doJSONRequest(ctx, requestOptions{
			Method:   "GET",
			Path:     u,
			CacheKey: cacheKey,
		}, &issues)
		if err != nil {
			return nil, fmt.Errorf("list repo issues %s/%s: %w", owner, repo, err)
		}

		// Filter out pull requests (the issues endpoint returns PRs too)
		for _, iss := range issues {
			if iss.PullRequest == nil {
				allIssues = append(allIssues, iss)
			}
		}

		if len(issues) < perPage {
			break
		}
		if maxItems > 0 && len(allIssues) >= maxItems {
			allIssues = allIssues[:maxItems]
			break
		}

		// Check Link header for next page
		links := parseLinkHeader(resp.Header.Get("Link"))
		if _, ok := links["next"]; !ok {
			break
		}

		page++
	}

	return allIssues, nil
}

// fetchIssueComments fetches comments for a specific issue, respecting the max comments cap.
func fetchIssueComments(ctx context.Context, c *githubClient, owner, repo string, issueNumber int, maxComments int) ([]ghIssueComment, error) {
	var allComments []ghIssueComment
	page := 1

	for {
		perPage := 100
		if maxComments > 0 {
			remaining := maxComments - len(allComments)
			if remaining <= 0 {
				break
			}
			if remaining < 100 {
				perPage = remaining
			}
		}

		path := fmt.Sprintf("/repos/%s/%s/issues/%d/comments?per_page=%d&page=%d&sort=created&direction=asc",
			owner, repo, issueNumber, perPage, page)

		cacheKey := fmt.Sprintf("REST:GET:%s", path)

		var comments []ghIssueComment
		resp, err := c.doJSONRequest(ctx, requestOptions{
			Method:   "GET",
			Path:     path,
			CacheKey: cacheKey,
		}, &comments)
		if err != nil {
			return nil, fmt.Errorf("fetch comments for issue #%d: %w", issueNumber, err)
		}

		allComments = append(allComments, comments...)

		if len(comments) < perPage {
			break
		}
		if maxComments > 0 && len(allComments) >= maxComments {
			allComments = allComments[:maxComments]
			break
		}

		links := parseLinkHeader(resp.Header.Get("Link"))
		if _, ok := links["next"]; !ok {
			break
		}

		page++
	}

	return allComments, nil
}

// buildSearchQuery constructs the GitHub search query from the collection scope.
func buildSearchQuery(scope collectionScope) string {
	var parts []string

	parts = append(parts, "is:issue", "is:open")

	// Add language filter
	if len(scope.languages) > 0 {
		for _, lang := range scope.languages {
			if lang != "" {
				parts = append(parts, "language:"+lang)
			}
		}
	}

	// Add label filter
	if len(scope.labels) > 0 {
		for _, label := range scope.labels {
			if label != "" {
				parts = append(parts, "label:"+label)
			}
		}
	}

	// Add repository filter (only for search strategy with repos specified)
	if len(scope.repos) > 0 && scope.strategy == strategySearch {
		for _, repo := range scope.repos {
			if repo != "" {
				parts = append(parts, "repo:"+repo)
			}
		}
	}

	// Add created/updated filter (search API doesn't support "since" directly for issues,
	// but we can use updated:>=YYYY-MM-DD)
	if scope.since != "" {
		// Convert ISO date to search format
		sinceDate := scope.since
		if len(sinceDate) > 10 {
			sinceDate = sinceDate[:10] // just YYYY-MM-DD
		}
		parts = append(parts, "updated:>="+sinceDate)
	}

	return strings.Join(parts, " ")
}

// parseSince extracts the "since" timestamp from the scope.
func parseSince(s string) string {
	if s == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t, err = time.Parse("2006-01-02", s)
		if err != nil {
			return s
		}
		return t.Format("2006-01-02T15:04:05Z")
	}
	return t.Format("2006-01-02T15:04:05Z")
}

// ensure interface check for unused import

