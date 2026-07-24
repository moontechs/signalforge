// Package cache provides a thread-safe, namespace-isolated on-disk cache.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/moontechs/signalforge/internal/storage"
)

var validNamespace = regexp.MustCompile(`^[A-Za-z0-9]+$`)

// CacheEntry is a response body and its per-entry expiration policy.
type CacheEntry struct {
	Body     []byte        `json:"body"`
	TTL      time.Duration `json:"ttl"`
	StoredAt time.Time     `json:"stored_at"`
}

// Cache stores entries under cache/<namespace>. Keys are hashed before being
// used as filenames, and an entry is reusable only until StoredAt+TTL.
type Cache struct {
	mu        sync.RWMutex
	store     *storage.Storage
	namespace string
	prefix    string
}

// NewCache creates a cache for namespace. Invalid namespaces panic because a
// cache cannot function safely without a valid, isolated storage path.
func NewCache(store *storage.Storage, namespace string) *Cache {
	if store == nil {
		panic("cache: storage must not be nil")
	}
	if !validNamespace.MatchString(namespace) {
		panic(fmt.Sprintf("cache: invalid namespace %q (must be alphanumeric)", namespace))
	}
	return &Cache{store: store, namespace: namespace, prefix: filepath.Join(store.BaseDir(), "cache", namespace)}
}

func (c *Cache) path(key string) string {
	digest := sha256.Sum256([]byte(key))
	return filepath.Join(c.prefix, hex.EncodeToString(digest[:])+".json")
}

// Get returns a defensive copy of a fresh entry's body.
func (c *Cache) Get(key string) ([]byte, bool) {
	if key == "" {
		return nil, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	var entry CacheEntry
	if err := c.store.LoadJSON(c.path(key), &entry); err != nil || entry.TTL <= 0 || !time.Now().Before(entry.StoredAt.Add(entry.TTL)) {
		return nil, false
	}
	return append([]byte(nil), entry.Body...), true
}

// Set stores entry atomically. Keys must be non-empty and TTL must be positive.
func (c *Cache) Set(key string, entry CacheEntry) error {
	if key == "" {
		return fmt.Errorf("cache: key must not be empty")
	}
	if entry.TTL <= 0 {
		return fmt.Errorf("cache: TTL must be positive")
	}
	if entry.StoredAt.IsZero() {
		entry.StoredAt = time.Now()
	}
	entry.Body = append([]byte(nil), entry.Body...)
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.store.SaveJSON(c.path(key), entry)
}

// Delete removes an entry. It is safe to call when the entry is absent.
func (c *Cache) Delete(key string) error {
	if key == "" {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	err := os.Remove(c.path(key))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
