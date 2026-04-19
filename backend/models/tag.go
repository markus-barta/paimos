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

type Tag struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description"`
	System      bool   `json:"system"`
	CreatedAt   string `json:"created_at"`
}

type SystemTagRule struct {
	ID               int64   `json:"id"`
	TagID            int64   `json:"tag_id"`
	ConditionType    string  `json:"condition_type"`
	Threshold        float64 `json:"threshold"`
	ExcludedStatuses string  `json:"excluded_statuses"`
	CreatedAt        string  `json:"created_at"`
}
