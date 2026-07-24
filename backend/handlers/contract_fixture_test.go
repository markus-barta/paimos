package handlers_test

import (
	"fmt"
	"net/http"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/inspr-at/paimos/backend/contracts"
)

func TestSchemaAlignmentFixture_RawHTTP(t *testing.T) {
	ts := newTestServer(t)
	projectID := responseID(t, ts.post(t, "/api/projects", ts.adminCookie, map[string]any{
		"name": "Contract Test",
		"key":  "TEST",
	}))

	fixture, err := contracts.LoadContractFixture(filepath.Join("..", "contracts", "fixtures", "schema_alignment.yaml"))
	if err != nil {
		t.Fatalf("LoadContractFixture: %v", err)
	}

	var sourceID, targetID int64
	for _, c := range fixture.Cases {
		if len(c.Expect.Normalized) > 0 {
			continue
		}
		switch c.Operation {
		case "issue.create":
			body := issueCreateFixtureBody(c.Inputs)
			resp := ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, body)
			if c.Expect.Error.Code != "" {
				assertProblem(t, resp, c.Expect.Error)
				continue
			}
			assertStatus(t, resp, http.StatusCreated)
			var got map[string]any
			decode(t, resp, &got)
			for key, want := range c.Expect.Body {
				if got[key] != want {
					t.Fatalf("%s: body[%q] = %v, want %v", c.Name, key, got[key], want)
				}
			}
		case "relation.add":
			if sourceID == 0 {
				sourceID = responseID(t, ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]any{
					"title": "Relation source",
					"type":  "ticket",
				}))
				targetID = responseID(t, ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]any{
					"title": "Relation target",
					"type":  "task",
				}))
			}
			resp := ts.post(t, fmt.Sprintf("/api/issues/%d/relations", sourceID), ts.adminCookie, map[string]any{
				"target_id": targetID,
				"type":      c.Inputs["type"],
			})
			assertProblem(t, resp, c.Expect.Error)
		}
	}
}

func issueCreateFixtureBody(inputs map[string]any) map[string]any {
	body := map[string]any{}
	for _, key := range []string{"title", "type", "status", "priority"} {
		if value, ok := inputs[key]; ok {
			body[key] = value
		}
	}
	return body
}

func assertProblem(t *testing.T, resp *http.Response, want contracts.ContractErrorExpected) {
	t.Helper()
	assertStatus(t, resp, want.Status)
	var got struct {
		Code        string   `json:"code"`
		Field       string   `json:"field"`
		ValidValues []string `json:"valid_values"`
		RequestID   string   `json:"request_id"`
	}
	decode(t, resp, &got)
	if got.Code != want.Code {
		t.Fatalf("code = %q, want %q", got.Code, want.Code)
	}
	if got.Field != want.Field {
		t.Fatalf("field = %q, want %q", got.Field, want.Field)
	}
	if !reflect.DeepEqual(got.ValidValues, want.ValidValues) {
		t.Fatalf("valid_values = %#v, want %#v", got.ValidValues, want.ValidValues)
	}
	if header := resp.Header.Get("X-PAIMOS-Request-Id"); header == "" || got.RequestID != header {
		t.Fatalf("request_id = %q, response header = %q", got.RequestID, header)
	}
}
