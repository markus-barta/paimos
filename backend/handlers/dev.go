// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public
// License along with this program. If not, see <https://www.gnu.org/licenses/>.

package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
)

type TestReportMeta struct {
	Filename    string `json:"filename"`
	Version     string `json:"version"`
	GeneratedAt string `json:"generated_at"`
	SizeBytes   int64  `json:"size_bytes"`
	Passed      *int   `json:"passed,omitempty"`
	Failed      *int   `json:"failed,omitempty"`
	Total       *int   `json:"total,omitempty"`
}

type TestReportSummary struct {
	Version     string `json:"version"`
	Failures    int    `json:"failures"`
	Passed      int    `json:"passed"`
	Total       int    `json:"total"`
	GeneratedAt string `json:"generated_at,omitempty"`
	Available   bool   `json:"available"`
	Status      string `json:"status,omitempty"`
	ReportCount int    `json:"report_count,omitempty"`
}

func testReportsDir() string {
	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "/app/data"
	}
	return filepath.Join(dataDir, "test-reports")
}

func defaultTestReportSummary(status string, reportCount int) TestReportSummary {
	return TestReportSummary{
		Version:     "",
		Failures:    0,
		Passed:      0,
		Total:       0,
		GeneratedAt: "",
		Available:   false,
		Status:      status,
		ReportCount: reportCount,
	}
}

func listStoredTestReports() ([]TestReportMeta, error) {
	return listStoredTestReportsFromDir(testReportsDir())
}

func readTestReportSummary() TestReportSummary {
	return readTestReportSummaryFromDir(testReportsDir())
}

func isValidTestReportFilename(name string) bool {
	return strings.HasPrefix(name, "test-results-") && strings.HasSuffix(name, ".html") && !strings.Contains(name, "/") && !strings.Contains(name, "..")
}

func isValidSummaryFilename(name string) bool {
	return strings.HasPrefix(name, "test-results-") && strings.HasSuffix(name, "-summary.json") && !strings.Contains(name, "/") && !strings.Contains(name, "..")
}

type testReportCounts struct {
	Version     string `json:"version"`
	Failures    int    `json:"failures"`
	Passed      int    `json:"passed"`
	Total       int    `json:"total"`
	GeneratedAt string `json:"generated_at"`
}

func parseTestReportCounts(data []byte) (testReportCounts, error) {
	var out testReportCounts
	if err := json.Unmarshal(data, &out); err != nil {
		return testReportCounts{}, err
	}
	return out, nil
}

// GET /api/dev/test-reports — list all report files, newest first (admin only).
func ListTestReports(w http.ResponseWriter, r *http.Request) {
	reports, err := listStoredTestReports()
	if err != nil {
		jsonError(w, "list failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(reports)
}

// POST /api/dev/test-reports — upload an HTML report bundle (admin only).
// multipart/form-data:
//   - report: required test-results-<version>.html
//   - summary: optional test-results-<version>-summary.json
//   - latest_summary: optional latest-summary.json
func UploadTestReport(w http.ResponseWriter, r *http.Request) {
	bundle, err := parseUploadedTestReportBundle(r)
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	latestSummary, err := storeUploadedTestReportBundle(testReportsDir(), bundle)
	if err != nil {
		jsonError(w, "failed to store report bundle", http.StatusInternalServerError)
		return
	}

	jsonOK(w, map[string]any{
		"filename": bundle.reportFilename,
		"version":  latestSummary.Version,
		"status":   latestSummary.Status,
	})
}

// GET /api/dev/test-reports/{filename} — serve raw HTML report (admin only).
func GetTestReport(w http.ResponseWriter, r *http.Request) {
	filename := chi.URLParam(r, "filename")

	// Safety: only allow test-results-*.html filenames.
	if !strings.HasPrefix(filename, "test-results-") || !strings.HasSuffix(filename, ".html") {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	// Extra safety: no path traversal.
	if strings.Contains(filename, "/") || strings.Contains(filename, "..") {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	path := filepath.Join(testReportsDir(), filename)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeFile(w, r, path)
}

// GET /api/dev/test-reports/latest-summary — quick JSON summary for badge count.
// Returns {complete_failures: N} from the most recent report filename.
// Since we can't parse HTML server-side cheaply, we store a JSON sidecar.
func GetTestReportSummary(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(readTestReportSummary())
}
