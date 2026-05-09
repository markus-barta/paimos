// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package sync

// PAI-341 — coverage for the five knowledge Resources. Each test runs
// against a fakeClient (defined in sync_test.go) so we never touch the
// network. The shared knowledgeResource implementation is what's
// actually under test; the per-kind constructors are thin wrappers.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeKnowledgeEntryJSON returns one canonical entry shape for the
// `<kind>` and slug pair. The `metadata` field exercises the sorted-
// key encoder; the body has a trailing newline that should be
// normalised to a single \n in the rendered output.
func fakeKnowledgeEntryJSON(kind, slug, title, body string) string {
	return `{
		"id": 100,
		"project_id": 7,
		"type": "` + kind + `",
		"slug": "` + slug + `",
		"title": "` + title + `",
		"body": "` + body + `",
		"status": "backlog",
		"metadata": {"b": 1, "a": "alpha"},
		"created_at": "2026-05-01 10:00:00",
		"updated_at": "2026-05-01 10:00:00"
	}`
}

func fakeKnowledgeListJSON(kind string, slugs ...string) string {
	parts := make([]string, 0, len(slugs))
	for _, s := range slugs {
		parts = append(parts, fakeKnowledgeEntryJSON(kind, s, "Title "+s, "Body "+s))
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func TestRegistry_KnowledgeKindsRegisterCleanly(t *testing.T) {
	reg := NewRegistry()
	reg.Register(NewMemoryResource())
	reg.Register(NewRunbookResource())
	reg.Register(NewExternalSystemResource())
	reg.Register(NewRelatedProjectResource())
	reg.Register(NewGuidelineResource())

	want := []string{"external_system", "guideline", "memory", "related_project", "runbook"}
	got := reg.Kinds()
	if len(got) != len(want) {
		t.Fatalf("Kinds = %v, want %v", got, want)
	}
	for i, k := range want {
		if got[i] != k {
			t.Errorf("Kinds[%d] = %q, want %q", i, got[i], k)
		}
	}
}

func TestRegistry_DefaultPlusKnowledgeIsSix(t *testing.T) {
	// Spec says PAI-341 lands six total kinds (skill + 5 knowledge).
	// Replicate the production wiring in-package so the test doesn't
	// need to import the cmd/paimos main package.
	reg := NewRegistry()
	skill := newSkillResourceForTest(t)
	reg.Register(skill)
	reg.Register(NewMemoryResource())
	reg.Register(NewRunbookResource())
	reg.Register(NewExternalSystemResource())
	reg.Register(NewRelatedProjectResource())
	reg.Register(NewGuidelineResource())

	if got := len(reg.Kinds()); got != 6 {
		t.Fatalf("registry.Kinds() count = %d, want 6 (skill + 5 knowledge)", got)
	}
}

func TestKnowledgeResource_SyncWritesCachedFile(t *testing.T) {
	res := NewMemoryResource().(*knowledgeResource)
	c := &fakeClient{
		routes: map[string][]byte{
			"/api/projects/7/memory": []byte(fakeKnowledgeListJSON("memory", "alpha", "beta")),
		},
	}
	work := t.TempDir()

	var written []SyncedItem
	if err := res.Sync(context.Background(), c, 7, "ACME", work, "", func(it SyncedItem) {
		written = append(written, it)
	}); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(written) != 2 {
		t.Fatalf("written = %d, want 2: %+v", len(written), written)
	}

	for _, it := range written {
		if it.Action != "wrote" {
			t.Errorf("first sync should write, got action=%q for %s", it.Action, it.Name)
		}
		// Cache layout: .paimos/cache/ACME/memory/<slug>.md
		expected := filepath.Join(work, ".paimos", "cache", "ACME", "memory", it.Name+".md")
		if it.Path != expected {
			t.Errorf("path = %q, want %q", it.Path, expected)
		}
		body, err := os.ReadFile(it.Path)
		if err != nil {
			t.Fatalf("read %s: %v", it.Path, err)
		}
		// Header line follows the canonical pattern; kind=memory tail
		// distinguishes it from skills.
		if !strings.HasPrefix(string(body), "<!-- paimos: rendered from ACME/"+it.Name+"@") {
			t.Errorf("missing canonical header in %s: %.120q", it.Path, string(body))
		}
		if !strings.Contains(string(body), "kind=memory -->") {
			t.Errorf("missing kind tail in %s", it.Path)
		}
	}
}

func TestKnowledgeResource_SecondSyncIsUnchanged(t *testing.T) {
	res := NewRunbookResource().(*knowledgeResource)
	c := &fakeClient{
		routes: map[string][]byte{
			"/api/projects/7/runbooks": []byte(fakeKnowledgeListJSON("runbook", "deploy")),
		},
	}
	work := t.TempDir()
	if err := res.Sync(context.Background(), c, 7, "ACME", work, "", nil); err != nil {
		t.Fatalf("first Sync: %v", err)
	}

	var second []SyncedItem
	if err := res.Sync(context.Background(), c, 7, "ACME", work, "", func(it SyncedItem) {
		second = append(second, it)
	}); err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	if len(second) != 1 || second[0].Action != "unchanged" {
		t.Fatalf("second sync action = %+v, want unchanged", second)
	}
}

func TestKnowledgeResource_SelectName(t *testing.T) {
	res := NewExternalSystemResource().(*knowledgeResource)
	c := &fakeClient{
		routes: map[string][]byte{
			"/api/projects/7/external-systems/clickhouse": []byte(fakeKnowledgeEntryJSON("external_system", "clickhouse", "ClickHouse Cluster", "Used for analytics.")),
		},
	}
	work := t.TempDir()
	var written []SyncedItem
	if err := res.Sync(context.Background(), c, 7, "ACME", work, "clickhouse", func(it SyncedItem) {
		written = append(written, it)
	}); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(written) != 1 || written[0].Name != "clickhouse" {
		t.Fatalf("written = %+v, want [clickhouse]", written)
	}
	// Verify list endpoint was NOT called.
	for _, p := range c.getCalls {
		if p == "/api/projects/7/external-systems" {
			t.Errorf("list endpoint should be skipped on selectName, got call %q", p)
		}
	}
}

func TestKnowledgeResource_CheckReportsDriftStates(t *testing.T) {
	res := NewGuidelineResource().(*knowledgeResource)
	c := &fakeClient{
		routes: map[string][]byte{
			"/api/projects/7/guidelines": []byte(fakeKnowledgeListJSON("guideline", "no-secrets-in-logs")),
		},
	}
	work := t.TempDir()

	// Seed-render so the file exists and matches.
	if err := res.Sync(context.Background(), c, 7, "ACME", work, "", nil); err != nil {
		t.Fatalf("seed Sync: %v", err)
	}
	records, err := res.Check(context.Background(), c, 7, "ACME", work)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(records) != 1 || records[0].State != "identical" {
		t.Fatalf("Check records = %+v, want one identical", records)
	}

	// Mutate the local file. PAI-331's drift detector should now
	// report "diff" (the canonical header is still there because we
	// only changed the body section).
	target := records[0].Path
	body, _ := os.ReadFile(target)
	mutated := strings.Replace(string(body), "Body no-secrets-in-logs", "edited locally", 1)
	if err := os.WriteFile(target, []byte(mutated), 0o644); err != nil {
		t.Fatal(err)
	}
	records, err = res.Check(context.Background(), c, 7, "ACME", work)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].State != "diff" {
		t.Fatalf("after edit, state = %+v, want diff", records)
	}

	// Strip the header → header_missing.
	if err := os.WriteFile(target, []byte("# bare\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	records, err = res.Check(context.Background(), c, 7, "ACME", work)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].State != "header_missing" {
		t.Fatalf("after header strip, state = %+v, want header_missing", records)
	}

	// Delete the file → missing_local.
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	records, err = res.Check(context.Background(), c, 7, "ACME", work)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].State != "missing_local" {
		t.Fatalf("after rm, state = %+v, want missing_local", records)
	}
}

func TestKnowledgeResource_RenderIsStable(t *testing.T) {
	// Render the same entry twice and verify byte-equality. Map
	// iteration in Metadata could otherwise reorder JSON keys, breaking
	// drift detection on the second sync.
	res := NewMemoryResource().(*knowledgeResource)
	entry := KnowledgeEntry{
		Slug:    "feedback_xyz",
		Title:   "Memory under test",
		Body:    "Some markdown body.",
		Status:  "done",
		Type:    "memory",
		Metadata: map[string]any{
			"zeta": "last",
			"alpha": 1,
			"middle": []any{"x", "y"},
		},
		UpdatedAt: "2026-05-01 12:00:00",
	}
	a := res.renderEntry("ACME", entry)
	b := res.renderEntry("ACME", entry)
	if string(a) != string(b) {
		t.Errorf("render unstable: a != b\n  a=%.200q\n  b=%.200q", a, b)
	}
	// Sanity: the JSON payload is sorted-key.
	if !strings.Contains(string(a), `"alpha":`) {
		t.Errorf("metadata block missing alpha key: %.200q", a)
	}
	idxAlpha := strings.Index(string(a), `"alpha":`)
	idxMiddle := strings.Index(string(a), `"middle":`)
	idxZeta := strings.Index(string(a), `"zeta":`)
	if !(idxAlpha < idxMiddle && idxMiddle < idxZeta) {
		t.Errorf("metadata keys not sorted: alpha=%d middle=%d zeta=%d", idxAlpha, idxMiddle, idxZeta)
	}
}

func TestKnowledgeRev_StableAcrossCallsAndSensitiveToEdits(t *testing.T) {
	entry := KnowledgeEntry{
		Slug: "x", Title: "Y", Body: "Z", Status: "backlog", Type: "memory",
		Metadata: map[string]any{"a": 1},
	}
	r1 := KnowledgeRev(entry)
	r2 := KnowledgeRev(entry)
	if r1 != r2 {
		t.Errorf("rev unstable: %q vs %q", r1, r2)
	}
	if len(r1) != 12 {
		t.Errorf("rev length = %d, want 12", len(r1))
	}
	mutated := entry
	mutated.Body = "Z2"
	if KnowledgeRev(mutated) == r1 {
		t.Errorf("rev did not change after body edit")
	}
}

func TestKnowledgeResource_SyncContextCancellation(t *testing.T) {
	res := NewMemoryResource().(*knowledgeResource)
	c := &fakeClient{
		routes: map[string][]byte{
			"/api/projects/7/memory": []byte(fakeKnowledgeListJSON("memory", "a", "b", "c")),
		},
	}
	work := t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := res.Sync(ctx, c, 7, "ACME", work, "", nil)
	if err == nil {
		t.Fatal("expected cancellation error")
	}
}

func TestEventKind_HandlesAllFiveKnowledgeKinds(t *testing.T) {
	cases := map[string]string{
		"memory_changed":           "memory",
		"runbook_changed":          "runbook",
		"external_system_changed":  "external_system",
		"related_project_changed":  "related_project",
		"guideline_changed":        "guideline",
	}
	for in, want := range cases {
		if got := EventKind(in); got != want {
			t.Errorf("EventKind(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestKnowledgeResource_LocalPathLayout(t *testing.T) {
	cases := []struct {
		res     Resource
		project string
		slug    string
		want    string
	}{
		{NewMemoryResource(), "ACME", "feedback_xyz", ".paimos/cache/ACME/memory/feedback_xyz.md"},
		{NewRunbookResource(), "ACME", "deploy", ".paimos/cache/ACME/runbooks/deploy.md"},
		{NewExternalSystemResource(), "PAI", "clickhouse", ".paimos/cache/PAI/external-systems/clickhouse.md"},
		{NewRelatedProjectResource(), "PAI", "frontend", ".paimos/cache/PAI/related-projects/frontend.md"},
		{NewGuidelineResource(), "PAI", "no-secrets", ".paimos/cache/PAI/guidelines/no-secrets.md"},
	}
	for _, tc := range cases {
		got := tc.res.LocalPath(tc.project, tc.slug)
		if got != tc.want {
			t.Errorf("%s LocalPath = %q, want %q", tc.res.Kind(), got, tc.want)
		}
	}
}
