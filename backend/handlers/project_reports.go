package handlers

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/markus-barta/paimos/backend/auth"
	"github.com/markus-barta/paimos/backend/db"
)

type projectReportPermission struct {
	ID         int64  `json:"id"`
	ProjectID  int64  `json:"project_id"`
	PersonName string `json:"person_name"`
	Company    string `json:"company"`
	RoleLabel  string `json:"role_label"`
	MayApprove bool   `json:"may_approve"`
	MayDeliver bool   `json:"may_deliver"`
	MayAccept  bool   `json:"may_accept"`
	SortOrder  int    `json:"sort_order"`
}

type projectReportSnapshot struct {
	ID                int64          `json:"id"`
	ProjectID         int64          `json:"project_id"`
	ProjectKey        string         `json:"project_key"`
	ProjectName       string         `json:"project_name"`
	Code              string         `json:"code"`
	ReportKey         string         `json:"report_key"`
	ReportType        string         `json:"report_type"`
	Lang              string         `json:"lang"`
	FilterQuery       string         `json:"filter_query"`
	IssueIDs          []int64        `json:"issue_ids"`
	TotalIssues       int            `json:"total_issues"`
	PDFSHA256         string         `json:"pdf_sha256"`
	Status            string         `json:"status"`
	SignedDocumentID  *int64         `json:"signed_document_id"`
	SignedAt          *string        `json:"signed_at"`
	SignerName        string         `json:"signer_name"`
	SignerCompany     string         `json:"signer_company"`
	SignerRole        string         `json:"signer_role"`
	AcceptedAt        *string        `json:"accepted_at"`
	AcceptedBy        *int64         `json:"accepted_by"`
	AcceptSummary     map[string]int `json:"accept_summary"`
	AcceptanceURL     string         `json:"acceptance_url"`
	CreatedBy         *int64         `json:"created_by"`
	CreatedAt         string         `json:"created_at"`
	UpdatedAt         string         `json:"updated_at"`
	EligibleCount     int            `json:"eligible_count,omitempty"`
	AlreadyFinalCount int            `json:"already_final_count,omitempty"`
	SkippedCount      int            `json:"skipped_count,omitempty"`
}

func issueIDsForReport(report *lbReport) []int64 {
	ids := []int64{}
	for _, g := range report.Groups {
		for _, issue := range g.Issues {
			if issue.ID > 0 {
				ids = append(ids, issue.ID)
			}
		}
	}
	return ids
}

func createProjectReportSnapshot(r *http.Request, report *lbReport, lang, filterQuery, code string, pdfBytes []byte) error {
	if code == "" {
		code = randHex(5)
	}
	ids := issueIDsForReport(report)
	idsJSON, _ := json.Marshal(ids)
	sum := sha256.Sum256(pdfBytes)
	user := auth.GetUser(r)
	var userID *int64
	if user != nil {
		userID = &user.ID
	}
	reportKey := fmt.Sprintf("PB-%s-%s", report.ProjectKey, time.Now().UTC().Format("2006-01-02"))
	_, err := db.DB.Exec(`
		INSERT INTO project_report_snapshots(
			project_id, code, report_key, report_type, lang, filter_query,
			issue_ids_json, total_issues, pdf_sha256, created_by
		) VALUES (?, ?, ?, 'projektbericht', ?, ?, ?, ?, ?, ?)
	`, report.ProjectID, code, reportKey, lang, filterQuery, string(idsJSON), len(ids), hex.EncodeToString(sum[:]), userID)
	return err
}

func acceptanceURLForCode(r *http.Request, code string) string {
	base := reportRequestBaseURL(r)
	if base == "" || code == "" {
		return ""
	}
	return strings.TrimRight(base, "/") + "/accept/" + code
}

func ListProjectReportPermissions(w http.ResponseWriter, r *http.Request) {
	projectID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	out, err := loadProjectReportPermissions(projectID)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	jsonOK(w, out)
}

func loadProjectReportPermissions(projectID int64) ([]projectReportPermission, error) {
	rows, err := db.DB.Query(`
		SELECT id, project_id, person_name, company, role_label,
		       may_approve, may_deliver, may_accept, sort_order
		FROM project_report_permissions
		WHERE project_id=?
		ORDER BY sort_order, id
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []projectReportPermission{}
	for rows.Next() {
		var p projectReportPermission
		var approve, deliver, accept int
		if err := rows.Scan(&p.ID, &p.ProjectID, &p.PersonName, &p.Company, &p.RoleLabel, &approve, &deliver, &accept, &p.SortOrder); err != nil {
			return nil, err
		}
		p.MayApprove = approve != 0
		p.MayDeliver = deliver != 0
		p.MayAccept = accept != 0
		out = append(out, p)
	}
	return out, rows.Err()
}

func PutProjectReportPermissions(w http.ResponseWriter, r *http.Request) {
	projectID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	var rows []projectReportPermission
	if err := json.NewDecoder(r.Body).Decode(&rows); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}
	tx, err := db.DB.Begin()
	if err != nil {
		jsonError(w, "begin failed", http.StatusInternalServerError)
		return
	}
	if _, err := tx.Exec(`DELETE FROM project_report_permissions WHERE project_id=?`, projectID); err != nil {
		_ = tx.Rollback()
		jsonError(w, "delete failed", http.StatusInternalServerError)
		return
	}
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	for idx, row := range rows {
		if strings.TrimSpace(row.PersonName) == "" && strings.TrimSpace(row.Company) == "" {
			continue
		}
		if _, err := tx.Exec(`
			INSERT INTO project_report_permissions(
				project_id, person_name, company, role_label,
				may_approve, may_deliver, may_accept, sort_order, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, projectID, strings.TrimSpace(row.PersonName), strings.TrimSpace(row.Company), strings.TrimSpace(row.RoleLabel),
			boolInt(row.MayApprove), boolInt(row.MayDeliver), boolInt(row.MayAccept), idx, now, now); err != nil {
			_ = tx.Rollback()
			jsonError(w, "insert failed", http.StatusInternalServerError)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		jsonError(w, "commit failed", http.StatusInternalServerError)
		return
	}
	ListProjectReportPermissions(w, r)
}

func ListProjectReports(w http.ResponseWriter, r *http.Request) {
	projectID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	if !auth.CanViewProject(r, projectID) {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	rows, err := db.DB.Query(reportSnapshotSelect()+` WHERE prs.project_id=? ORDER BY prs.created_at DESC`, projectID)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	out := []projectReportSnapshot{}
	for rows.Next() {
		snap, err := scanProjectReportSnapshot(rows, r)
		if err != nil {
			jsonError(w, "scan failed", http.StatusInternalServerError)
			return
		}
		fillSnapshotCounts(&snap)
		out = append(out, snap)
	}
	jsonOK(w, out)
}

func GetProjectReportAcceptance(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(chi.URLParam(r, "code"))
	snap, err := loadProjectReportSnapshotByCode(code, r)
	if err != nil {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	if !auth.CanViewProject(r, snap.ProjectID) {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	fillSnapshotCounts(snap)
	jsonOK(w, snap)
}

func AcceptProjectReport(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(chi.URLParam(r, "code"))
	snap, err := loadProjectReportSnapshotByCode(code, r)
	if err != nil {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	if !auth.CanEditProject(r, snap.ProjectID) {
		jsonError(w, "forbidden", http.StatusForbidden)
		return
	}
	if snap.Status == "accepted" {
		fillSnapshotCounts(snap)
		jsonOK(w, snap)
		return
	}
	user := auth.GetUser(r)
	if user == nil {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	summary, err := acceptProjectReportIssues(r, snap, user.ID)
	if err != nil {
		jsonError(w, "accept failed", http.StatusInternalServerError)
		return
	}
	summaryJSON, _ := json.Marshal(summary)
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	if _, err := db.DB.Exec(`
		UPDATE project_report_snapshots
		   SET status='accepted', accepted_at=?, accepted_by=?,
		       accept_summary_json=?, updated_at=?
		 WHERE id=?
	`, now, user.ID, string(summaryJSON), now, snap.ID); err != nil {
		jsonError(w, "snapshot update failed", http.StatusInternalServerError)
		return
	}
	next, _ := loadProjectReportSnapshotByCode(code, r)
	fillSnapshotCounts(next)
	jsonOK(w, next)
}

func LinkProjectReportSignedArtifact(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(chi.URLParam(r, "code"))
	snap, err := loadProjectReportSnapshotByCode(code, r)
	if err != nil {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	if !auth.CanEditProject(r, snap.ProjectID) {
		jsonError(w, "forbidden", http.StatusForbidden)
		return
	}
	var body struct {
		DocumentID    *int64  `json:"document_id"`
		SignerName    string  `json:"signer_name"`
		SignerCompany string  `json:"signer_company"`
		SignerRole    string  `json:"signer_role"`
		SignedAt      *string `json:"signed_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}
	signedAt := body.SignedAt
	if signedAt == nil {
		now := time.Now().UTC().Format("2006-01-02 15:04:05")
		signedAt = &now
	}
	if body.DocumentID != nil {
		var exists int
		if err := db.DB.QueryRow(`SELECT 1 FROM documents WHERE id=? AND project_id=?`, *body.DocumentID, snap.ProjectID).Scan(&exists); err != nil {
			jsonError(w, "document not found for project", http.StatusBadRequest)
			return
		}
	}
	if _, err := db.DB.Exec(`
		UPDATE project_report_snapshots
		   SET signed_document_id=?, signed_at=?, signer_name=?, signer_company=?, signer_role=?, updated_at=datetime('now')
		 WHERE id=?
	`, body.DocumentID, signedAt, strings.TrimSpace(body.SignerName), strings.TrimSpace(body.SignerCompany), strings.TrimSpace(body.SignerRole), snap.ID); err != nil {
		jsonError(w, "save failed", http.StatusInternalServerError)
		return
	}
	next, _ := loadProjectReportSnapshotByCode(code, r)
	fillSnapshotCounts(next)
	jsonOK(w, next)
}

func GetProjectReportPDF(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(chi.URLParam(r, "code"))
	snap, err := loadProjectReportSnapshotByCode(code, r)
	if err != nil {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	if !auth.CanViewProject(r, snap.ProjectID) {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	report, err := buildLieferbericht(snap.ProjectID, "date_range", "", "", "", lbFilters{IssueIDs: snap.IssueIDs})
	if err != nil {
		jsonError(w, "report generation failed", http.StatusInternalServerError)
		return
	}
	pdf := renderLieferberichtPDF(report, lbRenderOpts{
		Lang:          snap.Lang,
		Cols:          defaultLBColSet(),
		BaseURL:       reportRequestBaseURL(r),
		ReportCode:    snap.Code,
		AcceptanceURL: acceptanceURLForCode(r, snap.Code),
	})
	writePDFResponse(w, pdf, snap.ReportKey+".pdf")
}

func acceptProjectReportIssues(r *http.Request, snap *projectReportSnapshot, userID int64) (map[string]int, error) {
	summary := map[string]int{"accepted": 0, "already_final": 0, "skipped": 0}
	tx, err := db.DB.Begin()
	if err != nil {
		return summary, err
	}
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	batchID := "projektbericht:" + snap.Code
	for _, id := range snap.IssueIDs {
		var status string
		var projectID int64
		if err := tx.QueryRow(`SELECT project_id, status FROM issues WHERE id=? AND deleted_at IS NULL`, id).Scan(&projectID, &status); err != nil {
			if err == sql.ErrNoRows {
				summary["skipped"]++
				continue
			}
			_ = tx.Rollback()
			return summary, err
		}
		if projectID != snap.ProjectID {
			summary["skipped"]++
			continue
		}
		if status == "accepted" || status == "invoiced" {
			summary["already_final"]++
			continue
		}
		if status != "done" && status != "delivered" {
			summary["skipped"]++
			continue
		}
		before, err := fetchIssueMutationSnapshotTx(tx, id)
		if err != nil {
			_ = tx.Rollback()
			return summary, err
		}
		if _, err := tx.Exec(`UPDATE issues SET status='accepted', accepted_at=?, accepted_by=?, updated_at=? WHERE id=?`, now, userID, now, id); err != nil {
			_ = tx.Rollback()
			return summary, err
		}
		after, err := fetchIssueMutationSnapshotTx(tx, id)
		if err != nil {
			_ = tx.Rollback()
			return summary, err
		}
		if _, err := recordMutation(r.Context(), tx, mutationRecordArgs{
			RequestID:    requestIDFromRequest(r),
			UserID:       &userID,
			SessionID:    sessionIDFromRequest(r),
			AgentName:    agentNameFromRequest(r),
			MutationType: "issue.update",
			SubjectType:  "issue",
			SubjectID:    id,
			BatchID:      batchID,
			InverseOp:    InverseOp{Method: "PATCH", Path: fmt.Sprintf("/api/issues/%d", id), Body: map[string]any{"status": status}},
			BeforeState:  before,
			AfterState:   after,
			Undoable:     true,
		}); err != nil {
			_ = tx.Rollback()
			return summary, err
		}
		summary["accepted"]++
	}
	if err := tx.Commit(); err != nil {
		return summary, err
	}
	user := auth.GetUser(r)
	for _, id := range snap.IssueIDs {
		if issue := getIssueByID(id); issue != nil && issue.Status == "accepted" {
			saveSnapshot(issue, user, r)
		}
	}
	return summary, nil
}

func reportSnapshotSelect() string {
	return `
		SELECT prs.id, prs.project_id, p.key, p.name, prs.code, prs.report_key,
		       prs.report_type, prs.lang, prs.filter_query, prs.issue_ids_json,
		       prs.total_issues, prs.pdf_sha256, prs.status, prs.signed_document_id,
		       prs.signed_at, prs.signer_name, prs.signer_company, prs.signer_role,
		       prs.accepted_at, prs.accepted_by, prs.accept_summary_json,
		       prs.created_by, prs.created_at, prs.updated_at
		FROM project_report_snapshots prs
		JOIN projects p ON p.id = prs.project_id`
}

type reportSnapshotScanner interface {
	Scan(dest ...any) error
}

func scanProjectReportSnapshot(row reportSnapshotScanner, r *http.Request) (projectReportSnapshot, error) {
	var snap projectReportSnapshot
	var issueIDsJSON, summaryJSON string
	err := row.Scan(&snap.ID, &snap.ProjectID, &snap.ProjectKey, &snap.ProjectName, &snap.Code, &snap.ReportKey,
		&snap.ReportType, &snap.Lang, &snap.FilterQuery, &issueIDsJSON,
		&snap.TotalIssues, &snap.PDFSHA256, &snap.Status, &snap.SignedDocumentID,
		&snap.SignedAt, &snap.SignerName, &snap.SignerCompany, &snap.SignerRole,
		&snap.AcceptedAt, &snap.AcceptedBy, &summaryJSON,
		&snap.CreatedBy, &snap.CreatedAt, &snap.UpdatedAt)
	if err != nil {
		return snap, err
	}
	_ = json.Unmarshal([]byte(issueIDsJSON), &snap.IssueIDs)
	_ = json.Unmarshal([]byte(summaryJSON), &snap.AcceptSummary)
	if snap.AcceptSummary == nil {
		snap.AcceptSummary = map[string]int{}
	}
	snap.AcceptanceURL = acceptanceURLForCode(r, snap.Code)
	return snap, nil
}

func loadProjectReportSnapshotByCode(code string, r *http.Request) (*projectReportSnapshot, error) {
	if code == "" {
		return nil, sql.ErrNoRows
	}
	row := db.DB.QueryRow(reportSnapshotSelect()+` WHERE prs.code=?`, code)
	snap, err := scanProjectReportSnapshot(row, r)
	if err != nil {
		return nil, err
	}
	return &snap, nil
}

func fillSnapshotCounts(snap *projectReportSnapshot) {
	if snap == nil || len(snap.IssueIDs) == 0 {
		return
	}
	snap.EligibleCount = 0
	snap.AlreadyFinalCount = 0
	snap.SkippedCount = 0
	for _, id := range snap.IssueIDs {
		var status string
		if err := db.DB.QueryRow(`SELECT status FROM issues WHERE id=? AND project_id=? AND deleted_at IS NULL`, id, snap.ProjectID).Scan(&status); err != nil {
			snap.SkippedCount++
			continue
		}
		switch status {
		case "done", "delivered":
			snap.EligibleCount++
		case "accepted", "invoiced":
			snap.AlreadyFinalCount++
		default:
			snap.SkippedCount++
		}
	}
}
