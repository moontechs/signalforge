package github

import "time"

// SearchIssuesResponse is the subset of the GitHub REST search response used by MVP collection.
type SearchIssuesResponse struct {
	TotalCount int         `json:"total_count"`
	Items      []IssueItem `json:"items"`
}

// IssueItem models the fields needed from GitHub issue search results.
type IssueItem struct {
	ID             int64           `json:"id"`
	Number         int             `json:"number"`
	NodeID         string          `json:"node_id"`
	HTMLURL        string          `json:"html_url"`
	Title          string          `json:"title"`
	Body           string          `json:"body"`
	State          string          `json:"state"`
	Locked         bool            `json:"locked"`
	Comments       int             `json:"comments"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
	ClosedAt       *time.Time      `json:"closed_at"`
	RepositoryURL  string          `json:"repository_url"`
	RepositoryName string          `json:"repository_name,omitempty"`
	Labels         []Label         `json:"labels"`
	User           User            `json:"user"`
	Reactions      ReactionSummary `json:"reactions"`
}

// IssueCommentsResponse is the REST response body for issue comments.
type IssueCommentsResponse []IssueComment

// IssueComment models the fields needed from GitHub issue comments.
type IssueComment struct {
	ID                int64           `json:"id"`
	NodeID            string          `json:"node_id"`
	HTMLURL           string          `json:"html_url"`
	Body              string          `json:"body"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
	User              User            `json:"user"`
	Reactions         ReactionSummary `json:"reactions"`
	AuthorAssociation string          `json:"author_association"`
}

// Label models a GitHub issue label.
type Label struct {
	Name string `json:"name"`
}

// User models the subset of author data needed for metadata.
type User struct {
	Login string `json:"login"`
	Type  string `json:"type"`
}

// ReactionSummary models GitHub reaction counts used for engagement totals.
type ReactionSummary struct {
	TotalCount int `json:"total_count"`
}

// GraphQLResponse wraps GitHub GraphQL responses and errors.
type GraphQLResponse[T any] struct {
	Data   T              `json:"data"`
	Errors []GraphQLError `json:"errors,omitempty"`
}

// GraphQLError models a GitHub GraphQL error object.
type GraphQLError struct {
	Message string `json:"message"`
	Type    string `json:"type,omitempty"`
	Path    []any  `json:"path,omitempty"`
}

// DiscussionsQueryData contains the MVP discussion query response.
type DiscussionsQueryData struct {
	Search SearchResultConnection `json:"search"`
}

// SearchResultConnection models the search connection used for discussions.
type SearchResultConnection struct {
	PageInfo PageInfo     `json:"pageInfo"`
	Nodes    []Discussion `json:"nodes"`
}

// Discussion contains the fields needed to map a GitHub discussion into RawSignal.
type Discussion struct {
	ID          string                      `json:"id"`
	Number      int                         `json:"number"`
	Title       string                      `json:"title"`
	Body        string                      `json:"body"`
	URL         string                      `json:"url"`
	Locked      bool                        `json:"locked"`
	Closed      bool                        `json:"closed"`
	CreatedAt   time.Time                   `json:"createdAt"`
	UpdatedAt   time.Time                   `json:"updatedAt"`
	Repository  Repository                  `json:"repository"`
	Category    DiscussionCategory          `json:"category"`
	Labels      LabelConnection             `json:"labels"`
	Comments    DiscussionCommentConnection `json:"comments"`
	Reactions   ReactionConnection          `json:"reactions"`
	UpvoteCount int                         `json:"upvoteCount"`
	Author      Actor                       `json:"author"`
}

// Repository identifies a GitHub repository.
type Repository struct {
	NameWithOwner string `json:"nameWithOwner"`
}

// DiscussionCategory identifies a discussion category.
type DiscussionCategory struct {
	Name string `json:"name"`
}

// LabelConnection contains discussion labels.
type LabelConnection struct {
	Nodes []Label `json:"nodes"`
}

// DiscussionCommentConnection contains discussion comments and paging.
type DiscussionCommentConnection struct {
	TotalCount int                 `json:"totalCount"`
	PageInfo   PageInfo            `json:"pageInfo"`
	Nodes      []DiscussionComment `json:"nodes"`
}

// DiscussionComment models the fields needed from a GitHub discussion comment.
type DiscussionComment struct {
	ID         string             `json:"id"`
	Body       string             `json:"body"`
	URL        string             `json:"url"`
	CreatedAt  time.Time          `json:"createdAt"`
	UpdatedAt  time.Time          `json:"updatedAt"`
	ReplyCount int                `json:"replyCount"`
	IsAnswer   bool               `json:"isAnswer"`
	Author     Actor              `json:"author"`
	Reactions  ReactionConnection `json:"reactions"`
}

// ReactionConnection models reaction totals in GraphQL.
type ReactionConnection struct {
	TotalCount int `json:"totalCount"`
}

// Actor models the minimal GraphQL author object.
type Actor struct {
	Login string `json:"login"`
}

// PageInfo models GraphQL pagination state.
type PageInfo struct {
	EndCursor   string `json:"endCursor"`
	HasNextPage bool   `json:"hasNextPage"`
}
