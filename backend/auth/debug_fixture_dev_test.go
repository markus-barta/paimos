// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

//go:build dev_login

package auth

import (
	"testing"

	"github.com/inspr-at/paimos/backend/models"
)

func TestSuppressSecurityNags_DevLoginBuild(t *testing.T) {
	if !SuppressSecurityNags(&models.User{Username: "debug-admin"}) {
		t.Fatal("debug fixture user should suppress security nags in dev_login build")
	}
	if SuppressSecurityNags(&models.User{Username: "dev_admin"}) {
		t.Fatal("token-only dev fixture user should not suppress security nags")
	}
	if SuppressSecurityNags(&models.User{Username: "alice"}) {
		t.Fatal("normal user should not suppress security nags")
	}
}
