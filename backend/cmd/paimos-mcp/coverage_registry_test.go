package main

import "testing"

func TestRouteCoverageRegistry(t *testing.T) {
	allowed := map[coverageClassification]bool{
		coverageMCPTool:              true,
		coverageCoveredByTool:        true,
		coverageAgentReadonlyContext: true,
		coverageHumanAdminOnly:       true,
		coverageFileOrStream:         true,
		coverageUIInternal:           true,
		coverageDangerousDefault:     true,
		coverageDeferredGap:          true,
	}
	toolNames := map[string]bool{}
	for _, tool := range (&Server{}).tools() {
		toolNames[tool.Name] = true
	}
	seenRoutes := map[string]bool{}
	toolsWithRoute := map[string]bool{}
	for _, row := range routeCoverageRegistry {
		if row.Method == "" || row.Path == "" {
			t.Fatalf("coverage row missing method/path: %+v", row)
		}
		key := routeCoverageKey(row.Method, row.Path)
		if seenRoutes[key] {
			t.Fatalf("duplicate coverage row for %s", key)
		}
		seenRoutes[key] = true
		if !allowed[row.Classification] {
			t.Fatalf("%s has unknown classification %q", key, row.Classification)
		}
		if row.Reason == "" {
			t.Fatalf("%s is missing reason", key)
		}
		if row.Classification == coverageMCPTool {
			if row.Tool == "" {
				t.Fatalf("%s is mcp_tool but has no tool", key)
			}
			if !toolNames[row.Tool] {
				t.Fatalf("%s maps to unknown MCP tool %q", key, row.Tool)
			}
			toolsWithRoute[row.Tool] = true
		}
		if row.Classification == coverageDeferredGap && row.Ticket == "" {
			t.Fatalf("%s is deferred_gap but has no ticket", key)
		}
	}
	for tool := range toolNames {
		if !toolsWithRoute[tool] {
			t.Fatalf("MCP tool %q has no mcp_tool route coverage row", tool)
		}
	}
}
