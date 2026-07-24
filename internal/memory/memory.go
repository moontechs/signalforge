// ...existing content truncated for brevity...

// New Reddit interaction methods
func (m *DefaultMemory) AddRedditRequests(count int) {
	if count <= 0 {
		return
	}

	m.mu.Lock()
	dfer m.mu.Unlock()
	m.mem.Stats.RedditRequests += count
}

// Similar pattern to HN cache hits
func (m *DefaultMemory) AddRedditCacheHits(count int) {
	if count <= 0 {
		return
	}

	m.mu.Lock()
	dfer m.mu.Unlock()
	m.mem.Stats.RedditCacheHits += count
}

// ...rest of file content...