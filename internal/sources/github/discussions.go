package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ---- Upstream API types (GraphQL) ----.

// discussionsQuery is the GraphQL query for fetching discussions.
const discussionsQuery = `
query($owner: String!, $repo: String!, $cursor: String, $first: Int = 50) {
  repository(owner: $owner, name: $repo) {
    discussions(first: $first, after: $cursor, orderBy: {field: UPDATED_AT, direction: ASC}) {
      pageInfo { hasNextPage endCursor }
      nodes {
        id
        number
        title
        body
        url
        createdAt
        updatedAt
        category { name slug }
        labels(first: 10) { nodes { name } }
        comments(first: 20) { totalCount nodes { id body createdAt } }
        upvoteCount
      }
    }
  }
}`

// graphQLDiscussionResponse mirrors the data portion returned by the GitHub GraphQL API.
// It is deserialized from the json.RawMessage inside graphQLResponse.Data,.
// so the outer {"data": ...} wrapper is already removed.
type graphQLDiscussionResponse struct {
	Repository *struct {
		Discussions struct {
			PageInfo struct {
				HasNextPage bool   `json:"hasNextPage"`
				EndCursor   string `json:"endCursor"`
			} `json:"pageInfo"`
			Nodes []graphQLDiscussionNode `json:"nodes"`
		} `json:"discussions"`
	} `json:"repository"`
}

type graphQLDiscussionNode struct {
	ID        string    `json:"id"`
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	URL       string    `json:"url"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	Category  *struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	} `json:"category"`
	Labels *struct {
		Nodes []struct {
			Name string `json:"name"`
		} `json:"nodes"`
	} `json:"labels"`
	Comments *struct {
		TotalCount int                        `json:"totalCount"`
		Nodes      []graphQLDiscussionComment `json:"nodes"`
	} `json:"comments"`
	UpvoteCount int `json:"upvoteCount"`
}

type graphQLDiscussionComment struct {
	ID        string    `json:"id"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`
}

// ---- Fetching discussions ----.

// fetchDiscussions fetches GitHub Discussions for the given repositories using GraphQL.
func fetchDiscussions(ctx context.Context, c *githubClient, repos []string, scope collectionScope) ([]graphQLDiscussionNode, error) {
	if !scope.searchDiscussions {
		return nil, nil
	}

	if len(repos) == 0 {
		// Discussions can only be fetched per-repo; use repos from scope.
		repos = scope.repos
	}

	if len(repos) == 0 {
		return nil, nil
	}

	var allDiscussions []graphQLDiscussionNode
	maxItems := scope.maxItems
	var lastErr error

	for _, repo := range repos {
		if maxItems > 0 && len(allDiscussions) >= maxItems {
			break
		}

		owner, repoName, err := parseRepo(repo)
		if err != nil {
			lastErr = err
			continue
		}

		discussions, err := listRepoDiscussions(ctx, c, owner, repoName, scope.since, maxItems)
		if err != nil {
			lastErr = err
			continue // Partial failure: skip repo.
		}

		allDiscussions = append(allDiscussions, discussions...)
		if maxItems > 0 && len(allDiscussions) >= maxItems {
			allDiscussions = allDiscussions[:maxItems]
			break
		}
	}

	// If no discussions were returned and there were errors, surface the error.
	if len(allDiscussions) == 0 && lastErr != nil {
		return nil, lastErr
	}

	return allDiscussions, nil
}

// listRepoDiscussions fetches discussions for a single repository with cursor-based pagination.
func listRepoDiscussions(ctx context.Context, c *githubClient, owner, repo, since string, maxItems int) ([]graphQLDiscussionNode, error) {
	var allDiscussions []graphQLDiscussionNode
	var cursor *string

	for {
		vars := map[string]any{
			"owner": owner,
			"repo":  repo,
			"first": 50,
		}
		if cursor != nil {
			vars["cursor"] = *cursor
		}

		gqlResp, err := c.doGraphQL(ctx, discussionsQuery, vars)
		if err != nil {
			// Return partial results if we have some, wrap the error.
			if len(allDiscussions) > 0 {
				return allDiscussions, fmt.Errorf("graphql error after partial results: %w", err)
			}
			return nil, fmt.Errorf("fetch discussions %s/%s: %w", owner, repo, err)
		}

		// Parse the response.
		var resp graphQLDiscussionResponse
		if err := parseGraphQLResponse(gqlResp, &resp); err != nil {
			return allDiscussions, fmt.Errorf("parse discussions %s/%s: %w", owner, repo, err)
		}

		if resp.Repository == nil {
			break // repository not found or no access.
		}

		nodes := resp.Repository.Discussions.Nodes

		// Filter by since date if provided.
		if since != "" {
			sinceTime, err := time.Parse(time.RFC3339, since)
			if err == nil {
				var filtered []graphQLDiscussionNode
				for _, n := range nodes {
					if n.UpdatedAt.After(sinceTime) || n.UpdatedAt.Equal(sinceTime) {
						filtered = append(filtered, n)
					}
				}
				nodes = filtered
			}
		}

		allDiscussions = append(allDiscussions, nodes...)

		if maxItems > 0 && len(allDiscussions) >= maxItems {
			allDiscussions = allDiscussions[:maxItems]
			break
		}

		if !resp.Repository.Discussions.PageInfo.HasNextPage {
			break
		}

		cursor = &resp.Repository.Discussions.PageInfo.EndCursor
	}

	return allDiscussions, nil
}

// parseGraphQLResponse unmarshals the data portion of a GraphQL response into the target.
func parseGraphQLResponse(gqlResp *graphQLResponse, target any) error {
	if gqlResp == nil {
		return errors.New("nil graphql response")
	}

	if len(gqlResp.Data) == 0 {
		return errors.New("empty graphql response data")
	}

	if err := json.Unmarshal(gqlResp.Data, target); err != nil {
		return fmt.Errorf("unmarshal graphql data: %w", err)
	}

	return nil
}

// getDiscussionLabelNames extracts label names from a discussion node.
func getDiscussionLabelNames(node *graphQLDiscussionNode) []string {
	if node.Labels == nil {
		return nil
	}
	names := make([]string, len(node.Labels.Nodes))
	for i, l := range node.Labels.Nodes {
		names[i] = l.Name
	}
	return names
}

// getDiscussionCategory returns the category name from a discussion node.
func getDiscussionCategory(node *graphQLDiscussionNode) string {
	if node.Category == nil {
		return ""
	}
	return node.Category.Name
}
