package handlers

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/markus-barta/paimos/backend/auth"
	"github.com/markus-barta/paimos/backend/db"
)

const idempotencyHeader = "Idempotency-Key"

type capturedResponse struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func (c *capturedResponse) Header() http.Header {
	return c.header
}

func (c *capturedResponse) WriteHeader(status int) {
	if c.status != 0 {
		return
	}
	c.status = status
}

func (c *capturedResponse) Write(b []byte) (int, error) {
	if c.status == 0 {
		c.status = http.StatusOK
	}
	return c.body.Write(b)
}

// IdempotencyMiddleware replays responses for repeated create-style
// writes that carry the same Idempotency-Key and identical body.
func IdempotencyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimSpace(r.Header.Get(idempotencyHeader))
		if key == "" || !isIdempotencyMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}

		raw, err := readAndRestoreBody(r)
		if err != nil {
			jsonError(w, "invalid body", http.StatusBadRequest)
			return
		}
		hash := requestHash(raw)
		userID := int64(0)
		if user := auth.GetUser(r); user != nil {
			userID = user.ID
		}
		route := r.URL.Path
		method := strings.ToUpper(r.Method)

		deleteExpiredIdempotencyKeys()
		if replay, ok := loadIdempotencyReplay(key, userID); ok {
			if replay.RequestHash != hash || replay.Route != route || replay.Method != method {
				problemJSON(w, r, ProblemDetails{
					Type:   "https://paimos.com/errors/idempotency_key_conflict",
					Title:  "Idempotency key conflict",
					Status: http.StatusConflict,
					Detail: "Idempotency-Key was reused with a different request method, path, or body",
					Code:   "idempotency_key_conflict",
				})
				return
			}
			writeReplay(w, replay)
			return
		}

		rec := &capturedResponse{header: make(http.Header)}
		next.ServeHTTP(rec, r)
		if rec.status == 0 {
			rec.status = http.StatusOK
		}
		storeIdempotencyReplay(key, userID, route, method, hash, rec)
		copyHeader(w.Header(), rec.header)
		w.WriteHeader(rec.status)
		_, _ = w.Write(rec.body.Bytes())
	})
}

func isIdempotencyMethod(method string) bool {
	return strings.EqualFold(method, http.MethodPost)
}

func readAndRestoreBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	_ = r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(raw))
	return raw, nil
}

func requestHash(raw []byte) string {
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}

type idempotencyReplay struct {
	RequestHash string
	Route       string
	Method      string
	StatusCode  int
	Response    []byte
	Headers     http.Header
}

func loadIdempotencyReplay(key string, userID int64) (idempotencyReplay, bool) {
	var out idempotencyReplay
	var headersJSON string
	err := db.DB.QueryRow(`
		SELECT request_hash, route, method, status_code, response, headers_json
		FROM idempotency_keys
		WHERE key=? AND user_id=? AND expires_at > ?
		ORDER BY created_at ASC
		LIMIT 1
	`, key, userID, time.Now().UTC().Format(time.RFC3339)).Scan(
		&out.RequestHash, &out.Route, &out.Method, &out.StatusCode, &out.Response, &headersJSON,
	)
	if err != nil {
		return idempotencyReplay{}, false
	}
	_ = json.Unmarshal([]byte(headersJSON), &out.Headers)
	return out, true
}

func storeIdempotencyReplay(key string, userID int64, route, method, hash string, rec *capturedResponse) {
	headersJSON, _ := json.Marshal(replayHeaders(rec.header))
	now := time.Now().UTC()
	_, _ = db.DB.Exec(`
		INSERT OR IGNORE INTO idempotency_keys
			(key, user_id, route, method, request_hash, status_code, response, headers_json, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, key, userID, route, method, hash, rec.status, rec.body.Bytes(), string(headersJSON),
		now.Format(time.RFC3339), now.Add(24*time.Hour).Format(time.RFC3339))
}

func deleteExpiredIdempotencyKeys() {
	_, _ = db.DB.Exec(`DELETE FROM idempotency_keys WHERE expires_at <= ?`, time.Now().UTC().Format(time.RFC3339))
}

func replayHeaders(h http.Header) http.Header {
	out := make(http.Header)
	for _, key := range []string{"Content-Type", "Location", RequestIDHeader} {
		if values, ok := h[key]; ok {
			out[key] = append([]string(nil), values...)
		}
	}
	return out
}

func writeReplay(w http.ResponseWriter, replay idempotencyReplay) {
	copyHeader(w.Header(), replay.Headers)
	w.Header().Set("X-PAIMOS-Idempotency-Replay", "true")
	w.WriteHeader(replay.StatusCode)
	_, _ = w.Write(replay.Response)
}

func copyHeader(dst, src http.Header) {
	for k, values := range src {
		dst.Del(k)
		for _, value := range values {
			dst.Add(k, value)
		}
	}
}
