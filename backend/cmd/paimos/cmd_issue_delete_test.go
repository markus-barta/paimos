// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package main

import (
	"strings"
	"testing"
)

// PAI-481: delete resolves the ref to an id, then soft-deletes (default)
// or — guarded — permanently purges. These cover the helper that the
// cobra command delegates to.

func TestDeleteIssueByRef_SoftDelete(t *testing.T) {
	srv := startFakeAPI(t, map[string]string{
		"GET /api/issues/PAI-481": `{"id":1234,"issue_key":"PAI-481"}`,
		"DELETE /api/issues/1234": ``,
	})
	id, summary, err := deleteIssueByRef(newClientForTest(srv.URL), "PAI-481", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 1234 {
		t.Errorf("id = %d, want 1234", id)
	}
	if !strings.Contains(summary, "trash") {
		t.Errorf("summary = %q, want it to mention the trash", summary)
	}
}

func TestDeleteIssueByRef_PurgeEmpty(t *testing.T) {
	srv := startFakeAPI(t, map[string]string{
		"GET /api/issues/PAI-481":          `{"id":1234}`,
		"GET /api/issues/1234/comments":    `[]`,
		"GET /api/issues/1234/attachments": `[]`,
		"DELETE /api/issues/1234":          ``,
		"DELETE /api/issues/1234/purge":    ``,
	})
	_, summary, err := deleteIssueByRef(newClientForTest(srv.URL), "PAI-481", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(summary, "permanently deleted") {
		t.Errorf("summary = %q, want a permanent-delete confirmation", summary)
	}
}

func TestDeleteIssueByRef_PurgeRefusedWithComments(t *testing.T) {
	// The DELETE routes are deliberately unmocked: the guard must refuse
	// before any destructive call is made.
	srv := startFakeAPI(t, map[string]string{
		"GET /api/issues/PAI-481":       `{"id":1234}`,
		"GET /api/issues/1234/comments": `[{"id":1,"body":"keep me"}]`,
	})
	_, _, err := deleteIssueByRef(newClientForTest(srv.URL), "PAI-481", true)
	if err == nil {
		t.Fatal("expected a refusal error, got nil")
	}
	if !strings.Contains(err.Error(), "refusing to purge") || !strings.Contains(err.Error(), "comment") {
		t.Errorf("err = %v, want a comment-based purge refusal", err)
	}
}

func TestDeleteIssueByRef_PurgeRefusedWithAttachments(t *testing.T) {
	srv := startFakeAPI(t, map[string]string{
		"GET /api/issues/PAI-481":          `{"id":1234}`,
		"GET /api/issues/1234/comments":    `[]`,
		"GET /api/issues/1234/attachments": `[{"id":7,"filename":"spec.pdf"}]`,
	})
	_, _, err := deleteIssueByRef(newClientForTest(srv.URL), "PAI-481", true)
	if err == nil {
		t.Fatal("expected a refusal error, got nil")
	}
	if !strings.Contains(err.Error(), "attachment") {
		t.Errorf("err = %v, want an attachment-based purge refusal", err)
	}
}
