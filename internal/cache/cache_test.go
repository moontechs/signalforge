package cache

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/moontechs/signalforge/internal/storage"
)

func testCache(t *testing.T, namespace string) *Cache {
	t.Helper()
	return NewCache(storage.New(t.TempDir()), namespace)
}

func TestCacheRoundTripExpiryAndCopy(t *testing.T) {
	c := testCache(t, "tests")
	stored := time.Now().Add(-time.Millisecond)
	if err := c.Set("key", CacheEntry{Body: []byte("body"), TTL: time.Hour, StoredAt: stored}); err != nil {
		t.Fatal(err)
	}
	body, ok := c.Get("key")
	if !ok || string(body) != "body" {
		t.Fatalf("got %q, %v", body, ok)
	}
	body[0] = 'X'
	body, _ = c.Get("key")
	if string(body) != "body" {
		t.Fatal("Get did not return a defensive copy")
	}
	if err := c.Set("zero", CacheEntry{Body: []byte("x"), TTL: time.Hour}); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Get("zero"); !ok {
		t.Fatal("zero StoredAt entry was not readable")
	}
	if err := c.Set("old", CacheEntry{Body: []byte("x"), TTL: time.Millisecond, StoredAt: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Get("old"); ok {
		t.Fatal("expired entry was readable")
	}
}

func TestCacheDeleteNamespacesAndHashedKeys(t *testing.T) {
	root := t.TempDir()
	a, b := NewCache(storage.New(root), "one"), NewCache(storage.New(root), "two")
	if err := a.Set("same", CacheEntry{Body: []byte("a"), TTL: time.Hour}); err != nil {
		t.Fatal(err)
	}
	if err := b.Set("same", CacheEntry{Body: []byte("b"), TTL: time.Hour}); err != nil {
		t.Fatal(err)
	}
	if got, _ := b.Get("same"); string(got) != "b" {
		t.Fatal("namespace collision")
	}
	if err := a.Delete("same"); err != nil {
		t.Fatal(err)
	}
	if err := a.Delete("missing"); err != nil {
		t.Fatal(err)
	}
	files, err := os.ReadDir(filepath.Join(root, "cache", "two"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f.Name() == "same" {
			t.Fatal("raw key used as filename")
		}
	}
}

func TestCacheValidationAndConcurrency(t *testing.T) {
	for _, ns := range []string{"", "../bad", "has-hyphen"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("namespace %q did not panic", ns)
				}
			}()
			NewCache(storage.New(t.TempDir()), ns)
		}()
	}
	c := testCache(t, "concurrent")
	if err := c.Set("", CacheEntry{TTL: time.Hour}); err == nil {
		t.Fatal("empty key accepted")
	}
	if err := c.Set("bad", CacheEntry{TTL: 0}); err == nil {
		t.Fatal("invalid TTL accepted")
	}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				_ = c.Set("k", CacheEntry{Body: []byte("v"), TTL: time.Hour})
				c.Get("k")
				_ = c.Delete("k")
			}
		}()
	}
	wg.Wait()
}
