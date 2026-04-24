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

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/markus-barta/paimos/backend/auth"
	"github.com/markus-barta/paimos/backend/brand"
	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/handlers"
	"github.com/markus-barta/paimos/backend/handlers/crm"
	"github.com/markus-barta/paimos/backend/storage"

	// CRM provider plugins. Blank-import each provider so its init()
	// registers it with the crm package's registry. Adding a new
	// provider = one line here + one new subpackage under crm/.
	_ "github.com/markus-barta/paimos/backend/handlers/crm/hubspot"
)

func main() {
	if err := db.Open(); err != nil {
		log.Fatalf("db: %v", err)
	}
	seedAdmin()
	handlers.EnsureAtRiskTag()

	if err := storage.Init(); err != nil {
		log.Printf("WARN: MinIO init failed (attachments disabled): %v", err)
	} else if storage.Enabled() {
		log.Printf("MinIO connected: bucket=%s", storage.Bucket)
	}

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Compress(5))
	// PAI-114: baseline security headers on every response. Non-breaking
	// (X-Frame-Options=SAMEORIGIN keeps the in-app PDF preview iframes
	// working, CSP runs in Report-Only mode until PAI-118 lands).
	r.Use(handlers.SecurityHeaders)
	// PAI-97: session-scoped mutation audit. No-op unless
	// PAIMOS_AUDIT_SESSIONS=true is set — off by default in v1.
	r.Use(handlers.SessionAuditMiddleware)

	r.Route("/api", func(r chi.Router) {
		// ── Strictly public endpoints ────────────────────────────────
		// Everything here is accessible without a session. Keep this
		// list short and auditable — the only valid reasons for a
		// route to live outside the auth group are:
		//   (a) health checks for Docker / CI / monitoring, or
		//   (b) the login page needs it before any session can exist, or
		//   (c) agent-discovery endpoints the CLI / MCP fetch before
		//       any API key is issued (e.g. /api/schema).
		// Audited 2026-04-21.
		r.Get("/health", healthHandler)                // (a) Docker + CI
		r.Get("/branding", handlers.GetBranding)       // (b) login page logo + colors
		r.Get("/schema", handlers.GetAPISchema)        // (c) CLI / MCP discovery (PAI-87)
		// PAI-114: CSP violation reports. Browser-driven, unauthenticated.
		r.Post("/csp-report", handlers.CSPReport)

		// Auth (public — no session possible yet)
		r.Post("/auth/login", auth.LoginHandler)
		r.Post("/auth/totp/verify", auth.TOTPVerify)

		// Forgot / reset password — all three must stay public since a
		// user who has forgotten their password has no session cookie.
		r.Post("/auth/forgot", handlers.ForgotPassword)
		r.Get("/auth/reset/validate", handlers.ValidateResetToken)
		r.Post("/auth/reset", handlers.ResetPassword)

		// Auth (authenticated but open to all roles, including external)
		r.Group(func(r chi.Router) {
			r.Use(auth.Middleware)
			r.Use(auth.CSRFMiddleware) // PAI-113

			r.Post("/auth/logout", auth.LogoutHandler)
			r.Get("/auth/me", auth.MeHandler)
			r.Post("/auth/password", auth.ChangePassword)
			r.Patch("/auth/me", handlers.UpdateProfile)
			r.Post("/auth/avatar", handlers.UploadAvatar)
			r.Delete("/auth/avatar", handlers.DeleteAvatar)

			// TOTP 2FA (authenticated)
			r.Get("/auth/totp/status", auth.TOTPStatus)
			r.Get("/auth/totp/setup",  auth.TOTPSetup)
			r.Post("/auth/totp/enable",  auth.TOTPEnable)
			r.Post("/auth/totp/disable", auth.TOTPDisable)

			// API keys
			r.Get("/auth/api-keys", handlers.ListAPIKeys)
			r.Post("/auth/api-keys", handlers.CreateAPIKey)
			r.Delete("/auth/api-keys/{id}", handlers.DeleteAPIKey)

			// moved inside the auth group. Any authed role
			// (including external/portal users) may fetch these — they
			// are non-sensitive but there's no good reason for them to
			// be reachable pre-authentication.
			r.Get("/instance", instanceHandler)
			r.Get("/brandings", handlers.ListBrandings)
			r.Get("/logos/{filename}", serveLogoHandler)
			r.Get("/avatars/{filename}", serveAvatarHandler)
		})

		// Portal (external + admin)
		r.Group(func(r chi.Router) {
			r.Use(auth.Middleware)
			r.Use(auth.CSRFMiddleware) // PAI-113
			r.Use(auth.RequirePortalAccess)

			r.Get("/portal/projects", handlers.PortalListProjects)
			r.Get("/portal/projects/{id}", handlers.PortalGetProject)
			r.Get("/portal/projects/{id}/issues", handlers.PortalListIssues)
			r.Get("/portal/projects/{id}/issues/{issueId}", handlers.PortalGetIssue)
			r.Post("/portal/projects/{id}/requests", handlers.PortalSubmitRequest)
			r.Post("/portal/issues/{id}/accept", handlers.PortalAcceptIssue)
			r.Post("/portal/issues/{id}/reject", handlers.PortalRejectIssue)
			r.Post("/portal/issues/{id}/undo-accept", handlers.PortalUndoAccept)
			r.Post("/portal/issues/{id}/undo-reject", handlers.PortalUndoReject)
			r.Get("/portal/projects/{id}/summary", handlers.PortalProjectSummary)
			r.Get("/portal/projects/{id}/acceptance-report", handlers.AcceptanceReport)
		})

		// Internal (admin + member; blocked for external)
		r.Group(func(r chi.Router) {
			r.Use(auth.Middleware)
			r.Use(auth.CSRFMiddleware) // PAI-113
			r.Use(auth.BlockExternal)

			// Projects
			r.Get("/projects", handlers.ListProjects)
			r.With(auth.RequireAdmin).Post("/projects", handlers.CreateProject)
			r.With(auth.RequireProjectView).Get("/projects/{id}", handlers.GetProject)
			r.With(auth.RequireAdmin, auth.RequireProjectView).Put("/projects/{id}", handlers.UpdateProject)
			r.With(auth.RequireAdmin, auth.RequireProjectView).Delete("/projects/{id}", handlers.DeleteProject)
			r.With(auth.RequireAdmin, auth.RequireProjectView).Post("/projects/{id}/logo", handlers.UploadProjectLogo)
			r.With(auth.RequireAdmin, auth.RequireProjectView).Delete("/projects/{id}/logo", handlers.DeleteProjectLogo)

			// Project key suggestion
			r.Get("/projects/suggest-key", handlers.SuggestProjectKey)

			// Issues nested under project
			r.With(auth.RequireProjectView).Get("/projects/{id}/issues", handlers.ListIssues)
			r.With(auth.RequireProjectEdit).Post("/projects/{id}/issues", handlers.CreateIssue)
			r.With(auth.RequireProjectView).Get("/projects/{id}/issues/tree", handlers.GetIssueTree)
			r.With(auth.RequireProjectView).Get("/projects/{id}/cost-units", handlers.ListCostUnits)
			r.With(auth.RequireProjectView).Get("/projects/{id}/releases", handlers.ListReleases)
			r.With(auth.RequireProjectView).Get("/projects/{id}/export/csv", handlers.ExportCSV)
			r.With(auth.RequireProjectView).Get("/projects/{id}/acceptance-log", handlers.AcceptanceLog)
			r.With(auth.RequireProjectView).Get("/projects/{id}/acceptance-report", handlers.AcceptanceReport)
			r.With(auth.RequireProjectView).Get("/projects/{id}/reports/lieferbericht", handlers.GetLieferbericht)
			r.With(auth.RequireProjectView).Get("/projects/{id}/reports/lieferbericht/pdf", handlers.GetLieferberichtPDF)
			// Project accruals (Vorräte) — admin only
			r.With(auth.RequireAdmin).Get("/reports/accruals", handlers.GetAccruals)
			r.With(auth.RequireAdmin, auth.RequireProjectView).Post("/projects/{id}/import/csv/preflight", handlers.ImportCSVPreflight)
			r.With(auth.RequireAdmin, auth.RequireProjectView).Post("/projects/{id}/import/csv", handlers.ImportCSV)
			r.With(auth.RequireAdmin, auth.RequireProjectView).Get("/projects/{id}/time-entries/users", handlers.PurgeUsers)
			r.With(auth.RequireAdmin, auth.RequireProjectView).Post("/projects/{id}/time-entries/purge-preview", handlers.PurgePreview)
			r.With(auth.RequireAdmin, auth.RequireProjectView).Post("/projects/{id}/time-entries/purge", handlers.PurgeTimeEntries)
			r.With(auth.RequireAdmin).Post("/import/csv/preflight", handlers.ImportCSVGlobalPreflight)
			r.With(auth.RequireAdmin).Post("/import/csv", handlers.ImportCSVGlobal)

			// Dashboard recent activity (must be before /issues/{id})
			r.Get("/issues/recent", handlers.RecentIssues)
			// Trash (soft-deleted issues) — admin-only (must be before /issues/{id})
			r.With(auth.RequireAdmin).Get("/issues/trash", handlers.ListTrashIssues)
			// Cross-project issue list + orphan sprint creation (must be before /issues/{id})
			// ListOrLookupIssues dispatches to the key-pick handler when ?keys= is
			// present (PAI-88) and falls through to ListAllIssues otherwise.
			r.Get("/issues", handlers.ListOrLookupIssues)
			r.Post("/issues", handlers.CreateOrphanIssue)

			// Bulk issue ops (PAI-88). Admin-only for v1 — simpler to reason
			// about than cross-project per-item access checks, and the primary
			// caller is the paimos CLI run under an admin key. Single-issue
			// endpoints still serve non-admins.
			r.With(auth.RequireAdmin).Patch("/issues", handlers.UpdateIssuesBatch)
			r.With(auth.RequireAdmin).Post("/projects/{key}/issues/batch", handlers.CreateIssuesBatch)

			// Sprints
			r.Get("/sprints", handlers.ListSprints)
			r.Get("/sprints/years", handlers.ListSprintYears)
			r.Get("/sprints/{year}", handlers.ListSprintsByYear)
			r.With(auth.RequireAdmin).Post("/sprints/batch", handlers.CreateSprintsBatch)
			r.With(auth.RequireAdmin).Put("/sprints/{id}", handlers.UpdateSprint)
			r.With(auth.RequireAdmin).Post("/sprints/{id}/move-incomplete", handlers.MoveIncompleteToNextSprint)
			r.With(auth.RequireAdmin).Put("/sprints/{id}/reorder", handlers.ReorderSprintMembers)
			// Cross-project distinct values
			r.Get("/cost-units", handlers.ListAllCostUnits)
			r.Get("/releases", handlers.ListAllReleases)

			// Issues by ID
			r.With(auth.RequireIssueAccess).Get("/issues/{id}", handlers.GetIssue)
			r.With(auth.RequireIssueEdit).Put("/issues/{id}", handlers.UpdateIssue)
			r.With(auth.RequireAdmin, auth.RequireIssueAccess).Delete("/issues/{id}", handlers.DeleteIssue)
			r.With(auth.RequireAdmin, auth.RequireIssueAccess).Post("/issues/{id}/restore", handlers.RestoreIssue)
			r.With(auth.RequireAdmin, auth.RequireIssueAccess).Delete("/issues/{id}/purge", handlers.PurgeIssue)
			r.With(auth.RequireAdmin, auth.RequireIssueAccess).Patch("/issues/{id}/archive", handlers.ArchiveIssue)
			// Attachments
			r.With(auth.RequireIssueAccess).Get("/issues/{id}/attachments", handlers.ListAttachments)
			r.With(auth.RequireIssueEdit).Post("/issues/{id}/attachments", handlers.UploadAttachment)
			r.With(auth.RequireAttachmentAccess).Get("/attachments/{id}", handlers.GetAttachmentFile)
			r.With(auth.RequireAttachmentEdit).Delete("/attachments/{id}", handlers.DeleteAttachment)
			r.Post("/attachments", handlers.UploadPendingAttachment)
			r.Patch("/attachments/link", handlers.LinkAttachments)

			r.With(auth.RequireIssueEdit).Post("/issues/{id}/clone", handlers.CloneIssue)
			r.With(auth.RequireIssueAccess).Get("/issues/{id}/aggregation", handlers.GetIssueAggregation)
			r.With(auth.RequireIssueAccess).Get("/issues/{id}/children", handlers.GetIssueChildren)
			r.With(auth.RequireIssueAccess).Get("/issues/{id}/history", handlers.GetIssueHistory)
			r.With(auth.RequireIssueEdit).Post("/issues/{id}/complete-epic", handlers.CompleteEpic)

			// Issue relations (v2)
			r.With(auth.RequireIssueAccess).Get("/issues/{id}/relations", handlers.ListIssueRelations)
			r.With(auth.RequireIssueEdit).Post("/issues/{id}/relations", handlers.CreateIssueRelation)
			r.With(auth.RequireAdmin, auth.RequireIssueAccess).Delete("/issues/{id}/relations", handlers.DeleteIssueRelation)
			r.With(auth.RequireIssueAccess).Get("/issues/{id}/members", handlers.ListIssuesByRelation)

			// Time entries (v2)
			r.With(auth.RequireIssueAccess).Get("/issues/{id}/time-entries", handlers.ListTimeEntries)
			r.With(auth.RequireIssueEdit).Post("/issues/{id}/time-entries", handlers.CreateTimeEntry)
			r.With(auth.RequireTimeEntryEdit).Put("/time-entries/{id}", handlers.UpdateTimeEntry)
			r.With(auth.RequireTimeEntryEdit).Delete("/time-entries/{id}", handlers.DeleteTimeEntry)
			r.Get("/time-entries/running", handlers.GetRunningTimers)
			r.Get("/time-entries/recent", handlers.GetRecentTimers)

			// System tag rules (admin only)
			r.Get("/system-tag-rules", handlers.ListSystemTagRules)
			r.With(auth.RequireAdmin).Put("/system-tag-rules", handlers.UpdateSystemTagRule)

			// Recent projects (current user)
			r.Get("/users/me/recent-projects", handlers.GetRecentProjects)
			r.Post("/users/me/recent-projects", handlers.UpsertRecentProject)

			// Users (admin only for create/update/delete)
			r.Get("/users", handlers.ListUsers)
			r.With(auth.RequireAdmin).Post("/users", handlers.CreateUser)
			r.With(auth.RequireAdmin).Put("/users/{id}", handlers.UpdateUser)
			r.With(auth.RequireAdmin).Post("/users/{id}/disable", handlers.DisableUser)
			r.With(auth.RequireAdmin).Delete("/users/{id}", handlers.DeleteUser)
			r.With(auth.RequireAdmin).Post("/users/{id}/reset-totp", handlers.ResetUserTOTP)

			// User project access (admin only). The legacy /users/{id}/projects
			// endpoints drive the external-portal grants page; the new
			// /users/{id}/memberships endpoints drive the full matrix editor
			// (with viewer/editor/none levels). Both write the same table.
			r.With(auth.RequireAdmin).Get("/users/{id}/projects", handlers.ListUserProjects)
			r.With(auth.RequireAdmin).Post("/users/{id}/projects", handlers.AddUserProject)
			r.With(auth.RequireAdmin).Delete("/users/{id}/projects/{projectId}", handlers.RemoveUserProject)
			r.With(auth.RequireAdmin).Get("/users/{id}/memberships", handlers.ListUserMemberships)
			r.With(auth.RequireAdmin).Put("/users/{id}/memberships/{projectId}", handlers.UpsertUserMembership)
			r.With(auth.RequireAdmin).Delete("/users/{id}/memberships/{projectId}", handlers.DeleteUserMembership)

			// Permissions matrix (read-only; any authenticated internal user
			// may view it, but the settings page is admin-gated on the UI side).
			r.Get("/permissions/matrix", handlers.GetPermissionsMatrix)

			// Access-change audit log (admin only).
			r.With(auth.RequireAdmin).Get("/access-audit", handlers.ListAccessAudit)

			// Session-scoped mutation audit (PAI-97). Admin only. Returns
			// the mutations recorded under a session, keyset-paginated by
			// `id > cursor`. Empty array for unknown sessions.
			r.With(auth.RequireAdmin).Get("/sessions/{id}/activity", handlers.GetSessionActivity)

			// Tags (admin only for write)
			r.Get("/tags", handlers.ListTags)
			r.With(auth.RequireAdmin).Post("/tags", handlers.CreateTag)
			r.With(auth.RequireAdmin).Put("/tags/{id}", handlers.UpdateTag)
			r.With(auth.RequireAdmin).Delete("/tags/{id}", handlers.DeleteTag)

			// Tag associations
			r.With(auth.RequireIssueEdit).Post("/issues/{id}/tags", handlers.AddTagToIssue)
			r.With(auth.RequireIssueEdit).Delete("/issues/{id}/tags/{tag_id}", handlers.RemoveTagFromIssue)
			r.With(auth.RequireProjectEdit).Post("/projects/{id}/tags", handlers.AddTagToProject)
			r.With(auth.RequireProjectEdit).Delete("/projects/{id}/tags/{tag_id}", handlers.RemoveTagFromProject)

			// Comments
			r.With(auth.RequireIssueAccess).Get("/issues/{id}/comments", handlers.ListComments)
			r.With(auth.RequireIssueEdit).Post("/issues/{id}/comments", handlers.CreateComment)
			r.With(auth.RequireCommentAccess).Delete("/comments/{id}", handlers.DeleteComment)

			// Branding write endpoints (admin only). GET /api/branding stays
			// public (see the public group above) because the login page needs
			// it pre-auth. Writes are admin-gated; assets are served by
			// /brand/{filename} at the root, below.
			r.With(auth.RequireAdmin).Put("/branding", handlers.PutBranding)
			r.With(auth.RequireAdmin).Post("/branding/logo", handlers.UploadBrandingLogo)
			r.With(auth.RequireAdmin).Post("/branding/favicon", handlers.UploadBrandingFavicon)

			// Integrations (admin only for write)
			r.Get("/integrations/jira", handlers.GetJiraIntegration)
			r.With(auth.RequireAdmin).Put("/integrations/jira", handlers.PutJiraIntegration)
			r.With(auth.RequireAdmin).Post("/integrations/jira/test", handlers.TestJiraIntegration)

			// Mite integration
			r.Get("/integrations/mite", handlers.GetMiteIntegration)
			r.With(auth.RequireAdmin).Put("/integrations/mite", handlers.PutMiteIntegration)
			r.With(auth.RequireAdmin).Post("/integrations/mite/test", handlers.TestMiteIntegration)

			// CRM provider plugin layer (PAI-101). All admin-only.
			// The plugin layer does not assume any specific provider —
			// `crm.List()` only returns whatever was blank-imported in
			// this binary.
			r.With(auth.RequireAdmin).Get("/integrations/crm", crm.ListProviders)
			r.With(auth.RequireAdmin).Get("/integrations/crm/{id}/config", crm.GetProviderConfig)
			r.With(auth.RequireAdmin).Put("/integrations/crm/{id}/config", crm.PutProviderConfig)
			r.With(auth.RequireAdmin).Put("/integrations/crm/{id}/enabled", crm.PutProviderEnabled)

			// Customers (PAI-53). CRM-agnostic CRUD; manual customers
			// fully supported (no provider required).
			r.Get("/customers", handlers.ListCustomers)
			r.Get("/customers/{id}", handlers.GetCustomer)
			r.With(auth.RequireAdmin).Post("/customers", handlers.CreateCustomer)
			r.With(auth.RequireAdmin).Put("/customers/{id}", handlers.UpdateCustomer)
			r.With(auth.RequireAdmin).Delete("/customers/{id}", handlers.DeleteCustomer)
			// Provider-driven import / sync (PAI-103).
			r.With(auth.RequireAdmin).Post("/customers/import", crm.ImportCustomer)
			r.With(auth.RequireAdmin).Post("/customers/{id}/sync", crm.SyncCustomer)

			// Documents (PAI-55). Scoped under customer / project for
			// list + upload; unscoped paths for read / mutate / delete
			// since documents are uniquely id-addressable.
			r.Get("/customers/{id}/documents", handlers.ListCustomerDocuments)
			r.With(auth.RequireAdmin).Post("/customers/{id}/documents", handlers.UploadCustomerDocument)
			r.With(auth.RequireProjectView).Get("/projects/{id}/documents", handlers.ListProjectDocuments)
			r.With(auth.RequireAdmin, auth.RequireProjectView).Post("/projects/{id}/documents", handlers.UploadProjectDocument)
			r.Get("/documents/{id}/download", handlers.DownloadDocument)
			r.With(auth.RequireAdmin).Put("/documents/{id}", handlers.UpdateDocument)
			r.With(auth.RequireAdmin).Delete("/documents/{id}", handlers.DeleteDocument)

			// Cooperation metadata (PAI-61). Per-project engagement
			// profile; informational only in v1.
			r.With(auth.RequireProjectView).Get("/projects/{id}/cooperation", handlers.GetCooperation)
			r.With(auth.RequireAdmin, auth.RequireProjectView).Put("/projects/{id}/cooperation", handlers.PutCooperation)

			// Mite import
			r.With(auth.RequireAdmin).Post("/import/mite", handlers.ImportFromMite)
			r.With(auth.RequireAdmin).Get("/import/mite/jobs/{id}", handlers.GetMiteImportJobStatus)
			r.With(auth.RequireAdmin).Post("/import/mite/jobs/{id}/cancel", handlers.CancelMiteImportJob)
			r.With(auth.RequireAdmin).Get("/import/mite/resume-date", handlers.GetMiteResumeDate)
			r.With(auth.RequireAdmin).Delete("/import/mite/entries", handlers.DeleteMiteEntries)

			// Jira import
			r.With(auth.RequireAdmin).Get("/import/jira/projects", handlers.ListJiraProjects)
			r.With(auth.RequireAdmin).Get("/import/jira/debug", handlers.DebugJiraFetch)
			r.With(auth.RequireAdmin).Post("/import/jira", handlers.ImportFromJira)
			r.With(auth.RequireAdmin).Get("/import/jira/jobs/{id}", handlers.GetImportJobStatus)

			// Views (saved column+filter sets)
			r.Get("/views", handlers.ListViews)
			r.Post("/views", handlers.CreateView)
			r.Put("/views/{id}", handlers.UpdateView)
			r.Delete("/views/{id}", handlers.DeleteView)
			r.With(auth.RequireAdmin).Patch("/views/order", handlers.ReorderViews)
			r.Post("/views/{id}/pin", handlers.PinView)
			r.Delete("/views/{id}/pin", handlers.UnpinView)

			// Search
			r.Get("/search", handlers.Search)

			// Dev panel (admin only)
			r.With(auth.RequireAdmin).Get("/dev/test-reports", handlers.ListTestReports)
			r.With(auth.RequireAdmin).Get("/dev/test-reports/summary", handlers.GetTestReportSummary)
			r.With(auth.RequireAdmin).Get("/dev/test-reports/{filename}", handlers.GetTestReport)
		})
	})

	// Project logos + avatars are served from DATA_DIR subdirs, which
	// are volume-mounted and survive container rebuilds. The handlers
	// themselves are defined as package-level funcs (serveLogoHandler,
	// serveAvatarHandler below) so they can be wired inside the
	// authenticated route group via the chi r.Get() above.

	// Public branding assets — the login page needs the logo/favicon
	// before any session exists. Path is /brand/<filename>, served from
	// $DATA_DIR/branding-assets/ with a strict filename whitelist. Must
	// be registered BEFORE the SPA catchall ("/*") below, otherwise
	// chi's route matcher falls through to index.html.
	r.Get("/brand/{filename}", handlers.ServeBrandingAsset)

	// Serve static frontend files (SPA fallback)
	staticDir := getStaticDir()
	if _, err := os.Stat(staticDir); err == nil {
		fileServer := http.FileServer(http.Dir(staticDir))
		indexPath := filepath.Join(staticDir, "index.html")
		r.Handle("/*", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := filepath.Join(staticDir, r.URL.Path)
			info, err := os.Stat(path)
			if os.IsNotExist(err) || (info != nil && info.IsDir()) {
				// SPA fallback (or root /): serve index.html with no-cache so
				// browsers always fetch the latest version after deploys.
				w.Header().Set("Cache-Control", "no-cache")
				http.ServeFile(w, r, indexPath)
				return
			}
			// Cache strategy under /assets/:
			//   - Vite-hashed bundle files (e.g. `index-D6h5dtXR.js`)
			//     have a content hash in the filename, so they're safe
			//     to cache immutably — content can't change for a given
			//     URL.
			//   - Stable-name files dropped in via `public/` (e.g.
			//     `crm/hubspot.svg`) keep the same URL across versions,
			//     so caching them immutably is wrong: a future re-skin
			//     under the same name would never reach the user, and
			//     a transient 404 (e.g. asset added in v1.6.1 after a
			//     v1.6.0 visit) would also be locked in.
			// Hash check: Vite emits `<name>-<8+ char base64ish>.<ext>`.
			if len(r.URL.Path) > 8 && r.URL.Path[:8] == "/assets/" {
				if isViteHashed(r.URL.Path) {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				} else {
					// 1 hour cache for stable-name assets — long enough
					// to be CDN-friendly, short enough to recover from
					// a typo/404 within a coffee break.
					w.Header().Set("Cache-Control", "public, max-age=3600")
				}
			}
			fileServer.ServeHTTP(w, r)
		}))
	}

	port := getPort()
	fmt.Printf("%s server starting on :%s\n", brand.Default.ProductName, port)
	log.Fatal(http.ListenAndServe(":"+port, r))
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "service": brand.Default.HealthServiceName})
}

func instanceHandler(w http.ResponseWriter, r *http.Request) {
	label := os.Getenv("INSTANCE_LABEL")
	hostname, _ := os.Hostname()
	w.Header().Set("Content-Type", "application/json")
	// `attachments_enabled` lets the SPA hide drop zones on instances that
	// don't have MinIO wired up — otherwise the first upload surfaces a
	// 503 with a stuck 0% progress bar. See ACME-1.
	json.NewEncoder(w).Encode(map[string]any{
		"label":               label,
		"hostname":            hostname,
		"attachments_enabled": storage.Enabled(),
	})
}

// serveLogoHandler + serveAvatarHandler serve image files from
// DATA_DIR/logos/ and DATA_DIR/avatars/ respectively. Both are
// registered inside the auth.Middleware group by main() so any
// unauthenticated request returns 401 before reaching the file system.
// Filename whitelist is kept intentionally tight: digits, lowercase,
// dot only — this is defence-in-depth against path traversal even
// though chi.URLParam already decodes away slashes.
func serveLogoHandler(w http.ResponseWriter, r *http.Request) {
	filename := chi.URLParam(r, "filename")
	for _, c := range filename {
		if !((c >= '0' && c <= '9') || c == '.' || (c >= 'a' && c <= 'z')) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
	}
	http.ServeFile(w, r, filepath.Join(getDataDir(), "logos", filename))
}

func serveAvatarHandler(w http.ResponseWriter, r *http.Request) {
	filename := chi.URLParam(r, "filename")
	for _, c := range filename {
		if !((c >= '0' && c <= '9') || c == '.' || (c >= 'a' && c <= 'z')) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
	}
	http.ServeFile(w, r, filepath.Join(getDataDir(), "avatars", filename))
}

func seedAdmin() {
	var count int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM users").Scan(&count); err != nil {
		log.Printf("seed: failed to query user count: %v", err)
		return
	}
	if count > 0 {
		return
	}
	password := os.Getenv("ADMIN_PASSWORD")
	if password == "" {
		log.Println("seed: ADMIN_PASSWORD missing; refusing to create default admin user")
		return
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		log.Printf("seed: hash error: %v", err)
		return
	}
	_, err = db.DB.Exec(
		"INSERT INTO users(username,password,role) VALUES(?,?,?)",
		"admin", hash, "admin",
	)
	if err != nil {
		log.Printf("seed: insert error: %v", err)
		return
	}
	log.Println("seed: created admin user (username: admin)")
}

func getPort() string {
	if port := os.Getenv("PORT"); port != "" {
		return port
	}
	return "8888"
}

func getStaticDir() string {
	if dir := os.Getenv("STATIC_DIR"); dir != "" {
		return dir
	}
	return "/app/static"
}

func getDataDir() string {
	if dir := os.Getenv("DATA_DIR"); dir != "" {
		return dir
	}
	return "/app/data"
}

// viteHashedRe matches Vite's bundled-asset filename pattern:
// `<name>-<8+ char base62 hash>.<ext>`. Hashed files are immutable —
// the URL changes when content does, so the cache can never go stale.
// Stable-name files dropped via `public/` (e.g. `crm/hubspot.svg`)
// don't match and get a short cache so they can be re-skinned.
var viteHashedRe = regexp.MustCompile(`-[A-Za-z0-9_-]{8,}\.[a-z0-9]+$`)

func isViteHashed(urlPath string) bool {
	return viteHashedRe.MatchString(urlPath)
}
