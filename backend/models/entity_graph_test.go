package models

import "testing"

func TestEntityEdgeDocsCoverDeclaredEdgeTypes(t *testing.T) {
	declared := []EntityEdgeType{
		EntityEdgeParentOf,
		EntityEdgeDependsOn,
		EntityEdgeImpacts,
		EntityEdgeAnchoredToIssue,
		EntityEdgeAnchoredInside,
		EntityEdgeInRepo,
		EntityEdgeAffectsEnv,
		EntityEdgeCitesIssue,
		EntityEdgeHasADR,
		EntityEdgeHasNFR,
		EntityEdgeProjectUsesRepo,
		EntityEdgeRelated,
		EntityEdgeBlocks,
	}
	for _, edge := range declared {
		if EntityEdgeDocs[edge] == "" {
			t.Fatalf("missing documentation for edge %q", edge)
		}
	}
}
