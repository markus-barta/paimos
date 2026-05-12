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

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/models"
)

type impersonationKeyType struct{}
type sessionIDKeyType struct{}

var impersonationKey = impersonationKeyType{}
var sessionIDKey = sessionIDKeyType{}

type ImpersonationState struct {
	Active bool
	Actor  *models.User
	Target *models.User
}

type ImpersonationResponse struct {
	Active bool         `json:"active"`
	Actor  *models.User `json:"actor,omitempty"`
	Target *models.User `json:"target,omitempty"`
}

func withImpersonation(ctx context.Context, actor, target *models.User, active bool) context.Context {
	if actor == nil {
		actor = target
	}
	return context.WithValue(ctx, impersonationKey, ImpersonationState{
		Active: active,
		Actor:  actor,
		Target: target,
	})
}

func withSessionID(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, sessionIDKey, sessionID)
}

func GetImpersonation(r *http.Request) ImpersonationState {
	if r == nil {
		return ImpersonationState{}
	}
	if imp, ok := r.Context().Value(impersonationKey).(ImpersonationState); ok {
		return imp
	}
	user := GetUser(r)
	return ImpersonationState{Actor: user, Target: user}
}

func IsImpersonating(r *http.Request) bool {
	return GetImpersonation(r).Active
}

func GetActor(r *http.Request) *models.User {
	imp := GetImpersonation(r)
	if imp.Actor != nil {
		return imp.Actor
	}
	return GetUser(r)
}

func SessionIDFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	if sid, ok := r.Context().Value(sessionIDKey).(string); ok && strings.TrimSpace(sid) != "" {
		return sid
	}
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return ""
	}
	return cookie.Value
}

type impersonationAuditResponseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *impersonationAuditResponseWriter) WriteHeader(status int) {
	rw.status = status
	rw.ResponseWriter.WriteHeader(status)
}

func (rw *impersonationAuditResponseWriter) Write(b []byte) (int, error) {
	if rw.status == 0 {
		rw.status = http.StatusOK
	}
	return rw.ResponseWriter.Write(b)
}

func (rw *impersonationAuditResponseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

func (rw *impersonationAuditResponseWriter) Flush() {
	if flusher, ok := rw.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func shouldAuditImpersonatedRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	switch r.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
	default:
		return false
	}
	return r.URL.Path != "/api/auth/impersonation/end"
}

func requestIDForImpersonationAudit(r *http.Request, h http.Header) string {
	for _, key := range []string{"X-PAIMOS-Request-Id", "X-PAIMOS-AI-Request-Id", "X-Request-Id"} {
		if v := strings.TrimSpace(r.Header.Get(key)); v != "" {
			return v
		}
		if h != nil {
			if v := strings.TrimSpace(h.Get(key)); v != "" {
				return v
			}
		}
	}
	return ""
}

func recordImpersonatedActionAudit(r *http.Request, rec *sessionRecord, status int, requestID string) {
	if rec == nil || !rec.impersonating || rec.actor == nil || rec.user == nil {
		return
	}
	if rec.actor.ID == rec.user.ID || !shouldAuditImpersonatedRequest(r) {
		return
	}
	if status == 0 {
		status = http.StatusOK
	}
	detailsJSON, err := json.Marshal(map[string]any{
		"action":      "impersonated_request",
		"status_code": status,
	})
	if err != nil {
		log.Printf("recordImpersonatedActionAudit: marshal details: %v", err)
		return
	}
	endpoint := strings.TrimSpace(r.Method + " " + r.URL.Path)
	if _, err := db.DB.ExecContext(r.Context(), `
		INSERT INTO super_admin_audit(actor_user_id, target_user_id, capability, endpoint, request_id, details_json)
		VALUES(?,?,?,?,?,?)
	`, rec.actor.ID, rec.user.ID, CapabilityImpersonationAction, endpoint, requestID, string(detailsJSON)); err != nil {
		log.Printf("recordImpersonatedActionAudit: insert actor_user_id=%d target_user_id=%d: %v", rec.actor.ID, rec.user.ID, err)
	}
}
