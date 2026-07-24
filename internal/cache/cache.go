package cache

import (
	"fmt"
	"sync"
	"time"
)

const (
	NamespaceGitHub             = "github"
	NamespaceHackerNews         = "hackernews"
	NamespaceStackExchange      = "stackexchange"
	NamespaceReddit             = "reddit"
	NamespaceBrightDataSERP     = "brightdata-serp"
	NamespaceBrightDataUnlocker = "brightdata-unlocker"
	NamespaceOpenRouter         = "openrouter"
)

var supportedNamespaces = map[string]struct{}{
	NamespaceGitHub: {}, NamespaceHackerNews: {}, NamespaceStackExchange: {},
	NamespaceReddit: {}, NamespaceBrightDataSERP: {}, NamespaceBrightDataUnlocker: {},
	NamespaceOpenRouter: {},
}

type entry struct {
	value     any
	expiresAt time.Time
}

// Config contains the per-source cache TTLs. Every configured namespace must
// be one of the supported namespaces and have a positive TTL.
type Config struct {
	Sources map[string]time.Duration
}

// Cache is a thread-safe, process-local cache partitioned by source namespace.
type Cache struct {
	mu      sync.RWMutex
	entries map[string]map[string]entry
	ttls    map[string]time.Duration
}

// New creates a cache using the supplied per-source TTL configuration.
func New(cfg Config) (*Cache, error) {
	ttls := make(map[string]time.Duration, len(cfg.Sources))
	for namespace, ttl := range cfg.Sources {
		if _, ok := supportedNamespaces[namespace]; !ok {
			return nil, fmt.Errorf("unsupported cache namespace %q", namespace)
		}
		if ttl <= 0 {
			return nil, fmt.Errorf("cache TTL for namespace %q must be positive", namespace)
		}
		ttls[namespace] = ttl
	}
	return &Cache{entries: make(map[string]map[string]entry), ttls: ttls}, nil
}

// Get returns the fresh value stored under key in namespace. An absent,
// expired, or unconfigured namespace returns false.
func (c *Cache) Get(namespace, key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cleanup(time.Now())

	entries, ok := c.entries[namespace]
	if !ok {
		return nil, false
	}
	item, ok := entries[key]
	if !ok {
		return nil, false
	}
	return item.value, true
}

// Set stores value under key in namespace. Writes to unconfigured namespaces
// are ignored because no expiration policy exists for them.
func (c *Cache) Set(namespace, key string, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	c.cleanup(now)
	ttl, ok := c.ttls[namespace]
	if !ok {
		return
	}
	if c.entries[namespace] == nil {
		c.entries[namespace] = make(map[string]entry)
	}
	c.entries[namespace][key] = entry{value: value, expiresAt: now.Add(ttl)}
}

// Delete removes key from namespace.
func (c *Cache) Delete(namespace, key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cleanup(time.Now())
	if entries := c.entries[namespace]; entries != nil {
		delete(entries, key)
	}
}

// Clear removes all entries from namespace.
func (c *Cache) Clear(namespace string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cleanup(time.Now())
	delete(c.entries, namespace)
}

func (c *Cache) cleanup(now time.Time) {
	for namespace, entries := range c.entries {
		for key, item := range entries {
			if !now.Before(item.expiresAt) {
				delete(entries, key)
			}
		}
		if len(entries) == 0 {
			delete(c.entries, namespace)
		}
	}
}
