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

package auth

// Capability describes a single action that can be performed within a
// project, annotated with which access levels unlock it. Used by the
// /api/permissions/matrix endpoint to render the permissions settings
// page — the matrix is derived from this list server-side so the
// frontend stays in sync automatically as new capabilities ship.
type Capability struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Viewer      bool   `json:"viewer"`
	Editor      bool   `json:"editor"`
	Admin       bool   `json:"admin"`
}

// Capabilities is the canonical list rendered by the permissions matrix.
// The order controls render order in the UI.
var Capabilities = []Capability{
	{
		Key: "project.view", Label: "View project",
		Description: "See the project and its issues, reports, and members.",
		Viewer:      true, Editor: true, Admin: true,
	},
	{
		Key: "project.edit", Label: "Edit project",
		Description: "Change name, description, rates, and other settings.",
		Viewer:      false, Editor: false, Admin: true,
	},
	{
		Key: "project.delete", Label: "Delete / archive project",
		Description: "Archive or soft-delete the project.",
		Viewer:      false, Editor: false, Admin: true,
	},
	{
		Key: "issue.view", Label: "View issues",
		Description: "Read issues, their descriptions, and history.",
		Viewer:      true, Editor: true, Admin: true,
	},
	{
		Key: "issue.create", Label: "Create issues",
		Description: "Add new tickets, tasks, or epics.",
		Viewer:      false, Editor: true, Admin: true,
	},
	{
		Key: "issue.edit", Label: "Edit issues",
		Description: "Change status, estimates, tags, and other fields.",
		Viewer:      false, Editor: true, Admin: true,
	},
	{
		Key: "issue.delete", Label: "Delete issues",
		Description: "Permanently remove issues from the project.",
		Viewer:      false, Editor: false, Admin: true,
	},
	{
		Key: "comment.create", Label: "Post comments",
		Description: "Add comments on issues.",
		Viewer:      false, Editor: true, Admin: true,
	},
	{
		Key: "attachment.upload", Label: "Upload attachments",
		Description: "Attach files to issues.",
		Viewer:      false, Editor: true, Admin: true,
	},
	{
		Key: "attachment.download", Label: "Download attachments",
		Description: "Read file attachments.",
		Viewer:      true, Editor: true, Admin: true,
	},
	{
		Key: "time.log", Label: "Log time entries",
		Description: "Record work time against issues.",
		Viewer:      false, Editor: true, Admin: true,
	},
	{
		Key: "time.view", Label: "View time entries",
		Description: "See time logged by others.",
		Viewer:      true, Editor: true, Admin: true,
	},
	{
		Key: "report.export", Label: "Export reports",
		Description: "Download CSV exports and delivery reports.",
		Viewer:      true, Editor: true, Admin: true,
	},
	{
		Key: "members.manage", Label: "Manage members",
		Description: "Change per-project access levels for other users.",
		Viewer:      false, Editor: false, Admin: true,
	},
}
