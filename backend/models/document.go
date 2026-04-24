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

// Document is a customer- or project-scoped file upload (PAI-55).
//
// scope determines which of CustomerID / ProjectID is set; the other is
// nil. Enforced both at the DB level (CHECK constraint) and at the API
// layer.
type Document struct {
	ID          int64   `json:"id"`
	Scope       string  `json:"scope"`
	CustomerID  *int64  `json:"customer_id"`
	ProjectID   *int64  `json:"project_id"`
	Filename    string  `json:"filename"`
	MimeType    string  `json:"mime_type"`
	SizeBytes   int64   `json:"size_bytes"`
	ObjectKey   string  `json:"-"` // MinIO object key; never echoed to clients
	Label       string  `json:"label"`
	Status      string  `json:"status"`
	ValidFrom   *string `json:"valid_from"`
	ValidUntil  *string `json:"valid_until"`
	UploadedBy  *int64  `json:"uploaded_by"`
	UploadedAt  string  `json:"uploaded_at"`
	UpdatedAt   string  `json:"updated_at"`
}
