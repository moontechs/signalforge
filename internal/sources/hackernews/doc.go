// Package hackernews implements the Hacker News source collector for SignalForge.
//
// It fetches stories and comments from the HN Firebase API
// (https://hacker-news.firebaseio.com/v0), maps them to domain.RawSignal,
// and caches public responses on disk. The package is structured into
// separate client, parser, cache, and collector layers so tests can
// isolate each concern without network access.
//
// Hacker News requires no authentication. The Firebase API provides no
// ETags, Last-Modified headers, or conditional request support, so the
// cache is simple TTL-based only.
package hackernews
