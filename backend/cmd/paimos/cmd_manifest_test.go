package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteManagedAgentsFilePreservesUserContent(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "AGENTS.md")
	original := "# Notes\n\nKeep this.\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeManagedAgentsFile(path, "<!-- pmo-manifest: managed:start -->\nmanaged\n<!-- pmo-manifest: managed:end -->\n"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, "Keep this.") {
		t.Fatalf("user content was lost: %s", text)
	}
	if !strings.Contains(text, "managed") {
		t.Fatalf("managed block missing: %s", text)
	}
}
