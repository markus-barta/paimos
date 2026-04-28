// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

//go:build !dev_login

// Package devseed contains the dev-fixture seeder used by `paimos
// dev-seed` (PAI-267). The real implementation lives in
// devseed_dev.go behind the `dev_login` build tag — production
// binaries do not contain the seeding code at all. Run is exported
// as a no-op stub here so main.go links cleanly without a build
// guard, but it can never actually be invoked: main.go gates on
// auth.DevLoginEnabled() which is also build-tag-gated.

package devseed

import "errors"

// Run is a no-op in production builds. main.go refuses to dispatch
// `dev-seed` unless auth.DevLoginEnabled() is true, but we return
// an error here as a belt + suspenders against future code paths
// that might call Run directly.
func Run() error {
	return errors.New("devseed.Run: this binary was not built with -tags dev_login")
}
