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
)

type uploadedTestReportBundle struct {
	reportFilename  string
	reportData      []byte
	summaryFilename string
	summaryData     []byte
	latestSummary   TestReportSummary
}

func listStoredTestReportsFromDir(dir string) ([]TestReportMeta, error) {
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
			GeneratedAt: info.ModTime().UTC().Format(timeRFC3339),
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

func readTestReportSummaryFromDir(dir string) TestReportSummary {
	reports, err := listStoredTestReportsFromDir(dir)
	if err != nil {
		return defaultTestReportSummary("error", 0)
	}

	data, err := os.ReadFile(filepath.Join(dir, "latest-summary.json"))
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

func parseUploadedTestReportBundle(r *http.Request) (uploadedTestReportBundle, error) {
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		return uploadedTestReportBundle{}, errors.New("invalid multipart form")
	}

	report, reportHeader, err := r.FormFile("report")
	if err != nil {
		return uploadedTestReportBundle{}, errors.New("report file required")
	}
	defer report.Close()

	if !isValidTestReportFilename(reportHeader.Filename) {
		return uploadedTestReportBundle{}, errors.New("invalid report filename")
	}
	reportData, err := io.ReadAll(report)
	if err != nil || len(reportData) == 0 {
		return uploadedTestReportBundle{}, errors.New("invalid report file")
	}

	version := strings.TrimSuffix(strings.TrimPrefix(reportHeader.Filename, "test-results-"), ".html")
	bundle := uploadedTestReportBundle{
		reportFilename: reportHeader.Filename,
		reportData:     reportData,
		latestSummary: TestReportSummary{
			Version:   version,
			Available: true,
			Status:    "ready",
		},
	}

	summaryFile, summaryHeader, err := r.FormFile("summary")
	if err == nil {
		defer summaryFile.Close()
		if !isValidSummaryFilename(summaryHeader.Filename) {
			return uploadedTestReportBundle{}, errors.New("invalid summary filename")
		}
		readSummaryData, readErr := io.ReadAll(summaryFile)
		if readErr != nil {
			return uploadedTestReportBundle{}, errors.New("invalid summary file")
		}
		parsed, parseErr := parseTestReportCounts(readSummaryData)
		if parseErr != nil {
			return uploadedTestReportBundle{}, errors.New("summary must be valid json")
		}
		if parsed.Version != "" && parsed.Version != version {
			return uploadedTestReportBundle{}, errors.New("summary version does not match report filename")
		}
		bundle.summaryFilename = summaryHeader.Filename
		bundle.summaryData = readSummaryData
		if parsed.Version != "" {
			bundle.latestSummary.Version = parsed.Version
		}
		bundle.latestSummary.Failures = parsed.Failures
		bundle.latestSummary.Passed = parsed.Passed
		bundle.latestSummary.Total = parsed.Total
		bundle.latestSummary.GeneratedAt = parsed.GeneratedAt
	} else if !errors.Is(err, http.ErrMissingFile) {
		return uploadedTestReportBundle{}, errors.New("invalid summary file")
	}

	latestFile, _, err := r.FormFile("latest_summary")
	if err == nil {
		defer latestFile.Close()
		data, readErr := io.ReadAll(latestFile)
		if readErr != nil {
			return uploadedTestReportBundle{}, errors.New("invalid latest summary file")
		}
		if json.Unmarshal(data, &bundle.latestSummary) != nil {
			return uploadedTestReportBundle{}, errors.New("latest summary must be valid json")
		}
		if bundle.latestSummary.Version != "" && bundle.latestSummary.Version != version {
			return uploadedTestReportBundle{}, errors.New("latest summary version does not match report filename")
		}
		bundle.latestSummary.Available = true
		if bundle.latestSummary.Status == "" {
			bundle.latestSummary.Status = "ready"
		}
	} else if !errors.Is(err, http.ErrMissingFile) {
		return uploadedTestReportBundle{}, errors.New("invalid latest summary file")
	}

	return bundle, nil
}

func storeUploadedTestReportBundle(dir string, bundle uploadedTestReportBundle) (TestReportSummary, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return TestReportSummary{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, bundle.reportFilename), bundle.reportData, 0o644); err != nil {
		return TestReportSummary{}, err
	}
	if bundle.summaryFilename != "" {
		if err := os.WriteFile(filepath.Join(dir, bundle.summaryFilename), bundle.summaryData, 0o644); err != nil {
			return TestReportSummary{}, err
		}
	}
	reports, _ := listStoredTestReportsFromDir(dir)
	bundle.latestSummary.ReportCount = len(reports)
	latestSummaryJSON, _ := json.Marshal(bundle.latestSummary)
	if err := os.WriteFile(filepath.Join(dir, "latest-summary.json"), latestSummaryJSON, 0o644); err != nil {
		return TestReportSummary{}, err
	}
	return bundle.latestSummary, nil
}
