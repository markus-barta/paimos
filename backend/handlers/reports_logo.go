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

	"github.com/go-pdf/fpdf"
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
	raw, ok := activeBrandingLogoBytes()
	if !ok {
		return logoPNG, "PNG"
	}

	// PAI-406: never trust the on-disk extension alone — a corrupt or
	// misclassified file would slip through to fpdf and surface as 500. Sniff
	// the magic bytes; only return what we actually recognize, otherwise fall
	// back to the embedded mark.
	switch sniffImageFormat(raw) {
	case "png":
		return raw, "PNG"
	case "jpg":
		return raw, "JPG"
	case "svg":
		if pngBytes, err := rasterizeSVG(raw, 256); err == nil {
			return pngBytes, "PNG"
		}
		return logoPNG, "PNG"
	default:
		return logoPNG, "PNG"
	}
}

func activeBrandingLogoBytes() ([]byte, bool) {
	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "/app/data"
	}

	// #nosec G304 G703 -- path is DATA_DIR (operator-set env, "/app/data" default) plus a fixed filename; no client input.
	cfg, err := os.ReadFile(filepath.Join(dataDir, "branding.json"))
	if err != nil {
		return nil, false
	}
	var parsed struct {
		Logo string `json:"logo"`
	}
	if err := json.Unmarshal(cfg, &parsed); err != nil || parsed.Logo == "" {
		return nil, false
	}

	const prefix = "/brand/"
	if !strings.HasPrefix(parsed.Logo, prefix) {
		return nil, false
	}
	filename := strings.TrimPrefix(parsed.Logo, prefix)
	if !brandingAssetFilenamePattern.MatchString(filename) {
		return nil, false
	}

	assetPath := filepath.Join(dataDir, "branding-assets", filename)
	// #nosec G304 G703 -- filename comes from server-side branding.json and is validated against brandingAssetFilenamePattern above (no separators or traversal).
	raw, err := os.ReadFile(assetPath)
	if err != nil {
		return nil, false
	}
	return raw, true
}

func resolveBrandingLogoBasicSVGForPDF() (*fpdf.SVGBasicType, bool) {
	raw, ok := activeBrandingLogoBytes()
	if !ok || sniffImageFormat(raw) != "svg" || !canUseBasicStrokeSVG(raw) {
		return nil, false
	}
	sig, err := fpdf.SVGBasicParse(raw)
	if err != nil || sig.Wd <= 0 || sig.Ht <= 0 || len(sig.Segments) == 0 {
		return nil, false
	}
	return &sig, true
}

func canUseBasicStrokeSVG(raw []byte) bool {
	s := strings.ToLower(string(raw))
	if strings.Contains(s, "<rect") || strings.Contains(s, "<circle") || strings.Contains(s, "<polygon") || strings.Contains(s, "<polyline") || strings.Contains(s, "<line") {
		return false
	}
	if strings.Contains(s, "fill=") && !strings.Contains(s, `fill="none"`) && !strings.Contains(s, `fill='none'`) {
		return false
	}
	if strings.Contains(s, "stroke=") && !strings.Contains(s, `stroke="currentcolor"`) && !strings.Contains(s, `stroke='currentcolor'`) {
		return false
	}
	return strings.Contains(s, "<path")
}

// sniffImageFormat returns "png", "jpg", "svg", "ico", or "" based on the
// leading bytes of the file. Used by the PDF logo resolver and by the
// branding upload handler so we never trust the client's declared
// Content-Type or the filename extension alone.
func sniffImageFormat(data []byte) string {
	if len(data) >= 8 && bytes.Equal(data[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
		return "png"
	}
	if len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff {
		return "jpg"
	}
	if len(data) >= 4 && bytes.Equal(data[:4], []byte{0x00, 0x00, 0x01, 0x00}) {
		return "ico"
	}
	if looksLikeSVG(data) {
		return "svg"
	}
	return ""
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
