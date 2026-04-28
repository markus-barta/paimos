// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

//go:build !dev_login

// PAI-267 — production stub. The real implementation in dev_login_dev.go
// only compiles under the `dev_login` build tag. This file's exported
// surfaces match exactly so main.go's wiring is build-tag-agnostic.
//
// devLoginEnabled() returns false here, which gates main.go away from
// mounting POST /api/auth/dev-login at all — a request to that path
// returns chi's stock 404. Even if some path inadvertently called
// DevLoginHandler, it would 404 the request itself.
//
// ValidateDevLoginConfig is a no-op so main.go can call it
// unconditionally without #if-style guards.

package auth

import "net/http"

// DevLoginEnabled reports whether the dev_login build tag is active.
func DevLoginEnabled() bool { return false }

// ValidateDevLoginConfig is a no-op in production builds.
func ValidateDevLoginConfig() {}

// DevLoginHandler returns 404 in production builds. main.go does not
// mount this handler under the production build, but the symbol must
// exist for shared route-registration code to compile cleanly.
func DevLoginHandler(w http.ResponseWriter, _ *http.Request) {
	http.NotFound(w, nil)
	_ = w
}
