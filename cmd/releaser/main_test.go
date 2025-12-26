package main

import (
	"testing"
)

func TestParseVersion(t *testing.T) {
	tests := []struct {
		input           string
		maj, min, patch int
		wantErr         bool
	}{
		{"v1.2.3", 1, 2, 3, false},
		{"1.2.3", 1, 2, 3, false},
		{"v0.0.1", 0, 0, 1, false},
		{"latest", 0, 0, 0, true},
		{"v1.2", 0, 0, 0, true},
	}

	for _, tt := range tests {
		maj, min, patch, err := parseVersion(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseVersion(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if maj != tt.maj || min != tt.min || patch != tt.patch {
			t.Errorf("parseVersion(%q) = %d.%d.%d, want %d.%d.%d", tt.input, maj, min, patch, tt.maj, tt.min, tt.patch)
		}
	}
}

func TestDetermineIncrementType(t *testing.T) {
	tests := []struct {
		oldVer, newVer string
		want           IncrementType
	}{
		{"v1.0.0", "v1.0.1", IncrementPatch},
		{"v1.0.0", "v1.1.0", IncrementMinor},
		{"v1.0.0", "v2.0.0", IncrementMajor},
		{"v1.0.0", "v1.0.0", IncrementPatch}, // No change, but shouldn't happen in loop logic
		{"latest", "v1.0.0", IncrementPatch}, // Fallback
		{"v1.0.0", "latest", IncrementPatch}, // Fallback
	}

	for _, tt := range tests {
		got := determineIncrementType(tt.oldVer, tt.newVer)
		if got != tt.want {
			t.Errorf("determineIncrementType(%q, %q) = %v, want %v", tt.oldVer, tt.newVer, got, tt.want)
		}
	}
}
