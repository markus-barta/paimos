package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/markus-barta/paimos/backend/cmd/paimos-mcp/mcpclient"
)

func TestToolsCallRoundTripRetrieveAndBlastRadius(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/projects", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"id":7,"key":"PAI"}]`))
	})
	mux.HandleFunc("/api/projects/7/retrieve", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"hits":[{"entity_type":"symbol","entity_id":42,"title":"function RetrieveProjectContext"}],"strategy":{"fusion":"rrf"}}`))
	})
	mux.HandleFunc("/api/projects/7/graph/blast-radius", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"root":{"entity_type":"issue","entity_id":11},"reached":{"symbol":[{"id":42,"title":"RetrieveProjectContext"}]}}`))
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	client := testMCPClient(t, ts.URL)
	srv := &Server{
		client: client,
		logger: func(string, ...any) {},
	}

	retrieveReq := rpcRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "tools/call",
		Params: mustJSON(t, map[string]any{
			"name": "paimos_retrieve",
			"arguments": map[string]any{
				"project": "PAI",
				"q":       "retrieve symbol context",
				"k":       5,
			},
		}),
	}
	retrieveResp := srv.dispatch(&retrieveReq)
	if retrieveResp.Error != nil {
		t.Fatalf("retrieve error: %#v", retrieveResp.Error)
	}
	retrieveText := extractToolText(t, retrieveResp.Result)
	if !strings.Contains(retrieveText, `"fusion":"rrf"`) {
		t.Fatalf("retrieve result missing rrf payload: %s", retrieveText)
	}

	blastReq := rpcRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`2`),
		Method:  "tools/call",
		Params: mustJSON(t, map[string]any{
			"name": "paimos_blast_radius",
			"arguments": map[string]any{
				"project": "PAI",
				"issue":   "PAI-79",
				"depth":   3,
			},
		}),
	}
	blastResp := srv.dispatch(&blastReq)
	if blastResp.Error != nil {
		t.Fatalf("blast radius error: %#v", blastResp.Error)
	}
	blastText := extractToolText(t, blastResp.Result)
	if !strings.Contains(blastText, `"symbol"`) {
		t.Fatalf("blast radius result missing symbol payload: %s", blastText)
	}
}

func testMCPClient(t *testing.T, baseURL string) *mcpclient.Client {
	t.Helper()
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.yaml")
	config := "default_instance: test\ninstances:\n  test:\n    url: " + baseURL + "\n    api_key: test-key\n"
	if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	prevConfig := os.Getenv("PAIMOS_CONFIG")
	prevInstance := os.Getenv("PAIMOS_INSTANCE")
	t.Cleanup(func() {
		_ = os.Setenv("PAIMOS_CONFIG", prevConfig)
		_ = os.Setenv("PAIMOS_INSTANCE", prevInstance)
	})
	if err := os.Setenv("PAIMOS_CONFIG", configPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Setenv("PAIMOS_INSTANCE", "test"); err != nil {
		t.Fatal(err)
	}
	client, err := mcpclient.NewFromConfig()
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func extractToolText(t *testing.T, result any) string {
	t.Helper()
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Content) == 0 {
		t.Fatalf("missing content in result: %s", string(raw))
	}
	return payload.Content[0].Text
}
