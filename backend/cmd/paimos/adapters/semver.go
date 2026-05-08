// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package adapters

import (
	"fmt"
	"strconv"
	"strings"
)

// CheckSupports validates a Bosun-style range (e.g. `>=1.0.0 <2.0.0`)
// against a concrete version string. Returns nil on a match, or a
// human-readable error explaining the mismatch.
//
// The range grammar is intentionally minimal — space-separated
// constraint clauses (AND), each clause is a comparator (>=, >, <=, <,
// =, ==, !=) followed by a 3-part SemVer (major.minor.patch). Pre-
// release tags / build metadata are tolerated but ignored for
// comparison; the canonical schema doesn't use them today.
//
// Empty range = "no constraint" → matches anything (used as a soft
// default for adapters that haven't declared a range yet).
func CheckSupports(rangeExpr, version string) error {
	rangeExpr = strings.TrimSpace(rangeExpr)
	if rangeExpr == "" {
		return nil
	}
	v, err := parseSemver(version)
	if err != nil {
		return fmt.Errorf("invalid canonical version %q: %w", version, err)
	}
	for _, clause := range strings.Fields(rangeExpr) {
		op, want, err := parseClause(clause)
		if err != nil {
			return fmt.Errorf("invalid range %q: %w", rangeExpr, err)
		}
		if !compareSemver(v, op, want) {
			return fmt.Errorf("canonical schema %s does not satisfy adapter range %q", version, rangeExpr)
		}
	}
	return nil
}

type semver struct {
	major, minor, patch int
}

func parseSemver(s string) (semver, error) {
	// Strip pre-release / build metadata if present (`-`, `+`).
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(strings.TrimSpace(s), ".")
	if len(parts) < 1 || len(parts) > 3 {
		return semver{}, fmt.Errorf("expected major[.minor[.patch]]")
	}
	out := semver{}
	if len(parts) >= 1 {
		n, err := strconv.Atoi(parts[0])
		if err != nil {
			return semver{}, fmt.Errorf("major: %w", err)
		}
		out.major = n
	}
	if len(parts) >= 2 {
		n, err := strconv.Atoi(parts[1])
		if err != nil {
			return semver{}, fmt.Errorf("minor: %w", err)
		}
		out.minor = n
	}
	if len(parts) == 3 {
		n, err := strconv.Atoi(parts[2])
		if err != nil {
			return semver{}, fmt.Errorf("patch: %w", err)
		}
		out.patch = n
	}
	return out, nil
}

// parseClause splits "<op><version>" into its parts. Recognised ops:
// >=, <=, !=, ==, =, >, <. Bare versions (no op) are treated as `=`.
func parseClause(c string) (string, semver, error) {
	c = strings.TrimSpace(c)
	if c == "" {
		return "", semver{}, fmt.Errorf("empty clause")
	}
	for _, op := range []string{">=", "<=", "!=", "==", ">", "<", "="} {
		if strings.HasPrefix(c, op) {
			rest := strings.TrimSpace(strings.TrimPrefix(c, op))
			v, err := parseSemver(rest)
			if err != nil {
				return "", semver{}, err
			}
			return normalizeOp(op), v, nil
		}
	}
	v, err := parseSemver(c)
	if err != nil {
		return "", semver{}, err
	}
	return "=", v, nil
}

func normalizeOp(op string) string {
	if op == "==" {
		return "="
	}
	return op
}

// compareSemver returns true iff `v <op> want`.
func compareSemver(v semver, op string, want semver) bool {
	cmp := cmpSemver(v, want)
	switch op {
	case "=":
		return cmp == 0
	case "!=":
		return cmp != 0
	case ">":
		return cmp > 0
	case ">=":
		return cmp >= 0
	case "<":
		return cmp < 0
	case "<=":
		return cmp <= 0
	}
	return false
}

func cmpSemver(a, b semver) int {
	if a.major != b.major {
		return signOf(a.major - b.major)
	}
	if a.minor != b.minor {
		return signOf(a.minor - b.minor)
	}
	return signOf(a.patch - b.patch)
}

func signOf(n int) int {
	switch {
	case n > 0:
		return 1
	case n < 0:
		return -1
	}
	return 0
}
