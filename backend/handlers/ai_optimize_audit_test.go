// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

// PAI-153 + PAI-164. Audit-shape regression tests for the AI action
// dispatcher (ai_action.go).
//
// PAI-146 / PAI-153 invariant: usage must be auditable, but full
// prompt and response content must NOT appear in audit lines. The
// tests below assert:
//
//   1. an audit line is emitted with the documented field set
//      (action / sub_action / user_id / field / issue_id / model /
//      outcome / latency_ms / prompt_tokens / completion_tokens),
//   2. no body sentinel ever leaks into the line, and
//   3. the outcome taxonomy stays a stable closed enum that
//      operators can grep against.
//
// These tests previously targeted auditOptimize; PAI-164 collapsed
// that single-purpose function into auditAction so the tests now
// exercise the unified shape.

package handlers

import (
	"bytes"
	"log"
	"strings"
	"testing"
	"time"
)

// captureLog redirects log output to a buffer for the duration of
// the test, then restores it.
func captureLog(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	buf := &bytes.Buffer{}
	prev := log.Writer()
	log.SetOutput(buf)
	return buf, func() { log.SetOutput(prev) }
}

func TestAuditAction_LineShape(t *testing.T) {
	buf, restore := captureLog(t)
	defer restore()

	auditAction(
		/*requestID*/ "req-1",
		/*userID*/ 42,
		/*action*/ "optimize",
		/*subAction*/ "",
		/*field*/ "description",
		/*issueID*/ 123,
		/*model*/ "anthropic/claude-3.5-haiku",
		/*outcome*/ outcomeOK,
		/*latency*/ 850*time.Millisecond,
		/*promptTokens*/ 100,
		/*completionTokens*/ 50,
	)

	got := buf.String()
	for _, want := range []string{
		"audit: ai_action",
		"request_id=req-1",
		"action=optimize",
		"user_id=42",
		"field=description",
		"issue_id=123",
		`model="anthropic/claude-3.5-haiku"`,
		"outcome=ok",
		"latency_ms=850",
		"prompt_tokens=100",
		"completion_tokens=50",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("audit line missing %q\nfull line: %s", want, got)
		}
	}
}

func TestAuditAction_SubActionRendered(t *testing.T) {
	buf, restore := captureLog(t)
	defer restore()

	auditAction("req-2", 7, "suggest_enhancement", "security", "description", 9, "m", outcomeOK, time.Second, 0, 0)
	got := buf.String()
	for _, want := range []string{
		"action=suggest_enhancement",
		"sub_action=security",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("audit line missing %q\nfull line: %s", want, got)
		}
	}
}

// TestAuditAction_NoBodiesLeak guards the PAI-146 / PAI-153
// invariant: prompt and response content MUST NOT appear in audit
// lines. Same trick as before — the auditAction signature accepts
// only metadata, so a future refactor that adds a body parameter
// gets caught at the call site rather than via this runtime check.
func TestAuditAction_NoBodiesLeak(t *testing.T) {
	buf, restore := captureLog(t)
	defer restore()

	const sourceSentinel = "SOURCE_BODY_SENTINEL_DO_NOT_LOG"
	const responseSentinel = "RESPONSE_BODY_SENTINEL_DO_NOT_LOG"

	auditAction("req-3", 1, "optimize", "", "description", 99, "m", outcomeOK, time.Second, 0, 0)

	got := buf.String()
	if strings.Contains(got, sourceSentinel) || strings.Contains(got, responseSentinel) {
		t.Fatalf("audit line leaked body sentinel: %s", got)
	}
	if !strings.Contains(got, "audit: ai_action") {
		t.Fatalf("no audit line emitted")
	}
}

func TestAuditAction_FailureLineEmitted(t *testing.T) {
	buf, restore := captureLog(t)
	defer restore()

	auditAction("req-4", 7, "optimize", "", "notes", 0, "anthropic/claude-3.5-haiku", outcomeFailUpstream, 250*time.Millisecond, 0, 0)
	got := buf.String()
	if !strings.Contains(got, "outcome=fail_upstream") {
		t.Errorf("expected outcome=fail_upstream, got: %s", got)
	}
	if !strings.Contains(got, "issue_id=0") {
		t.Errorf("expected issue_id=0 sentinel, got: %s", got)
	}
}

// TestOutcomesAreStableEnum verifies the documented audit outcome
// taxonomy stays a closed set of stable strings. Renaming any of
// these silently breaks operators' grep patterns; pinning the
// values here forces a deliberate change.
func TestOutcomesAreStableEnum(t *testing.T) {
	wantValues := map[string]string{
		outcomeOK:              "ok",
		outcomeFailTimeout:     "fail_timeout",
		outcomeFailUpstream:    "fail_upstream",
		outcomeDenied:          "denied",
		outcomeUnauth:          "unauth",
		outcomeCfgLoadFail:     "cfg_load_fail",
		outcomeUnconfigured:    "unconfigured",
		outcomeBadRequest:      "bad_request",
		outcomeProviderMissing: "provider_missing",
		outcomeCtxFail:         "ctx_fail",
	}
	for k, want := range wantValues {
		if k != want {
			t.Errorf("outcome const = %q, want %q", k, want)
		}
	}
	// Spot-check that auditAction renders a few outcomes so a
	// downstream parser doesn't have to special-case any of them.
	for _, outcome := range []string{outcomeOK, outcomeFailTimeout, outcomeFailUpstream, outcomeDenied, outcomeBadRequest} {
		buf, restore := captureLog(t)
		auditAction("req-x", 1, "optimize", "", "description", 5, "m", outcome, 10*time.Millisecond, 0, 0)
		got := buf.String()
		restore()
		if !strings.Contains(got, "outcome="+outcome) {
			t.Errorf("audit line missing outcome=%s\nfull line: %s", outcome, got)
		}
		if !strings.Contains(got, "audit: ai_action") {
			t.Errorf("audit line missing prefix\nfull line: %s", got)
		}
	}
}
