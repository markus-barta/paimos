// PAIMOS -- Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package httpcrm

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestVerifySignature(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	body := []byte(`{"ref":"acme"}`)
	secret := "shared-secret"
	timestamp := "1700000000"
	signature := ComputeSignature(secret, timestamp, body)

	if err := VerifySignature(secret, now, timestamp, body, signature); err != nil {
		t.Fatalf("VerifySignature valid: %v", err)
	}
	if err := VerifySignature(secret, now, timestamp, body, signature[:len(signature)-2]+"00"); err == nil {
		t.Fatalf("VerifySignature accepted mismatched signature")
	}
	if err := VerifySignature(secret, now.Add(301*time.Second), timestamp, body, signature); err == nil {
		t.Fatalf("VerifySignature accepted stale timestamp")
	}
	if err := VerifySignature(secret, now.Add(-301*time.Second), timestamp, body, signature); err == nil {
		t.Fatalf("VerifySignature accepted future timestamp outside window")
	}
}

func TestSignRequest(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	body := []byte(`{"query":"acme"}`)
	req := httptest.NewRequest("POST", "/v1/search", nil)

	SignRequest(req, "shared-secret", now, body)

	if got := req.Header.Get(HeaderTimestamp); got != "1700000000" {
		t.Fatalf("timestamp: got %q", got)
	}
	if err := VerifyRequest(req, "shared-secret", now, body); err != nil {
		t.Fatalf("VerifyRequest signed request: %v", err)
	}
}
