# Plan: Add Thread-Safe In-Memory Source Cache with Per-Source TTLs

### Task 1: Create the `internal/cache` package — types, doc, and constructor

- [x] Add `internal/cache/doc.go` with package-level documentation describing the process-local cache, source namespaces, TTL behavior, lazy automatic eviction, concurrency guarantees, and the prohibition on including credentials or API tokens in cache keys.
- [x] Define constants for supported source namespaces: `NamespaceGitHub`, `NamespaceHackerNews`, `NamespaceStackExchange`, `NamespaceReddit`, `NamespaceBrightDataSERP`, `NamespaceBrightDataUnlocker`, `NamespaceOpenRouter`.
- [x] Define `Cache` struct with `sync.RWMutex`, private `entries map[string]map[string]entry` and `ttls map[string]time.Duration`.
- [x] Define `entry` struct with `value any` and `expiresAt time.Time`.
- [x] Define `Config` struct with `Sources map[string]time.Duration` for per-source TTL configuration.
- [x] Implement `New(cfg Config) (*Cache, error)` — validates source names, rejects unknown namespaces and non-positive TTLs, stores defaults.
- [x] Store each entry with its value and absolute expiration timestamp.

### Task 2: Implement cache operations — Get, Set, Delete, Clear

- [ ] Implement `Get(namespace, key string) (any, bool)` — read-lock, lookup namespace→key, check expiry, return nil+false if absent or expired, value+true if fresh. Remove expired entries before returning.
- [ ] Implement `Set(namespace, key string, value any)` — write-lock, store value with expiry = now + namespace's TTL.
- [ ] Implement `Delete(namespace, key string)` — write-lock, remove entry from one namespace only.
- [ ] Implement `Clear(namespace string)` — write-lock, remove all entries for one namespace.
- [ ] Run lazy expiration cleanup on every Get/Set/Delete/Clear — scan and remove expired entries under the held lock.
- [ ] Keep source namespaces fully isolated — same key in different namespaces cannot collide.

### Task 3: Add comprehensive tests

- [ ] Add `internal/cache/cache_test.go` with proper package name `cache_test` (external test package).
- [ ] Test: New with valid config returns no error.
- [ ] Test: New with invalid namespace returns error.
- [ ] Test: New with zero/negative TTL returns error.
- [ ] Test: Set + Get returns stored value within TTL.
- [ ] Test: Get returns nil+false for missing key.
- [ ] Test: Get returns nil+false for expired entry (use short TTL + time.Sleep or time-based mock).
- [ ] Test: Identical keys in different namespaces return independent values.
- [ ] Test: Delete removes only the specified key.
- [ ] Test: Clear removes all entries from one namespace, leaves others intact.
- [ ] Test: Concurrent access with goroutines and race detector.

## Validation Commands

gofmt -w internal/cache
go test ./internal/cache/...
go test -race ./internal/cache/...
go test ./...
go vet ./...
golangci-lint run ./...
