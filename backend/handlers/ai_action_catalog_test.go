package handlers_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestAIActionCatalogIsMounted(t *testing.T) {
	ts := newTestServer(t)

	resp := ts.get(t, "/api/ai/actions", ts.memberCookie)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	defer resp.Body.Close()

	var body struct {
		Actions []struct {
			Key       string `json:"key"`
			Surface   string `json:"surface"`
			Placement string `json:"placement"`
		} `json:"actions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode catalog: %v", err)
	}
	if len(body.Actions) == 0 {
		t.Fatal("expected at least one AI action")
	}

	foundOptimize := false
	for _, a := range body.Actions {
		if a.Key != "optimize" {
			continue
		}
		foundOptimize = true
		if a.Surface != "issue" {
			t.Fatalf("optimize surface=%q want issue", a.Surface)
		}
		if a.Placement == "" {
			t.Fatal("optimize placement should not be empty")
		}
	}
	if !foundOptimize {
		t.Fatal("expected optimize action in catalog")
	}
}
