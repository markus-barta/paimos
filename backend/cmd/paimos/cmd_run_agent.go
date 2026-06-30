// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

// PAI-608 (epic PAI-605): the local runner for the "Implement this" feature.
// It subscribes to a project's SSE stream advertising implement-capability;
// when the web UI fires "Implement this" on a ticket, this process — on the
// developer's own workstation — spawns Claude Code in the repo and reports the
// run's progress back to PAIMOS.
//
// Robustness (PAI-605 hardening): the SSE connection RECONNECTS with backoff on
// any drop, a single worker processes one job at a time, and a periodic
// catch-up poll drains queued runs the runner missed while offline/busy or that
// a server restart orphaned. Each job ATOMICALLY claims its run (queued ->
// running via an if_status guard), so two runners can never both execute the
// same open run.
//
// Safe by default: only ever runs in --repo-root, prompts before spawning
// (unless --yes), and NEVER deploys — deploy is triple-gated (--allow-deploy +
// --deploy-exec + a run deploy_target) and additionally needs its own consent
// (--yes-deploy or an interactive prompt).
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/markus-barta/paimos/backend/cmd/paimos/sync"
	"github.com/spf13/cobra"
)

func runAgentCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "run-agent",
		Short: "Local runner for UI-triggered 'Implement this' jobs (PAI-608)",
	}
	c.AddCommand(runAgentWatchCmd())
	return c
}

func runAgentWatchCmd() *cobra.Command {
	var (
		projectRef  string
		repoRoot    string
		execCmd     string
		testExec    string
		yes         bool
		allowDeploy bool
		deployExec  string
		yesDeploy   bool
		attachLogs  bool
	)
	c := &cobra.Command{
		Use:   "watch",
		Short: "Watch for 'Implement this' jobs and run Claude Code on them",
		Long: `Subscribe to a project's event stream advertising implement-capability.
When the web UI fires "Implement this" on a ticket, this runner — on YOUR
workstation — spawns the configured command (Claude Code by default) in the
repo and reports progress back to PAIMOS.

Robust: it reconnects with backoff if the stream drops, processes one job at a
time, and periodically catches up on queued runs it missed (offline/busy/server
restart). Each run is claimed atomically, so two runners never double-execute.

Safe by default: only ever runs in --repo-root, prompts for confirmation before
spawning (unless --yes), and does NOT deploy — deploy is triple-gated (it needs
--allow-deploy AND --deploy-exec AND a run deploy_target) and additionally
prompts for a separate deploy confirmation unless --yes-deploy is set.

The spawned command sees PAIMOS_RUN_ID and PAIMOS_ISSUE_KEY in its environment
plus PAIMOS_PROMPT_FILE with the generated issue-context prompt. The default
"claude" command is run as Claude Code print mode with that prompt, so it cannot
silently open an interactive TUI for a queued run. Custom commands can read the
prompt file themselves and may report richer progress via the paimos CLI; the
runner won't clobber a terminal status it set.

Use --test-exec to run a known test command after the agent command. Its bounded
output is reported as tests_summary; a failure reports tests_failed and skips
deploy.

Examples:
  paimos run-agent watch --project PAI --repo-root .
  paimos run-agent watch --project PAI --yes --test-exec "npm test"`,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runAgentWatch(runAgentOpts{
				projectRef: projectRef, repoRoot: repoRoot, execCmd: execCmd, testExec: testExec,
				yes: yes, allowDeploy: allowDeploy, deployExec: deployExec, yesDeploy: yesDeploy,
				attachLogs: attachLogs,
			})
		},
	}
	c.Flags().StringVarP(&projectRef, "project", "p", "", "project key or id (required)")
	c.Flags().StringVar(&repoRoot, "repo-root", "", "repo the runner operates in (default: cwd)")
	c.Flags().StringVar(&execCmd, "exec", "claude", "command to spawn for a job (run in --repo-root)")
	c.Flags().StringVar(&testExec, "test-exec", "", `optional test command to run after --exec; summary is reported as tests_summary`)
	c.Flags().BoolVar(&yes, "yes", false, "skip the per-job confirmation prompt (non-interactive)")
	c.Flags().BoolVar(&allowDeploy, "allow-deploy", false, "allow the runner to deploy after a successful run (off by default)")
	c.Flags().StringVar(&deployExec, "deploy-exec", "", `deploy command when --allow-deploy and the run has a deploy_target (e.g. "just deploy-ppm")`)
	c.Flags().BoolVar(&yesDeploy, "yes-deploy", false, "skip the separate deploy confirmation (still requires --allow-deploy + --deploy-exec)")
	c.Flags().BoolVar(&attachLogs, "attach-logs", false, "capture the job's output and attach it to the ticket (off by default — logs may contain secrets)")
	return c
}

type runAgentOpts struct {
	projectRef  string
	repoRoot    string
	execCmd     string
	testExec    string
	yes         bool
	allowDeploy bool
	deployExec  string
	yesDeploy   bool
	attachLogs  bool
}

// runJob is one unit of work for the worker. issueKey is best-effort (carried
// from the SSE event when available; empty for catch-up jobs).
type runJob struct {
	runID    int64
	issueKey string
}

func runAgentWatch(o runAgentOpts) error {
	if strings.TrimSpace(o.projectRef) == "" {
		return &usageError{msg: "--project is required"}
	}
	client, err := instanceClient()
	if err != nil {
		return err
	}
	projectID, err := resolveProjectID(client, o.projectRef)
	if err != nil {
		return err
	}
	projectKey, err := resolveProjectKey(client, projectID)
	if err != nil {
		return err
	}
	root, err := resolveWorkspaceRoot(o.repoRoot)
	if err != nil {
		return err
	}
	deviceID, err := resolveDeviceID()
	if err != nil {
		return err
	}

	syncer := &httpSyncClient{client: client}
	ctx, cancel := signalContext()
	defer cancel()

	runner := newAgentRunner(client, deviceID, root, o.execCmd, o.testExec, o.yes, o.allowDeploy, o.deployExec, o.yesDeploy, o.attachLogs)
	deployNote := "report-back only, no auto-deploy"
	if runner.allowDeploy && runner.deployExec != "" {
		deployNote = "deploy ENABLED via " + runner.deployExec + " (runs with a deploy_target only)"
	}
	fmt.Fprintf(stdout, "run-agent watching %s (device=%s, repo=%s) — %s\n",
		projectKey, deviceID, root, deployNote)

	// One worker → one job at a time. Each handleRun atomically claims its run,
	// so a job enqueued twice (SSE + catch-up) is harmless (the second claim 409s).
	jobs := make(chan runJob, 64)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case j := <-jobs:
				if err := runner.handleRun(ctx, j); err != nil {
					fmt.Fprintf(stderr, "run %d: %v\n", j.runID, err)
				}
			}
		}
	}()

	// Periodic catch-up: enqueue still-queued runs (covers runner-offline-at-
	// publish, busy-drop, and server-restart orphans). Claimed/terminal runs are
	// no longer 'queued', so re-enqueuing is cheap and self-limiting.
	go func() {
		enqueue := func() {
			for _, id := range runner.queuedRunIDs(ctx, projectID) {
				select {
				case jobs <- runJob{runID: id}:
				default:
				}
			}
		}
		enqueue()
		t := time.NewTicker(20 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				enqueue()
			}
		}
	}()

	// SSE with reconnect + capped backoff. Exits only on signal (ctx cancel).
	ssePath := sync.EventEndpoint(projectID, deviceID, "", true)
	backoff := time.Second
	const maxBackoff = 30 * time.Second
	for {
		if ctx.Err() != nil {
			return nil
		}
		connectedAt := time.Now()
		streamErr := syncer.Stream(ctx, ssePath, func(ev sync.Event) {
			if ev.Type != "implement_requested" {
				return
			}
			runID, perr := strconv.ParseInt(strings.TrimSpace(ev.Rev), 10, 64)
			if perr != nil {
				return
			}
			select {
			case jobs <- runJob{runID: runID, issueKey: ev.Name}:
			default:
				fmt.Fprintf(stderr, "job queue full; run %d will be caught up later\n", runID)
			}
		})
		if ctx.Err() != nil {
			return nil
		}
		// A connection that stayed up past the cap was healthy — reset the backoff
		// so a later blip reconnects promptly instead of waiting the ratcheted cap.
		if time.Since(connectedAt) > maxBackoff {
			backoff = time.Second
		}
		note := "server closed the stream"
		if streamErr != nil {
			note = streamErr.Error()
		}
		fmt.Fprintf(stderr, "run-agent: event stream ended (%s) — reconnecting in %s\n", note, backoff)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		if backoff < maxBackoff {
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

// agentRunner executes implement jobs. spawn/confirm/confirmDeploy are fields so
// tests can inject fakes without touching the SSE/exec machinery.
type agentRunner struct {
	client         *Client
	deviceID       string
	repoRoot       string
	execCmd        string
	testExec       string
	autoConfirm    bool
	allowDeploy    bool
	deployExec     string
	autoConfirmDep bool
	attachLogs     bool
	lastQueuedErr  string // dedupes catch-up error logging (single goroutine)
	spawn          func(ctx context.Context, repoRoot, execCmd string, env []string, logSink io.Writer) error
	confirm        func(issueKey string, runID int64, repoRoot string) bool
	confirmDeploy  func(issueKey string, runID int64, target string) bool
}

func newAgentRunner(client *Client, deviceID, repoRoot, execCmd, testExec string, autoConfirm, allowDeploy bool, deployExec string, autoConfirmDeploy, attachLogs bool) *agentRunner {
	return &agentRunner{
		client:         client,
		deviceID:       deviceID,
		repoRoot:       repoRoot,
		execCmd:        execCmd,
		testExec:       testExec,
		autoConfirm:    autoConfirm,
		allowDeploy:    allowDeploy,
		deployExec:     deployExec,
		autoConfirmDep: autoConfirmDeploy,
		attachLogs:     attachLogs,
		spawn:          defaultSpawn,
		confirm:        defaultConfirm,
		confirmDeploy:  defaultDeployConfirm,
	}
}

type agentRunDetail struct {
	IssueID      int64  `json:"issue_id"`
	DeviceID     string `json:"device_id"`
	DeployTarget string `json:"deploy_target"`
	Status       string `json:"status"`
}

type agentIssueContext struct {
	ID                 int64  `json:"id"`
	IssueKey           string `json:"issue_key"`
	Type               string `json:"type"`
	Title              string `json:"title"`
	Description        string `json:"description"`
	AcceptanceCriteria string `json:"acceptance_criteria"`
	Notes              string `json:"notes"`
	Status             string `json:"status"`
	Priority           string `json:"priority"`
}

func (a *agentRunner) handleRun(ctx context.Context, j runJob) error {
	runID := j.runID
	detail, err := a.fetchRun(runID)
	if err != nil {
		return err
	}
	// Only fresh, unclaimed runs are ours to take. (A catch-up enqueue can race
	// an SSE one; whichever loses sees a non-queued status and bows out.)
	if detail.Status != "queued" {
		return nil
	}
	// Device targeting: a run aimed at a specific device is only ours if it
	// names us. An empty device_id is open to any runner.
	if detail.DeviceID != "" && detail.DeviceID != a.deviceID {
		fmt.Fprintf(stdout, "run %d targets device %q (not %q) — skipping\n", runID, detail.DeviceID, a.deviceID)
		return nil
	}
	issueKey := j.issueKey
	if issueKey == "" {
		issueKey = fmt.Sprintf("issue#%d", detail.IssueID)
	}
	issueRef := issueKey
	if strings.HasPrefix(issueRef, "issue#") {
		issueRef = strconv.FormatInt(detail.IssueID, 10)
	}
	issueCtx, err := a.fetchIssueContext(issueRef)
	if err != nil {
		return fmt.Errorf("fetch issue context: %w", err)
	}
	if strings.TrimSpace(issueCtx.IssueKey) != "" {
		issueKey = strings.TrimSpace(issueCtx.IssueKey)
	}
	if strings.TrimSpace(issueCtx.IssueKey) == "" {
		issueCtx.IssueKey = issueKey
	}
	if !a.autoConfirm && !a.confirm(issueKey, runID, a.repoRoot) {
		fmt.Fprintf(stdout, "run %d declined\n", runID)
		a.report(runID, map[string]any{"status": "cancelled", "if_status": "queued"})
		return nil
	}
	// Atomic claim: queued -> running. A second runner that re-reads the run
	// loses here (the if_status guard) and skips — no double-spawn.
	if err := a.patch(runID, map[string]any{"status": "running", "if_status": "queued", "device_id": a.deviceID}); err != nil {
		if isConflict(err) {
			fmt.Fprintf(stdout, "run %d already claimed by another runner — skipping\n", runID)
			return nil
		}
		return fmt.Errorf("claim run %d: %w", runID, err)
	}
	fmt.Fprintf(stdout, "%s implementing %s (run %d) in %s\n",
		time.Now().Format(time.RFC3339), issueKey, runID, a.repoRoot)

	env := []string{
		"PAIMOS_RUN_ID=" + strconv.FormatInt(runID, 10),
		"PAIMOS_ISSUE_KEY=" + issueKey,
		"PAIMOS_ISSUE_TITLE=" + issueCtx.Title,
	}
	var logFile *os.File
	var logSink io.Writer
	promptPath, cleanupPrompt, promptErr := writeAgentPromptFile(runID, buildAgentPrompt(issueCtx, runID, a.repoRoot, a.testExec, detail.DeployTarget))
	if promptErr != nil {
		closeLog(logFile)
		a.finishRun(runID, detail.IssueID, logFile, map[string]any{"status": "failed", "error": "prompt: " + promptErr.Error()})
		return fmt.Errorf("run %d prompt failed: %w", runID, promptErr)
	}
	defer cleanupPrompt()
	env = append(env, "PAIMOS_PROMPT_FILE="+promptPath)
	// With --attach-logs, capture the job's combined output to a temp log and
	// attach it to the ticket (PAI-617). OFF by default (audit): agent output can
	// contain secrets, and an attachment is visible to every project member.
	// Best-effort: a capture/upload failure never fails the run.
	if a.attachLogs {
		if f, logErr := os.CreateTemp("", fmt.Sprintf("paimos-run-%d-*.log", runID)); logErr == nil {
			logFile = f
			logSink = f
			defer func() { _ = os.Remove(f.Name()) }()
		}
	}

	if spawnErr := a.spawn(ctx, a.repoRoot, a.execCmd, env, logSink); spawnErr != nil {
		closeLog(logFile)
		a.finishRun(runID, detail.IssueID, logFile, map[string]any{"status": "failed", "error": spawnErr.Error()})
		return fmt.Errorf("run %d failed: %w", runID, spawnErr)
	}

	resultFields := map[string]any{
		"version": readVersionFile(a.repoRoot),
	}
	if strings.TrimSpace(a.testExec) != "" {
		summary, testErr := a.runTests(ctx, env, logSink)
		if summary != "" {
			resultFields["tests_summary"] = summary
		}
		if testErr != nil {
			closeLog(logFile)
			resultFields["status"] = "tests_failed"
			resultFields["error"] = "tests: " + testErr.Error()
			a.finishRun(runID, detail.IssueID, logFile, resultFields)
			fmt.Fprintf(stdout, "run %d tests failed\n", runID)
			return nil
		}
	}

	// Deploy is triple-gated AND needs its own consent (PAI-605 M6): even under
	// --yes, the deploy step prompts unless --yes-deploy was passed.
	if a.allowDeploy && a.deployExec != "" && detail.DeployTarget != "" {
		if !a.autoConfirmDep && !a.confirmDeploy(issueKey, runID, detail.DeployTarget) {
			fmt.Fprintf(stdout, "run %d: deploy declined — reporting tests_passed\n", runID)
			closeLog(logFile)
			resultFields["status"] = "tests_passed"
			a.finishRun(runID, detail.IssueID, logFile, resultFields)
			return nil
		}
		fmt.Fprintf(stdout, "run %d: deploying to %s via %q\n", runID, detail.DeployTarget, a.deployExec)
		if depErr := a.spawn(ctx, a.repoRoot, a.deployExec, env, logSink); depErr != nil {
			closeLog(logFile)
			a.finishRun(runID, detail.IssueID, logFile, map[string]any{"status": "failed", "error": "deploy: " + depErr.Error()})
			return fmt.Errorf("run %d deploy failed: %w", runID, depErr)
		}
		closeLog(logFile)
		resultFields["status"] = "deployed"
		resultFields["version"] = readVersionFile(a.repoRoot)
		resultFields["deploy_target"] = detail.DeployTarget
		a.finishRun(runID, detail.IssueID, logFile, resultFields)
		fmt.Fprintf(stdout, "run %d deployed to %s\n", runID, detail.DeployTarget)
		return nil
	}

	// Report-back only. The server rejects clobbering a terminal status (so if
	// the agent already advanced the run, this is a harmless 409, not a clobber).
	closeLog(logFile)
	resultFields["status"] = "tests_passed"
	a.finishRun(runID, detail.IssueID, logFile, resultFields)
	fmt.Fprintf(stdout, "run %d complete\n", runID)
	return nil
}

func closeLog(f *os.File) {
	if f != nil {
		_ = f.Close()
	}
}

func (a *agentRunner) runTests(ctx context.Context, env []string, logSink io.Writer) (string, error) {
	var out bytes.Buffer
	sink := io.Writer(&out)
	if logSink != nil {
		sink = io.MultiWriter(logSink, &out)
	}
	err := a.spawn(ctx, a.repoRoot, a.testExec, env, sink)
	return commandResultSummary(a.testExec, out.String(), err), err
}

func commandResultSummary(cmd, output string, err error) string {
	name := truncateRunes(strings.TrimSpace(cmd), 80)
	if name == "" {
		name = "tests"
	}
	state := "passed"
	if err != nil {
		state = "failed"
	}
	detail := compactCommandOutput(output)
	if detail == "" && err != nil {
		detail = err.Error()
	}
	summary := name + " " + state
	if detail != "" {
		summary += ": " + detail
	}
	return truncateRunes(summary, 500)
}

func compactCommandOutput(output string) string {
	output = strings.ReplaceAll(output, "\r", "\n")
	lines := strings.Split(output, "\n")
	compact := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.Join(strings.Fields(stripANSI(line)), " ")
		if line != "" {
			compact = append(compact, line)
		}
	}
	const keepLines = 4
	if len(compact) > keepLines {
		compact = compact[len(compact)-keepLines:]
	}
	return strings.Join(compact, " | ")
}

func stripANSI(s string) string {
	var b strings.Builder
	skipping := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == 0x1b {
			skipping = true
			continue
		}
		if skipping {
			if c >= '@' && c <= '~' {
				skipping = false
			}
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 3 {
		return string(r[:max])
	}
	return string(r[:max-3]) + "..."
}

func writeAgentPromptFile(runID int64, prompt string) (string, func(), error) {
	f, err := os.CreateTemp("", fmt.Sprintf("paimos-run-%d-prompt-*.md", runID))
	if err != nil {
		return "", func() {}, err
	}
	path := f.Name()
	cleanup := func() { _ = os.Remove(path) }
	if _, err := f.WriteString(prompt); err != nil {
		_ = f.Close()
		cleanup()
		return "", func() {}, err
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return path, cleanup, nil
}

func buildAgentPrompt(issue *agentIssueContext, runID int64, repoRoot, testExec, deployTarget string) string {
	var b strings.Builder
	key := strings.TrimSpace(issue.IssueKey)
	if key == "" {
		key = fmt.Sprintf("issue#%d", issue.ID)
	}
	fmt.Fprintf(&b, "You are running as a PAIMOS local Implement-this worker.\n\n")
	fmt.Fprintf(&b, "Repository root: %s\n", repoRoot)
	fmt.Fprintf(&b, "Run id: %d\n", runID)
	fmt.Fprintf(&b, "Issue: %s\n\n", key)
	fmt.Fprintf(&b, "Task:\n")
	appendPromptField(&b, "Title", issue.Title)
	appendPromptField(&b, "Type", issue.Type)
	appendPromptField(&b, "Status", issue.Status)
	appendPromptField(&b, "Priority", issue.Priority)
	appendPromptSection(&b, "Description", issue.Description)
	appendPromptSection(&b, "Acceptance Criteria", issue.AcceptanceCriteria)
	appendPromptSection(&b, "Notes", issue.Notes)
	fmt.Fprintf(&b, "\nRules:\n")
	fmt.Fprintf(&b, "- Implement only this PAIMOS issue in the repository root above.\n")
	fmt.Fprintf(&b, "- Keep the change minimal and scoped; do not modify unrelated files.\n")
	fmt.Fprintf(&b, "- Do not commit, push, tag, or deploy.\n")
	if strings.TrimSpace(testExec) != "" {
		fmt.Fprintf(&b, "- The runner will execute `%s` after you stop; keep it passing.\n", strings.TrimSpace(testExec))
	} else {
		fmt.Fprintf(&b, "- Run a reasonable quick check if the issue asks for one, and mention the result.\n")
	}
	if strings.TrimSpace(deployTarget) != "" {
		fmt.Fprintf(&b, "- The runner may deploy to `%s` after successful tests; do not deploy yourself.\n", strings.TrimSpace(deployTarget))
	}
	fmt.Fprintf(&b, "- If the issue is impossible or unsafe, make no destructive changes and explain the blocker.\n")
	return b.String()
}

func appendPromptField(b *strings.Builder, label, value string) {
	value = strings.TrimSpace(value)
	if value != "" {
		fmt.Fprintf(b, "- %s: %s\n", label, value)
	}
}

func appendPromptSection(b *strings.Builder, label, value string) {
	value = strings.TrimSpace(value)
	if value != "" {
		fmt.Fprintf(b, "\n%s:\n%s\n", label, value)
	}
}

// finishRun uploads the captured log (best-effort), stamps log_attachment_id
// when the upload succeeds, then reports the terminal state (PAI-617).
func (a *agentRunner) finishRun(runID, issueID int64, logFile *os.File, fields map[string]any) {
	if logFile != nil {
		if id := a.uploadLog(issueID, runID, logFile.Name()); id > 0 {
			fields["log_attachment_id"] = id
		}
	}
	a.report(runID, fields)
}

// uploadLog uploads the run log as an attachment and links it to the issue,
// returning the new attachment id (0 on any failure or an empty log — the whole
// path is best-effort and must never fail the run).
func (a *agentRunner) uploadLog(issueID, runID int64, path string) int64 {
	if info, err := os.Stat(path); err != nil || info.Size() == 0 {
		return 0 // nothing captured
	}
	raw, err := a.client.doMultipartFile("/api/attachments", "file", path)
	if err != nil {
		fmt.Fprintf(stderr, "run %d: log upload failed: %v\n", runID, err)
		return 0
	}
	var up struct {
		ID int64 `json:"id"`
	}
	if json.Unmarshal(raw, &up) != nil || up.ID <= 0 {
		return 0
	}
	if _, err := a.client.do("PATCH", "/api/attachments/link", map[string]any{
		"issue_id":       issueID,
		"attachment_ids": []int64{up.ID},
	}); err != nil {
		fmt.Fprintf(stderr, "run %d: log link failed: %v\n", runID, err)
		return 0
	}
	return up.ID
}

// queuedRunIDs returns the ids of runs still queued for the project — the
// catch-up source.
func (a *agentRunner) queuedRunIDs(ctx context.Context, projectID int64) []int64 {
	if ctx.Err() != nil {
		return nil
	}
	body, err := a.client.do("GET", fmt.Sprintf("/api/projects/%d/runs?status=queued", projectID), nil)
	if err != nil {
		// Surface a persistently failing catch-up, but only once per distinct
		// error so a broken endpoint doesn't spam stderr every 20s.
		if msg := err.Error(); msg != a.lastQueuedErr {
			a.lastQueuedErr = msg
			fmt.Fprintf(stderr, "run-agent: catch-up poll failed: %v\n", err)
		}
		return nil
	}
	a.lastQueuedErr = ""
	var resp struct {
		Runs []struct {
			ID int64 `json:"id"`
		} `json:"runs"`
	}
	if json.Unmarshal(body, &resp) != nil {
		return nil
	}
	ids := make([]int64, 0, len(resp.Runs))
	for _, r := range resp.Runs {
		ids = append(ids, r.ID)
	}
	return ids
}

func (a *agentRunner) fetchRun(runID int64) (*agentRunDetail, error) {
	body, err := a.client.do("GET", fmt.Sprintf("/api/runs/%d", runID), nil)
	if err != nil {
		return nil, err
	}
	var d agentRunDetail
	if err := json.Unmarshal(body, &d); err != nil {
		return nil, fmt.Errorf("decode run: %w", err)
	}
	return &d, nil
}

func (a *agentRunner) fetchIssueContext(ref string) (*agentIssueContext, error) {
	body, err := a.client.do("GET", "/api/issues/"+url.PathEscape(ref), nil)
	if err != nil {
		return nil, err
	}
	var iss agentIssueContext
	if err := json.Unmarshal(body, &iss); err != nil {
		return nil, fmt.Errorf("decode issue context: %w", err)
	}
	return &iss, nil
}

func (a *agentRunner) patch(runID int64, fields map[string]any) error {
	_, err := a.client.do("PATCH", fmt.Sprintf("/api/runs/%d", runID), fields)
	return err
}

// report PATCHes the run's final state and LOGS (rather than swallows) a
// failure, so a network blip on the report doesn't silently lose the outcome. A
// 409 just means the run already reached a terminal status — not worth shouting.
func (a *agentRunner) report(runID int64, fields map[string]any) {
	if err := a.patch(runID, fields); err != nil && !isConflict(err) {
		fmt.Fprintf(stderr, "run %d: reporting %v failed: %v\n", runID, fields["status"], err)
	}
}

// isConflict reports whether err is an HTTP 409 from the API (a lost claim or a
// rejected terminal-status transition).
func isConflict(err error) bool {
	var he *httpError
	return errors.As(err, &he) && he.Code == http.StatusConflict
}

// readVersionFile returns the trimmed contents of <repoRoot>/VERSION, or "" if
// it can't be read — best-effort version capture for a deploy report (PAI-613).
func readVersionFile(repoRoot string) string {
	// #nosec G304 -- repoRoot is the operator's own --repo-root flag and the
	// filename is the fixed literal "VERSION"; neither is network/user input.
	b, err := os.ReadFile(filepath.Join(repoRoot, "VERSION"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func defaultSpawn(ctx context.Context, repoRoot, execCmd string, env []string, logSink io.Writer) error {
	if strings.TrimSpace(execCmd) == "" {
		return fmt.Errorf("empty --exec command")
	}
	effectiveCmd := effectiveAgentExec(execCmd)
	// Run through a shell so the operator's --exec can use quotes, pipes, and
	// chaining (PAI-619). execCmd is the operator's own --exec flag, run in their
	// own repo — that is the entire purpose of the runner.
	cmd := exec.CommandContext(ctx, "sh", "-c", effectiveCmd) // #nosec G204 -- operator's own --exec flag
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), env...)
	// Tee output to the run log (PAI-617) when a sink is provided.
	out, errOut := io.Writer(stdout), io.Writer(stderr)
	if logSink != nil {
		out = io.MultiWriter(stdout, logSink)
		errOut = io.MultiWriter(stderr, logSink)
	}
	cmd.Stdout = out
	cmd.Stderr = errOut
	if prompt, err := promptForCommand(effectiveCmd, env); err != nil {
		return err
	} else if prompt != "" {
		cmd.Stdin = strings.NewReader(prompt)
	} else {
		cmd.Stdin = os.Stdin
	}
	return cmd.Run()
}

func effectiveAgentExec(execCmd string) string {
	trimmed := strings.TrimSpace(execCmd)
	if trimmed == "claude" {
		return "claude -p --permission-mode acceptEdits"
	}
	return execCmd
}

func promptForCommand(execCmd string, env []string) (string, error) {
	if !commandReadsPromptOnStdin(execCmd) {
		return "", nil
	}
	path := envValue(env, "PAIMOS_PROMPT_FILE")
	if path == "" {
		return "", nil
	}
	// #nosec G304 -- path is a temp file this runner just created for the
	// current job and passed through the child environment.
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read prompt file: %w", err)
	}
	return string(b), nil
}

func commandReadsPromptOnStdin(execCmd string) bool {
	fields := strings.Fields(execCmd)
	if len(fields) == 0 || fields[0] != "claude" {
		return false
	}
	for _, f := range fields[1:] {
		if f == "-p" || f == "--print" {
			return true
		}
	}
	return false
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	return ""
}

func defaultConfirm(issueKey string, runID int64, repoRoot string) bool {
	fmt.Fprintf(stdout, "Implement %s (run %d) in %s? [y/N] ", issueKey, runID, repoRoot)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes"
}

func defaultDeployConfirm(issueKey string, runID int64, target string) bool {
	fmt.Fprintf(stdout, "DEPLOY %s (run %d) to %s? [y/N] ", issueKey, runID, target)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes"
}
