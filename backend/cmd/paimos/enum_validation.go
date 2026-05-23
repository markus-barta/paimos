package main

import (
	"fmt"
	"strings"

	"github.com/markus-barta/paimos/backend/contracts"
)

func normalizeEnumValue(binding, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	schema, err := activeSchema()
	if err != nil {
		return raw, err
	}
	domain := schema.EnumFields[binding]
	if domain == "" {
		domain = fallbackEnumDomain(binding)
	}
	values := schema.Enums[domain]
	if len(values) == 0 {
		return raw, nil
	}
	for _, value := range values {
		if raw == value {
			return raw, nil
		}
	}
	for _, value := range values {
		if strings.EqualFold(raw, value) {
			return value, nil
		}
	}
	return "", &usageError{msg: fmt.Sprintf("%s must be one of: %s", bindingField(binding), strings.Join(values, ", "))}
}

func activeSchema() (*CachedSchema, error) {
	name, _, err := resolveActiveInstance()
	if err != nil {
		return fallbackCachedSchema(), nil
	}
	if schema, err := loadCachedSchema(name); err == nil && schema != nil {
		return schema, nil
	}
	return fallbackCachedSchema(), nil
}

func fallbackCachedSchema() *CachedSchema {
	return &CachedSchema{
		Enums: map[string][]string{
			"status":           append([]string(nil), contracts.IssueStatuses...),
			"knowledge_status": append([]string(nil), contracts.KnowledgeStatuses...),
			"priority":         append([]string(nil), contracts.IssuePriorities...),
			"type":             append([]string(nil), contracts.IssueTypes...),
			"relation":         append([]string(nil), contracts.RelationTypes...),
		},
		EnumFields: map[string]string{
			"issue.type":       "type",
			"issue.status":     "status",
			"issue.priority":   "priority",
			"relation.type":    "relation",
			"knowledge.status": "knowledge_status",
		},
	}
}

func fallbackEnumDomain(binding string) string {
	switch binding {
	case "issue.type":
		return "type"
	case "issue.status":
		return "status"
	case "issue.priority":
		return "priority"
	case "relation.type":
		return "relation"
	case "tag.color":
		return "tag_colors"
	case "knowledge.type":
		return "knowledge_types"
	case "knowledge.status":
		return "knowledge_status"
	default:
		return bindingField(binding)
	}
}

func bindingField(binding string) string {
	if i := strings.LastIndex(binding, "."); i >= 0 && i+1 < len(binding) {
		return binding[i+1:]
	}
	return binding
}
