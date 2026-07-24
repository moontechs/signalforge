// Package cache provides a process-local, in-memory cache for source data.
//
// Entries are grouped into source namespaces and expire according to the
// namespace's configured TTL. Expired entries are evicted lazily during cache
// operations. Cache methods are safe for concurrent use. Cache keys must not
// contain credentials, API tokens, or other secrets.
package cache
