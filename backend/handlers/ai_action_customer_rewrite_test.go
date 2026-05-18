package handlers_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

// TestSummaryActionsInCatalog verifies the two PAI-418 actions are
// registered. We don't dial the upstream provider here (no API key in
// CI); the dispatcher test below covers the empty-issue path that
// short-circuits before any provider call.
func TestSummaryActionsInCatalog(t *testing.T) {
	ts := newTestServer(t)

	resp := ts.get(t, "/api/ai/actions", ts.memberCookie)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	defer resp.Body.Close()

	var body struct {
		Actions []struct {
			Key         string `json:"key"`
			Surface     string `json:"surface"`
			Placement   string `json:"placement"`
			Implemented bool   `json:"implemented"`
		} `json:"actions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode catalog: %v", err)
	}

	// Both actions target the customer-facing report_summary field, so
	// both register on the customer surface — the AiActionMenu groups
	// them together when the menu is mounted on that field.
	want := map[string]struct {
		Surface     string
		Implemented bool
	}{
		"customer_rewrite": {Surface: "customer", Implemented: true},
		"exec_summary":     {Surface: "customer", Implemented: true},
	}
	for _, a := range body.Actions {
		w, ok := want[a.Key]
		if !ok {
			continue
		}
		if a.Surface != w.Surface {
			t.Errorf("%s surface=%q want %q", a.Key, a.Surface, w.Surface)
		}
		if a.Placement != "text" {
			t.Errorf("%s placement=%q want text", a.Key, a.Placement)
		}
		if a.Implemented != w.Implemented {
			t.Errorf("%s implemented=%v want %v", a.Key, a.Implemented, w.Implemented)
		}
		delete(want, a.Key)
	}
	if len(want) > 0 {
		t.Fatalf("catalog missing actions: %v", want)
	}
}
