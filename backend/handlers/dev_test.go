package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"testing"
)

func TestTestReportSummaryMissing(t *testing.T) {
	ts := newTestServer(t)

	resp := ts.get(t, "/api/dev/test-reports/summary", ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)

	var summary struct {
		Status      string `json:"status"`
		Available   bool   `json:"available"`
		ReportCount int    `json:"report_count"`
	}
	decode(t, resp, &summary)
	if summary.Status != "missing_reports" {
		t.Fatalf("status: got %q want missing_reports", summary.Status)
	}
	if summary.Available {
		t.Fatalf("available: got true want false")
	}
	if summary.ReportCount != 0 {
		t.Fatalf("report_count: got %d want 0", summary.ReportCount)
	}
}

func TestUploadTestReportBundle(t *testing.T) {
	ts := newTestServer(t)

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	report, err := w.CreateFormFile("report", "test-results-1.2.3.html")
	if err != nil {
		t.Fatalf("create report form file: %v", err)
	}
	report.Write([]byte("<html><body>ok</body></html>"))

	summary, err := w.CreateFormFile("summary", "test-results-1.2.3-summary.json")
	if err != nil {
		t.Fatalf("create summary form file: %v", err)
	}
	summary.Write([]byte(`{"version":"1.2.3","passed":12,"failures":1,"total":13,"generated_at":"2026-04-26T07:30:00Z"}`))
	w.Close()

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, ts.srv.URL+"/api/dev/test-reports", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Cookie", ts.adminCookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upload report bundle: %v", err)
	}
	assertStatus(t, resp, http.StatusOK)

	listResp := ts.get(t, "/api/dev/test-reports", ts.adminCookie)
	assertStatus(t, listResp, http.StatusOK)
	var reports []struct {
		Filename string `json:"filename"`
		Version  string `json:"version"`
		Passed   *int   `json:"passed"`
		Failed   *int   `json:"failed"`
	}
	decode(t, listResp, &reports)
	if len(reports) != 1 {
		t.Fatalf("reports length: got %d want 1", len(reports))
	}
	if reports[0].Filename != "test-results-1.2.3.html" {
		t.Fatalf("filename: got %q", reports[0].Filename)
	}
	if reports[0].Passed == nil || *reports[0].Passed != 12 {
		t.Fatalf("passed: got %#v want 12", reports[0].Passed)
	}
	if reports[0].Failed == nil || *reports[0].Failed != 1 {
		t.Fatalf("failed: got %#v want 1", reports[0].Failed)
	}

	summaryResp := ts.get(t, "/api/dev/test-reports/summary", ts.adminCookie)
	assertStatus(t, summaryResp, http.StatusOK)
	var latest struct {
		Version     string `json:"version"`
		Failures    int    `json:"failures"`
		Passed      int    `json:"passed"`
		Total       int    `json:"total"`
		Available   bool   `json:"available"`
		Status      string `json:"status"`
		ReportCount int    `json:"report_count"`
	}
	decode(t, summaryResp, &latest)
	if latest.Version != "1.2.3" || latest.Failures != 1 || latest.Passed != 12 || latest.Total != 13 {
		b, _ := json.Marshal(latest)
		t.Fatalf("unexpected latest summary: %s", b)
	}
	if !latest.Available || latest.Status != "ready" || latest.ReportCount != 1 {
		t.Fatalf("unexpected availability: %#v", latest)
	}

	reportResp := ts.get(t, "/api/dev/test-reports/test-results-1.2.3.html", ts.adminCookie)
	assertStatus(t, reportResp, http.StatusOK)
	defer reportResp.Body.Close()
	body, _ := io.ReadAll(reportResp.Body)
	if !bytes.Contains(body, []byte("<body>ok</body>")) {
		t.Fatalf("report body missing uploaded content")
	}
}

func TestUploadTestReportBundleRejectsVersionMismatch(t *testing.T) {
	ts := newTestServer(t)

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	report, err := w.CreateFormFile("report", "test-results-1.2.3.html")
	if err != nil {
		t.Fatalf("create report form file: %v", err)
	}
	report.Write([]byte("<html><body>ok</body></html>"))

	summary, err := w.CreateFormFile("summary", "test-results-1.2.3-summary.json")
	if err != nil {
		t.Fatalf("create summary form file: %v", err)
	}
	summary.Write([]byte(`{"version":"9.9.9","passed":12,"failures":1,"total":13,"generated_at":"2026-04-26T07:30:00Z"}`))
	w.Close()

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, ts.srv.URL+"/api/dev/test-reports", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Cookie", ts.adminCookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upload mismatched report bundle: %v", err)
	}
	assertStatus(t, resp, http.StatusBadRequest)

	listResp := ts.get(t, "/api/dev/test-reports", ts.adminCookie)
	assertStatus(t, listResp, http.StatusOK)
	var reports []map[string]any
	decode(t, listResp, &reports)
	if len(reports) != 0 {
		t.Fatalf("reports length: got %d want 0 after rejected upload", len(reports))
	}
}
