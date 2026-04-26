package handlers

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadTestReportSummaryFromDir_Partial(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test-results-2.0.0.html"), []byte("<html></html>"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := readTestReportSummaryFromDir(dir)
	if got.Status != "partial" {
		t.Fatalf("status=%q want partial", got.Status)
	}
	if got.ReportCount != 1 {
		t.Fatalf("report_count=%d want 1", got.ReportCount)
	}
	if got.Available {
		t.Fatalf("available=true want false for partial summary state")
	}
}

func TestReadTestReportSummaryFromDir_Ready(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test-results-2.0.1.html"), []byte("<html></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "latest-summary.json"), []byte(`{"version":"2.0.1","failures":0,"passed":5,"total":5,"status":"ready"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	got := readTestReportSummaryFromDir(dir)
	if got.Status != "ready" {
		t.Fatalf("status=%q want ready", got.Status)
	}
	if !got.Available {
		t.Fatalf("available=false want true")
	}
	if got.ReportCount != 1 {
		t.Fatalf("report_count=%d want 1", got.ReportCount)
	}
}
