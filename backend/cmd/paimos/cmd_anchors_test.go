package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildAnchorIndexSupportsCommentForms(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"backend/handlers/context.go":        "// @paimos PAI-68 \"anchor ingest endpoint\"\nfunc x() {}\n",
		"frontend/src/components/example.ts": "// @pmo PAI-65 \"scanner command\"\nexport const x = 1\n",
		"schema/example.sql":                 "-- @paimos PAI-79 \"blast radius fixture\"\nselect 1;\n",
		"docs/example.md":                    "<!-- @paimos PAI-81 \"agent onboarding\" -->\n",
		"config/example.yaml":                "# @paimos PAI-72 \"manifest mirror\"\nkey: value\n",
	}
	for rel, body := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	index, err := buildAnchorIndex(root, "paimos-app", "1")
	if err != nil {
		t.Fatal(err)
	}

	for _, issueKey := range []string{"PAI-68", "PAI-65", "PAI-79", "PAI-81", "PAI-72"} {
		if got := len(index.Anchors[issueKey]); got != 1 {
			t.Fatalf("%s anchors: got %d want 1", issueKey, got)
		}
	}
	if index.Anchors["PAI-72"][0].Confidence != "declared" {
		t.Fatalf("confidence = %q want declared", index.Anchors["PAI-72"][0].Confidence)
	}
}

func TestCompareAnchorIndexesToleratesLineDriftButFailsMissingAnchors(t *testing.T) {
	expected := &anchorIndex{
		Anchors: map[string][]anchorRecord{
			"PAI-65": {{File: "a.go", Line: 10, Label: "scan", Confidence: "declared"}},
			"PAI-68": {{File: "b.go", Line: 20, Label: "upload", Confidence: "declared"}},
		},
	}
	current := &anchorIndex{
		Anchors: map[string][]anchorRecord{
			"PAI-65": {{File: "a.go", Line: 14, Label: "scan", Confidence: "declared"}},
		},
	}
	report := compareAnchorIndexes(expected, current)
	if len(report.Warnings) != 1 {
		t.Fatalf("warnings: got %d want 1", len(report.Warnings))
	}
	if len(report.Errors) != 1 {
		t.Fatalf("errors: got %d want 1", len(report.Errors))
	}
}
