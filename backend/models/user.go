// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public
// License along with this program. If not, see <https://www.gnu.org/licenses/>.

package models

type User struct {
	ID         int64  `json:"id"`
	Username   string `json:"username"`
	Role       string `json:"role"`
	Status     string `json:"status"`
	CreatedAt  string `json:"created_at"`
	// Profile fields (migration 25)
	Nickname   string `json:"nickname"`
	FirstName  string `json:"first_name"`
	LastName   string `json:"last_name"`
	Email      string `json:"email"`
	AvatarPath string `json:"avatar_path"` // relative path, served under /static/
	// Editor preferences (migration 29)
	MarkdownDefault  bool `json:"markdown_default"`
	MonospaceFields  bool `json:"monospace_fields"`
	// Recent projects limit (migration 38)
	RecentProjectsLimit int `json:"recent_projects_limit"`
	// Internal hourly rate (migration 39)
	InternalRateHourly *float64 `json:"internal_rate_hourly"`
	// Alt-unit display preferences (migration 44)
	ShowAltUnitTable  bool `json:"show_alt_unit_table"`
	ShowAltUnitDetail bool `json:"show_alt_unit_detail"`
	// Locale (migration 47)
	Locale string `json:"locale"`
	// Recent timers limit (migration 49)
	RecentTimersLimit int `json:"recent_timers_limit"`
	// Display timezone (migration 50) — 'auto' = browser local, or IANA tz like 'UTC', 'Europe/Vienna'
	Timezone string `json:"timezone"`
	// Preview hover delay in ms (migration 53) — 0=instant, 1000=default
	PreviewHoverDelay int `json:"preview_hover_delay"`
	// Last login timestamp (migration 54)
	LastLoginAt *string `json:"last_login_at"`
	// 2FA status — populated by admin list/update endpoints
	TotpEnabled bool `json:"totp_enabled"`
	// Accruals report preferences (migration 62) — admin-only feature
	AccrualsStatsEnabled  bool   `json:"accruals_stats_enabled"`
	AccrualsExtraStatuses string `json:"accruals_extra_statuses"`
}

type UserWithPassword struct {
	User
	Password string `json:"-"`
}
