# Plan: Create shared cache module at `internal/cache/` and migrate HN + SE

### Task 1: Implement `internal/cache` shared cache module

- [x] Create `internal/cache/` package with a public `CacheEntry` type:
  - `Body []byte` — cached response body
  - `TTL time.Duration` — per-entry TTL (callers set this per source/use-case)
  - `StoredAt time.Time` — when the entry was stored
- [x] Provide a `NewCache(store *storage.Storage, namespace string) *Cache` constructor that:
  - stores the `storage.Storage` reference and namespace string
  - validates namespace is non-empty and alphanumeric (no path traversal)
  - uses `cache/<namespace>/` as the on-disk prefix under the storage base
- [x] Implement `Get(key string) ([]byte, bool)` (no context — cache is local I/O, not network):
  - derive a deterministic SHA-256 filename from the key
  - read the serialized entry through `storage.LoadJSON`
  - return `nil, false` for absent entries
  - treat entries whose `StoredAt.Add(TTL)` is before `time.Now()` as expired (cache miss)
  - return a defensive copy of `Body`
- [x] Implement `Set(key string, entry CacheEntry)`:
  - reject empty keys
  - set `StoredAt` to `time.Now()` if zero
  - write via `storage.SaveJSON` (atomic temp → sync → rename)
  - store only SHA-256 digest as filename, never raw key content
- [x] Implement `Delete(key string)` as idempotent removal of the hashed entry file
- [x] Make the module thread-safe using `sync.RWMutex` (RLock for Get, Lock for Set/Delete)
- [x] Document package with clear cache-key, expiration, namespace-isolation semantics

### Task 3: Add comprehensive shared-cache tests

- [x] Add `internal/cache` tests using a temporary storage root and deterministic test keys.
- [x] Verify set/get round trips preserve body, TTL, and stored timestamp behavior.
- [x] Verify `Get` returns a defensive body copy that callers cannot mutate in-place.
- [x] Verify zero `StoredAt` is initialized during `Set`.
- [x] Verify expired entries are cache misses and are deleted or otherwise no longer reusable.
- [x] Verify unexpired entries remain readable.
- [x] Verify `Delete` removes an existing entry and succeeds for a missing entry.
- [x] Verify distinct keys produce distinct cached values and no raw key is present in the resulting filename.
- [x] Verify the same key in two namespaces cannot collide or be read across namespaces.
- [x] Verify invalid namespaces, empty keys, and invalid TTLs return useful errors.
- [x] Add a concurrent test exercising simultaneous `Get`, `Set`, and `Delete` calls, suitable for `go test -race`.

### Task 4: Migrate Hacker News inline cache usage

- [ ] Replace HN's `WithCache(s *storage.Storage)` with `WithCache(c *cache.Cache)` — accept a shared cache instance instead of raw storage.
- [ ] Wire the shared cache into HN client/collector initialization with `cache.NewCache(store, "hackernews")`.
- [ ] Replace HN’s local `cached()` reads with shared-cache `Get` calls while preserving current cache-key inputs and cache-hit statistics.
- [ ] Replace HN’s local `save()` writes with shared-cache `Set` calls, preserving the existing feed and item TTL policy.
- [ ] Remove obsolete HN cache-path, serialization, `cached()`, and `save()` code once all call sites use `internal/cache`.
- [ ] Update HN tests to assert the existing caching behavior through the shared module without changing request/cache-hit semantics.

### Task 5: Migrate Stack Exchange inline cache usage

- [ ] Replace SE's `WithCache(s *storage.Storage)` with `WithCache(c *cache.Cache)` — accept a shared cache instance instead of raw storage.
- [ ] Wire the shared cache into SE client/collector initialization with `cache.NewCache(store, "stackexchange")`.
- [ ] Replace Stack Exchange `cached()` and `save()` call sites with shared-cache `Get` and `Set`.
- [ ] Preserve current Stack Exchange cache keys, TTL selection, response decoding, cache-hit accounting, and network fallback behavior.
- [ ] Remove obsolete Stack Exchange cache helpers and source-local cache serialization/path code.
- [ ] Update Stack Exchange tests to cover cache hits, expiry behavior, and source-specific namespace usage.

### Task 6: Validate integration and repository quality

- [ ] Run `gofmt` on all modified Go files.
- [ ] Run the focused shared-cache tests and source-package tests.
- [ ] Run the complete test suite and race detector.
- [ ] Run static analysis and the repository linter if installed.
- [ ] Confirm GitHub collector code and its `responseCache` remain unchanged.

## Validation Commands

gofmt -w internal/cache/*.go internal/sources/hackernews/*.go internal/sources/stackexchange/*.go
go test ./internal/cache/...
go test ./internal/sources/hackernews/... ./internal/sources/stackexchange/...
go test ./...
go test -race ./...
go vet ./...
golangci-lint run ./...
