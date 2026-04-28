/*
 * PAIMOS — Your Professional & Personal AI Project OS
 * Copyright (C) 2026 Markus Barta <markus@barta.com>
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as
 * published by the Free Software Foundation, version 3.
 */

/**
 * PAI-267 — single source of truth for the "is this a dev fixture user?"
 * predicate. Dev users created by `paimos dev-seed` follow the
 * `dev_*` username convention (dev_admin / dev_editor / dev_viewer /
 * dev_outsider). Production user pickers use this helper to filter
 * them out so fixture rows never pollute real assignment dropdowns
 * or member lists.
 *
 * The convention is enforced server-side (the seeder hard-codes
 * those four usernames) so the regex here is the contract — change
 * it only in lock-step with backend/devseed/devseed_dev.go.
 */
export function isDevFixtureUser(username: string | null | undefined): boolean {
  if (!username) return false
  return username.startsWith('dev_')
}
