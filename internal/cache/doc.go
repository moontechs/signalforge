// Package cache provides a shared, local JSON cache. Each cache has one
// alphanumeric namespace beneath cache/, preventing sources from colliding.
// Keys are hashed, so raw keys never become filenames. Entries carry their
// own TTL and expire lazily on reads; cache methods are safe for concurrent use.
package cache
