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

// Package brand centralises operator-configurable product identity.
// All BRAND_* env vars are read once at startup into Default.
package brand

import (
	"os"
	"strings"
)

// Brand holds the operator-configurable product identity.
// Loaded once from env at startup; subsequent reads go through Default.
type Brand struct {
	ProductName       string
	CompanyName       string
	WebsiteURL        string
	PublicURL         string
	EmailFrom         string
	TOTPIssuer        string
	HealthServiceName string
	PageTitle         string
	APIKeyPrefix      string
	DBFilename        string
	MinIOBucket       string
}

// Default is the brand loaded at package init from environment variables.
// Tests may replace it with a synthetic value before calling brand-dependent code.
var Default = Load()

// Load reads BRAND_* environment variables into a Brand, applying derived
// defaults (TOTPIssuer, HealthServiceName, PageTitle) when those env vars
// are unset.
func Load() Brand {
	b := Brand{
		ProductName:  envOr("BRAND_PRODUCT_NAME", "PAIMOS"),
		CompanyName:  os.Getenv("BRAND_COMPANY_NAME"),
		WebsiteURL:   envOr("BRAND_WEBSITE_URL", "https://paimos.com"),
		PublicURL:    strings.TrimRight(os.Getenv("BRAND_PUBLIC_URL"), "/"),
		EmailFrom:    os.Getenv("BRAND_EMAIL_FROM"),
		APIKeyPrefix: envOr("BRAND_API_KEY_PREFIX", "paimos_"),
		DBFilename:   envOr("BRAND_DB_FILENAME", "paimos.db"),
		MinIOBucket:  envOr("BRAND_MINIO_BUCKET", "paimos-attachments"),
	}
	b.TOTPIssuer = envOr("BRAND_TOTP_ISSUER", b.ProductName)
	b.HealthServiceName = envOr("BRAND_HEALTH_SERVICE_NAME", strings.ToLower(b.ProductName))
	b.PageTitle = envOr("BRAND_PAGE_TITLE", b.defaultPageTitle())
	return b
}

func (b Brand) defaultPageTitle() string {
	if b.CompanyName != "" {
		return b.ProductName + " — " + b.CompanyName
	}
	return b.ProductName
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
