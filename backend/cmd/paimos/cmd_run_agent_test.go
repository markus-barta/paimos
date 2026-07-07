// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newRunServer serves a canned run detail for GET /api/runs/{id} and records
// the body of every PATCH so a test can assert the status transitions. PATCHes
// to the claim (if_status) succeed; override patchStatus to simulate a lost
// claim (409).
func newRunServer(t *testing.T, detail string, patchStatus int) (*httptest.Server, *[]map[string]any) {
	t.Helper()
	patches := &[]map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/runs/"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(detail))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/issues/"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"id":5,
				"issue_key":"PAI-5",
				"type":"ticket",
				"title":"Implement the demo change",
				"description":"Change VERSION to 0.2.0.",
				"acceptance_criteria":"npm test passes.",
				"notes":"Do not deploy from the agent.",
				"status":"new",
				"priority":"low"
			}`))
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/runs/"):
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			*patches = append(*patches, body)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(patchStatus)
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/attachments":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":99}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/attachments/link":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"linked":1}`))
		default:
			http.Error(w, `{"error":"unmocked"}`, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, patches
}

func aJob() runJob { return runJob{runID: 1, issueKey: "PAI-5"} }

func envMap(env []string) map[string]string {
	out := map[string]string{}
	for _, entry := range env {
		k, v, ok := strings.Cut(entry, "=")
		if ok {
			out[k] = v
		}
	}
	return out
}

func TestAgentRunnerSuccess(t *testing.T) {
	srv, patches := newRunServer(t, `{"issue_id":5,"device_id":"","status":"queued"}`, http.StatusOK)
	spawned := false
	a := &agentRunner{
		client: newClientForTest(srv.URL), deviceID: "dev-1", repoRoot: "/tmp/repo",
		autoConfirm: true,
		spawn: func(_ context.Context, root, _ string, env []string, _ io.Writer) error {
			spawned = true
			if root != "/tmp/repo" {
				t.Errorf("spawn root=%q, want /tmp/repo", root)
			}
			em := envMap(env)
			if em["PAIMOS_RUN_ID"] != "1" || em["PAIMOS_ISSUE_KEY"] != "PAI-5" || em["PAIMOS_ISSUE_TITLE"] != "Implement the demo change" {
				t.Errorf("spawn env=%v", env)
			}
			promptPath := em["PAIMOS_PROMPT_FILE"]
			if promptPath == "" {
				t.Fatal("spawn env missing PAIMOS_PROMPT_FILE")
			}
			prompt, err := os.ReadFile(promptPath)
			if err != nil {
				t.Fatalf("read prompt: %v", err)
			}
			for _, want := range []string{"PAIMOS local Implement-this worker", "Issue: PAI-5", "Change VERSION to 0.2.0.", "npm test passes."} {
				if !strings.Contains(string(prompt), want) {
					t.Fatalf("prompt %q missing %q", string(prompt), want)
				}
			}
			return nil
		},
	}
	if err := a.handleRun(context.Background(), aJob()); err != nil {
		t.Fatalf("handleRun: %v", err)
	}
	if !spawned {
		t.Error("spawn was not called")
	}
	// claim (running, stamping the actual device) then report (tests_passed).
	if len(*patches) != 2 ||
		(*patches)[0]["status"] != "running" ||
		(*patches)[0]["if_status"] != "queued" ||
		(*patches)[0]["device_id"] != "dev-1" ||
		(*patches)[0]["action_key"] != "claude_cli.implement" ||
		(*patches)[1]["status"] != "tests_passed" {
		t.Fatalf("patches=%+v, want claim(running,if_status=queued,device_id=dev-1,action_key=claude_cli.implement) then tests_passed", *patches)
	}
}

func TestResolveRunnerActionInfersCodexFromExec(t *testing.T) {
	key, label, err := resolveRunnerAction("", "codex exec --full-auto")
	if err != nil {
		t.Fatalf("resolveRunnerAction: %v", err)
	}
	if key != "codex_cli.implement" || label != "Codex CLI" {
		t.Fatalf("action=%s label=%s, want Codex CLI", key, label)
	}
	key, label, err = resolveRunnerAction("", "claude")
	if err != nil {
		t.Fatalf("resolveRunnerAction claude: %v", err)
	}
	if key != "claude_cli.implement" || label != "Claude Code" {
		t.Fatalf("action=%s label=%s, want Claude Code", key, label)
	}
}

func TestAgentRunnerSkipsMismatchedAction(t *testing.T) {
	srv, patches := newRunServer(t, `{"issue_id":5,"device_id":"","action_key":"codex_cli.implement","provider_label":"Codex CLI","status":"queued"}`, http.StatusOK)
	spawned := false
	a := &agentRunner{
		client: newClientForTest(srv.URL), deviceID: "dev-1", repoRoot: "/tmp/repo",
		execCmd: "claude", autoConfirm: true,
		spawn: func(_ context.Context, _, _ string, _ []string, _ io.Writer) error {
			spawned = true
			return nil
		},
	}
	if err := a.handleRun(context.Background(), aJob()); err != nil {
		t.Fatalf("handleRun: %v", err)
	}
	if spawned {
		t.Fatal("runner spawned for a mismatched action")
	}
	if len(*patches) != 0 {
		t.Fatalf("patches=%+v, want none for mismatched action", *patches)
	}
}

func TestAgentRunnerTestExecReportsVersionAndSummary(t *testing.T) {
	srv, patches := newRunServer(t, `{"issue_id":5,"device_id":"","status":"queued"}`, http.StatusOK)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "VERSION"), []byte("0.2.0\n"), 0o600); err != nil {
		t.Fatalf("seed VERSION: %v", err)
	}
	var calls []string
	a := &agentRunner{
		client: newClientForTest(srv.URL), deviceID: "dev-1", repoRoot: root,
		execCmd: "claude", testExec: "npm test", autoConfirm: true,
		spawn: func(_ context.Context, _, cmd string, _ []string, logSink io.Writer) error {
			calls = append(calls, cmd)
			if cmd == "npm test" {
				if logSink == nil {
					t.Fatal("test command should receive a summary sink")
				}
				_, _ = logSink.Write([]byte("PASS test.mjs\n2 passed\n"))
			} else if logSink != nil {
				t.Fatalf("agent command should not capture logs by default")
			}
			return nil
		},
	}
	if err := a.handleRun(context.Background(), aJob()); err != nil {
		t.Fatalf("handleRun: %v", err)
	}
	if strings.Join(calls, ",") != "claude,npm test" {
		t.Fatalf("spawn calls = %v, want claude then npm test", calls)
	}
	last := (*patches)[len(*patches)-1]
	if last["status"] != "tests_passed" || last["version"] != "0.2.0" {
		t.Fatalf("final patch = %+v, want tests_passed v0.2.0", last)
	}
	summary, _ := last["tests_summary"].(string)
	if !strings.Contains(summary, "npm test passed") || !strings.Contains(summary, "2 passed") {
		t.Fatalf("tests_summary=%q, want command and output evidence", summary)
	}
	if last["log_attachment_id"] != nil {
		t.Fatalf("test summary must not imply log attachment by default, got %+v", last)
	}
}

func TestAgentRunnerTestExecFailureReportsTestsFailed(t *testing.T) {
	srv, patches := newRunServer(t, `{"issue_id":5,"device_id":"","deploy_target":"ppm","status":"queued"}`, http.StatusOK)
	var calls []string
	a := &agentRunner{
		client: newClientForTest(srv.URL), deviceID: "dev-1", repoRoot: t.TempDir(),
		execCmd: "claude", testExec: "npm test", autoConfirm: true,
		allowDeploy: true, deployExec: "just deploy-ppm", autoConfirmDep: true,
		spawn: func(_ context.Context, _, cmd string, _ []string, logSink io.Writer) error {
			calls = append(calls, cmd)
			if cmd == "npm test" {
				_, _ = logSink.Write([]byte("FAIL test.mjs\nexpected true\n"))
				return errors.New("exit status 1")
			}
			return nil
		},
	}
	if err := a.handleRun(context.Background(), aJob()); err != nil {
		t.Fatalf("test failure is a reported result, not a runner error: %v", err)
	}
	if strings.Join(calls, ",") != "claude,npm test" {
		t.Fatalf("spawn calls = %v, want no deploy after failed tests", calls)
	}
	last := (*patches)[len(*patches)-1]
	if last["status"] != "tests_failed" {
		t.Fatalf("final patch = %+v, want tests_failed", last)
	}
	if !strings.Contains(fmt.Sprint(last["error"]), "tests: exit status 1") {
		t.Fatalf("error = %v, want test failure detail", last["error"])
	}
	summary, _ := last["tests_summary"].(string)
	if !strings.Contains(summary, "npm test failed") || !strings.Contains(summary, "expected true") {
		t.Fatalf("tests_summary=%q, want failed test evidence", summary)
	}
}

func TestAgentRunnerAttachesLog(t *testing.T) {
	// When the spawn produces output, the runner uploads it as an attachment and
	// stamps log_attachment_id on the terminal report (PAI-617).
	srv, patches := newRunServer(t, `{"issue_id":5,"device_id":"","status":"queued"}`, http.StatusOK)
	a := &agentRunner{
		client: newClientForTest(srv.URL), deviceID: "dev-1", repoRoot: "/tmp",
		autoConfirm: true, attachLogs: true,
		spawn: func(_ context.Context, _, _ string, _ []string, logSink io.Writer) error {
			if logSink != nil {
				_, _ = logSink.Write([]byte("build output\n"))
			}
			return nil
		},
	}
	if err := a.handleRun(context.Background(), aJob()); err != nil {
		t.Fatalf("handleRun: %v", err)
	}
	last := (*patches)[len(*patches)-1]
	if last["status"] != "tests_passed" {
		t.Fatalf("final patch = %+v, want tests_passed", last)
	}
	if last["log_attachment_id"] == nil {
		t.Fatalf("expected log_attachment_id to be set after upload, got %+v", last)
	}
}

// TestDefaultSpawnTeesOutput proves the REAL defaultSpawn runs via a shell
// (PAI-619) and tees combined output to the log sink (PAI-617) — the end-to-end
// capture the AttachesLog test's fake spawn bypasses (audit F6).
func TestDefaultSpawnTeesOutput(t *testing.T) {
	var sink bytes.Buffer
	if err := defaultSpawn(context.Background(), t.TempDir(), "echo hello-from-spawn", nil, &sink); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if !strings.Contains(sink.String(), "hello-from-spawn") {
		t.Fatalf("log sink = %q, want it to contain the command output", sink.String())
	}
}

func TestClaudeDefaultIsNonInteractivePromptMode(t *testing.T) {
	if got := effectiveAgentExec("claude"); got != "claude -p --permission-mode acceptEdits" {
		t.Fatalf("effectiveAgentExec(claude)=%q", got)
	}
	if !commandReadsPromptOnStdin(effectiveAgentExec("claude")) {
		t.Fatal("normalized claude command should read prompt from stdin")
	}
	promptFile := filepath.Join(t.TempDir(), "prompt.md")
	if err := os.WriteFile(promptFile, []byte("implement PAI-5"), 0o600); err != nil {
		t.Fatalf("seed prompt: %v", err)
	}
	prompt, err := promptForCommand(effectiveAgentExec("claude"), []string{"PAIMOS_PROMPT_FILE=" + promptFile})
	if err != nil {
		t.Fatalf("promptForCommand: %v", err)
	}
	if prompt != "implement PAI-5" {
		t.Fatalf("prompt=%q", prompt)
	}
	if prompt, err := promptForCommand("npm test", []string{"PAIMOS_PROMPT_FILE=" + promptFile}); err != nil || prompt != "" {
		t.Fatalf("non-agent command prompt=%q err=%v, want empty nil", prompt, err)
	}
}

// TestAgentRunnerDefaultDoesNotAttachLog: without --attach-logs the runner must
// not capture or upload the job output (audit MED-2 — logs can carry secrets).
func TestAgentRunnerDefaultDoesNotAttachLog(t *testing.T) {
	srv, patches := newRunServer(t, `{"issue_id":5,"device_id":"","status":"queued"}`, http.StatusOK)
	a := &agentRunner{
		client: newClientForTest(srv.URL), deviceID: "dev-1", repoRoot: "/tmp",
		autoConfirm: true, // attachLogs defaults to false
		spawn: func(_ context.Context, _, _ string, _ []string, logSink io.Writer) error {
			if logSink != nil {
				_, _ = logSink.Write([]byte("secret output"))
			}
			return nil
		},
	}
	if err := a.handleRun(context.Background(), aJob()); err != nil {
		t.Fatalf("handleRun: %v", err)
	}
	if last := (*patches)[len(*patches)-1]; last["log_attachment_id"] != nil {
		t.Fatalf("no log should be attached by default, got %+v", last)
	}
}

func TestAgentRunnerSpawnFailure(t *testing.T) {
	srv, patches := newRunServer(t, `{"issue_id":5,"device_id":"","status":"queued"}`, http.StatusOK)
	a := &agentRunner{
		client: newClientForTest(srv.URL), deviceID: "dev-1", repoRoot: "/tmp",
		autoConfirm: true,
		spawn:       func(_ context.Context, _, _ string, _ []string, _ io.Writer) error { return errors.New("exit 1") },
	}
	if err := a.handleRun(context.Background(), aJob()); err == nil {
		t.Fatal("expected an error when the spawned command fails")
	}
	if len(*patches) != 2 || (*patches)[0]["status"] != "running" || (*patches)[1]["status"] != "failed" {
		t.Fatalf("patches=%+v, want running then failed", *patches)
	}
}

func TestAgentRunnerClaimLost(t *testing.T) {
	// The claim PATCH returns 409 — another runner won. We must NOT spawn.
	srv, _ := newRunServer(t, `{"issue_id":5,"device_id":"","status":"queued"}`, http.StatusConflict)
	spawned := false
	a := &agentRunner{
		client: newClientForTest(srv.URL), deviceID: "dev-1", repoRoot: "/tmp",
		autoConfirm: true,
		spawn:       func(_ context.Context, _, _ string, _ []string, _ io.Writer) error { spawned = true; return nil },
	}
	if err := a.handleRun(context.Background(), aJob()); err != nil {
		t.Fatalf("a lost claim should not be a hard error: %v", err)
	}
	if spawned {
		t.Error("a run claimed by another runner must not spawn")
	}
}

func TestAgentRunnerSkipsNonQueued(t *testing.T) {
	// A run already past 'queued' (claimed/handled) is not ours.
	srv, patches := newRunServer(t, `{"issue_id":5,"device_id":"","status":"running"}`, http.StatusOK)
	spawned := false
	a := &agentRunner{
		client: newClientForTest(srv.URL), deviceID: "dev-1", repoRoot: "/tmp",
		autoConfirm: true,
		spawn:       func(_ context.Context, _, _ string, _ []string, _ io.Writer) error { spawned = true; return nil },
	}
	if err := a.handleRun(context.Background(), aJob()); err != nil {
		t.Fatalf("handleRun: %v", err)
	}
	if spawned || len(*patches) != 0 {
		t.Errorf("a non-queued run must be skipped (spawned=%v patches=%+v)", spawned, *patches)
	}
}

func TestAgentRunnerDeviceTargetingSkips(t *testing.T) {
	srv, patches := newRunServer(t, `{"issue_id":5,"device_id":"other-device","status":"queued"}`, http.StatusOK)
	spawned := false
	a := &agentRunner{
		client: newClientForTest(srv.URL), deviceID: "dev-1", repoRoot: "/tmp",
		autoConfirm: true,
		spawn:       func(_ context.Context, _, _ string, _ []string, _ io.Writer) error { spawned = true; return nil },
	}
	if err := a.handleRun(context.Background(), aJob()); err != nil {
		t.Fatalf("handleRun: %v", err)
	}
	if spawned {
		t.Error("a run targeted at another device must not spawn")
	}
	if len(*patches) != 0 {
		t.Errorf("no patches expected for a skipped run, got %+v", *patches)
	}
}

func TestAgentRunnerDeclineCancels(t *testing.T) {
	srv, patches := newRunServer(t, `{"issue_id":5,"device_id":"","status":"queued"}`, http.StatusOK)
	spawned := false
	a := &agentRunner{
		client: newClientForTest(srv.URL), deviceID: "dev-1", repoRoot: "/tmp",
		autoConfirm: false,
		confirm:     func(_ string, _ int64, _ string) bool { return false },
		spawn:       func(_ context.Context, _, _ string, _ []string, _ io.Writer) error { spawned = true; return nil },
	}
	if err := a.handleRun(context.Background(), aJob()); err != nil {
		t.Fatalf("handleRun: %v", err)
	}
	if spawned {
		t.Error("a declined run must not spawn")
	}
	if len(*patches) != 1 || (*patches)[0]["status"] != "cancelled" {
		t.Fatalf("patches=%+v, want a single cancelled", *patches)
	}
}

func TestAgentRunnerDeployGated(t *testing.T) {
	srv, patches := newRunServer(t, `{"issue_id":5,"device_id":"","deploy_target":"ppm","status":"queued"}`, http.StatusOK)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "VERSION"), []byte("4.6.1\n"), 0o600); err != nil {
		t.Fatalf("seed VERSION: %v", err)
	}
	var calls []string
	a := &agentRunner{
		client: newClientForTest(srv.URL), deviceID: "dev-1", repoRoot: root,
		execCmd: "claude", autoConfirm: true,
		allowDeploy: true, deployExec: "just deploy-ppm", autoConfirmDep: true,
		spawn: func(_ context.Context, _, cmd string, _ []string, _ io.Writer) error {
			calls = append(calls, cmd)
			return nil
		},
	}
	if err := a.handleRun(context.Background(), aJob()); err != nil {
		t.Fatalf("handleRun: %v", err)
	}
	if len(calls) != 2 || calls[0] != "claude" || calls[1] != "just deploy-ppm" {
		t.Fatalf("spawn calls = %v, want [claude, just deploy-ppm]", calls)
	}
	last := (*patches)[len(*patches)-1]
	if last["status"] != "deployed" || last["version"] != "4.6.1" || last["deploy_target"] != "ppm" {
		t.Fatalf("final patch = %+v, want deployed v4.6.1 ppm", last)
	}
}

func TestAgentRunnerDeployCarriesTestSummary(t *testing.T) {
	srv, patches := newRunServer(t, `{"issue_id":5,"device_id":"","deploy_target":"local-dev","status":"queued"}`, http.StatusOK)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "VERSION"), []byte("1.2.3\n"), 0o600); err != nil {
		t.Fatalf("seed VERSION: %v", err)
	}
	var calls []string
	a := &agentRunner{
		client: newClientForTest(srv.URL), deviceID: "dev-1", repoRoot: root,
		execCmd: "claude", testExec: "npm test", autoConfirm: true,
		allowDeploy: true, deployExec: "npm run deploy:local", autoConfirmDep: true,
		spawn: func(_ context.Context, _, cmd string, _ []string, logSink io.Writer) error {
			calls = append(calls, cmd)
			if cmd == "npm test" {
				_, _ = logSink.Write([]byte("all demo tests passed\n"))
			}
			return nil
		},
	}
	if err := a.handleRun(context.Background(), aJob()); err != nil {
		t.Fatalf("handleRun: %v", err)
	}
	if strings.Join(calls, ",") != "claude,npm test,npm run deploy:local" {
		t.Fatalf("spawn calls = %v, want agent, tests, deploy", calls)
	}
	last := (*patches)[len(*patches)-1]
	if last["status"] != "deployed" || last["version"] != "1.2.3" || last["deploy_target"] != "local-dev" {
		t.Fatalf("final patch = %+v, want deployed v1.2.3 local-dev", last)
	}
	if !strings.Contains(fmt.Sprint(last["tests_summary"]), "all demo tests passed") {
		t.Fatalf("tests_summary=%v, want deploy report to carry test evidence", last["tests_summary"])
	}
}

func TestAgentRunnerDeployNeedsItsOwnConsent(t *testing.T) {
	// --allow-deploy + --deploy-exec + deploy_target, but the deploy confirm is
	// declined (and --yes-deploy not set) → no deploy, report tests_passed.
	srv, patches := newRunServer(t, `{"issue_id":5,"device_id":"","deploy_target":"ppm","status":"queued"}`, http.StatusOK)
	var calls []string
	a := &agentRunner{
		client: newClientForTest(srv.URL), deviceID: "dev-1", repoRoot: t.TempDir(),
		execCmd: "claude", autoConfirm: true,
		allowDeploy: true, deployExec: "just deploy-ppm", autoConfirmDep: false,
		confirmDeploy: func(_ string, _ int64, _ string) bool { return false },
		spawn: func(_ context.Context, _, cmd string, _ []string, _ io.Writer) error {
			calls = append(calls, cmd)
			return nil
		},
	}
	if err := a.handleRun(context.Background(), aJob()); err != nil {
		t.Fatalf("handleRun: %v", err)
	}
	if len(calls) != 1 || calls[0] != "claude" {
		t.Fatalf("spawn calls = %v, want just [claude] (deploy declined)", calls)
	}
	if last := (*patches)[len(*patches)-1]; last["status"] != "tests_passed" {
		t.Fatalf("final patch = %+v, want tests_passed (deploy declined)", last)
	}
}

func TestAgentRunnerDeployStaysGatedOff(t *testing.T) {
	srv, patches := newRunServer(t, `{"issue_id":5,"device_id":"","deploy_target":"ppm","status":"queued"}`, http.StatusOK)
	var calls []string
	a := &agentRunner{
		client: newClientForTest(srv.URL), deviceID: "dev-1", repoRoot: "/tmp",
		execCmd: "claude", autoConfirm: true,
		allowDeploy: false, deployExec: "just deploy-ppm",
		spawn: func(_ context.Context, _, cmd string, _ []string, _ io.Writer) error {
			calls = append(calls, cmd)
			return nil
		},
	}
	if err := a.handleRun(context.Background(), aJob()); err != nil {
		t.Fatalf("handleRun: %v", err)
	}
	if len(calls) != 1 || calls[0] != "claude" {
		t.Fatalf("spawn calls = %v, want just [claude] (deploy gated off)", calls)
	}
	if last := (*patches)[len(*patches)-1]; last["status"] != "tests_passed" {
		t.Fatalf("final patch = %+v, want tests_passed (no deploy)", last)
	}
}

func TestAgentRunnerQueuedRunIDsCatchUp(t *testing.T) {
	var seenPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.String()
		if r.URL.Path != "/api/projects/7/runs" || r.URL.Query().Get("status") != "queued" {
			http.Error(w, `{"error":"unexpected route"}`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"runs":[{"id":11},{"id":12}]}`))
	}))
	t.Cleanup(srv.Close)

	a := &agentRunner{client: newClientForTest(srv.URL)}
	got := a.queuedRunIDs(context.Background(), 7)
	if len(got) != 2 || got[0] != 11 || got[1] != 12 {
		t.Fatalf("queuedRunIDs=%v, want [11 12]", got)
	}
	if seenPath != "/api/projects/7/runs?status=queued" {
		t.Fatalf("catch-up path=%q", seenPath)
	}
}

func TestAgentRunnerQueuedRunIDsDedupesPollErrors(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls += 1
		if calls < 3 {
			http.Error(w, `{"error":"temporary"}`, http.StatusInternalServerError)
			return
		}
		http.Error(w, `{"error":"still temporary"}`, http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	oldStderr := stderr
	var errOut bytes.Buffer
	stderr = &errOut
	t.Cleanup(func() { stderr = oldStderr })

	a := &agentRunner{client: newClientForTest(srv.URL)}
	_ = a.queuedRunIDs(context.Background(), 7)
	_ = a.queuedRunIDs(context.Background(), 7)
	if got := strings.Count(errOut.String(), "catch-up poll failed"); got != 1 {
		t.Fatalf("same catch-up error logged %d times, want 1; stderr=%q", got, errOut.String())
	}
	_ = a.queuedRunIDs(context.Background(), 7)
	if got := strings.Count(errOut.String(), "catch-up poll failed"); got != 2 {
		t.Fatalf("distinct catch-up error logged %d times, want 2; stderr=%q", got, errOut.String())
	}
}
