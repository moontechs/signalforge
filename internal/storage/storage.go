// Package storage provides atomic JSON/JSONL file storage.
package storage

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Storage handles file-based data persistence.
type Storage struct {
	baseDir string
	mu      sync.RWMutex
}

// New creates a new Storage instance.
func New(baseDir string) *Storage {
	return &Storage{baseDir: baseDir}
}

// BaseDir returns the base directory.
func (s *Storage) BaseDir() string {
	return s.baseDir
}

// ensureDir ensures a directory exists.
func (s *Storage) ensureDir(path string) error {
	return os.MkdirAll(path, 0755)
}

// SaveJSON atomically writes a JSON file.
func (s *Storage) SaveJSON(path string, v any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := filepath.Dir(path)
	if err := s.ensureDir(dir); err != nil {
		return fmt.Errorf("ensure dir: %w", err)
	}

	tmpFile, err := os.CreateTemp(dir, "tmp-*.json")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmpFile.Name()

	encoder := json.NewEncoder(tmpFile)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(v); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("encode: %w", err)
	}

	if err := tmpFile.Sync(); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("sync: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename: %w", err)
	}

	// Sync directory
	if f, err := os.Open(dir); err == nil {
		f.Sync()
		f.Close()
	}

	return nil
}

// LoadJSON reads and deserializes a JSON file.
func (s *Storage) LoadJSON(path string, v any) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}

	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	return nil
}

// SaveJSONL appends a JSON line to a JSONL file atomically.
func (s *Storage) SaveJSONL(path string, v any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := filepath.Dir(path)
	if err := s.ensureDir(dir); err != nil {
		return fmt.Errorf("ensure dir: %w", err)
	}

	line, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}

	if _, err := f.Write(line); err != nil {
		f.Close()
		return fmt.Errorf("write: %w", err)
	}
	if _, err := f.Write([]byte("\n")); err != nil {
		f.Close()
		return fmt.Errorf("write newline: %w", err)
	}

	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("sync: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}
	return nil
}

// ReadJSONL reads all lines from a JSONL file.
func (s *Storage) ReadJSONL(path string) ([][]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var results [][]byte
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		results = append(results, []byte(line))
	}
	return results, nil
}

// ReadJSONLInto reads a JSONL file into a slice.
func (s *Storage) ReadJSONLInto(path string, into any) error {
	lines, err := s.ReadJSONL(path)
	if err != nil {
		return err
	}

	// Ensure into is a pointer to slice
	for _, line := range lines {
		if err := json.Unmarshal(line, into); err != nil {
			return fmt.Errorf("unmarshal line: %w", err)
		}
	}
	return nil
}

// CheckLastJSONLLine verifies the last line of a JSONL file is valid JSON.
func (s *Storage) CheckLastJSONLLine(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[len(lines)-1]) == "" {
		return nil
	}

	lastLine := strings.TrimSpace(lines[len(lines)-1])
	if !json.Valid([]byte(lastLine)) {
		return fmt.Errorf("corrupt last line in %s", path)
	}
	return nil
}

// ListFiles returns all files in a directory with a given extension.
func (s *Storage) ListFiles(dir, ext string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(s.baseDir, dir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list dir: %w", err)
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ext) {
			files = append(files, filepath.Join(s.baseDir, dir, e.Name()))
		}
	}
	return files, nil
}

// Exists checks if a path exists.
func (s *Storage) Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// ReadFile reads a file's contents.
func (s *Storage) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// WriteFile writes data to a file.
func (s *Storage) WriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := s.ensureDir(dir); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// Path returns the full path for a relative path under the base directory.
func (s *Storage) Path(rel string) string {
	return filepath.Join(s.baseDir, rel)
}

// GenerateID generates a unique ID based on content and timestamp.
func GenerateID(prefix string) string {
	h := sha256.New()
	io.WriteString(h, fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano()))
	return fmt.Sprintf("%s_%x", prefix, h.Sum(nil)[:16])
}

// ContentHash computes a SHA-256 hash of the given parts.
func ContentHash(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		io.WriteString(h, p)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// BackupJSON creates a backup of a JSON file before modification.
func (s *Storage) BackupJSON(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	backupDir := filepath.Join(s.baseDir, "backups")
	if err := s.ensureDir(backupDir); err != nil {
		return err
	}

	ts := time.Now().Format("20060102_150405")
	backupPath := filepath.Join(backupDir, fmt.Sprintf("%s.%s.bak", filepath.Base(path), ts))
	return os.WriteFile(backupPath, data, 0644)
}

// JSONLRecovery checks and repairs the last line of a JSONL file.
func (s *Storage) JSONLRecovery(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	lines := strings.Split(string(data), "\n")
	if len(lines) < 2 {
		return nil
	}

	lastLine := strings.TrimSpace(lines[len(lines)-2])
	if lastLine == "" {
		return nil
	}

	if !json.Valid([]byte(lastLine)) {
		// Remove the last line
		content := strings.Join(lines[:len(lines)-2], "\n") + "\n"
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return fmt.Errorf("repair: %w", err)
		}
	}

	return nil
}