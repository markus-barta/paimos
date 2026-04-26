package models

// Confidence tiers are inspired by code-review-graph's declared / derived /
// suggested model and shared across anchors and entity graph relations.
type EntityConfidence string

const (
	EntityConfidenceDeclared  EntityConfidence = "declared"
	EntityConfidenceDerived   EntityConfidence = "derived"
	EntityConfidenceSuggested EntityConfidence = "suggested"
)

type EntityType string

const (
	EntityTypeIssue    EntityType = "issue"
	EntityTypeAnchor   EntityType = "anchor"
	EntityTypeADR      EntityType = "adr"
	EntityTypeNFR      EntityType = "nfr"
	EntityTypeEnv      EntityType = "env"
	EntityTypeRepo     EntityType = "repo"
	EntityTypeSymbol   EntityType = "symbol"
	EntityTypeTag      EntityType = "tag"
	EntityTypeUser     EntityType = "user"
	EntityTypeLogEntry EntityType = "log_entry"
	EntityTypeSprint   EntityType = "sprint"
	EntityTypeCostUnit EntityType = "cost_unit"
	EntityTypeRelease  EntityType = "release"
	EntityTypeProject  EntityType = "project"
	EntityTypeManifest EntityType = "manifest"
)

type EntityEdgeType string

const (
	EntityEdgeParentOf        EntityEdgeType = "parent_of"
	EntityEdgeDependsOn       EntityEdgeType = "depends_on"
	EntityEdgeImpacts         EntityEdgeType = "impacts"
	EntityEdgeAnchoredToIssue EntityEdgeType = "anchored_to_issue"
	EntityEdgeAnchoredInside  EntityEdgeType = "anchored_inside"
	EntityEdgeInRepo          EntityEdgeType = "in_repo"
	EntityEdgeAffectsEnv      EntityEdgeType = "affects_env"
	EntityEdgeCitesIssue      EntityEdgeType = "cites_issue"
	EntityEdgeHasADR          EntityEdgeType = "has_adr"
	EntityEdgeHasNFR          EntityEdgeType = "has_nfr"
	EntityEdgeProjectUsesRepo EntityEdgeType = "project_uses_repo"
	EntityEdgeRelated         EntityEdgeType = "related"
	EntityEdgeBlocks          EntityEdgeType = "blocks"
)

var EntityEdgeDocs = map[EntityEdgeType]string{
	EntityEdgeParentOf:        "hierarchy relation between parent and child entities",
	EntityEdgeDependsOn:       "upstream dependency relation",
	EntityEdgeImpacts:         "change impact relation",
	EntityEdgeAnchoredToIssue: "anchor maps a code location to an issue",
	EntityEdgeAnchoredInside:  "anchor falls inside a derived symbol range",
	EntityEdgeInRepo:          "entity belongs to a linked repo",
	EntityEdgeAffectsEnv:      "entity affects a named environment",
	EntityEdgeCitesIssue:      "document or note cites an issue",
	EntityEdgeHasADR:          "project or entity links to an ADR",
	EntityEdgeHasNFR:          "project or entity links to an NFR",
	EntityEdgeProjectUsesRepo: "project declares a linked repo",
	EntityEdgeRelated:         "generic related issue relation",
	EntityEdgeBlocks:          "blocking issue relation",
}
