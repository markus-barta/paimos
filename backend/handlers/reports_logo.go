// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
)

// resolveBrandingLogoForPDF returns the active branding logo as raster bytes
// suitable for fpdf's RegisterImageOptionsReader, along with its image type
// ("PNG" or "JPG"). Resolution order:
//
//  1. $DATA_DIR/branding.json → logo URL field
//  2. URL must start with "/brand/" (assets uploaded via the admin UI); the
//     default "/logo.svg" from the SPA bundle is treated as "no upload"
//  3. Read $DATA_DIR/branding-assets/<filename>
//  4. PNG/JPG → use bytes as-is; SVG → rasterize to PNG
//
// Any failure path returns the embedded PAIMOS fallback (logoPNG) so PDF
// generation never breaks because of branding misconfiguration.
func resolveBrandingLogoForPDF() (data []byte, imgType string) {
	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "/app/data"
	}

	cfg, err := os.ReadFile(filepath.Join(dataDir, "branding.json"))
	if err != nil {
		return logoPNG, "PNG"
	}
	var parsed struct {
		Logo string `json:"logo"`
	}
	if err := json.Unmarshal(cfg, &parsed); err != nil || parsed.Logo == "" {
		return logoPNG, "PNG"
	}

	const prefix = "/brand/"
	if !strings.HasPrefix(parsed.Logo, prefix) {
		return logoPNG, "PNG"
	}
	filename := strings.TrimPrefix(parsed.Logo, prefix)
	if !brandingAssetFilenamePattern.MatchString(filename) {
		return logoPNG, "PNG"
	}

	assetPath := filepath.Join(dataDir, "branding-assets", filename)
	raw, err := os.ReadFile(assetPath)
	if err != nil {
		return logoPNG, "PNG"
	}

	switch strings.ToLower(filepath.Ext(filename)) {
	case ".png":
		return raw, "PNG"
	case ".jpg", ".jpeg":
		return raw, "JPG"
	case ".svg":
		if pngBytes, err := rasterizeSVG(raw, 256); err == nil {
			return pngBytes, "PNG"
		}
		return logoPNG, "PNG"
	default:
		return logoPNG, "PNG"
	}
}

// rasterizeSVG renders an SVG to a PNG at the given pixel width, preserving
// aspect ratio from the SVG's viewBox.
func rasterizeSVG(svgBytes []byte, width int) ([]byte, error) {
	icon, err := oksvg.ReadIconStream(bytes.NewReader(svgBytes))
	if err != nil {
		return nil, fmt.Errorf("parse svg: %w", err)
	}
	height := width
	if icon.ViewBox.W > 0 && icon.ViewBox.H > 0 {
		height = int(float64(width) * icon.ViewBox.H / icon.ViewBox.W)
		if height < 1 {
			height = 1
		}
	}
	icon.SetTarget(0, 0, float64(width), float64(height))

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	scanner := rasterx.NewScannerGV(width, height, img, img.Bounds())
	dasher := rasterx.NewDasher(width, height, scanner)
	icon.Draw(dasher, 1.0)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("encode png: %w", err)
	}
	return buf.Bytes(), nil
}
