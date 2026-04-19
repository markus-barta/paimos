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
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type rateLimitEntry struct {
	Count       int
	WindowStart time.Time
	LastSeen    time.Time
}

type rateLimiter struct {
	mux     sync.Mutex
	entries map[string]rateLimitEntry
}

var authLimiter = &rateLimiter{entries: map[string]rateLimitEntry{}}

func (r *rateLimiter) Check(key string, now time.Time) (bool, time.Duration) {
	r.mux.Lock()
	defer r.mux.Unlock()

	r.pruneLocked(now)

	entry, ok := r.entries[key]
	if !ok || now.Sub(entry.WindowStart) >= authRateLimitWindow {
		return true, 0
	}

	entry.LastSeen = now
	if entry.Count >= authRateLimitMaxAttempts {
		r.entries[key] = entry
		retryAfter := authRateLimitWindow - now.Sub(entry.WindowStart)
		if retryAfter < 0 {
			retryAfter = 0
		}
		return false, retryAfter
	}

	r.entries[key] = entry
	return true, 0
}

func (r *rateLimiter) RecordFailure(key string, now time.Time) {
	r.mux.Lock()
	defer r.mux.Unlock()

	r.pruneLocked(now)

	entry, ok := r.entries[key]
	if !ok || now.Sub(entry.WindowStart) >= authRateLimitWindow {
		r.entries[key] = rateLimitEntry{Count: 1, WindowStart: now, LastSeen: now}
		return
	}

	entry.Count++
	entry.LastSeen = now
	r.entries[key] = entry
}

func (r *rateLimiter) Reset(key string) {
	r.mux.Lock()
	defer r.mux.Unlock()
	delete(r.entries, key)
}

func (r *rateLimiter) pruneLocked(now time.Time) {
	cutoff := now.Add(-2 * authRateLimitWindow)
	for key, entry := range r.entries {
		if entry.LastSeen.Before(cutoff) {
			delete(r.entries, key)
		}
	}
}

func clientIP(r *http.Request) string {
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}
	if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
		return realIP
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

func authAttemptKeys(scope string, r *http.Request, identity string) []string {
	keys := []string{scope + ":ip:" + clientIP(r)}
	identity = strings.TrimSpace(strings.ToLower(identity))
	if identity != "" {
		keys = append(keys, scope+":id:"+identity)
	}
	return keys
}

func allowAuthAttempt(scope string, r *http.Request, identity string) (bool, time.Duration) {
	now := time.Now()
	var longest time.Duration
	for _, key := range authAttemptKeys(scope, r, identity) {
		allowed, retryAfter := authLimiter.Check(key, now)
		if !allowed {
			if retryAfter > longest {
				longest = retryAfter
			}
			return false, longest
		}
	}
	return true, 0
}

func recordAuthFailure(scope string, r *http.Request, identity string) {
	now := time.Now()
	for _, key := range authAttemptKeys(scope, r, identity) {
		authLimiter.RecordFailure(key, now)
	}
}

func resetAuthFailures(scope string, r *http.Request, identity string) {
	for _, key := range authAttemptKeys(scope, r, identity) {
		authLimiter.Reset(key)
	}
}

func setRetryAfter(w http.ResponseWriter, retryAfter time.Duration) {
	seconds := int(retryAfter.Seconds())
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
}

func truncateToken(token string) string {
	token = strings.TrimSpace(token)
	if len(token) <= 8 {
		return token
	}
	return token[:8]
}

// ── Exported wrappers for use from outside the auth package ───────────────
// These let other packages (e.g., handlers for the password-reset flow)
// share the same IP + identity rate-limit window as login, without
// reaching into unexported internals.

// AllowAttempt returns (allowed, retryAfter). The scope string keys the
// limiter so that different flows (login, forgot, reset) share the same
// 5-per-10-minutes window per IP+identity but are tracked independently.
func AllowAttempt(scope string, r *http.Request, identity string) (bool, time.Duration) {
	return allowAuthAttempt(scope, r, identity)
}

// RecordFailure increments the failure counter for (scope, IP, identity).
func RecordFailure(scope string, r *http.Request, identity string) {
	recordAuthFailure(scope, r, identity)
}

// ResetFailures clears the counters for (scope, IP, identity). Call after
// a successful operation so honest users aren't penalised for earlier
// fat-fingers.
func ResetFailures(scope string, r *http.Request, identity string) {
	resetAuthFailures(scope, r, identity)
}

// SetRetryAfter writes a Retry-After header based on the rate-limiter's
// suggested wait.
func SetRetryAfter(w http.ResponseWriter, retryAfter time.Duration) {
	setRetryAfter(w, retryAfter)
}

// ClientIP extracts the best-guess client IP from forwarded headers /
// RemoteAddr. Useful for audit logging in other packages.
func ClientIP(r *http.Request) string {
	return clientIP(r)
}
