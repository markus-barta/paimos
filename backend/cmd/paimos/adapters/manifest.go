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

// ManifestAdapter is an adapter loaded from a YAML/JSON manifest file
// via `--harness-from-file`. It's the v1 escape hatch before PAI-332's
// SDK formalises external adapter packaging.
//
// The manifest format is intentionally minimal: name + version +
// supports range + suggested-path template + a Go text/template body.
// Templates have access to the canonical artifact as `.` so users can
// reference `.project.key`, `.agent.body`, etc.
type ManifestAdapter struct {
	NameValue          string
	VersionValue       string
	SupportsValue      string
	DescribeValue      string
	SuggestedPathTpl   *template.Template
	BodyTpl            *template.Template
	OriginalSourcePath string // for error messages
}

// Manifest is the on-disk shape the loader parses. JSON for v1 — YAML
// can come later if anyone wants it; the existing CLI commands use a
// mix and it's not worth the dep churn for the escape hatch.
type Manifest struct {
	Name          string `json:"name"`
	Version       string `json:"version"`
	Supports      string `json:"supports"`
	Describe      string `json:"describe"`
	SuggestedPath string `json:"suggested_path"`
	Body          string `json:"body"`
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
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("parse manifest %s: %w", abs, err)
	}
	if strings.TrimSpace(m.Name) == "" {
		return nil, fmt.Errorf("manifest %s: name is required", abs)
	}
	if strings.TrimSpace(m.Body) == "" {
		return nil, fmt.Errorf("manifest %s: body is required", abs)
	}
	bodyTpl, err := template.New("body").Parse(m.Body)
	if err != nil {
		return nil, fmt.Errorf("manifest %s: body template: %w", abs, err)
	}
	pathTpl := (*template.Template)(nil)
	if strings.TrimSpace(m.SuggestedPath) != "" {
		pathTpl, err = template.New("path").Parse(m.SuggestedPath)
		if err != nil {
			return nil, fmt.Errorf("manifest %s: suggested_path template: %w", abs, err)
		}
	}
	return &ManifestAdapter{
		NameValue:          m.Name,
		VersionValue:       firstNonEmpty(m.Version, "0.0.0"),
		SupportsValue:      m.Supports,
		DescribeValue:      firstNonEmpty(m.Describe, "Manifest adapter loaded from "+abs),
		SuggestedPathTpl:   pathTpl,
		BodyTpl:            bodyTpl,
		OriginalSourcePath: abs,
	}, nil
}

// Name returns the registry key.
func (m *ManifestAdapter) Name() string { return m.NameValue }

// Version returns the adapter version declared in the manifest.
func (m *ManifestAdapter) Version() string { return m.VersionValue }

// Supports returns the canonical-schema range declared in the manifest.
func (m *ManifestAdapter) Supports() string { return m.SupportsValue }

// Describe returns the manifest's describe string (or a generated one).
func (m *ManifestAdapter) Describe() string { return m.DescribeValue }

// Render applies the manifest's templates to the canonical artifact.
func (m *ManifestAdapter) Render(canonical []byte) (RenderResult, error) {
	var data any
	if err := json.Unmarshal(canonical, &data); err != nil {
		return RenderResult{}, fmt.Errorf("manifest %s: decode canonical: %w",
			m.OriginalSourcePath, err)
	}
	body := &strings.Builder{}
	if err := m.BodyTpl.Execute(body, data); err != nil {
		return RenderResult{}, fmt.Errorf("manifest %s: render body: %w",
			m.OriginalSourcePath, err)
	}
	suggested := ""
	if m.SuggestedPathTpl != nil {
		var p strings.Builder
		if err := m.SuggestedPathTpl.Execute(&p, data); err != nil {
			return RenderResult{}, fmt.Errorf("manifest %s: render suggested_path: %w",
				m.OriginalSourcePath, err)
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
