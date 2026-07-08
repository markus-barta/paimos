// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

//go:build dev_login

package auth

import (
	"strings"

	"github.com/markus-barta/paimos/backend/models"
)

// SuppressSecurityNags is true only for local debug fixture accounts in
// dev-login builds. Production binaries compile the false-returning stub.
func SuppressSecurityNags(user *models.User) bool {
	return user != nil && strings.HasPrefix(user.Username, "debug-")
}
