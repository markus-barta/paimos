package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/markus-barta/paimos/backend/contracts"
)

func TestSchemaAlignmentFixture_MCP(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("MCP validation fixture should fail before HTTP, got %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(ts.Close)
	srv := &Server{client: testMCPClient(t, ts.URL), logger: func(string, ...any) {}}

	fixture, err := contracts.LoadContractFixture(filepath.Join("..", "..", "contracts", "fixtures", "schema_alignment.yaml"))
	if err != nil {
		t.Fatalf("LoadContractFixture: %v", err)
	}

	for _, c := range fixture.Cases {
		if c.Expect.Error.Code == "" {
			continue
		}
		var callErr error
		switch c.Operation {
		case "issue.create":
			_, callErr = srv.toolIssueCreate(map[string]any{
				"project_key": stringInput(c.Inputs, "project_key"),
				"title":       stringInput(c.Inputs, "title"),
				"type":        stringInput(c.Inputs, "type"),
			})
		case "relation.add":
			_, callErr = srv.toolRelationAdd(map[string]any{
				"source": stringInput(c.Inputs, "source"),
				"type":   stringInput(c.Inputs, "type"),
				"target": stringInput(c.Inputs, "target"),
			})
		}
		if callErr == nil {
			t.Fatalf("%s: expected MCP enum validation error", c.Name)
		}
		if !strings.Contains(callErr.Error(), c.Expect.Error.Field) {
			t.Fatalf("%s: error = %q, want field %q", c.Name, callErr.Error(), c.Expect.Error.Field)
		}
	}
}

func TestToolSchemasExposeSchemaEnums(t *testing.T) {
	srv := &Server{logger: func(string, ...any) {}}
	tools := map[string]Tool{}
	for _, tool := range srv.tools() {
		tools[tool.Name] = tool
	}
	assertToolEnum(t, tools["paimos_issue_create"], "type", contracts.IssueTypes)
	assertToolEnum(t, tools["paimos_issue_create"], "status", contracts.IssueStatuses)
	assertToolEnum(t, tools["paimos_issue_create"], "priority", contracts.IssuePriorities)
	assertToolEnum(t, tools["paimos_issue_update"], "type", contracts.IssueTypes)
	assertToolEnum(t, tools["paimos_issue_list"], "status", contracts.IssueStatuses)
	assertToolEnum(t, tools["paimos_relation_add"], "type", contracts.RelationTypes)
}

func assertToolEnum(t *testing.T, tool Tool, field string, want []string) {
	t.Helper()
	props, _ := tool.InputSchema["properties"].(map[string]any)
	schema, _ := props[field].(map[string]any)
	raw, _ := schema["enum"].([]string)
	if len(raw) != len(want) {
		t.Fatalf("%s.%s enum len = %d, want %d (%#v)", tool.Name, field, len(raw), len(want), raw)
	}
	for i := range want {
		if raw[i] != want[i] {
			t.Fatalf("%s.%s enum[%d] = %q, want %q", tool.Name, field, i, raw[i], want[i])
		}
	}
}

func stringInput(inputs map[string]any, key string) string {
	if raw, ok := inputs[key].(string); ok {
		return raw
	}
	return ""
}
