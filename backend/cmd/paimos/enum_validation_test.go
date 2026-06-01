// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package main

import "testing"

func TestNormalizeEnumValueFallbacksWhenSchemaCacheMissingOrOld(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(envURL, "https://example.test")
	t.Setenv(envAPIKey, "test_key")

	got, err := normalizeEnumValue("issue.type", "Task")
	if err != nil {
		t.Fatalf("normalize missing cache: %v", err)
	}
	if got != "task" {
		t.Fatalf("normalize missing cache = %q, want task", got)
	}

	if err := saveCachedSchema("env", &CachedSchema{
		Version: "1.3.0",
		Enums: map[string][]string{
			"type": {"epic", "cost_unit", "release", "sprint", "ticket", "task"},
		},
		// Deliberately no EnumFields: old caches predate PAI-494.
	}); err != nil {
		t.Fatalf("save old cache: %v", err)
	}

	got, err = normalizeEnumValue("issue.type", "TASK")
	if err != nil {
		t.Fatalf("normalize old cache: %v", err)
	}
	if got != "task" {
		t.Fatalf("normalize old cache = %q, want task", got)
	}
}
