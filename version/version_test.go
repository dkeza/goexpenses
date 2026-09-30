package version

import (
	"strings"
	"testing"
)

func TestVersionFromBuildFlags(t *testing.T) {
	originalNumber, originalCommit := Number, Commit
	t.Cleanup(func() { Number, Commit = originalNumber, originalCommit })

	Number, Commit = "58", "8e503d1"
	if got := Label(); got != "v58 · 8e503d1" {
		t.Errorf("Label() = %q", got)
	}
	if got := AssetTag(); got != "58-8e503d1" {
		t.Errorf("AssetTag() = %q", got)
	}

	Number, Commit = "58", ""
	if got := Label(); got != "v58" {
		t.Errorf("Label() without commit = %q", got)
	}
	if got := AssetTag(); got != "58" {
		t.Errorf("AssetTag() without commit = %q", got)
	}
}

func TestDevelopmentVersion(t *testing.T) {
	originalNumber, originalCommit := Number, Commit
	t.Cleanup(func() { Number, Commit = originalNumber, originalCommit })

	Number, Commit = "", ""
	if got := Label(); got != "dev" {
		t.Errorf("Label() = %q, want dev", got)
	}
	if got := AssetTag(); !strings.HasPrefix(got, "dev-") || got != AssetTag() {
		t.Errorf("AssetTag() = %q, want a stable dev- tag", got)
	}
}
