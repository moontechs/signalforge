package cli

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/moontechs/signalforge/internal/domain"
	"github.com/moontechs/signalforge/internal/storage"
)

func TestList_RawSignalsAlias(t *testing.T) {
	t.Parallel()

	// Verify that "raw-signals" is accepted as an input type.
	subDir, ok := validTypes["raw-signals"]
	if !ok {
		t.Fatal("expected 'raw-signals' to be a valid type in validTypes map")
	}
	if subDir != "raw-signals" {
		t.Errorf("expected subDir 'raw-signals', got %q", subDir)
	}
}

func TestList_ValidTypesMap(t *testing.T) {
	t.Parallel()

	// Verify that "signals" and "raw-signals" both map to "raw-signals" directory.
	if dir := validTypes["signals"]; dir != "raw-signals" {
		t.Errorf("signals -> expected 'raw-signals', got %q", dir)
	}
	if dir := validTypes["raw-signals"]; dir != "raw-signals" {
		t.Errorf("raw-signals -> expected 'raw-signals', got %q", dir)
	}
}

func TestList_HelpTextIncludesAlias(t *testing.T) {
	t.Parallel()

	// Verify the help text mentions the alias.
	longHelp := ListCmd.Long
	if !strings.Contains(longHelp, "signals (raw-signals)") {
		t.Errorf("expected help text to mention 'signals (raw-signals)', got: %s", longHelp)
	}
}

func TestList_SignalDetailFields(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := storage.New(dir)

	// Create a valid RawSignal JSON file.
	signal := domain.RawSignal{
		ID:        "test-id-001",
		Source:    "HN",
		Title:     "Test Signal Title",
		URL:       "https://example.com/test",
		CreatedAt: time.Date(2026, 7, 25, 22, 23, 37, 0, time.UTC),
	}

	signalPath := filepath.Join(dir, "raw-signals", "test-id-001.json")
	if err := store.SaveJSON(signalPath, signal); err != nil {
		t.Fatal(err)
	}

	items, err := listItems(store, "raw-signals", 50, 0)
	if err != nil {
		t.Fatal(err)
	}

	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d: %v", len(items), items)
	}

	item := items[0]
	if !strings.Contains(item, "source: HN") {
		t.Errorf("expected output to contain 'source: HN', got: %s", item)
	}
	if !strings.Contains(item, "title: \"Test Signal Title\"") {
		t.Errorf("expected output to contain title, got: %s", item)
	}
	if !strings.Contains(item, "url: https://example.com/test") {
		t.Errorf("expected output to contain url, got: %s", item)
	}
	if !strings.Contains(item, "created: 2026-07-25T22:23:37Z") {
		t.Errorf("expected output to contain created date, got: %s", item)
	}
}

func TestList_SignalJsonParseError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := storage.New(dir)

	// Create a JSON file that is valid JSON but not a RawSignal (no "source" field).
	nonSignalPath := filepath.Join(dir, "raw-signals", "not-a-signal.json")
	if err := store.SaveJSON(nonSignalPath, map[string]string{"foo": "bar"}); err != nil {
		t.Fatal(err)
	}

	items, err := listItems(store, "raw-signals", 50, 0)
	if err != nil {
		t.Fatal(err)
	}

	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d: %v", len(items), items)
	}

	// Should fall back to file info format (no "source:" in output).
	item := items[0]
	if strings.Contains(item, "source:") {
		t.Errorf("expected fallback output without source field, got: %s", item)
	}
	if !strings.Contains(item, "modified:") {
		t.Errorf("expected fallback output to contain 'modified:', got: %s", item)
	}
	if !strings.Contains(item, "size:") {
		t.Errorf("expected fallback output to contain 'size:', got: %s", item)
	}
}
