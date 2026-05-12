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

package handlers

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"

	"github.com/markus-barta/paimos/backend/auth"
	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/models"
)

func StartImpersonation(w http.ResponseWriter, r *http.Request) {
	actor := auth.GetUser(r)
	if actor == nil {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if auth.IsImpersonating(r) {
		jsonError(w, "already impersonating", http.StatusConflict)
		return
	}
	if !auth.HasCapability(r.Context(), actor, auth.CapabilityImpersonationStart) {
		jsonError(w, "forbidden", http.StatusForbidden)
		return
	}
	sessionID := auth.SessionIDFromRequest(r)
	if sessionID == "" {
		jsonError(w, "session required", http.StatusUnauthorized)
		return
	}

	var body struct {
		UserID       int64 `json:"user_id"`
		TargetUserID int64 `json:"target_user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}
	targetID := body.UserID
	if targetID == 0 {
		targetID = body.TargetUserID
	}
	if targetID <= 0 {
		jsonError(w, "user_id required", http.StatusBadRequest)
		return
	}
	if targetID == actor.ID {
		jsonError(w, "cannot impersonate yourself", http.StatusBadRequest)
		return
	}

	tx, err := db.DB.BeginTx(r.Context(), nil)
	if err != nil {
		jsonError(w, "transaction failed", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	var target models.User
	if err := scanUser(tx.QueryRowContext(r.Context(), "SELECT "+userSelectCols+" FROM users WHERE id=?", targetID), &target); err != nil {
		if err == sql.ErrNoRows {
			jsonError(w, "target user not found", http.StatusNotFound)
			return
		}
		log.Printf("StartImpersonation: load target_user_id=%d: %v", targetID, err)
		jsonError(w, "target lookup failed", http.StatusInternalServerError)
		return
	}
	if target.Status != "active" {
		jsonError(w, "target user is not active", http.StatusBadRequest)
		return
	}

	res, err := tx.ExecContext(r.Context(), `
		UPDATE sessions
		SET actor_user_id = ?, acting_as_user_id = ?
		WHERE id = ? AND user_id = ? AND acting_as_user_id IS NULL
	`, actor.ID, target.ID, sessionID, actor.ID)
	if err != nil {
		log.Printf("StartImpersonation: update session actor_user_id=%d target_user_id=%d: %v", actor.ID, target.ID, err)
		jsonError(w, "session update failed", http.StatusInternalServerError)
		return
	}
	if rows, _ := res.RowsAffected(); rows != 1 {
		jsonError(w, "session is no longer eligible", http.StatusConflict)
		return
	}

	if err := recordSuperAdminAuditTx(r.Context(), tx, r, actor, target.ID, auth.CapabilityImpersonationStart, map[string]any{
		"action":          "impersonation_start",
		"actor_username":  actor.Username,
		"actor_role":      actor.Role,
		"target_username": target.Username,
		"target_role":     target.Role,
	}); err != nil {
		log.Printf("StartImpersonation: audit actor_user_id=%d target_user_id=%d: %v", actor.ID, target.ID, err)
		jsonError(w, "audit failed", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		jsonError(w, "commit failed", http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]bool{"ok": true})
}

func EndImpersonation(w http.ResponseWriter, r *http.Request) {
	imp := auth.GetImpersonation(r)
	if !imp.Active || imp.Actor == nil || imp.Target == nil {
		jsonError(w, "not impersonating", http.StatusConflict)
		return
	}
	if !auth.HasCapability(r.Context(), imp.Actor, auth.CapabilityImpersonationEnd) {
		jsonError(w, "forbidden", http.StatusForbidden)
		return
	}
	sessionID := auth.SessionIDFromRequest(r)
	if sessionID == "" {
		jsonError(w, "session required", http.StatusUnauthorized)
		return
	}

	tx, err := db.DB.BeginTx(r.Context(), nil)
	if err != nil {
		jsonError(w, "transaction failed", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(r.Context(), `
		UPDATE sessions
		SET actor_user_id = NULL, acting_as_user_id = NULL
		WHERE id = ? AND user_id = ? AND acting_as_user_id = ?
	`, sessionID, imp.Actor.ID, imp.Target.ID)
	if err != nil {
		log.Printf("EndImpersonation: update session actor_user_id=%d target_user_id=%d: %v", imp.Actor.ID, imp.Target.ID, err)
		jsonError(w, "session update failed", http.StatusInternalServerError)
		return
	}
	if rows, _ := res.RowsAffected(); rows != 1 {
		jsonError(w, "not impersonating", http.StatusConflict)
		return
	}

	if err := recordSuperAdminAuditTx(r.Context(), tx, r, imp.Actor, imp.Target.ID, auth.CapabilityImpersonationEnd, map[string]any{
		"action":          "impersonation_end",
		"actor_username":  imp.Actor.Username,
		"actor_role":      imp.Actor.Role,
		"target_username": imp.Target.Username,
		"target_role":     imp.Target.Role,
	}); err != nil {
		log.Printf("EndImpersonation: audit actor_user_id=%d target_user_id=%d: %v", imp.Actor.ID, imp.Target.ID, err)
		jsonError(w, "audit failed", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		jsonError(w, "commit failed", http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]bool{"ok": true})
}
