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
	query := `SELECT COALESCE(MAX(i.updated_at), '0'), COUNT(*) FROM issues i WHERE ` + whereSQL
	var maxUpdated string
	var total int
	if err := db.DB.QueryRow(query, args...).Scan(&maxUpdated, &total); err != nil {
		// PAI-283: surface the underlying SQL error so operators can diagnose
		// "etag computation failed" 500s instead of guessing at the cause.
		// whereSQL + args length are diagnostic but bounded (no PII leaks
		// beyond what's already in the request).
		log.Printf("computeIssueListETag: %v (whereSQL=%q args=%d)", err, whereSQL, len(args))
		return "", err
	}
	h := sha256.New()
	fmt.Fprintf(h, "%s|%d|%s", maxUpdated, total, whereSQL)
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
