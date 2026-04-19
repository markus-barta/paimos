// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public
// License along with this program. If not, see <https://www.gnu.org/licenses/>.

package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/markus-barta/paimos/backend/db"
)

// jiraConfig is stored as JSON in integrations.config
type jiraConfig struct {
	Host  string `json:"host"`
	Email string `json:"email"`
	Token string `json:"token"` // never returned to clients
}

// GET /api/integrations/jira — returns host, email, has_token (admin only)
func GetJiraIntegration(w http.ResponseWriter, r *http.Request) {
	cfg, err := loadJiraConfig()
	if err != nil {
		// Not configured yet — return empty
		jsonOK(w, map[string]any{"host": "", "email": "", "has_token": false})
		return
	}
	jsonOK(w, map[string]any{
		"host":      cfg.Host,
		"email":     cfg.Email,
		"has_token": cfg.Token != "",
	})
}

// PUT /api/integrations/jira — save credentials (admin only)
// Body: { "host": "...", "email": "...", "token": "..." }
// Omitting token keeps the existing one.
func PutJiraIntegration(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Host  string `json:"host"`
		Email string `json:"email"`
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if body.Host == "" || body.Email == "" {
		jsonError(w, "host and email required", http.StatusBadRequest)
		return
	}

	// Normalize host — strip trailing slash and ensure https://
	host := strings.TrimRight(body.Host, "/")
	if !strings.HasPrefix(host, "http") {
		host = "https://" + host
	}

	// If token not supplied, keep existing
	token := body.Token
	if token == "" {
		if existing, err := loadJiraConfig(); err == nil {
			token = existing.Token
		}
	}

	cfg := jiraConfig{Host: host, Email: body.Email, Token: token}
	raw, err := json.Marshal(cfg)
	if err != nil {
		jsonError(w, "marshal failed", http.StatusInternalServerError)
		return
	}

	_, err = db.DB.Exec(`
		INSERT INTO integrations(provider, config, updated_at)
		VALUES('jira', ?, datetime('now'))
		ON CONFLICT(provider) DO UPDATE SET config=excluded.config, updated_at=excluded.updated_at
	`, string(raw))
	if handleDBError(w, err, "jira integration") {
		return
	}

	jsonOK(w, map[string]any{"host": host, "email": body.Email, "has_token": token != ""})
}

// POST /api/integrations/jira/test — verify creds against Jira /rest/api/3/myself
func TestJiraIntegration(w http.ResponseWriter, r *http.Request) {
	cfg, err := loadJiraConfig()
	if err != nil || cfg.Host == "" || cfg.Email == "" || cfg.Token == "" {
		jsonError(w, "Jira not configured — save credentials first", http.StatusBadRequest)
		return
	}

	url := strings.TrimRight(cfg.Host, "/") + "/rest/api/3/myself"
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, url, nil)
	if err != nil {
		jsonError(w, "failed to build request: "+err.Error(), http.StatusInternalServerError)
		return
	}
	req.SetBasicAuth(cfg.Email, cfg.Token)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		jsonError(w, "connection failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		jsonError(w, fmt.Sprintf("Jira returned %d: %s", resp.StatusCode, truncate(string(body), 120)), http.StatusBadGateway)
		return
	}

	var me struct {
		DisplayName  string `json:"displayName"`
		EmailAddress string `json:"emailAddress"`
	}
	if err := json.Unmarshal(body, &me); err != nil {
		jsonError(w, "unexpected Jira response", http.StatusBadGateway)
		return
	}

	jsonOK(w, map[string]any{"ok": true, "display_name": me.DisplayName, "email": me.EmailAddress})
}

// loadJiraConfig reads the stored Jira config from DB.
func loadJiraConfig() (*jiraConfig, error) {
	var raw string
	err := db.DB.QueryRow(
		"SELECT config FROM integrations WHERE provider='jira'",
	).Scan(&raw)
	if err != nil {
		return nil, err
	}
	var cfg jiraConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// LoadJiraConfig is exported for use by the import engine.
func LoadJiraConfig() (*jiraConfig, error) { return loadJiraConfig() }

// JiraConfig fields exported for import engine
func (c *jiraConfig) GetHost() string  { return c.Host }
func (c *jiraConfig) GetEmail() string { return c.Email }
func (c *jiraConfig) GetToken() string { return c.Token }

// ── Mite integration ────────────────────────────────────────────────────────

// miteConfig is stored as JSON in integrations.config (provider=mite)
type miteConfig struct {
	APIKey            string `json:"api_key"`              // never returned to clients
	BaseURL           string `json:"base_url"`
	LoadDataSinceDate string `json:"load_data_since_date"` // YYYY-MM-DD
}

// GET /api/integrations/mite — returns baseUrl, loadDataSinceDate, has_api_key
func GetMiteIntegration(w http.ResponseWriter, r *http.Request) {
	cfg, err := loadMiteConfig()
	if err != nil {
		jsonOK(w, map[string]any{"base_url": "", "load_data_since_date": "", "has_api_key": false})
		return
	}
	jsonOK(w, map[string]any{
		"base_url":             cfg.BaseURL,
		"load_data_since_date": cfg.LoadDataSinceDate,
		"has_api_key":          cfg.APIKey != "",
	})
}

// PUT /api/integrations/mite — save config (omitting api_key keeps existing)
func PutMiteIntegration(w http.ResponseWriter, r *http.Request) {
	var body struct {
		BaseURL           string `json:"base_url"`
		APIKey            string `json:"api_key"`
		LoadDataSinceDate string `json:"load_data_since_date"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if body.BaseURL == "" {
		jsonError(w, "base_url is required", http.StatusBadRequest)
		return
	}

	baseURL := strings.TrimRight(body.BaseURL, "/")
	if !strings.HasPrefix(baseURL, "http") {
		baseURL = "https://" + baseURL
	}

	apiKey := body.APIKey
	if apiKey == "" {
		if existing, err := loadMiteConfig(); err == nil {
			apiKey = existing.APIKey
		}
	}

	cfg := miteConfig{APIKey: apiKey, BaseURL: baseURL, LoadDataSinceDate: body.LoadDataSinceDate}
	raw, err := json.Marshal(cfg)
	if err != nil {
		jsonError(w, "marshal failed", http.StatusInternalServerError)
		return
	}

	_, err = db.DB.Exec(`
		INSERT INTO integrations(provider, config, updated_at)
		VALUES('mite', ?, datetime('now'))
		ON CONFLICT(provider) DO UPDATE SET config=excluded.config, updated_at=excluded.updated_at
	`, string(raw))
	if handleDBError(w, err, "mite integration") {
		return
	}

	jsonOK(w, map[string]any{"base_url": baseURL, "load_data_since_date": cfg.LoadDataSinceDate, "has_api_key": apiKey != ""})
}

// POST /api/integrations/mite/test — verify connectivity via GET /account.json
func TestMiteIntegration(w http.ResponseWriter, r *http.Request) {
	cfg, err := loadMiteConfig()
	if err != nil || cfg.BaseURL == "" || cfg.APIKey == "" {
		jsonError(w, "Mite not configured — save credentials first", http.StatusBadRequest)
		return
	}

	url := strings.TrimRight(cfg.BaseURL, "/") + "/account.json"
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, url, nil)
	if err != nil {
		jsonError(w, "failed to build request: "+err.Error(), http.StatusInternalServerError)
		return
	}
	req.Header.Set("X-MiteApiKey", cfg.APIKey)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		jsonError(w, "connection failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		jsonError(w, fmt.Sprintf("Mite returned %d: %s", resp.StatusCode, truncate(string(respBody), 120)), http.StatusBadGateway)
		return
	}

	var account struct {
		Account struct {
			Name string `json:"name"`
		} `json:"account"`
	}
	if err := json.Unmarshal(respBody, &account); err != nil {
		jsonError(w, "unexpected Mite response", http.StatusBadGateway)
		return
	}

	jsonOK(w, map[string]any{"ok": true, "account_name": account.Account.Name})
}

// loadMiteConfig reads the stored mite config from DB.
func loadMiteConfig() (*miteConfig, error) {
	var raw string
	err := db.DB.QueryRow(
		"SELECT config FROM integrations WHERE provider='mite'",
	).Scan(&raw)
	if err != nil {
		return nil, err
	}
	var cfg miteConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// LoadMiteConfig is exported for use by the mite import engine.
func LoadMiteConfig() (*miteConfig, error) { return loadMiteConfig() }

func (c *miteConfig) GetAPIKey() string            { return c.APIKey }
func (c *miteConfig) GetBaseURL() string            { return c.BaseURL }
func (c *miteConfig) GetLoadDataSinceDate() string  { return c.LoadDataSinceDate }
