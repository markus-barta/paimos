package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/markus-barta/paimos/backend/db"
)

type systemSettingsPayload struct {
	UndoStackDepth int `json:"undo_stack_depth"`
}

func GetSystemSettings(w http.ResponseWriter, r *http.Request) {
	depth, err := loadUndoStackDepth(db.DB)
	if err != nil {
		log.Printf("system settings load: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonOK(w, systemSettingsPayload{UndoStackDepth: depth})
}

func PutSystemSettings(w http.ResponseWriter, r *http.Request) {
	var body systemSettingsPayload
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if body.UndoStackDepth < 1 || body.UndoStackDepth > 20 {
		jsonError(w, "undo_stack_depth must be between 1 and 20", http.StatusBadRequest)
		return
	}
	if _, err := db.DB.Exec(
		`INSERT INTO app_settings(key, value, updated_at) VALUES('undo_stack_depth', ?, datetime('now'))
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=datetime('now')`,
		strings.TrimSpace(strconv.Itoa(body.UndoStackDepth)),
	); err != nil {
		log.Printf("system settings save: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonOK(w, body)
}
