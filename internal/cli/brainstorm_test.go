package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/moontechs/signalforge/internal/config"
	"github.com/moontechs/signalforge/internal/domain"
	"github.com/moontechs/signalforge/internal/storage"
)

func TestExecuteDiscoverEmptyClusters(t *testing.T) {
	cmd := newDiscoverCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetContext(context.Background())
	env := &discoverEnv{store: storage.New(t.TempDir()), cfg: &config.Config{}}
	if err := executeDiscover(cmd, env); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "No problem clusters found") {
		t.Fatalf("unexpected output: %s", out.String())
	}
}

func TestExecuteDiscoverMissingAPIKey(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "clusters"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := storage.New(dir).SaveJSON(filepath.Join(dir, "clusters", "c.json"), map[string]string{"id": "c"}); err != nil {
		t.Fatal(err)
	}
	cmd := newDiscoverCmd()
	env := &discoverEnv{store: storage.New(dir), cfg: &config.Config{}}
	t.Setenv("OPENROUTER_API_KEY", "")
	if err := executeDiscover(cmd, env); err == nil || !strings.Contains(err.Error(), "OPENROUTER_API_KEY") {
		t.Fatalf("expected missing key error, got %v", err)
	}
}

func TestExecuteDiscoverDryRunDoesNotRequireAPIKey(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "clusters"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := storage.New(dir).SaveJSON(filepath.Join(dir, "clusters", "c.json"), map[string]string{"id": "c"}); err != nil {
		t.Fatal(err)
	}
	cmd := newDiscoverCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	env := &discoverEnv{store: storage.New(dir), cfg: &config.Config{}, dryRun: true}
	if err := executeDiscover(cmd, env); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Would discover") {
		t.Fatalf("unexpected output: %s", out.String())
	}
}

func TestDiscoverResultPersistence(t *testing.T) {
	dir := t.TempDir()
	store := storage.New(dir)
	want := DiscoverResult{JTBDs: []domain.JobToBeDone{{ID: "job", Statement: "statement"}}, Solutions: []domain.SolutionHypothesis{{ID: "idea", Title: "Idea"}}}
	path := filepath.Join(dir, "discover.json")
	if err := store.SaveJSON(path, want); err != nil {
		t.Fatal(err)
	}
	var got DiscoverResult
	if err := store.LoadJSON(path, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip mismatch: got %#v want %#v", got, want)
	}
	if files, err := filepath.Glob(filepath.Join(dir, "tmp-*.json")); err != nil || len(files) != 0 {
		t.Fatalf("temporary files remain: %v", files)
	}
}
