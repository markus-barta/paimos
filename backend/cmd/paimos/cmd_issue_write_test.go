// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReadMultilineInput covers the file-vs-inline mutual exclusion
// rules that are the whole point of PAI-91: every mutation command
// promises "either --foo or --foo-file, never both, and file wins
// precedence when tests can't infer". Breaking this is how the
// shell-quoted-JSON foot-gun crept back in.
func TestReadMultilineInput(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "desc.md")
	fileContent := "# Heading\n\nBody with **markdown**.\n"
	if err := os.WriteFile(filePath, []byte(fileContent), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	cases := []struct {
		name        string
		inline      string
		file        string
		wantValue   string
		wantSet     bool
		wantErr     bool
		errContains string
	}{
		{
			name:      "neither set",
			wantValue: "",
			wantSet:   false,
		},
		{
			name:      "inline only",
			inline:    "single line",
			wantValue: "single line",
			wantSet:   true,
		},
		{
			name:      "file only",
			file:      filePath,
			wantValue: fileContent,
			wantSet:   true,
		},
		{
			name:        "both set → error",
			inline:      "x",
			file:        filePath,
			wantErr:     true,
			errContains: "mutually exclusive",
		},
		{
			name:        "file points at non-existent path",
			file:        filepath.Join(dir, "missing.md"),
			wantErr:     true,
			errContains: "no such file",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, set, err := readMultilineInput(tc.inline, tc.file, "description")
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v, wantErr=%v", err, tc.wantErr)
			}
			if tc.wantErr {
				if tc.errContains != "" && !containsFold(err.Error(), tc.errContains) {
					t.Errorf("err = %q, want substring %q", err.Error(), tc.errContains)
				}
				return
			}
			if got != tc.wantValue {
				t.Errorf("value = %q, want %q", got, tc.wantValue)
			}
			if set != tc.wantSet {
				t.Errorf("set = %v, want %v", set, tc.wantSet)
			}
		})
	}
}

// TestReadMultilineInput_Stdin verifies the "-" convention for
// file-flag → stdin. Uses a temp pipe since os.Stdin is process-wide.
func TestReadMultilineInput_Stdin(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	origStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = origStdin })

	go func() {
		_, _ = w.Write([]byte("from stdin"))
		_ = w.Close()
	}()

	got, set, err := readMultilineInput("", "-", "description")
	if err != nil {
		t.Fatalf("readMultilineInput: %v", err)
	}
	if !set {
		t.Error("set = false, want true for stdin input")
	}
	if got != "from stdin" {
		t.Errorf("value = %q, want %q", got, "from stdin")
	}
}

// containsFold is case-insensitive substring check. "no such file" vs
// "No such file" differ across OSes, so the error-message assertion
// needs a fuzzy compare.
func containsFold(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}
