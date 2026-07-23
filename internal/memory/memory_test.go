package memory

import (
	"sync"
	"testing"

	"github.com/moontechs/signalforge/internal/storage"
)

func setupTestMemory(t *testing.T) *DefaultMemory {
	t.Helper()
	s := storage.New(t.TempDir())
	return New(s)
}

func TestAddHNRequests(t *testing.T) {
	t.Parallel()

	t.Run("basic increment", func(t *testing.T) {
		t.Parallel()
		m := setupTestMemory(t)
		m.AddHNRequests(5)
		stats := m.GetStats()
		if stats.HackerNewsRequests != 5 {
			t.Errorf("expected 5 HN requests, got %d", stats.HackerNewsRequests)
		}
	})

	t.Run("zero no change", func(t *testing.T) {
		t.Parallel()
		m := setupTestMemory(t)
		m.AddHNRequests(0)
		stats := m.GetStats()
		if stats.HackerNewsRequests != 0 {
			t.Errorf("expected 0 HN requests, got %d", stats.HackerNewsRequests)
		}
	})

	t.Run("negative no change", func(t *testing.T) {
		t.Parallel()
		m := setupTestMemory(t)
		m.AddHNRequests(-3)
		stats := m.GetStats()
		if stats.HackerNewsRequests != 0 {
			t.Errorf("expected 0 HN requests, got %d", stats.HackerNewsRequests)
		}
	})

	t.Run("accumulation", func(t *testing.T) {
		t.Parallel()
		m := setupTestMemory(t)
		m.AddHNRequests(3)
		m.AddHNRequests(7)
		stats := m.GetStats()
		if stats.HackerNewsRequests != 10 {
			t.Errorf("expected 10 HN requests, got %d", stats.HackerNewsRequests)
		}
	})

	t.Run("concurrent safety", func(t *testing.T) {
		t.Parallel()
		m := setupTestMemory(t)
		var wg sync.WaitGroup
		n := 10
		wg.Add(n)
		for i := 0; i < n; i++ {
			go func() {
				defer wg.Done()
				m.AddHNRequests(1)
			}()
		}
		wg.Wait()
		stats := m.GetStats()
		if stats.HackerNewsRequests != n {
			t.Errorf("expected %d HN requests, got %d", n, stats.HackerNewsRequests)
		}
	})
}

func TestAddHNCacheHits(t *testing.T) {
	t.Parallel()

	t.Run("basic increment", func(t *testing.T) {
		t.Parallel()
		m := setupTestMemory(t)
		m.AddHNCacheHits(5)
		stats := m.GetStats()
		if stats.HackerNewsCacheHits != 5 {
			t.Errorf("expected 5 HN cache hits, got %d", stats.HackerNewsCacheHits)
		}
	})

	t.Run("zero no change", func(t *testing.T) {
		t.Parallel()
		m := setupTestMemory(t)
		m.AddHNCacheHits(0)
		stats := m.GetStats()
		if stats.HackerNewsCacheHits != 0 {
			t.Errorf("expected 0 HN cache hits, got %d", stats.HackerNewsCacheHits)
		}
	})

	t.Run("negative no change", func(t *testing.T) {
		t.Parallel()
		m := setupTestMemory(t)
		m.AddHNCacheHits(-3)
		stats := m.GetStats()
		if stats.HackerNewsCacheHits != 0 {
			t.Errorf("expected 0 HN cache hits, got %d", stats.HackerNewsCacheHits)
		}
	})

	t.Run("accumulation", func(t *testing.T) {
		t.Parallel()
		m := setupTestMemory(t)
		m.AddHNCacheHits(3)
		m.AddHNCacheHits(7)
		stats := m.GetStats()
		if stats.HackerNewsCacheHits != 10 {
			t.Errorf("expected 10 HN cache hits, got %d", stats.HackerNewsCacheHits)
		}
	})

	t.Run("concurrent safety", func(t *testing.T) {
		t.Parallel()
		m := setupTestMemory(t)
		var wg sync.WaitGroup
		n := 10
		wg.Add(n)
		for i := 0; i < n; i++ {
			go func() {
				defer wg.Done()
				m.AddHNCacheHits(1)
			}()
		}
		wg.Wait()
		stats := m.GetStats()
		if stats.HackerNewsCacheHits != n {
			t.Errorf("expected %d HN cache hits, got %d", n, stats.HackerNewsCacheHits)
		}
	})
}
