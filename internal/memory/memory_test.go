package memory

import (
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/moontechs/signalforge/internal/domain"
	"github.com/moontechs/signalforge/internal/storage"
)

func setupTestMemory(t *testing.T) *DefaultMemory {
	t.Helper()
	s := storage.New(t.TempDir())
	return New(s)
}

func TestNew(t *testing.T) {
	t.Parallel()
	m := New(storage.New(t.TempDir()))
	if m == nil {
		t.Fatal("New returned nil")
	}
	stats := m.GetStats()
	if stats.RawSignalsCollected != 0 {
		t.Errorf("expected 0 collected, got %d", stats.RawSignalsCollected)
	}
	if m.RawSignalCount() != 0 {
		t.Errorf("expected 0 raw signals, got %d", m.RawSignalCount())
	}
}

func TestAddRawSignal_HasRawSignal(t *testing.T) {
	t.Parallel()
	m := setupTestMemory(t)

	if m.HasRawSignal("hn", "123") {
		t.Error("expected no signal before adding")
	}

	m.AddRawSignal("hn", "123")
	if !m.HasRawSignal("hn", "123") {
		t.Error("expected signal after adding")
	}

	stats := m.GetStats()
	if stats.RawSignalsCollected != 1 {
		t.Errorf("expected 1 collected, got %d", stats.RawSignalsCollected)
	}
}

func TestAddRawSignal_multiple(t *testing.T) {
	t.Parallel()
	m := setupTestMemory(t)

	m.AddRawSignal("hn", "1")
	m.AddRawSignal("hn", "2")
	m.AddRawSignal("github", "3")

	if m.RawSignalCount() != 3 {
		t.Errorf("expected 3 signals, got %d", m.RawSignalCount())
	}

	stats := m.GetStats()
	if stats.RawSignalsCollected != 3 {
		t.Errorf("expected 3 collected, got %d", stats.RawSignalsCollected)
	}
}

func TestHasContentHash_AddContentHash(t *testing.T) {
	t.Parallel()
	m := setupTestMemory(t)

	hash := "abc123"
	if m.HasContentHash(hash) {
		t.Error("expected no hash before adding")
	}

	m.AddContentHash(hash, "hn:1")
	if !m.HasContentHash(hash) {
		t.Error("expected hash after adding")
	}

	if m.ContentHashCount() != 1 {
		t.Errorf("expected 1 hash, got %d", m.ContentHashCount())
	}
}

func TestHasProblemFingerprint_AddProblemFingerprint(t *testing.T) {
	t.Parallel()
	m := setupTestMemory(t)

	fp := "problem:123"
	if m.HasProblemFingerprint(fp) {
		t.Error("expected no fingerprint before adding")
	}

	m.AddProblemFingerprint(fp, "signal:1")
	if !m.HasProblemFingerprint(fp) {
		t.Error("expected fingerprint after adding")
	}
}

func TestHasClusterFingerprint_AddClusterFingerprint(t *testing.T) {
	t.Parallel()
	m := setupTestMemory(t)

	fp := "cluster:abc"
	if m.HasClusterFingerprint(fp) {
		t.Error("expected no cluster fingerprint before adding")
	}

	m.AddClusterFingerprint(fp, "cluster:1")
	if !m.HasClusterFingerprint(fp) {
		t.Error("expected cluster fingerprint after adding")
	}
}

func TestHasIdeaFingerprint_AddIdeaFingerprint(t *testing.T) {
	t.Parallel()
	m := setupTestMemory(t)

	fp := "idea:xyz"
	if m.HasIdeaFingerprint(fp) {
		t.Error("expected no idea fingerprint before adding")
	}

	m.AddIdeaFingerprint(fp, "idea:1")
	if !m.HasIdeaFingerprint(fp) {
		t.Error("expected idea fingerprint after adding")
	}
}

func TestRecordQuery_HasQuery(t *testing.T) {
	t.Parallel()
	m := setupTestMemory(t)

	if m.HasQuery("hn", "test query") {
		t.Error("expected no query before recording")
	}

	m.RecordQuery("hn", "test query", 5)
	if !m.HasQuery("hn", "test query") {
		t.Error("expected query after recording")
	}
}

func TestAddRejectedPattern(t *testing.T) {
	t.Parallel()
	m := setupTestMemory(t)

	m.AddRejectedPattern("bad pattern", "too vague")
	mem := m.GetMemory()
	if len(mem.RejectedPatterns) != 1 {
		t.Errorf("expected 1 rejected pattern, got %d", len(mem.RejectedPatterns))
	}
	if mem.RejectedPatterns[0].Pattern != "bad pattern" {
		t.Errorf("expected 'bad pattern', got %q", mem.RejectedPatterns[0].Pattern)
	}
	if mem.RejectedPatterns[0].Reason != "too vague" {
		t.Errorf("expected 'too vague', got %q", mem.RejectedPatterns[0].Reason)
	}
}

func TestIncrementStat(t *testing.T) {
	t.Parallel()
	m := setupTestMemory(t)

	tests := []struct {
		field string
		get   func(domain.ResearchStats) int
	}{
		{"raw_signals_collected", func(s domain.ResearchStats) int { return s.RawSignalsCollected }},
		{"raw_signals_skipped", func(s domain.ResearchStats) int { return s.RawSignalsSkipped }},
		{"problem_signals_found", func(s domain.ResearchStats) int { return s.ProblemSignalsFound }},
		{"noise_signals", func(s domain.ResearchStats) int { return s.NoiseSignals }},
		{"clusters_created", func(s domain.ResearchStats) int { return s.ClustersCreated }},
		{"jobs_created", func(s domain.ResearchStats) int { return s.JobsCreated }},
		{"ideas_created", func(s domain.ResearchStats) int { return s.IdeasCreated }},
		{"duplicate_ideas", func(s domain.ResearchStats) int { return s.DuplicateIdeas }},
	}

	for _, tt := range tests {
		m.IncrementStat(tt.field)
		stats := m.GetStats()
		if tt.get(stats) != 1 {
			t.Errorf("IncrementStat(%q) expected 1, got %d", tt.field, tt.get(stats))
		}
	}

	// Unknown field should not panic.
	m.IncrementStat("unknown_field")
}

func TestAddGitHubRequests(t *testing.T) {
	t.Parallel()
	m := setupTestMemory(t)

	m.AddGitHubRequests(5)
	stats := m.GetStats()
	if stats.GitHubRequests != 5 {
		t.Errorf("expected 5 GitHub requests, got %d", stats.GitHubRequests)
	}

	// Zero and negative should not change.
	m.AddGitHubRequests(0)
	m.AddGitHubRequests(-3)
	stats = m.GetStats()
	if stats.GitHubRequests != 5 {
		t.Errorf("expected still 5 GitHub requests, got %d", stats.GitHubRequests)
	}

	// Accumulation.
	m.AddGitHubRequests(3)
	m.AddGitHubRequests(7)
	stats = m.GetStats()
	if stats.GitHubRequests != 15 {
		t.Errorf("expected 15 GitHub requests, got %d", stats.GitHubRequests)
	}
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

func TestLoad_Save(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := storage.New(dir)
	m := New(store)

	// Add some data.
	m.AddRawSignal("hn", "1")
	m.AddContentHash("hash1", "hn:1")
	m.AddHNRequests(5)

	// Save.
	if err := m.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Create a new memory and load.
	m2 := New(store)
	if err := m2.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if !m2.HasRawSignal("hn", "1") {
		t.Error("expected raw signal after load")
	}
	if !m2.HasContentHash("hash1") {
		t.Error("expected content hash after load")
	}
	if m2.ContentHashCount() != 1 {
		t.Errorf("expected 1 hash after load, got %d", m2.ContentHashCount())
	}
	stats := m2.GetStats()
	if stats.HackerNewsRequests != 5 {
		t.Errorf("expected 5 HN requests after load, got %d", stats.HackerNewsRequests)
	}
}

func TestLoad_nonExistent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := storage.New(dir)
	m := New(store)

	// Load from non-existent file should succeed (returns empty state).
	if err := m.Load(); err != nil {
		t.Fatalf("Load from non-existent file should succeed: %v", err)
	}
}

func TestLoad_corruptedFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := storage.New(dir)

	// Write corrupted JSON.
	memoryPath := dir + "/memory.json"
	if err := os.WriteFile(memoryPath, []byte("{corrupted}"), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	m := New(store)
	err := m.Load()
	if err == nil {
		t.Fatal("expected error for corrupted file")
	}
}

func TestConcurrentAccess(t *testing.T) {
	t.Parallel()
	m := setupTestMemory(t)
	var wg sync.WaitGroup

	// Concurrently add raw signals.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			m.AddRawSignal("test", string(rune('0'+n)))
		}(i)
	}

	// Concurrently check and add content hashes.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			hash := string(rune('a' + n))
			if !m.HasContentHash(hash) {
				m.AddContentHash(hash, "test")
			}
		}(i)
	}

	// Concurrently increment stats.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.IncrementStat("raw_signals_collected")
		}()
	}

	wg.Wait()

	stats := m.GetStats()
	// 20 goroutines incremented raw_signals_collected.
	if stats.RawSignalsCollected != 40 { // 20 from AddRawSignal + 20 from IncrementStat
		t.Errorf("expected 40 collected, got %d", stats.RawSignalsCollected)
	}
}

func TestGetMemory(t *testing.T) {
	t.Parallel()
	m := setupTestMemory(t)

	mem := m.GetMemory()
	if mem == nil {
		t.Fatal("GetMemory returned nil")
	}
	if mem.Version != 1 {
		t.Errorf("expected version 1, got %d", mem.Version)
	}
}

func TestRawSignalCount(t *testing.T) {
	t.Parallel()
	m := setupTestMemory(t)

	if m.RawSignalCount() != 0 {
		t.Errorf("expected 0, got %d", m.RawSignalCount())
	}

	m.AddRawSignal("hn", "1")
	m.AddRawSignal("hn", "2")
	if m.RawSignalCount() != 2 {
		t.Errorf("expected 2, got %d", m.RawSignalCount())
	}
}

func TestContentHashCount(t *testing.T) {
	t.Parallel()
	m := setupTestMemory(t)

	if m.ContentHashCount() != 0 {
		t.Errorf("expected 0, got %d", m.ContentHashCount())
	}

	m.AddContentHash("a", "1")
	m.AddContentHash("b", "2")
	m.AddContentHash("c", "3")
	if m.ContentHashCount() != 3 {
		t.Errorf("expected 3, got %d", m.ContentHashCount())
	}
}

func TestHasQuery(t *testing.T) {
	t.Parallel()
	m := setupTestMemory(t)

	if m.HasQuery("hn", "query1") {
		t.Error("expected no query before recording")
	}

	m.RecordQuery("hn", "query1", 10)
	if !m.HasQuery("hn", "query1") {
		t.Error("expected query after recording")
	}

	// Different source should not match.
	if m.HasQuery("github", "query1") {
		t.Error("expected no query for different source")
	}
}

func TestGetSetCursor(t *testing.T) {
	t.Parallel()
	m := setupTestMemory(t)

	// Non-existent cursor returns empty string and false.
	cursor, exists := m.GetCursor("github")
	if exists {
		t.Error("expected no cursor for github before setting")
	}
	if cursor != "" {
		t.Errorf("expected empty cursor, got %q", cursor)
	}

	// Set and retrieve.
	m.SetCursor("github", "cursor-abc-123")
	cursor, exists = m.GetCursor("github")
	if !exists {
		t.Error("expected cursor to exist after setting")
	}
	if cursor != "cursor-abc-123" {
		t.Errorf("expected cursor-abc-123, got %q", cursor)
	}

	// Overwrite.
	m.SetCursor("github", "cursor-xyz-789")
	cursor, exists = m.GetCursor("github")
	if !exists {
		t.Error("expected cursor to exist after overwrite")
	}
	if cursor != "cursor-xyz-789" {
		t.Errorf("expected cursor-xyz-789, got %q", cursor)
	}
}

func TestSetCursor_EmptyDeletes(t *testing.T) {
	t.Parallel()
	m := setupTestMemory(t)

	m.SetCursor("hn", "some-cursor")
	_, exists := m.GetCursor("hn")
	if !exists {
		t.Error("expected cursor to exist after setting")
	}

	// Setting to empty string should delete the entry.
	m.SetCursor("hn", "")
	_, exists = m.GetCursor("hn")
	if exists {
		t.Error("expected cursor to be deleted after setting empty string")
	}
}

func TestCursorPerSourceIsolation(t *testing.T) {
	t.Parallel()
	m := setupTestMemory(t)

	m.SetCursor("github", "gh-cursor")
	m.SetCursor("hackernews", "hn-cursor")
	m.SetCursor("stackexchange", "se-cursor")

	ghCursor, _ := m.GetCursor("github")
	if ghCursor != "gh-cursor" {
		t.Errorf("expected gh-cursor, got %q", ghCursor)
	}

	hnCursor, _ := m.GetCursor("hackernews")
	if hnCursor != "hn-cursor" {
		t.Errorf("expected hn-cursor, got %q", hnCursor)
	}

	seCursor, _ := m.GetCursor("stackexchange")
	if seCursor != "se-cursor" {
		t.Errorf("expected se-cursor, got %q", seCursor)
	}

	// Unset source returns nothing.
	_, exists := m.GetCursor("reddit")
	if exists {
		t.Error("expected no cursor for reddit")
	}
}

func TestClearCursors(t *testing.T) {
	t.Parallel()
	m := setupTestMemory(t)

	m.SetCursor("github", "gh-cursor")
	m.SetCursor("hackernews", "hn-cursor")

	m.ClearCursors()

	_, ghExists := m.GetCursor("github")
	if ghExists {
		t.Error("expected github cursor to be cleared")
	}
	_, hnExists := m.GetCursor("hackernews")
	if hnExists {
		t.Error("expected hackernews cursor to be cleared")
	}
}

func TestSourceCursors_ReturnsCopy(t *testing.T) {
	t.Parallel()
	m := setupTestMemory(t)

	m.SetCursor("github", "gh-cursor")
	m.SetCursor("hackernews", "hn-cursor")

	cursors := m.SourceCursors()
	if len(cursors) != 2 {
		t.Errorf("expected 2 cursors, got %d", len(cursors))
	}
	if cursors["github"] != "gh-cursor" {
		t.Errorf("expected gh-cursor, got %q", cursors["github"])
	}
	if cursors["hackernews"] != "hn-cursor" {
		t.Errorf("expected hn-cursor, got %q", cursors["hackernews"])
	}

	// Modifying the returned map should not affect the original.
	cursors["github"] = "modified"
	original, _ := m.GetCursor("github")
	if original != "gh-cursor" {
		t.Errorf("original should be unchanged, got %q", original)
	}
}

func TestCursorRoundTripPersistence(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := storage.New(dir)
	m := New(store)

	m.SetCursor("github", "gh-cursor")
	m.SetCursor("hackernews", "hn-cursor")

	if err := m.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	m2 := New(store)
	if err := m2.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	ghCursor, exists := m2.GetCursor("github")
	if !exists {
		t.Error("expected github cursor after round-trip")
	}
	if ghCursor != "gh-cursor" {
		t.Errorf("expected gh-cursor, got %q", ghCursor)
	}

	hnCursor, exists := m2.GetCursor("hackernews")
	if !exists {
		t.Error("expected hackernews cursor after round-trip")
	}
	if hnCursor != "hn-cursor" {
		t.Errorf("expected hn-cursor, got %q", hnCursor)
	}
}

func TestCursorBackwardCompatibility(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := storage.New(dir)

	// Write a memory.json without source_cursors field (old format).
	oldData := `{
	  "version": 1,
	  "raw_signal_ids": {},
	  "content_hashes": {},
	  "problem_fingerprints": {},
	  "cluster_fingerprints": {},
	  "idea_fingerprints": {},
	  "used_queries": {},
	  "rejected_patterns": [],
	  "stats": {}
	}`
	memoryPath := store.Path("memory.json")
	if err := store.WriteFile(memoryPath, []byte(oldData)); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	m := New(store)
	if err := m.Load(); err != nil {
		t.Fatalf("Load of old-format memory failed: %v", err)
	}

	// SourceCursors should be initialized to empty map.
	_, exists := m.GetCursor("github")
	if exists {
		t.Error("expected no cursor for github in old-format memory")
	}

	// Setting and getting should work.
	m.SetCursor("github", "new-cursor")
	cursor, exists := m.GetCursor("github")
	if !exists || cursor != "new-cursor" {
		t.Errorf("expected new-cursor, got %q (exists=%v)", cursor, exists)
	}
}

func TestCursorAtomicWrite(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := storage.New(dir)
	m := New(store)

	m.SetCursor("github", "gh-cursor")

	// Save should use atomic write (temp file + rename).
	if err := m.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify file contents contain source_cursors field.
	data, err := store.ReadFile(store.Path("memory.json"))
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	if !strings.Contains(string(data), "source_cursors") {
		t.Error("expected source_cursors in persisted memory JSON")
	}
	if !strings.Contains(string(data), "gh-cursor") {
		t.Error("expected gh-cursor in persisted memory JSON")
	}
}
