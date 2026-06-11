package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"

	"github.com/markus-barta/paimos/backend/db"
)

func computeIssueListETag(whereSQL string, args []any) (string, error) {
	// PAI-577: contentRev = SUM(issues.content_rev) over the matched set.
	// content_rev is bumped by triggers whenever data the list renders from
	// other tables changes (time entries → booked, tag assignment, sprint
	// membership, tag rename). MAX(updated_at) only reflects the issues row
	// itself, so without this the ETag was blind to booked/time changes and
	// kept serving stale rows via 304. SUM (not MAX) is required: a
	// non-maximal row incrementing must still move the aggregate.
	query := `SELECT COALESCE(MAX(i.updated_at), '0'), COUNT(*), COALESCE(SUM(i.content_rev), 0) FROM issues i WHERE ` + whereSQL
	var maxUpdated string
	var total int
	var contentRev int64
	// #nosec G701 -- whereSQL is composed from fixed fragments; user values are placeholders.
	if err := db.DB.QueryRow(query, args...).Scan(&maxUpdated, &total, &contentRev); err != nil {
		// PAI-283: surface the underlying SQL error so operators can diagnose
		// "etag computation failed" 500s instead of guessing at the cause.
		// whereSQL + args length are diagnostic but bounded (no PII leaks
		// beyond what's already in the request).
		log.Printf("computeIssueListETag: %v (whereSQL=%q args=%d)", err, whereSQL, len(args))
		return "", err
	}
	h := sha256.New()
	fmt.Fprintf(h, "%s|%d|%d|%s", maxUpdated, total, contentRev, whereSQL)
	for _, arg := range args {
		fmt.Fprintf(h, "|%v", arg)
	}
	return `W/"` + hex.EncodeToString(h.Sum(nil)[:16]) + `"`, nil
}

func applyIssueListConditionalGET(w http.ResponseWriter, r *http.Request, whereSQL string, args []any) (bool, error) {
	etag, err := computeIssueListETag(whereSQL, args)
	if err != nil {
		return false, err
	}
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "private, must-revalidate")
	if inm := r.Header.Get("If-None-Match"); inm != "" && etagMatches(inm, etag) {
		w.WriteHeader(http.StatusNotModified)
		return true, nil
	}
	return false, nil
}
