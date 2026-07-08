// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

//go:build !dev_login

package auth

import "github.com/markus-barta/paimos/backend/models"

func SuppressSecurityNags(user *models.User) bool {
	return false
}
