// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package adapters

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

// PAI-332 — formal adapter manifest format.
//
// The manifest is the on-disk descriptor every external adapter ships
// alongside its binary (`paimos-adapter-<name>` next to a
// `paimos-adapter.json` file in the same directory, or referenced by
// the `PAIMOS_ADAPTER_PATH` discovery walk). In-tree adapters (e.g.
// the bundled claude-code reference) carry the same shape via
// Adapter.Manifest() so paimos can list, validate, and serve them
// uniformly.
//
// Format choice: JSON.
//
//   - The PAI-330 escape hatch (`--harness-from-file`) already loads a
//     JSON manifest, so JSON is the path of least churn.
//   - Adapter binaries emit JSON via `paimos-adapter-<name> describe`
//     per the PAI-332 ticket — keeping on-disk and over-stdin formats
//     identical avoids a second parser and lets `describe` literally
//     `cat` the manifest in the trivial case.
//   - The canonical artifact the adapter consumes is JSON, so the whole
//     contract is one syntax. TOML would be friendlier for hand-
//     authoring but adds a dep, two parsers, and a translation layer
//     between disk-format and the wire shape `describe` must produce.
//
// The format is versioned via `protocol_version` at the manifest root;
// breaking changes will bump the major. Today's manifest is
// protocol_version "1".

// ProtocolVersion is the on-disk manifest format version. Major bumps
// signal an incompatible shape change; loaders refuse manifests they
// don't recognise rather than silently dropping fields.
const ProtocolVersion = "1"

// ManifestFileName is the conventional filename loaders look for when
// walking $PAIMOS_ADAPTER_PATH directories or sitting next to an
// adapter binary. CLI-side conformance + registry serving accept any
// path the user names; this constant is just the discovery default.
const ManifestFileName = "paimos-adapter.json"

// Manifest is the formal v1 adapter descriptor. Every field corresponds
// directly to a ticket-listed knob (see PAI-332):
//
//	{
//	  "protocol_version": "1",
//	  "name": "claude-code",
//	  "version": "1.0.0",
//	  "supports": ">=1.0.0 <2.0.0",
//	  "description": "Claude Code skill markdown adapter.",
//	  "target_path_template": "{workspace}/.claude/commands/{slash_command_name}.md",
//	  "input_format": "json",
//	  "output_format": "markdown",
//
//	  // Optional — only used by --harness-from-file (lets a single
//	  // JSON file be both manifest and renderer):
//	  "body": "Go text/template body",
//	  "suggested_path": "Go text/template (deprecated alias)"
//	}
//
// Unknown fields are ignored to leave room for additive growth, but
// `protocol_version` mismatches are hard errors.
type Manifest struct {
	// ProtocolVersion identifies the manifest schema. "1" is current.
	ProtocolVersion string `json:"protocol_version,omitempty"`

	// Name is the registry key (matches `--harness <name>`).
	Name string `json:"name"`

	// Version is the adapter's own SemVer.
	Version string `json:"version,omitempty"`

	// Supports is the canonical-schema version range this adapter
	// consumes (Bosun-style: ">=1.0.0 <2.0.0").
	Supports string `json:"supports,omitempty"`

	// Description is the adapter's one-line CLI help string. Older
	// manifests used `describe`; the loader still accepts that.
	Description string `json:"description,omitempty"`

	// TargetPathTemplate is the harness-conventional output path with
	// `{placeholder}` tokens. Recognised tokens:
	//
	//   {workspace}             — caller-provided workspace root
	//   {slash_command_name}    — agent.slash_command_name (or agent.name)
	//   {agent_name}            — agent.name
	//   {project_key}           — project.key
	//
	// Example:
	//   "{workspace}/.claude/commands/{slash_command_name}.md"
	//
	// In-process adapters expose this for documentation / registry
	// listings; CLI rendering paths still use Adapter.Render's
	// SuggestedPath at runtime so the format itself is also a contract.
	TargetPathTemplate string `json:"target_path_template,omitempty"`

	// InputFormat is the format the adapter consumes on stdin. Today
	// every paimos adapter consumes the canonical-artifact JSON, so
	// this is "json" or empty (treated as "json" by readers). Reserved
	// for future binary / protobuf adapters.
	InputFormat string `json:"input_format,omitempty"`

	// OutputFormat is a hint at the rendered file extension.
	// Conventional values: "markdown", "json", "yaml", "text".
	OutputFormat string `json:"output_format,omitempty"`

	// Body — escape-hatch only. When present, the manifest itself is
	// rendered in-process by `--harness-from-file` via Go text/template.
	// External adapters set this to "" and use their binary's `render`
	// verb instead.
	Body string `json:"body,omitempty"`

	// SuggestedPath is the legacy alias for TargetPathTemplate read by
	// the v1 escape-hatch loader (PAI-330). New manifests should use
	// TargetPathTemplate; this exists so existing user manifests don't
	// break under PAI-332.
	SuggestedPath string `json:"suggested_path,omitempty"`

	// Describe is the legacy alias for Description.
	Describe string `json:"describe,omitempty"`
}

// effectiveDescription returns Description, falling back to the legacy
// Describe alias.
func (m *Manifest) effectiveDescription() string {
	if strings.TrimSpace(m.Description) != "" {
		return m.Description
	}
	return m.Describe
}

// effectiveTargetPath returns TargetPathTemplate, falling back to the
// legacy SuggestedPath alias.
func (m *Manifest) effectiveTargetPath() string {
	if strings.TrimSpace(m.TargetPathTemplate) != "" {
		return m.TargetPathTemplate
	}
	return m.SuggestedPath
}

// MarshalJSON normalises the manifest to its canonical v1 shape.
// Aliased fields (Describe, SuggestedPath) are dropped from the output;
// callers reading manifests over the registry endpoint or via
// `paimos-adapter-<name> describe` see only the canonical names.
func (m Manifest) MarshalJSON() ([]byte, error) {
	type Alias Manifest
	out := struct {
		*Alias
		// Force the canonical names — empty omitempty + setting them
		// at write time.
		ProtocolVersion    string `json:"protocol_version"`
		Name               string `json:"name"`
		Version            string `json:"version,omitempty"`
		Supports           string `json:"supports,omitempty"`
		Description        string `json:"description,omitempty"`
		TargetPathTemplate string `json:"target_path_template,omitempty"`
		InputFormat        string `json:"input_format,omitempty"`
		OutputFormat       string `json:"output_format,omitempty"`
		Body               string `json:"body,omitempty"`
		// Legacy aliases dropped from output. They live in the type
		// only for tolerant input parsing.
		SuggestedPath omitField `json:"suggested_path,omitempty"`
		Describe      omitField `json:"describe,omitempty"`
	}{
		Alias:              (*Alias)(&m),
		ProtocolVersion:    firstNonEmpty(m.ProtocolVersion, ProtocolVersion),
		Name:               m.Name,
		Version:            m.Version,
		Supports:           m.Supports,
		Description:        m.effectiveDescription(),
		TargetPathTemplate: m.effectiveTargetPath(),
		InputFormat:        m.InputFormat,
		OutputFormat:       m.OutputFormat,
		Body:               m.Body,
	}
	return json.Marshal(out)
}

// omitField is a JSON-marshal helper that always emits its zero value
// (so omitempty + the empty string drop the field) — used to mask
// legacy aliases out of canonical output.
type omitField string

// MarshalJSON makes omitField always look empty so omitempty drops it.
func (omitField) MarshalJSON() ([]byte, error) { return []byte(`""`), nil }

// Validate enforces the v1 contract: protocol_version compat, required
// fields, and well-formed range / version strings.
func (m *Manifest) Validate() error {
	pv := strings.TrimSpace(m.ProtocolVersion)
	if pv == "" {
		// Tolerate omitted protocol_version on read — current
		// implementations did not emit it before PAI-332. Fill it in
		// so downstream consumers see the canonical value.
		m.ProtocolVersion = ProtocolVersion
	} else if pv != ProtocolVersion {
		return fmt.Errorf("manifest: unsupported protocol_version %q (this paimos understands %q)",
			pv, ProtocolVersion)
	}
	if strings.TrimSpace(m.Name) == "" {
		return fmt.Errorf("manifest: name is required")
	}
	// Version + Supports are technically optional (escape-hatch
	// manifests can run anonymous), but if they are set they must
	// parse so we fail early instead of at dispatch time.
	if v := strings.TrimSpace(m.Version); v != "" {
		if _, err := parseSemver(v); err != nil {
			return fmt.Errorf("manifest: version %q invalid semver: %w", v, err)
		}
	}
	if r := strings.TrimSpace(m.Supports); r != "" {
		// Probe the range with a known-good version; CheckSupports
		// validates the range syntax even when the version satisfies.
		if err := CheckSupports(r, "1.0.0"); err != nil {
			// CheckSupports returns a "doesn't satisfy" error when the
			// range is well-formed but the probe version doesn't
			// match. We only care about syntax errors here, so re-
			// check with a different probe; if both reject for the
			// "invalid range" reason it's a syntax error.
			if strings.Contains(err.Error(), "invalid range") {
				return fmt.Errorf("manifest: supports %q invalid: %w", r, err)
			}
		}
	}
	if f := strings.TrimSpace(m.InputFormat); f != "" && f != "json" {
		return fmt.Errorf("manifest: input_format %q unsupported (only \"json\" today)", f)
	}
	return nil
}

// ManifestAdapter is an adapter loaded from a manifest file via
// `--harness-from-file`. It implements the in-process Adapter interface
// using Go text/template — that's the v1 escape hatch for ad-hoc
// adapters. External binary adapters wrap a Manifest directly via
// the conformance / registry surface; they do not need this struct.
type ManifestAdapter struct {
	manifest           Manifest
	suggestedPathTpl   *template.Template
	bodyTpl            *template.Template
	originalSourcePath string
}

// LoadManifestAdapter reads a manifest file from disk and returns a
// ready-to-register adapter. Errors point at concrete fields so the
// user can fix typos quickly.
func LoadManifestAdapter(path string) (*ManifestAdapter, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve manifest path: %w", err)
	}
	raw, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("read manifest %s: %w", abs, err)
	}
	m, err := ParseManifest(raw)
	if err != nil {
		return nil, fmt.Errorf("parse manifest %s: %w", abs, err)
	}
	if strings.TrimSpace(m.Body) == "" {
		return nil, fmt.Errorf("manifest %s: body is required for in-process (--harness-from-file) adapters", abs)
	}
	bodyTpl, err := template.New("body").Parse(m.Body)
	if err != nil {
		return nil, fmt.Errorf("manifest %s: body template: %w", abs, err)
	}
	pathTpl := (*template.Template)(nil)
	if pt := strings.TrimSpace(m.effectiveTargetPath()); pt != "" {
		pathTpl, err = template.New("path").Parse(pt)
		if err != nil {
			return nil, fmt.Errorf("manifest %s: target_path_template: %w", abs, err)
		}
	}
	if strings.TrimSpace(m.Version) == "" {
		m.Version = "0.0.0"
	}
	if strings.TrimSpace(m.Description) == "" && strings.TrimSpace(m.Describe) == "" {
		m.Description = "Manifest adapter loaded from " + abs
	}
	return &ManifestAdapter{
		manifest:           m,
		suggestedPathTpl:   pathTpl,
		bodyTpl:            bodyTpl,
		originalSourcePath: abs,
	}, nil
}

// ParseManifest parses raw JSON bytes into a validated Manifest. Used
// by both the on-disk loader and the registry endpoint, so syntax
// errors surface uniformly.
func ParseManifest(raw []byte) (Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return Manifest{}, fmt.Errorf("decode manifest JSON: %w", err)
	}
	if err := m.Validate(); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

// Name returns the registry key.
func (m *ManifestAdapter) Name() string { return m.manifest.Name }

// Version returns the adapter version declared in the manifest.
func (m *ManifestAdapter) Version() string { return m.manifest.Version }

// Supports returns the canonical-schema range declared in the manifest.
func (m *ManifestAdapter) Supports() string { return m.manifest.Supports }

// Describe returns the manifest's description (or the legacy
// describe alias, or a generated fallback set during load).
func (m *ManifestAdapter) Describe() string {
	if d := strings.TrimSpace(m.manifest.effectiveDescription()); d != "" {
		return d
	}
	return "Manifest adapter loaded from " + m.originalSourcePath
}

// Manifest returns the parsed manifest record.
func (m *ManifestAdapter) Manifest() Manifest { return m.manifest }

// Render applies the manifest's templates to the canonical artifact.
func (m *ManifestAdapter) Render(canonical []byte) (RenderResult, error) {
	var data any
	if err := json.Unmarshal(canonical, &data); err != nil {
		return RenderResult{}, fmt.Errorf("manifest %s: decode canonical: %w",
			m.originalSourcePath, err)
	}
	body := &strings.Builder{}
	if err := m.bodyTpl.Execute(body, data); err != nil {
		return RenderResult{}, fmt.Errorf("manifest %s: render body: %w",
			m.originalSourcePath, err)
	}
	suggested := ""
	if m.suggestedPathTpl != nil {
		var p strings.Builder
		if err := m.suggestedPathTpl.Execute(&p, data); err != nil {
			return RenderResult{}, fmt.Errorf("manifest %s: render target_path_template: %w",
				m.originalSourcePath, err)
		}
		suggested = strings.TrimSpace(p.String())
	}
	return RenderResult{
		Content:       body.String(),
		SuggestedPath: suggested,
	}, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
