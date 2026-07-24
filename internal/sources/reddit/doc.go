// Package reddit implements the Reddit source collector for SignalForge.
//
// It fetches subreddit listings and comment trees from the Reddit API via
// OAuth client-credentials flow, maps them to domain.RawSignal values, and
// caches public responses on disk. The package is structured into separate
// client, parser, and collector layers so tests can isolate each concern
// without network access.
//
// Reddit requires a registered application with REDDIT_CLIENT_ID and
// REDDIT_CLIENT_SECRET environment variables set. The collector uses the
// OAuth endpoints at https://www.reddit.com/api/v1/access_token and
// https://oauth.reddit.com for authenticated API access.
package reddit
