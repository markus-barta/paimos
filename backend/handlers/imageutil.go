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
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
	"io"

	"golang.org/x/image/draw"
)

// processImage decodes, resizes, and re-encodes an image.
// maxW/maxH: maximum dimensions (preserves aspect ratio).
// If the input is PNG and keepPNG is true, output is PNG (preserves alpha).
// Otherwise output is JPEG at the given quality.
// Re-encoding strips EXIF metadata automatically.
func processImage(src io.Reader, maxW, maxH int, quality int, keepPNG bool) ([]byte, string, error) {
	data, err := io.ReadAll(src)
	if err != nil {
		return nil, "", err
	}

	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", err
	}

	// Resize if needed
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w > maxW || h > maxH {
		scale := float64(maxW) / float64(w)
		if s2 := float64(maxH) / float64(h); s2 < scale {
			scale = s2
		}
		nw := int(float64(w) * scale)
		nh := int(float64(h) * scale)
		dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
		draw.BiLinear.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Over, nil)
		img = dst
	}

	// Encode
	var buf bytes.Buffer
	if keepPNG && format == "png" {
		if err := png.Encode(&buf, img); err != nil {
			return nil, "", err
		}
		return buf.Bytes(), "image/png", nil
	}
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), "image/jpeg", nil
}
