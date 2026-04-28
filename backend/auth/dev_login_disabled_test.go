// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

//go:build !dev_login

// PAI-267 — pin the production-build invariant: without the
// `dev_login` build tag, auth.DevLoginEnabled() must return false.
// Paired with the dev-build test in dev_login_test.go which pins the
// opposite. If a future refactor accidentally short-circuits the
// build-tag gate, one of these two tests fails.

package auth_test

import (
	"testing"

	"github.com/markus-barta/paimos/backend/auth"
)

func TestDevLoginEnabled_ReturnsFalseOnProdBuild(t *testing.T) {
	if auth.DevLoginEnabled() {
		t.Fatalf("DevLoginEnabled() = true on production build — dev-login route would be exposed in shipping binaries")
	}
}
