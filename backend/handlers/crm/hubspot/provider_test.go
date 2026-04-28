// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package hubspot

import (
	"strings"
	"testing"
)

// TestValidateConfig_AcceptsBothTokenFlavours pins the PAI-258 invariant:
// the validator must NOT gate on a `pat-` prefix because HubSpot also
// issues Personal Access Keys with a different opaque format.
func TestValidateConfig_AcceptsBothTokenFlavours(t *testing.T) {
	p := &Provider{}
	cases := []struct {
		name  string
		token string
	}{
		{"private app token", "pat-na1-FAKETOKEN-NOT-REAL-USED-ONLY-IN-TESTS"},
		{"personal access key", "CiRldTEtNxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := p.ValidateConfig(map[string]string{
				"portal_id": "12345678",
				"token":     c.token,
			})
			if err != nil {
				t.Fatalf("expected nil for %s, got %v", c.name, err)
			}
		})
	}
}

func TestValidateConfig_RejectsBadInput(t *testing.T) {
	p := &Provider{}
	cases := []struct {
		name      string
		portal    string
		token     string
		errSubstr string
	}{
		{"non-numeric portal", "abc", "pat-na1-FAKETOKEN-NOT-REAL-USED-ONLY-IN-TESTS", "portal_id"},
		{"empty token", "12345678", "", "empty"},
		{"whitespace-only token", "12345678", "   ", "empty"},
		{"too-short token", "12345678", "pat-short", "too short"},
		{"contains whitespace", "12345678", "pat-na1-FAKETOKEN NOT-REAL-USED-IN-TESTS", "whitespace"},
		{"includes bearer prefix", "12345678", "Bearer pat-na1-FAKETOKEN-NOT-REAL-USED-ONLY-IN-TESTS", "Bearer"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := p.ValidateConfig(map[string]string{
				"portal_id": c.portal,
				"token":     c.token,
			})
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", c.errSubstr)
			}
			if !strings.Contains(err.Error(), c.errSubstr) {
				t.Fatalf("expected error containing %q, got %q", c.errSubstr, err.Error())
			}
		})
	}
}
