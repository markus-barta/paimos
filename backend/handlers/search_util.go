package handlers

import (
	"strings"
	"unicode"
)

// sanitizeFTS5Token converts arbitrary user input into a valid FTS5
// prefix query. FTS5 has its own query language inside the MATCH
// parameter where characters like `/`, `"`, `(`, `)`, `:`, `*`, `^`,
// `-` are operators — passing raw user input crashes the FTS5 parser
// with `fts5: syntax error near "<char>"` (1) and the surrounding SQL
// query returns a generic SQL logic error to the caller. (Note: this
// is a parse-error / availability bug, NOT a SQL-injection vector —
// args still flow through `?` placeholders / prepared statements.)
//
// Strategy: strip everything that isn't a letter or digit, collapse
// runs of whitespace, append `*` for prefix matching. The substring
// LIKE fallback in the surrounding UNION clauses already covers
// matches the FTS5 branch can't reach with this conservative
// sanitization. ok=false signals the caller should drop the FTS5
// branch entirely (input had zero usable token content).
//
// PAI-283 phase 2.
func sanitizeFTS5Token(s string) (token string, ok bool) {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteRune(' ')
		}
	}
	cleaned := strings.TrimSpace(b.String())
	if cleaned == "" {
		return "", false
	}
	return strings.Join(strings.Fields(cleaned), " ") + "*", true
}
