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

func testReportsDir() string {
	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "/app/data"
	}
	return filepath.Join(dataDir, "test-reports")
}

// GET /api/dev/test-reports — list all report files, newest first (admin only).
func ListTestReports(w http.ResponseWriter, r *http.Request) {
	dir := testReportsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		// Directory doesn't exist yet — return empty list, not an error.
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("[]"))
		return
	}

	var reports []TestReportMeta
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".html") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		// Extract version from filename: test-results-{version}.html
		version := strings.TrimPrefix(e.Name(), "test-results-")
		version  = strings.TrimSuffix(version, ".html")
		meta := TestReportMeta{
			Filename:    e.Name(),
			Version:     version,
			GeneratedAt: info.ModTime().UTC().Format(time.RFC3339),
			SizeBytes:   info.Size(),
		}
		// Try to read per-version summary sidecar for pass/fail counts.
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
				meta.Total  = &s.Total
			}
		}
		reports = append(reports, meta)
	}

	// Newest first by modification time (filename order is version-sorted,
	// but sort by mod time to be safe).
	sort.Slice(reports, func(i, j int) bool {
		return reports[i].GeneratedAt > reports[j].GeneratedAt
	})

	if reports == nil {
		reports = []TestReportMeta{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(reports)
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
	dir := testReportsDir()
	summaryPath := filepath.Join(dir, "latest-summary.json")

	data, err := os.ReadFile(summaryPath)
	if err != nil {
		// No summary yet.
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"failures":0,"passed":0,"total":0,"version":""}`))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}
