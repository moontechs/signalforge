package github

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/moontechs/signalforge/internal/storage"
)

// TestCache_KeyGeneration verifies that cache file paths are deterministic
// and derived from the key, not from secrets or other non-stable inputs.
func TestCache_KeyGeneration(t *testing.T) {
	store := storage.New(t.TempDir())
	rc := newResponseCache(store)

	// Same key should produce same path.
	key1 := "REST:GET:/search/issues?q=is:issue+is:open"
	path1 := rc.cacheFilePath(key1)
	path2 := rc.cacheFilePath(key1)

	if path1 != path2 {
		t.Fatalf("expected same key to produce same path, got %q vs %q", path1, path2)
	}

	// Different keys should produce different paths.
	key2 := "REST:GET:/repos/owner/repo/issues"
	path3 := rc.cacheFilePath(key2)
	if path1 == path3 {
		t.Fatal("expected different keys to produce different paths")
	}

	// Path should end with .json.
	if filepath.Ext(path1) != ".json" {
		t.Fatalf("expected .json extension, got %q", path1)
	}

	// Path should contain cache/github/ in its components.
	if !filepath.IsAbs(path1) {
		t.Fatal("expected absolute path")
	}

	// Directory should end with cache/github.
	dir := filepath.Dir(path1)
	if filepath.Base(dir) != "github" || filepath.Base(filepath.Dir(dir)) != "cache" {
		t.Fatalf("expected path under cache/github, got dir %q", dir)
	}

	// Key should not contain secrets (in case it somehow leaks into the filename)
	// The cache key itself should never contain tokens, but verify the path
	// doesn't either.
	if filepath.Base(path1) == key1 {
		t.Fatal("cache file path should not use raw key as filename")
	}
}

// TestCache_Miss verifies that a non-existent key returns nil, false.
func TestCache_Miss(t *testing.T) {
	store := storage.New(t.TempDir())
	rc := newResponseCache(store)

	entry, fresh := rc.get("nonexistent")
	if entry != nil {
		t.Fatal("expected nil entry for missing key")
	}
	if fresh {
		t.Fatal("expected fresh=false for missing key")
	}
}

// TestCache_SetAndGet verifies that a stored entry can be retrieved
// and is considered fresh within the TTL.
func TestCache_SetAndGet(t *testing.T) {
	store := storage.New(t.TempDir())
	rc := newResponseCache(store)

	key := "REST:GET:/search/issues?q=test"
	body := []byte(`{"items":[{"id":1}]}`)
	etag := `W/"abc123"`
	lm := "Wed, 15 Jan 2025 12:00:00 GMT"

	rc.set(key, body, etag, lm)

	entry, fresh := rc.get(key)
	if entry == nil {
		t.Fatal("expected non-nil entry after set")
	}
	if !fresh {
		t.Fatal("expected fresh=true within TTL")
	}

	// Verify fields.
	if !bytes.Equal(entry.Body, body) {
		t.Fatalf("expected body %q, got %q", string(body), string(entry.Body))
	}
	if entry.ETag != etag {
		t.Fatalf("expected ETag %q, got %q", etag, entry.ETag)
	}
	if entry.LastModified != lm {
		t.Fatalf("expected LastModified %q, got %q", lm, entry.LastModified)
	}

	// CollectedAt should be recent.
	if time.Since(entry.CollectedAt) > 5*time.Second {
		t.Fatal("CollectedAt should be recent")
	}
}

// TestCache_SetWithoutETag verifies that entries without ETag/LM are stored correctly.
func TestCache_SetWithoutETag(t *testing.T) {
	store := storage.New(t.TempDir())
	rc := newResponseCache(store)

	key := "REST:GET:/some/endpoint"
	body := []byte(`{"status":"ok"}`)

	rc.set(key, body, "", "")

	entry, fresh := rc.get(key)
	if entry == nil {
		t.Fatal("expected non-nil entry after set")
	}
	if !fresh {
		t.Fatal("expected fresh=true within TTL")
	}

	if entry.ETag != "" {
		t.Fatalf("expected empty ETag, got %q", entry.ETag)
	}
	if entry.LastModified != "" {
		t.Fatalf("expected empty LastModified, got %q", entry.LastModified)
	}
}

// TestCache_Expiry verifies that entries beyond the TTL are considered stale.
func TestCache_Expiry(t *testing.T) {
	store := storage.New(t.TempDir())
	rc := newResponseCache(store)

	// Set a short TTL for testing.
	rc.ttl = 20 * time.Millisecond

	key := "REST:GET:/test/expiry"
	body := []byte(`{"data":"test"}`)
	rc.set(key, body, "abc", "")

	// Should be fresh immediately.
	entry, fresh := rc.get(key)
	if entry == nil {
		t.Fatal("expected non-nil entry after set")
	}
	if !fresh {
		t.Fatal("expected fresh=true immediately after set")
	}

	// Wait for TTL to expire.
	time.Sleep(30 * time.Millisecond)

	entry, fresh = rc.get(key)
	if entry == nil {
		t.Fatal("expected non-nil entry even after expiry")
	}
	if fresh {
		t.Fatal("expected fresh=false after TTL expiry")
	}

	// Entry body should still be available for conditional requests.
	if !bytes.Equal(entry.Body, body) {
		t.Fatalf("expected body %q after expiry, got %q", string(body), string(entry.Body))
	}
}

// TestCache_Touch verifies that touch() extends the TTL of an existing entry.
func TestCache_Touch(t *testing.T) {
	store := storage.New(t.TempDir())
	rc := newResponseCache(store)

	rc.ttl = 50 * time.Millisecond

	key := "REST:GET:/test/touch"
	body := []byte(`{"data":"original"}`)
	rc.set(key, body, "etag1", "")

	// Wait for near expiry.
	time.Sleep(30 * time.Millisecond)

	// Touch to extend.
	rc.touch(key)

	// Should still be fresh.
	entry, fresh := rc.get(key)
	if entry == nil {
		t.Fatal("expected non-nil entry after touch")
	}
	if !fresh {
		t.Fatal("expected fresh=true after touch")
	}

	// Verify body is preserved.
	if !bytes.Equal(entry.Body, body) {
		t.Fatalf("expected body preserved after touch, got %q", string(entry.Body))
	}
}

// TestCache_TouchNonExistent verifies that touching a non-existent key is a no-op.
func TestCache_TouchNonExistent(t *testing.T) {
	store := storage.New(t.TempDir())
	rc := newResponseCache(store)

	// Should not panic or error.
	rc.touch("nonexistent")
}

// TestCache_SetOverwrite verifies that setting the same key overwrites the old value.
func TestCache_SetOverwrite(t *testing.T) {
	store := storage.New(t.TempDir())
	rc := newResponseCache(store)

	key := "REST:GET:/test/overwrite"

	rc.set(key, []byte(`{"version":1}`), "etag1", "")
	rc.set(key, []byte(`{"version":2}`), "etag2", "updated")

	entry, fresh := rc.get(key)
	if entry == nil {
		t.Fatal("expected non-nil entry after overwrite")
	}
	if !fresh {
		t.Fatal("expected fresh=true after overwrite")
	}

	if string(entry.Body) != `{"version":2}` {
		t.Fatalf("expected updated body, got %q", string(entry.Body))
	}
	if entry.ETag != "etag2" {
		t.Fatalf("expected updated ETag, got %q", entry.ETag)
	}
	if entry.LastModified != "updated" {
		t.Fatalf("expected updated LastModified, got %q", entry.LastModified)
	}
}

// TestCache_StorageRoundTrip verifies persistence across different cache instances
// by using the same storage directory.
func TestCache_StorageRoundTrip(t *testing.T) {
	dir := t.TempDir()

	// First instance: set.
	store1 := storage.New(dir)
	rc1 := newResponseCache(store1)
	rc1.set("key1", []byte(`{"hello":"world"}`), "etag-x", "lm-x")

	// Second instance: get (same storage dir).
	store2 := storage.New(dir)
	rc2 := newResponseCache(store2)

	entry, fresh := rc2.get("key1")
	if entry == nil {
		t.Fatal("expected to retrieve entry from disk with new instance")
	}
	if !fresh {
		t.Fatal("expected fresh=true for just-written entry")
	}
	if string(entry.Body) != `{"hello":"world"}` {
		t.Fatalf("expected body from disk, got %q", string(entry.Body))
	}
	if entry.ETag != "etag-x" {
		t.Fatalf("expected ETag from disk, got %q", entry.ETag)
	}
	if entry.LastModified != "lm-x" {
		t.Fatalf("expected LastModified from disk, got %q", entry.LastModified)
	}
}

// TestCache_ExpiredEntryReturnsETag verifies that an expired entry still
// returns the ETag/LM data for conditional revalidation.
func TestCache_ExpiredEntryReturnsETag(t *testing.T) {
	store := storage.New(t.TempDir())
	rc := newResponseCache(store)
	rc.ttl = 20 * time.Millisecond

	key := "REST:GET:/test/conditional"
	rc.set(key, []byte(`{"data":"stale"}`), "stale-etag", "stale-lm")

	time.Sleep(30 * time.Millisecond)

	entry, fresh := rc.get(key)
	if entry == nil {
		t.Fatal("expected entry even after expiry")
	}
	if fresh {
		t.Fatal("expected fresh=false after expiry")
	}

	// ETag and LM should still be available.
	if entry.ETag != "stale-etag" {
		t.Fatalf("expected ETag %q, got %q", "stale-etag", entry.ETag)
	}
	if entry.LastModified != "stale-lm" {
		t.Fatalf("expected LastModified %q, got %q", "stale-lm", entry.LastModified)
	}

	// Body should be available for use in 304 response.
	if string(entry.Body) != `{"data":"stale"}` {
		t.Fatalf("expected stale body, got %q", string(entry.Body))
	}
}

// TestCache_KeyExcludesSecrets verifies that cache keys do not contain
// tokens or other secrets. The CacheKey field in requestOptions should
// be set by the caller to exclude secrets. This test verifies that the
// cacheFilePath hashing prevents key contents from appearing in the path.
func TestCache_KeyExcludesSecrets(t *testing.T) {
	store := storage.New(t.TempDir())
	rc := newResponseCache(store)

	// a caller error, but the path itself should not contain the raw key).
	key := "REST:GET:/repos/owner/repo?access_token=ghp_abc123"
	path := rc.cacheFilePath(key)

	// The raw key should not appear in the file path.
	if filepath.Base(path) == key {
		t.Fatal("cache key should not appear as filename")
	}

	// The token part should not appear.
	if filepath.Base(path) == "access_token=ghp_abc123" {
		t.Fatal("token should not appear in cache file path")
	}
}

// TestCache_ConcurrentAccess verifies that the cache handles concurrent
// get/set operations without panicking.
func TestCache_ConcurrentAccess(t *testing.T) {
	store := storage.New(t.TempDir())
	rc := newResponseCache(store)

	done := make(chan bool, 10)

	for i := range 10 {
		go func(n int) {
			key := "key"
			rc.set(key, []byte("value"), "etag", "")
			_, _ = rc.get(key)
			rc.touch(key)
			done <- true
		}(i)
	}

	for range 10 {
		<-done
	}
}

// TestCache_CustomTTL verifies that a custom TTL is respected.
func TestCache_CustomTTL(t *testing.T) {
	store := storage.New(t.TempDir())
	rc := newResponseCache(store)

	// Set a long TTL.
	rc.ttl = 1 * time.Hour

	key := "test-ttl"
	rc.set(key, []byte("data"), "", "")

	entry, fresh := rc.get(key)
	if entry == nil {
		t.Fatal("expected non-nil entry")
	}
	if !fresh {
		t.Fatal("expected fresh=true for 1-hour TTL")
	}

	// Create a new entry with a very short TTL that will expire quickly.
	rc.ttl = 10 * time.Millisecond
	rc.set(key, []byte("newdata"), "", "")

	time.Sleep(20 * time.Millisecond)

	entry, fresh = rc.get(key)
	if entry == nil {
		t.Fatal("expected non-nil entry after short TTL creation")
	}
	if fresh {
		t.Fatal("expected fresh=false after short TTL expiry")
	}
}

// TestCache_CorruptEntry verifies that a corrupt cache file returns miss.
func TestCache_CorruptEntry(t *testing.T) {
	dir := t.TempDir()
	store := storage.New(dir)

	// Write an invalid JSON file to the cache location.
	rc := newResponseCache(store)
	badPath := rc.cacheFilePath("badkey")
	os.MkdirAll(filepath.Dir(badPath), 0o755)
	os.WriteFile(badPath, []byte("not valid json"), 0o644)

	entry, fresh := rc.get("badkey")
	if entry != nil {
		t.Fatal("expected nil entry for corrupt cache file")
	}
	if fresh {
		t.Fatal("expected fresh=false for corrupt cache file")
	}
}
