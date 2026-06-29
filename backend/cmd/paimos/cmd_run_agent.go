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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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
		yes         bool
		allowDeploy bool
		deployExec  string
		yesDeploy   bool
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
and may report richer progress itself via the paimos CLI (e.g. PATCH the run to
deployed); the runner won't clobber a terminal status it set.

Examples:
  paimos run-agent watch --project PAI --repo-root .
  paimos run-agent watch --project PAI --yes --exec "claude --print"`,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runAgentWatch(runAgentOpts{
				projectRef: projectRef, repoRoot: repoRoot, execCmd: execCmd,
				yes: yes, allowDeploy: allowDeploy, deployExec: deployExec, yesDeploy: yesDeploy,
			})
		},
	}
	c.Flags().StringVarP(&projectRef, "project", "p", "", "project key or id (required)")
	c.Flags().StringVar(&repoRoot, "repo-root", "", "repo the runner operates in (default: cwd)")
	c.Flags().StringVar(&execCmd, "exec", "claude", "command to spawn for a job (run in --repo-root)")
	c.Flags().BoolVar(&yes, "yes", false, "skip the per-job confirmation prompt (non-interactive)")
	c.Flags().BoolVar(&allowDeploy, "allow-deploy", false, "allow the runner to deploy after a successful run (off by default)")
	c.Flags().StringVar(&deployExec, "deploy-exec", "", `deploy command when --allow-deploy and the run has a deploy_target (e.g. "just deploy-ppm")`)
	c.Flags().BoolVar(&yesDeploy, "yes-deploy", false, "skip the separate deploy confirmation (still requires --allow-deploy + --deploy-exec)")
	return c
}

type runAgentOpts struct {
	projectRef  string
	repoRoot    string
	execCmd     string
	yes         bool
	allowDeploy bool
	deployExec  string
	yesDeploy   bool
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

	runner := newAgentRunner(client, deviceID, root, o.execCmd, o.yes, o.allowDeploy, o.deployExec, o.yesDeploy)
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
	ssePath := sync.EventEndpoint(projectID, deviceID, "") + "&implement=1"
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return nil
		}
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
		if backoff < 30*time.Second {
			backoff *= 2
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
	autoConfirm    bool
	allowDeploy    bool
	deployExec     string
	autoConfirmDep bool
	spawn          func(ctx context.Context, repoRoot, execCmd string, env []string) error
	confirm        func(issueKey string, runID int64, repoRoot string) bool
	confirmDeploy  func(issueKey string, runID int64, target string) bool
}

func newAgentRunner(client *Client, deviceID, repoRoot, execCmd string, autoConfirm, allowDeploy bool, deployExec string, autoConfirmDeploy bool) *agentRunner {
	return &agentRunner{
		client:         client,
		deviceID:       deviceID,
		repoRoot:       repoRoot,
		execCmd:        execCmd,
		autoConfirm:    autoConfirm,
		allowDeploy:    allowDeploy,
		deployExec:     deployExec,
		autoConfirmDep: autoConfirmDeploy,
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
	if !a.autoConfirm && !a.confirm(issueKey, runID, a.repoRoot) {
		fmt.Fprintf(stdout, "run %d declined\n", runID)
		a.report(runID, map[string]any{"status": "cancelled", "if_status": "queued"})
		return nil
	}
	// Atomic claim: queued -> running. A second runner that re-reads the run
	// loses here (the if_status guard) and skips — no double-spawn.
	if err := a.patch(runID, map[string]any{"status": "running", "if_status": "queued"}); err != nil {
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
	}
	if spawnErr := a.spawn(ctx, a.repoRoot, a.execCmd, env); spawnErr != nil {
		a.report(runID, map[string]any{"status": "failed", "error": spawnErr.Error()})
		return fmt.Errorf("run %d failed: %w", runID, spawnErr)
	}

	// Deploy is triple-gated AND needs its own consent (PAI-605 M6): even under
	// --yes, the deploy step prompts unless --yes-deploy was passed.
	if a.allowDeploy && a.deployExec != "" && detail.DeployTarget != "" {
		if !a.autoConfirmDep && !a.confirmDeploy(issueKey, runID, detail.DeployTarget) {
			fmt.Fprintf(stdout, "run %d: deploy declined — reporting tests_passed\n", runID)
			a.report(runID, map[string]any{"status": "tests_passed"})
			return nil
		}
		fmt.Fprintf(stdout, "run %d: deploying to %s via %q\n", runID, detail.DeployTarget, a.deployExec)
		if depErr := a.spawn(ctx, a.repoRoot, a.deployExec, env); depErr != nil {
			a.report(runID, map[string]any{"status": "failed", "error": "deploy: " + depErr.Error()})
			return fmt.Errorf("run %d deploy failed: %w", runID, depErr)
		}
		a.report(runID, map[string]any{
			"status":        "deployed",
			"version":       readVersionFile(a.repoRoot),
			"deploy_target": detail.DeployTarget,
		})
		fmt.Fprintf(stdout, "run %d deployed to %s\n", runID, detail.DeployTarget)
		return nil
	}

	// Report-back only. The server rejects clobbering a terminal status (so if
	// the agent already advanced the run, this is a harmless 409, not a clobber).
	a.report(runID, map[string]any{"status": "tests_passed"})
	fmt.Fprintf(stdout, "run %d complete\n", runID)
	return nil
}

// queuedRunIDs returns the ids of runs still queued for the project — the
// catch-up source.
func (a *agentRunner) queuedRunIDs(ctx context.Context, projectID int64) []int64 {
	if ctx.Err() != nil {
		return nil
	}
	body, err := a.client.do("GET", fmt.Sprintf("/api/projects/%d/runs?status=queued", projectID), nil)
	if err != nil {
		return nil
	}
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

func defaultSpawn(ctx context.Context, repoRoot, execCmd string, env []string) error {
	parts := strings.Fields(execCmd)
	if len(parts) == 0 {
		return fmt.Errorf("empty --exec command")
	}
	// #nosec G204 -- execCmd is the operator's own --exec flag (default "claude"),
	// not network input. Running it in the operator's repo is the whole point.
	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
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
