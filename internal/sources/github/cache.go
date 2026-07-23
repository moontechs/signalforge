package github

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/moontechs/signalforge/internal/storage"
)

const (
	// DefaultCacheTTL is the default time-to-live for cached responses.
	DefaultCacheTTL = 30 * time.Minute

	// CacheDir is the relative directory under the storage base for GitHub cache.
	cacheDir = "cache/github"
)

// responseCache provides TTL-based disk caching for GitHub API responses.
// It stores response body, ETag, Last-Modified, and a collection timestamp.
// Fresh entries (within TTL) are returned directly from disk.
// Expired entries with ETag/LM headers trigger conditional revalidation.
// Expired entries without ETag/LM trigger a full re-fetch.
type responseCache struct {
	store *storage.Storage
	ttl   time.Duration
	mu    sync.RWMutex
}

// newResponseCache creates a new response cache backed by the given storage.
func newResponseCache(store *storage.Storage) *responseCache {
	return &responseCache{
		store: store,
		ttl:   DefaultCacheTTL,
		mu:    sync.RWMutex{},
	}
}

// cacheFilePath converts a cache key to a stable, filesystem-safe file path.
// The path is absolute, under the storage base directory's cache/github/.
func (rc *responseCache) cacheFilePath(key string) string {
	hash := sha256.Sum256([]byte(key))
	filename := fmt.Sprintf("%x.json", hash[:16])
	return rc.store.Path(filepath.Join(cacheDir, filename))
}

// get retrieves a cached response for the given key.
// It returns:
//   - a pointer to the cached response, and fresh=true if within TTL
//   - a pointer to the cached response, and fresh=false if expired
//   - nil, false if no cached entry exists
func (rc *responseCache) get(key string) (*cachedResponse, bool) {
	rc.mu.RLock()
	defer rc.mu.RUnlock()

	path := rc.cacheFilePath(key)
	if !rc.store.Exists(path) {
		return nil, false
	}

	var entry cachedResponse
	if err := rc.store.LoadJSON(path, &entry); err != nil {
		return nil, false
	}

	// Check TTL.
	if time.Since(entry.CollectedAt) <= rc.ttl {
		return &entry, true
	}

	// Expired but return the entry so callers can use ETag/LM for conditional requests.
	return &entry, false
}

// set stores a response in the cache.
// Cache write failures are non-fatal (logged only via error returns, not panics).
func (rc *responseCache) set(key string, body []byte, etag, lastModified string) {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	entry := cachedResponse{
		Body:         body,
		ETag:         etag,
		LastModified: lastModified,
		CollectedAt:  time.Now(),
	}

	path := rc.cacheFilePath(key)
	if err := rc.store.SaveJSON(path, &entry); err != nil {
		// Cache write failures are non-fatal.
		return
	}
}

// touch updates the collected-at timestamp for a cache entry,
// keeping the existing body and headers but extending the TTL.
// This is called when a 304 Not Modified response is received.
func (rc *responseCache) touch(key string) {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	path := rc.cacheFilePath(key)
	if !rc.store.Exists(path) {
		return
	}

	var entry cachedResponse
	if err := rc.store.LoadJSON(path, &entry); err != nil {
		return
	}

	entry.CollectedAt = time.Now()
	if err := rc.store.SaveJSON(path, &entry); err != nil {
		return
	}
}
