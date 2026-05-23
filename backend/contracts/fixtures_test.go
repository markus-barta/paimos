package contracts

import (
	"path/filepath"
	"testing"
)

func TestSchemaAlignmentFixtureLoads(t *testing.T) {
	fixture, err := LoadContractFixture(filepath.Join("fixtures", "schema_alignment.yaml"))
	if err != nil {
		t.Fatalf("LoadContractFixture: %v", err)
	}
	if fixture.Version != 1 {
		t.Fatalf("version = %d, want 1", fixture.Version)
	}
	if len(fixture.Cases) == 0 {
		t.Fatal("fixture has no cases")
	}
	seen := map[string]bool{}
	for _, c := range fixture.Cases {
		if c.Name == "" || c.Operation == "" {
			t.Fatalf("case missing name/operation: %+v", c)
		}
		if seen[c.Name] {
			t.Fatalf("duplicate case name %q", c.Name)
		}
		seen[c.Name] = true
	}
}
