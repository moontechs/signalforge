// Package github implements a collector for GitHub Issues + Discussions.
package github

import "time"

// collectionStrategy represents the approach used to collect issues.
type collectionStrategy int

const (
	// strategySearch uses the GitHub Search Issues API.
	strategySearch collectionStrategy = iota
	// strategyPerRepo uses per-repository issue listing.
	strategyPerRepo
)

// collectionScope is the concrete collection strategy derived from.
// configuration and request inputs.
type collectionScope struct {
	strategy          collectionStrategy
	repos             []string // populated per-repo targets (if strategyPerRepo).
	labels            []string
	languages         []string
	maxItems          int
	maxComments       int
	since             string // ISO date string for incremental collection.
	searchIssues      bool
	searchDiscussions bool
}

// deriveScope maps GitHubConfig + CollectRequest into a collectionScope.
func deriveScope(cfg configValues, repos []string, labels []string, languages []string, maxItems int, maxComments int, since string) collectionScope {
	scope := collectionScope{
		labels:            labels,
		languages:         languages,
		maxItems:          maxItems,
		maxComments:       maxComments,
		since:             since,
		searchIssues:      cfg.SearchIssues,
		searchDiscussions: cfg.SearchDiscussions,
	}

	if len(repos) > 0 {
		scope.strategy = strategyPerRepo
		scope.repos = repos
	} else {
		scope.strategy = strategySearch
	}

	return scope
}

// configValues holds the subset of config fields needed by the collector.
type configValues struct {
	Enabled            bool
	SearchIssues       bool
	SearchDiscussions  bool
	MaxItemsPerRun     int
	MaxCommentsPerItem int
	Repositories       []string
	Languages          []string
	Labels             []string
}

// cachedResponse holds response data for the on-disk cache.
type cachedResponse struct {
	Body         []byte    `json:"body"`
	ETag         string    `json:"etag,omitempty"`
	LastModified string    `json:"last_modified,omitempty"`
	CollectedAt  time.Time `json:"collected_at"`
}

// rateLimitCounters tracks remaining quota for REST and GraphQL APIs.
type rateLimitCounters struct {
	restRemaining    int
	restReset        time.Time
	graphQLRemaining int
	graphQLReset     time.Time
}
