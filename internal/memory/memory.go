// Package memory provides persistent memory management.
package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/moontechs/signalforge/internal/domain"
	"github.com/moontechs/signalforge/internal/storage"
)

// DefaultMemory manages the persistent memory state.
type DefaultMemory struct {
	mu      sync.RWMutex
	mem     *domain.Memory
	store   *storage.Storage
	path    string
	version int
}

// New creates a new DefaultMemory instance.
func New(store *storage.Storage) *DefaultMemory {
	path := filepath.Join(store.BaseDir(), "memory.json")
	return &DefaultMemory{
		mem: &domain.Memory{
			Version:             1,
			UpdatedAt:           time.Now(),
			RawSignalIDs:        make(map[string]string),
			ContentHashes:       make(map[string]string),
			ProblemFingerprints: make(map[string]string),
			ClusterFingerprints: make(map[string]string),
			IdeaFingerprints:    make(map[string]string),
			UsedQueries:         make(map[string]domain.QueryMemory),
			RejectedPatterns:    []domain.RejectedPattern{},
		},
		store:   store,
		path:    path,
		version: 1,
	}
}

// Load loads memory from disk.
func (m *DefaultMemory) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var mem domain.Memory
	if err := m.store.LoadJSON(m.path, &mem); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("load memory: %w", err)
	}

	if mem.RawSignalIDs == nil {
		mem.RawSignalIDs = make(map[string]string)
	}
	if mem.ContentHashes == nil {
		mem.ContentHashes = make(map[string]string)
	}
	if mem.ProblemFingerprints == nil {
		mem.ProblemFingerprints = make(map[string]string)
	}
	if mem.ClusterFingerprints == nil {
		mem.ClusterFingerprints = make(map[string]string)
	}
	if mem.IdeaFingerprints == nil {
		mem.IdeaFingerprints = make(map[string]string)
	}
	if mem.UsedQueries == nil {
		mem.UsedQueries = make(map[string]domain.QueryMemory)
	}
	if mem.RejectedPatterns == nil {
		mem.RejectedPatterns = []domain.RejectedPattern{}
	}

	m.version = mem.Version
	m.mem = &mem
	return nil
}

// Save persists memory to disk atomically.
func (m *DefaultMemory) Save() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.mem.UpdatedAt = time.Now()

	// Create backup before saving
	if err := m.store.BackupJSON(m.path); err != nil {
		return fmt.Errorf("backup memory: %w", err)
	}

	if err := m.store.SaveJSON(m.path, m.mem); err != nil {
		return fmt.Errorf("save memory: %w", err)
	}
	return nil
}

// HasRawSignal checks if a raw signal by source ID already exists.
func (m *DefaultMemory) HasRawSignal(source, sourceID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	key := source + ":" + sourceID
	_, exists := m.mem.RawSignalIDs[key]
	return exists
}

// AddRawSignal records a collected raw signal.
func (m *DefaultMemory) AddRawSignal(source, sourceID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := source + ":" + sourceID
	m.mem.RawSignalIDs[key] = sourceID
	m.mem.Stats.RawSignalsCollected++
}

// HasContentHash checks if a content hash already exists.
func (m *DefaultMemory) HasContentHash(hash string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, exists := m.mem.ContentHashes[hash]
	return exists
}

// AddContentHash records a content hash.
func (m *DefaultMemory) AddContentHash(hash, signalID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mem.ContentHashes[hash] = signalID
}

// HasProblemFingerprint checks if a problem fingerprint exists.
func (m *DefaultMemory) HasProblemFingerprint(fp string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, exists := m.mem.ProblemFingerprints[fp]
	return exists
}

// AddProblemFingerprint records a problem fingerprint.
func (m *DefaultMemory) AddProblemFingerprint(fp, signalID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mem.ProblemFingerprints[fp] = signalID
}

// HasClusterFingerprint checks if a cluster fingerprint exists.
func (m *DefaultMemory) HasClusterFingerprint(fp string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, exists := m.mem.ClusterFingerprints[fp]
	return exists
}

// AddClusterFingerprint records a cluster fingerprint.
func (m *DefaultMemory) AddClusterFingerprint(fp, clusterID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mem.ClusterFingerprints[fp] = clusterID
}

// HasIdeaFingerprint checks if an idea fingerprint exists.
func (m *DefaultMemory) HasIdeaFingerprint(fp string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, exists := m.mem.IdeaFingerprints[fp]
	return exists
}

// AddIdeaFingerprint records an idea fingerprint.
func (m *DefaultMemory) AddIdeaFingerprint(fp, ideaID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mem.IdeaFingerprints[fp] = ideaID
}

// RecordQuery records a used query.
func (m *DefaultMemory) RecordQuery(source, query string, count int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mem.UsedQueries[source+":"+query] = domain.QueryMemory{
		LastUsed:    time.Now(),
		ResultCount: count,
		Source:      source,
	}
}

// HasQuery checks if a query was already used.
func (m *DefaultMemory) HasQuery(source, query string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, exists := m.mem.UsedQueries[source+":"+query]
	return exists
}

// AddRejectedPattern adds a rejected pattern.
func (m *DefaultMemory) AddRejectedPattern(pattern, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mem.RejectedPatterns = append(m.mem.RejectedPatterns, domain.RejectedPattern{
		Pattern:   pattern,
		Reason:    reason,
		CreatedAt: time.Now(),
	})
}

// GetStats returns the current research stats.
func (m *DefaultMemory) GetStats() domain.ResearchStats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.mem.Stats
}

// IncrementStat increments a specific stat counter.
func (m *DefaultMemory) IncrementStat(field string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch field {
	case "raw_signals_collected":
		m.mem.Stats.RawSignalsCollected++
	case "raw_signals_skipped":
		m.mem.Stats.RawSignalsSkipped++
	case "problem_signals_found":
		m.mem.Stats.ProblemSignalsFound++
	case "noise_signals":
		m.mem.Stats.NoiseSignals++
	case "clusters_created":
		m.mem.Stats.ClustersCreated++
	case "jobs_created":
		m.mem.Stats.JobsCreated++
	case "ideas_created":
		m.mem.Stats.IdeasCreated++
	case "duplicate_ideas":
		m.mem.Stats.DuplicateIdeas++
	}
}

// AddGitHubRequests increments the GitHub request count.
func (m *DefaultMemory) AddGitHubRequests(count int) {
	if count <= 0 {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.mem.Stats.GitHubRequests += count
}

// GetMemory returns the full memory struct (for serialization).
func (m *DefaultMemory) GetMemory() *domain.Memory {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.mem
}

// RawSignalCount returns the number of collected raw signals.
func (m *DefaultMemory) RawSignalCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.mem.RawSignalIDs)
}

// ContentHashCount returns the number of content hashes.
func (m *DefaultMemory) ContentHashCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.mem.ContentHashes)
}
