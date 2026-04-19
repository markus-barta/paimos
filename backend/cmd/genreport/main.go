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

// genreport produces a self-contained HTML test report from `go test -json` output.
//
// Usage:
//
//	go test ./handlers/... -json > results.json
//	go run ./cmd/genreport -version=$VERSION -input=results.json -out=test-results-$VERSION.html
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// testEvent is a line from `go test -json` output.
type testEvent struct {
	Time    time.Time `json:"Time"`
	Action  string    `json:"Action"`
	Package string    `json:"Package"`
	Test    string    `json:"Test"`
	Elapsed float64   `json:"Elapsed"`
	Output  string    `json:"Output"`
}

type testResult struct {
	Name    string
	Passed  bool
	Elapsed float64
	Output  []string
}

func main() {
	version := flag.String("version", "dev", "Release version (e.g. 0.9.16)")
	input := flag.String("input", "", "Path to go test -json output")
	outFile := flag.String("out", "test-results.html", "Output HTML file path")
	flag.Parse()

	var results []testResult
	if *input != "" {
		results = parseJSON(*input)
	}

	htmlContent := buildHTML(*version, results)

	if err := os.WriteFile(*outFile, []byte(htmlContent), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "write %s: %v\n", *outFile, err)
		os.Exit(1)
	}
	fmt.Printf("Report written to %s\n", *outFile)

	// Write latest-summary.json alongside the report.
	passed, failed := summary(results)
	type summaryJSON struct {
		Version     string `json:"version"`
		Total       int    `json:"total"`
		Passed      int    `json:"passed"`
		Failures    int    `json:"failures"`
		GeneratedAt string `json:"generated_at"`
	}
	sum := summaryJSON{
		Version:     *version,
		Total:       passed + failed,
		Passed:      passed,
		Failures:    failed,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}
	sumBytes, _ := json.Marshal(sum)
	outDir := filepath.Dir(*outFile)
	sumPath := filepath.Join(outDir, "latest-summary.json")
	if err := os.WriteFile(sumPath, sumBytes, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "write summary: %v\n", err)
	} else {
		fmt.Printf("Summary written to %s\n", sumPath)
	}

	// Also write per-version summary sidecar.
	versionSumPath := filepath.Join(outDir, fmt.Sprintf("test-results-%s-summary.json", *version))
	if err := os.WriteFile(versionSumPath, sumBytes, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "write version summary: %v\n", err)
	} else {
		fmt.Printf("Version summary written to %s\n", versionSumPath)
	}
}

func parseJSON(path string) []testResult {
	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open %s: %v\n", path, err)
		return nil
	}
	defer f.Close()

	type acc struct {
		passed  bool
		failed  bool
		elapsed float64
		output  []string
	}
	tests := map[string]*acc{}
	var order []string

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var ev testEvent
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			continue
		}
		if ev.Test == "" {
			continue
		}
		if _, ok := tests[ev.Test]; !ok {
			tests[ev.Test] = &acc{}
			order = append(order, ev.Test)
		}
		a := tests[ev.Test]
		switch ev.Action {
		case "pass":
			a.passed = true
			a.elapsed = ev.Elapsed
		case "fail":
			a.failed = true
			a.elapsed = ev.Elapsed
		case "output":
			line := strings.TrimRight(ev.Output, "\n")
			if line != "" {
				a.output = append(a.output, line)
			}
		}
	}

	var results []testResult
	for _, name := range order {
		a := tests[name]
		results = append(results, testResult{
			Name:    name,
			Passed:  a.passed && !a.failed,
			Elapsed: a.elapsed,
			Output:  a.output,
		})
	}
	// Sort: failures first, then by name.
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Passed != results[j].Passed {
			return !results[i].Passed
		}
		return results[i].Name < results[j].Name
	})
	return results
}

func summary(results []testResult) (passed, failed int) {
	for _, r := range results {
		if r.Passed {
			passed++
		} else {
			failed++
		}
	}
	return
}

func buildHTML(version string, results []testResult) string {
	passed, failed := summary(results)
	total := passed + failed
	generatedAt := time.Now().UTC().Format("2006-01-02 15:04:05 UTC")

	var sb strings.Builder
	sb.WriteString(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>PAIMOS Test Results ` + html.EscapeString(version) + `</title>
<style>
  *{box-sizing:border-box;margin:0;padding:0}
  body{font-family:'DM Sans',system-ui,sans-serif;font-size:14px;background:#f2f5f8;color:#1a2636;padding:2rem}
  h1{font-size:20px;font-weight:700;margin-bottom:.25rem}
  .meta{font-size:12px;color:#637383;margin-bottom:1.5rem}
  .card{background:#fff;border-radius:8px;box-shadow:0 1px 4px rgba(0,0,0,.08);overflow:hidden;margin-bottom:2rem}
  .card-header{padding:1rem 1.25rem;border-bottom:1px solid #d1dce8;display:flex;align-items:center;gap:.75rem}
  .card-title{font-weight:700;font-size:15px}
  .badge{display:inline-flex;align-items:center;gap:.3rem;padding:.2rem .6rem;border-radius:99px;font-size:12px;font-weight:600}
  .badge-pass{background:#d1fae5;color:#065f46}
  .badge-fail{background:#fee2e2;color:#991b1b}
  .summary{padding:.6rem 1.25rem;background:#f8fafc;border-bottom:1px solid #d1dce8;font-size:12px;color:#637383}
  .test-list{list-style:none}
  .test-item{padding:.6rem 1.25rem;border-bottom:1px solid #f0f4f8;display:flex;align-items:flex-start;gap:.75rem}
  .test-item:last-child{border-bottom:none}
  .test-icon{font-size:15px;margin-top:.05rem;flex-shrink:0}
  .test-info{flex:1;min-width:0}
  .test-name{font-size:13px;font-weight:500;word-break:break-word}
  .test-name.fail{color:#991b1b}
  .test-name.pass{color:#1a2636}
  .test-elapsed{font-size:11px;color:#637383;margin-top:.1rem}
  .test-output{margin-top:.4rem;font-size:11px;font-family:'DM Mono',monospace;background:#fef2f2;border:1px solid #fecaca;border-radius:4px;padding:.4rem .6rem;color:#7f1d1d;white-space:pre-wrap;word-break:break-all}
  .no-data{padding:2rem 1.25rem;text-align:center;color:#637383;font-size:13px}
</style>
</head>
<body>
`)

	fmt.Fprintf(&sb, `<h1>PAIMOS Test Results — v%s</h1>
<p class="meta">Generated %s</p>
`, html.EscapeString(version), generatedAt)

	sb.WriteString(`<div class="card">`)
	sb.WriteString(`<div class="card-header">`)
	fmt.Fprintf(&sb, `<span class="card-title">Test Suite</span>`)

	if len(results) == 0 {
		sb.WriteString(`<span class="badge badge-fail">no results</span>`)
	} else if failed > 0 {
		fmt.Fprintf(&sb, `<span class="badge badge-fail">%d failed</span>`, failed)
	} else {
		fmt.Fprintf(&sb, `<span class="badge badge-pass">all %d passed</span>`, passed)
	}
	sb.WriteString(`</div>`)

	if len(results) == 0 {
		sb.WriteString(`<div class="no-data">No test results found.</div>`)
		sb.WriteString(`</div>`)
	} else {
		fmt.Fprintf(&sb, `<div class="summary">%d passed · %d failed · %d total</div>`,
			passed, failed, total)

		sb.WriteString(`<ul class="test-list">`)
		for _, r := range results {
			icon := "✅"
			cls := "pass"
			if !r.Passed {
				icon = "❌"
				cls = "fail"
			}
			displayName := r.Name
			if idx := strings.LastIndex(displayName, "/"); idx >= 0 {
				displayName = strings.Repeat("  ", strings.Count(r.Name, "/")-1) + "↳ " + displayName[idx+1:]
			}
			fmt.Fprintf(&sb, `<li class="test-item"><span class="test-icon">%s</span><div class="test-info">`, icon)
			fmt.Fprintf(&sb, `<div class="test-name %s">%s</div>`, cls, html.EscapeString(displayName))
			if r.Elapsed > 0 {
				fmt.Fprintf(&sb, `<div class="test-elapsed">%.3fs</div>`, r.Elapsed)
			}
			if !r.Passed && len(r.Output) > 0 {
				fmt.Fprintf(&sb, `<pre class="test-output">%s</pre>`, html.EscapeString(strings.Join(r.Output, "\n")))
			}
			sb.WriteString(`</div></li>`)
		}
		sb.WriteString(`</ul></div>`)
	}

	sb.WriteString(`
</body>
</html>
`)
	return sb.String()
}
