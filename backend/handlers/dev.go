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
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

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
	dir := testReportsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []TestReportMeta{}, nil
		}
		return nil, err
	}

	reports := make([]TestReportMeta, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".html") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		version := strings.TrimPrefix(e.Name(), "test-results-")
		version = strings.TrimSuffix(version, ".html")
		meta := TestReportMeta{
			Filename:    e.Name(),
			Version:     version,
			GeneratedAt: info.ModTime().UTC().Format(time.RFC3339),
			SizeBytes:   info.Size(),
		}
		sidecarPath := filepath.Join(dir, "test-results-"+version+"-summary.json")
		if sidecarData, err := os.ReadFile(sidecarPath); err == nil {
			var s struct {
				Passed   int `json:"passed"`
				Failures int `json:"failures"`
				Total    int `json:"total"`
			}
			if json.Unmarshal(sidecarData, &s) == nil {
				failed := s.Failures
				meta.Passed = &s.Passed
				meta.Failed = &failed
				meta.Total = &s.Total
			}
		}
		reports = append(reports, meta)
	}

	sort.Slice(reports, func(i, j int) bool {
		return reports[i].GeneratedAt > reports[j].GeneratedAt
	})

	return reports, nil
}

func readTestReportSummary() TestReportSummary {
	reports, err := listStoredTestReports()
	if err != nil {
		return defaultTestReportSummary("error", 0)
	}

	data, err := os.ReadFile(filepath.Join(testReportsDir(), "latest-summary.json"))
	if err != nil {
		if len(reports) == 0 {
			return defaultTestReportSummary("missing_reports", 0)
		}
		return defaultTestReportSummary("partial", len(reports))
	}

	var out TestReportSummary
	if err := json.Unmarshal(data, &out); err != nil {
		if len(reports) == 0 {
			return defaultTestReportSummary("missing_reports", 0)
		}
		return defaultTestReportSummary("partial", len(reports))
	}
	out.Available = len(reports) > 0
	if out.Status == "" {
		if out.Available {
			out.Status = "ready"
		} else {
			out.Status = "missing_reports"
		}
	}
	out.ReportCount = len(reports)
	return out
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
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		jsonError(w, "invalid multipart form", http.StatusBadRequest)
		return
	}

	report, reportHeader, err := r.FormFile("report")
	if err != nil {
		jsonError(w, "report file required", http.StatusBadRequest)
		return
	}
	defer report.Close()

	if !isValidTestReportFilename(reportHeader.Filename) {
		jsonError(w, "invalid report filename", http.StatusBadRequest)
		return
	}

	reportData, err := io.ReadAll(report)
	if err != nil || len(reportData) == 0 {
		jsonError(w, "invalid report file", http.StatusBadRequest)
		return
	}

	dir := testReportsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		jsonError(w, "failed to prepare report directory", http.StatusInternalServerError)
		return
	}

	version := strings.TrimSuffix(strings.TrimPrefix(reportHeader.Filename, "test-results-"), ".html")

	var latestSummary TestReportSummary
	latestSummary.Version = version
	latestSummary.Available = true
	latestSummary.Status = "ready"

	var summaryFilename string
	var summaryData []byte

	summaryFile, summaryHeader, err := r.FormFile("summary")
	if err == nil {
		defer summaryFile.Close()
		if !isValidSummaryFilename(summaryHeader.Filename) {
			jsonError(w, "invalid summary filename", http.StatusBadRequest)
			return
		}
		readSummaryData, readErr := io.ReadAll(summaryFile)
		if readErr != nil {
			jsonError(w, "invalid summary file", http.StatusBadRequest)
			return
		}
		parsed, parseErr := parseTestReportCounts(readSummaryData)
		if parseErr != nil {
			jsonError(w, "summary must be valid json", http.StatusBadRequest)
			return
		}
		if parsed.Version != "" && parsed.Version != version {
			jsonError(w, "summary version does not match report filename", http.StatusBadRequest)
			return
		}
		summaryFilename = summaryHeader.Filename
		summaryData = readSummaryData
		if parsed.Version != "" {
			latestSummary.Version = parsed.Version
		}
		latestSummary.Failures = parsed.Failures
		latestSummary.Passed = parsed.Passed
		latestSummary.Total = parsed.Total
		latestSummary.GeneratedAt = parsed.GeneratedAt
	} else if !errors.Is(err, http.ErrMissingFile) {
		jsonError(w, "invalid summary file", http.StatusBadRequest)
		return
	}

	latestFile, _, err := r.FormFile("latest_summary")
	if err == nil {
		defer latestFile.Close()
		data, readErr := io.ReadAll(latestFile)
		if readErr != nil {
			jsonError(w, "invalid latest summary file", http.StatusBadRequest)
			return
		}
		if json.Unmarshal(data, &latestSummary) != nil {
			jsonError(w, "latest summary must be valid json", http.StatusBadRequest)
			return
		}
		if latestSummary.Version != "" && latestSummary.Version != version {
			jsonError(w, "latest summary version does not match report filename", http.StatusBadRequest)
			return
		}
		latestSummary.Available = true
		if latestSummary.Status == "" {
			latestSummary.Status = "ready"
		}
	} else if !errors.Is(err, http.ErrMissingFile) {
		jsonError(w, "invalid latest summary file", http.StatusBadRequest)
		return
	}

	reportPath := filepath.Join(dir, reportHeader.Filename)
	if err := os.WriteFile(reportPath, reportData, 0o644); err != nil {
		jsonError(w, "failed to store report", http.StatusInternalServerError)
		return
	}
	if summaryFilename != "" {
		if err := os.WriteFile(filepath.Join(dir, summaryFilename), summaryData, 0o644); err != nil {
			jsonError(w, "failed to store summary", http.StatusInternalServerError)
			return
		}
	}
	reports, _ := listStoredTestReports()
	latestSummary.ReportCount = len(reports)
	latestSummaryJSON, _ := json.Marshal(latestSummary)
	if err := os.WriteFile(filepath.Join(dir, "latest-summary.json"), latestSummaryJSON, 0o644); err != nil {
		jsonError(w, "failed to store latest summary", http.StatusInternalServerError)
		return
	}

	jsonOK(w, map[string]any{
		"filename": reportHeader.Filename,
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
