// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package adapters

// PAI-332 — external (binary) adapter execution.
//
// Out-of-tree adapters live as standalone executables on disk. They
// follow the contract:
//
//   paimos-adapter-<name> render --input -          # stdin: canonical, stdout: rendered
//   paimos-adapter-<name> describe                  # stdout: manifest JSON
//   paimos-adapter-<name> validate --input -        # exit 0 if input is consumable
//
// Errors surface as non-zero exit + a single-line summary on stderr.
//
// The ExternalAdapter type wraps the binary so dispatch.go can route
// to it through the normal Adapter interface. The conformance suite
// exercises the same wrapper, which means the conformance pass
// guarantees real invocation paths (not just shape testing).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// DefaultExecTimeout is the wall-clock cap on a single adapter call.
// Adapters that need longer should be redesigned — `paimos skill render`
// is interactive in the user's terminal.
const DefaultExecTimeout = 30 * time.Second

// ExternalAdapter wraps a `paimos-adapter-<name>` executable so it
// implements the in-process Adapter interface. Each method translates
// to one short-lived process invocation.
type ExternalAdapter struct {
	manifest       Manifest
	executablePath string
	manifestPath   string

	// ExecTimeout is the wall-clock cap per invocation. Zero ⇒
	// DefaultExecTimeout.
	ExecTimeout time.Duration
}

// NewExternalAdapter constructs an ExternalAdapter from a discovered
// manifest + binary path. Callers typically build one via
// DiscoverAdapters → for-each → NewExternalAdapter, then register the
// result with the in-memory Registry so the dispatch layer treats it
// uniformly.
func NewExternalAdapter(d DiscoveredAdapter) (*ExternalAdapter, error) {
	if strings.TrimSpace(d.Manifest.Name) == "" {
		return nil, fmt.Errorf("external adapter: manifest missing name")
	}
	if strings.TrimSpace(d.ExecutablePath) == "" {
		return nil, fmt.Errorf("external adapter %q: no paimos-adapter-%s executable next to %s",
			d.Manifest.Name, d.Manifest.Name, d.ManifestPath)
	}
	abs, err := filepath.Abs(d.ExecutablePath)
	if err != nil {
		return nil, fmt.Errorf("external adapter %q: %w", d.Manifest.Name, err)
	}
	return &ExternalAdapter{
		manifest:       d.Manifest,
		executablePath: abs,
		manifestPath:   d.ManifestPath,
	}, nil
}

// Name returns the registry key.
func (e *ExternalAdapter) Name() string { return e.manifest.Name }

// Version returns the manifest version.
func (e *ExternalAdapter) Version() string { return e.manifest.Version }

// Supports returns the canonical-schema range from the manifest.
func (e *ExternalAdapter) Supports() string { return e.manifest.Supports }

// Describe returns the manifest description.
func (e *ExternalAdapter) Describe() string {
	return e.manifest.effectiveDescription()
}

// Manifest returns the parsed v1 manifest.
func (e *ExternalAdapter) Manifest() Manifest { return e.manifest }

// ExecutablePath returns the on-disk binary path. Used by diagnostics.
func (e *ExternalAdapter) ExecutablePath() string { return e.executablePath }

// Render invokes `paimos-adapter-<name> render --input -` with the
// canonical artifact on stdin. Stdout is the rendered file body;
// stderr is preserved on error.
func (e *ExternalAdapter) Render(canonical []byte) (RenderResult, error) {
	body, err := e.run("render", canonical, "--input", "-")
	if err != nil {
		return RenderResult{}, err
	}
	// SuggestedPath is computed from the manifest's TargetPathTemplate
	// — the binary itself doesn't emit a path. Tokens substituted from
	// the canonical artifact.
	suggested, _ := SubstituteTargetPath(e.manifest.effectiveTargetPath(), canonical, "")
	return RenderResult{
		Content:       string(body),
		SuggestedPath: suggested,
	}, nil
}

// Validate invokes `paimos-adapter-<name> validate --input -` to ask
// the adapter whether the canonical artifact is consumable. Returns
// nil on exit 0; surfaces stderr on non-zero.
func (e *ExternalAdapter) Validate(canonical []byte) error {
	_, err := e.run("validate", canonical, "--input", "-")
	return err
}

// DescribeJSON invokes `paimos-adapter-<name> describe` and returns
// the raw manifest JSON the binary emits. The conformance suite uses
// this to verify the manifest the binary self-reports matches the
// on-disk manifest paimos discovered.
func (e *ExternalAdapter) DescribeJSON() ([]byte, error) {
	return e.run("describe", nil)
}

// run is the shared subprocess driver. The verb + extra args are
// appended after the executable; stdin (when non-nil) is piped in;
// stdout returned on success; on failure, stderr's first line is
// folded into the returned error.
func (e *ExternalAdapter) run(verb string, stdin []byte, extraArgs ...string) ([]byte, error) {
	timeout := e.ExecTimeout
	if timeout <= 0 {
		timeout = DefaultExecTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	args := append([]string{verb}, extraArgs...)
	// #nosec G204 -- executablePath is the user-installed paimos-adapter-<name> binary located at discovery time; verb/extraArgs are fixed strings from our own callers.
	cmd := exec.CommandContext(ctx, e.executablePath, args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, fmt.Errorf("external adapter %q timed out after %s",
			e.manifest.Name, timeout)
	}
	if err != nil {
		summary := firstLine(stderr.String())
		if summary == "" {
			summary = err.Error()
		}
		return nil, fmt.Errorf("external adapter %q %s failed: %s",
			e.manifest.Name, verb, summary)
	}
	return stdout.Bytes(), nil
}

// SubstituteTargetPath replaces the `{token}` placeholders in the v1
// manifest's TargetPathTemplate with values pulled from the canonical
// artifact. Recognised tokens:
//
//   {workspace}             — the explicit workspaceRoot arg
//   {slash_command_name}    — agent.slash_command_name (or agent.name)
//   {agent_name}            — agent.name
//   {project_key}           — project.key
//
// An empty template returns "". Tokens that can't be resolved from the
// canonical artifact are replaced with an empty string — callers
// should validate the result is non-empty before treating it as a
// path. Returns the rendered path + the slug used (so callers can
// surface it in diagnostics).
func SubstituteTargetPath(tmpl string, canonical []byte, workspaceRoot string) (string, string) {
	tmpl = strings.TrimSpace(tmpl)
	if tmpl == "" {
		return "", ""
	}
	probe := struct {
		Project struct {
			Key  string `json:"key"`
			Name string `json:"name"`
		} `json:"project"`
		Agent struct {
			Name             string `json:"name"`
			SlashCommandName string `json:"slash_command_name"`
		} `json:"agent"`
	}{}
	_ = json.Unmarshal(canonical, &probe)
	slug := strings.TrimSpace(probe.Agent.SlashCommandName)
	if slug == "" {
		slug = strings.TrimSpace(probe.Agent.Name)
	}
	subs := map[string]string{
		"{workspace}":          workspaceRoot,
		"{slash_command_name}": slug,
		"{agent_name}":         strings.TrimSpace(probe.Agent.Name),
		"{project_key}":        strings.TrimSpace(probe.Project.Key),
	}
	out := tmpl
	for k, v := range subs {
		out = strings.ReplaceAll(out, k, v)
	}
	return out, slug
}

// firstLine extracts the first non-empty line of s. Used to fold
// adapter stderr into a single-line error summary per the contract.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return ""
}
