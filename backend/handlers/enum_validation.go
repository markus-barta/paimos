package handlers

import (
	"fmt"
	"net/http"
	"strings"
)

type enumViolation struct {
	Field       string
	Value       string
	Domain      string
	ValidValues []string
}

func (e *enumViolation) Error() string {
	return fmt.Sprintf("%s %q is not valid; expected one of: %s", e.Field, e.Value, strings.Join(e.ValidValues, ", "))
}

func validateEnumField(binding, value string) *enumViolation {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	domain := Schema.EnumFields[binding]
	if domain == "" {
		return &enumViolation{Field: binding, Value: value, Domain: "", ValidValues: nil}
	}
	valid := Schema.Enums[domain]
	for _, candidate := range valid {
		if value == candidate {
			return nil
		}
	}
	return &enumViolation{
		Field:       bindingField(binding),
		Value:       value,
		Domain:      domain,
		ValidValues: append([]string(nil), valid...),
	}
}

func bindingField(binding string) string {
	if i := strings.LastIndex(binding, "."); i >= 0 && i+1 < len(binding) {
		return binding[i+1:]
	}
	return binding
}

func writeEnumViolation(w http.ResponseWriter, r *http.Request, ev *enumViolation) {
	if ev == nil {
		return
	}
	problemJSON(w, r, ProblemDetails{
		Type:        "https://paimos.com/errors/enum_violation",
		Title:       "Invalid enum value",
		Status:      http.StatusBadRequest,
		Detail:      ev.Error(),
		Code:        "enum_violation",
		Field:       ev.Field,
		ValidValues: ev.ValidValues,
	})
}
