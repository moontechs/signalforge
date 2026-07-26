package cli

import (
	"strings"
	"testing"
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
