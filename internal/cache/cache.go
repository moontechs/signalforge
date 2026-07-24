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
