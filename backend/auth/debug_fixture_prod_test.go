// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

//go:build !dev_login

package auth

import (
	"testing"

	"github.com/markus-barta/paimos/backend/models"
)

func TestSuppressSecurityNags_ProductionBuild(t *testing.T) {
	if SuppressSecurityNags(&models.User{Username: "debug-admin"}) {
		t.Fatal("production build must never suppress security nags for debug fixture usernames")
	}
}
