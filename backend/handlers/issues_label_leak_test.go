package handlers_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/markus-barta/paimos/backend/db"
)

// PAI-602 — a soft-deleted cost_unit/release container must not leak its label
// onto member tickets (it's already gone from the picker dropdown). After the
// container is trashed, the member's cost_unit ref must be nil, not a
// half-populated {id, label:""} ghost.
func TestIssue_SoftDeletedCostUnitLabelDoesNotLeak(t *testing.T) {
	ts := newTestServer(t)
	pid := createTestProject(t, ts, "LabelLeak", "LEAK")
	id := responseID(t, ts.post(t, fmt.Sprintf("/api/projects/%d/issues", pid), ts.adminCookie, map[string]any{
		"title": "member", "type": "ticket", "cost_unit": "ENG",
	}))

	type labelRef struct {
		ID    int64  `json:"id"`
		Label string `json:"label"`
	}
	getCU := func() *labelRef {
		resp := ts.get(t, fmt.Sprintf("/api/issues/%d", id), ts.adminCookie)
		assertStatus(t, resp, http.StatusOK)
		var out struct {
			CostUnit *labelRef `json:"cost_unit"`
		}
		decode(t, resp, &out)
		return out.CostUnit
	}

	// Before deletion the label is present.
	if cu := getCU(); cu == nil || cu.Label != "ENG" {
		t.Fatalf("cost_unit should be ENG before deletion, got %#v", cu)
	}

	// Soft-delete the ENG container issue.
	var containerID int64
	if err := db.DB.QueryRow(`SELECT id FROM issues WHERE project_id=? AND type='cost_unit' AND title='ENG'`, pid).Scan(&containerID); err != nil {
		t.Fatalf("find container: %v", err)
	}
	if _, err := db.DB.Exec(`UPDATE issues SET deleted_at=datetime('now') WHERE id=?`, containerID); err != nil {
		t.Fatalf("soft-delete container: %v", err)
	}

	// The label must no longer surface on the member ticket.
	if cu := getCU(); cu != nil {
		t.Fatalf("a soft-deleted cost_unit must not leak onto the member ticket, got %#v", cu)
	}
}
