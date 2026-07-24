package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/inspr-at/paimos/backend/contracts"
)

func TestSchemaAlignmentFixture_CLI(t *testing.T) {
	t.Setenv(envURL, "https://example.test")
	t.Setenv(envAPIKey, "test_key")

	fixture, err := contracts.LoadContractFixture(filepath.Join("..", "..", "contracts", "fixtures", "schema_alignment.yaml"))
	if err != nil {
		t.Fatalf("LoadContractFixture: %v", err)
	}

	for _, c := range fixture.Cases {
		switch c.Operation {
		case "issue.create":
			if len(c.Expect.Normalized) > 0 {
				out, _, err := executeCLIForTest(t,
					"--json",
					"issue", "create",
					"--project", stringInput(c.Inputs, "project_key"),
					"--title", stringInput(c.Inputs, "title"),
					"--type", stringInput(c.Inputs, "type"),
					"--priority", stringInput(c.Inputs, "priority"),
					"--dry-run",
				)
				if err != nil {
					t.Fatalf("%s: executeCLIForTest: %v", c.Name, err)
				}
				var got struct {
					Body map[string]any `json:"body"`
				}
				if err := json.Unmarshal([]byte(out), &got); err != nil {
					t.Fatalf("%s: decode dry-run: %v\n%s", c.Name, err, out)
				}
				for key, want := range c.Expect.Normalized {
					if got.Body[key] != want {
						t.Fatalf("%s: body[%q] = %v, want %v", c.Name, key, got.Body[key], want)
					}
				}
				continue
			}
			if c.Expect.Error.Code != "" {
				_, _, err := executeCLIForTest(t,
					"issue", "create",
					"--project", stringInput(c.Inputs, "project_key"),
					"--title", stringInput(c.Inputs, "title"),
					"--type", stringInput(c.Inputs, "type"),
					"--dry-run",
				)
				if err == nil {
					t.Fatalf("%s: expected local enum validation error", c.Name)
				}
				if !strings.Contains(err.Error(), c.Expect.Error.Field) || !strings.Contains(err.Error(), "task") {
					t.Fatalf("%s: error = %q, want field + valid values", c.Name, err.Error())
				}
			}
		case "relation.add":
			_, _, err := executeCLIForTest(t,
				"relation", "add",
				stringInput(c.Inputs, "source"),
				stringInput(c.Inputs, "type"),
				stringInput(c.Inputs, "target"),
			)
			if err == nil {
				t.Fatalf("%s: expected local relation enum validation error", c.Name)
			}
			if !strings.Contains(err.Error(), c.Expect.Error.Field) || !strings.Contains(err.Error(), "blocks") {
				t.Fatalf("%s: error = %q, want field + valid values", c.Name, err.Error())
			}
		}
	}
}

func stringInput(inputs map[string]any, key string) string {
	if raw, ok := inputs[key].(string); ok {
		return raw
	}
	return ""
}
