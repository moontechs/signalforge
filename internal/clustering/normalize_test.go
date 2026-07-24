package clustering

import (
	"slices"
	"testing"
)

func TestNormalizeText(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "empty string",
			input: "",
			want:  nil,
		},
		{
			name:  "only whitespace",
			input: "   \t\n  ",
			want:  nil,
		},
		{
			name:  "only punctuation",
			input: "!!?.,;:-",
			want:  nil,
		},
		{
			name:  "simple lowercase",
			input: "hello world",
			want:  []string{"hello", "world"},
		},
		{
			name:  "casing normalization",
			input: "Hello WORLD",
			want:  []string{"hello", "world"},
		},
		{
			name:  "punctuation stripping",
			input: "hello! (world) 'test' \"quote\"",
			want:  []string{"hello", "quote", "test", "world"},
		},
		{
			name:  "punctuation at word boundaries",
			input: "  !!hello... --world__  ;;test;;  ",
			want:  []string{"hello", "test", "world"},
		},
		{
			name:  "deduplication",
			input: "hello hello HELLO world World",
			want:  []string{"hello", "world"},
		},
		{
			name:  "symbols stripped",
			input: "$money €uro ©opyright",
			want:  []string{"money", "opyright", "uro"},
		},
		{
			name:  "numbers preserved",
			input: "version 2.0 beta",
			want:  []string{"0", "2", "beta", "version"},
		},
		{
			name:  "hyphenated words",
			input: "well-known open-source",
			want:  []string{"known", "open", "source", "well"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeText(tt.input)
			if !slices.Equal(got, tt.want) {
				t.Errorf("NormalizeText(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestCanonicalizeAction(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty input",
			input: "",
			want:  "",
		},
		{
			name:  "whitespace only",
			input: "  ",
			want:  "",
		},
		{
			name:  "install stays install",
			input: "install",
			want:  "install",
		},
		{
			name:  "setup maps to install",
			input: "setup",
			want:  "install",
		},
		{
			name:  "set up maps to install",
			input: "set up",
			want:  "install",
		},
		{
			name:  "configure maps to install",
			input: "configure",
			want:  "install",
		},
		{
			name:  "deploy stays deploy",
			input: "deploy",
			want:  "deploy",
		},
		{
			name:  "migrate stays migrate",
			input: "migrate",
			want:  "migrate",
		},
		{
			name:  "casing handled",
			input: "SETUP",
			want:  "install",
		},
		{
			name:  "surrounding whitespace",
			input: "  Deploy  ",
			want:  "deploy",
		},
		{
			name:  "unknown action normalised",
			input: "  **Integrate**  ",
			want:  "integrate",
		},
		{
			name:  "multi-token unknown action",
			input: "  Scale Up ",
			want:  "scale up",
		},
		{
			name:  "mixed punctuation unknown",
			input: "!!! auto-scale !!!",
			want:  "auto-scale",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CanonicalizeAction(tt.input)
			if got != tt.want {
				t.Errorf("CanonicalizeAction(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestCanonicalizeActions(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{
			name:  "nil slice",
			input: nil,
			want:  nil,
		},
		{
			name:  "empty slice",
			input: []string{},
			want:  nil,
		},
		{
			name:  "all empty strings",
			input: []string{"", "  ", ""},
			want:  nil,
		},
		{
			name:  "dedup and sort canonical forms",
			input: []string{"install", "setup", "configure", "deploy", "migrate"},
			want:  []string{"deploy", "install", "migrate"},
		},
		{
			name:  "mixed known and unknown",
			input: []string{"Install", "SETUP", "Scale Up", "  Deploy  "},
			want:  []string{"deploy", "install", "scale up"},
		},
		{
			name:  "duplicates removed",
			input: []string{"install", "install", "INSTALL"},
			want:  []string{"install"},
		},
		{
			name:  "sort order preserved",
			input: []string{"migrate", "deploy", "install"},
			want:  []string{"deploy", "install", "migrate"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CanonicalizeActions(tt.input)
			if !slices.Equal(got, tt.want) {
				t.Errorf("CanonicalizeActions(%v) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
