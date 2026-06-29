// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

// PAI-608 (epic PAI-605): the local runner for the "Implement this" feature.
// It subscribes to a project's SSE stream advertising implement-capability;
// when the web UI fires "Implement this" on a ticket, this process — on the
// developer's own workstation — spawns Claude Code in the repo and reports
// the run's progress back to PAIMOS.
//
// Safe by default: one job at a time, only ever runs in --repo-root, prompts
// for confirmation before spawning (unless --yes), and NEVER deploys — it
// reports tests_passed / failed only. Deploy stays a separate, manual step
// until the PAI-611 security pass and PAI-613 deploy gating.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
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
	)
	c := &cobra.Command{
		Use:   "watch",
		Short: "Watch for 'Implement this' jobs and run Claude Code on them",
		Long: `Subscribe to a project's event stream advertising implement-capability.
When the web UI fires "Implement this" on a ticket, this runner — on YOUR
workstation — spawns the configured command (Claude Code by default) in the
repo and reports progress back to PAIMOS.

Safe by default: it processes one job at a time, only ever runs in --repo-root,
prompts for confirmation before spawning (unless --yes), and does NOT deploy —
it reports the run as tests_passed / failed only. Deploy is triple-gated: it
runs only when --allow-deploy AND --deploy-exec are set AND the run carries a
deploy_target.

The spawned command sees PAIMOS_RUN_ID and PAIMOS_ISSUE_KEY in its environment
and may report richer progress itself via the paimos CLI (e.g. PATCH the run to
deployed); the runner won't clobber a terminal status it set.

Examples:
  paimos run-agent watch --project PAI --repo-root .
  paimos run-agent watch --project PAI --yes --exec "claude --print"`,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runAgentWatch(runAgentOpts{
				projectRef: projectRef, repoRoot: repoRoot, execCmd: execCmd,
				yes: yes, allowDeploy: allowDeploy, deployExec: deployExec,
			})
		},
	}
	c.Flags().StringVarP(&projectRef, "project", "p", "", "project key or id (required)")
	c.Flags().StringVar(&repoRoot, "repo-root", "", "repo the runner operates in (default: cwd)")
	c.Flags().StringVar(&execCmd, "exec", "claude", "command to spawn for a job (run in --repo-root)")
	c.Flags().BoolVar(&yes, "yes", false, "skip the per-job confirmation prompt (non-interactive)")
	c.Flags().BoolVar(&allowDeploy, "allow-deploy", false, "allow the runner to deploy after a successful run (off by default)")
	c.Flags().StringVar(&deployExec, "deploy-exec", "", `deploy command when --allow-deploy and the run has a deploy_target (e.g. "just deploy-ppm")`)
	return c
}

type runAgentOpts struct {
	projectRef  string
	repoRoot    string
	execCmd     string
	yes         bool
	allowDeploy bool
	deployExec  string
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

	runner := newAgentRunner(client, deviceID, root, o.execCmd, o.yes, o.allowDeploy, o.deployExec)
	deployNote := "report-back only, no auto-deploy"
	if runner.allowDeploy && runner.deployExec != "" {
		deployNote = "deploy ENABLED via " + runner.deployExec + " (only for runs with a deploy_target)"
	}
	fmt.Fprintf(stdout, "run-agent watching %s (device=%s, repo=%s) — %s\n",
		projectKey, deviceID, root, deployNote)

	// One job at a time: a busy runner skips new events rather than spawning a
	// second Claude Code in the same repo. The skipped run stays queued and can
	// be re-triggered.
	var busy atomic.Bool
	ssePath := sync.EventEndpoint(projectID, deviceID, "") + "&implement=1"
	err = syncer.Stream(ctx, ssePath, func(ev sync.Event) {
		if ev.Type != "implement_requested" {
			return
		}
		if !busy.CompareAndSwap(false, true) {
			fmt.Fprintf(stderr, "runner busy; skipping run %s (still queued)\n", ev.Rev)
			return
		}
		go func() {
			defer busy.Store(false)
			if err := runner.handle(ctx, ev); err != nil {
				fmt.Fprintf(stderr, "run %s: %v\n", ev.Rev, err)
			}
		}()
	})
	if err != nil && ctx.Err() == nil {
		return err
	}
	return nil
}

// agentRunner executes one implement job. spawn + confirm are fields so tests
// can inject fakes without touching the SSE/exec machinery.
type agentRunner struct {
	client      *Client
	deviceID    string
	repoRoot    string
	execCmd     string
	autoConfirm bool
	allowDeploy bool
	deployExec  string
	spawn       func(ctx context.Context, repoRoot, execCmd string, env []string) error
	confirm     func(issueKey string, runID int64, repoRoot string) bool
}

func newAgentRunner(client *Client, deviceID, repoRoot, execCmd string, autoConfirm, allowDeploy bool, deployExec string) *agentRunner {
	return &agentRunner{
		client:      client,
		deviceID:    deviceID,
		repoRoot:    repoRoot,
		execCmd:     execCmd,
		autoConfirm: autoConfirm,
		allowDeploy: allowDeploy,
		deployExec:  deployExec,
		spawn:       defaultSpawn,
		confirm:     defaultConfirm,
	}
}

type agentRunDetail struct {
	IssueID      int64  `json:"issue_id"`
	DeviceID     string `json:"device_id"`
	DeployTarget string `json:"deploy_target"`
	Status       string `json:"status"`
}

func (a *agentRunner) handle(ctx context.Context, ev sync.Event) error {
	runID, err := strconv.ParseInt(strings.TrimSpace(ev.Rev), 10, 64)
	if err != nil {
		return fmt.Errorf("event carried a bad run id %q", ev.Rev)
	}
	detail, err := a.fetchRun(runID)
	if err != nil {
		return err
	}
	// Device targeting: a run aimed at a specific device is only ours if it
	// names us. An empty device_id is open to any runner.
	if detail.DeviceID != "" && detail.DeviceID != a.deviceID {
		fmt.Fprintf(stdout, "run %d targets device %q (not %q) — skipping\n", runID, detail.DeviceID, a.deviceID)
		return nil
	}
	issueKey := ev.Name
	if issueKey == "" {
		issueKey = fmt.Sprintf("issue#%d", detail.IssueID)
	}
	if !a.autoConfirm && !a.confirm(issueKey, runID, a.repoRoot) {
		fmt.Fprintf(stdout, "run %d declined\n", runID)
		_ = a.patch(runID, map[string]any{"status": "cancelled"})
		return nil
	}
	if err := a.patch(runID, map[string]any{"status": "running"}); err != nil {
		return fmt.Errorf("mark running: %w", err)
	}
	fmt.Fprintf(stdout, "%s implementing %s (run %d) in %s\n",
		time.Now().Format(time.RFC3339), issueKey, runID, a.repoRoot)

	env := []string{
		"PAIMOS_RUN_ID=" + strconv.FormatInt(runID, 10),
		"PAIMOS_ISSUE_KEY=" + issueKey,
	}
	if spawnErr := a.spawn(ctx, a.repoRoot, a.execCmd, env); spawnErr != nil {
		_ = a.patch(runID, map[string]any{"status": "failed", "error": spawnErr.Error()})
		return fmt.Errorf("run %d failed: %w", runID, spawnErr)
	}
	// Implement succeeded. Deploy is triple-gated (PAI-613): --allow-deploy AND
	// --deploy-exec AND a run-level deploy_target. When all three hold, run the
	// deploy and stamp deployed + the captured version; otherwise report-back
	// only (tests_passed, unless the agent already advanced the run itself).
	if a.allowDeploy && a.deployExec != "" && detail.DeployTarget != "" {
		fmt.Fprintf(stdout, "run %d: deploying to %s via %q\n", runID, detail.DeployTarget, a.deployExec)
		if depErr := a.spawn(ctx, a.repoRoot, a.deployExec, env); depErr != nil {
			_ = a.patch(runID, map[string]any{"status": "failed", "error": "deploy: " + depErr.Error()})
			return fmt.Errorf("run %d deploy failed: %w", runID, depErr)
		}
		_ = a.patch(runID, map[string]any{
			"status":        "deployed",
			"version":       readVersionFile(a.repoRoot),
			"deploy_target": detail.DeployTarget,
		})
		fmt.Fprintf(stdout, "run %d deployed to %s\n", runID, detail.DeployTarget)
		return nil
	}
	if cur, _ := a.fetchRun(runID); cur != nil && cur.Status == "running" {
		_ = a.patch(runID, map[string]any{"status": "tests_passed"})
	}
	fmt.Fprintf(stdout, "run %d complete\n", runID)
	return nil
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
