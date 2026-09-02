package main

import (
	"bytes"
	"fmt"
	"image"
	"strings"

	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// pendingImg marks an image fetch in flight in App.addModImgs, so the view
// can show a spinner and Update won't queue the fetch twice.
const pendingImg = "\x00pending"

// imgKey identifies a fetched image block by URL and target size, so the
// same icon can exist at list size and preview size independently.
func imgKey(u string, maxCols, maxRows int) string {
	return fmt.Sprintf("%s|%dx%d", u, maxCols, maxRows)
}

// terminalDoesColor reports whether image rendering is worth doing at all —
// even half-block art needs colors to read as an image.
func terminalDoesColor() bool {
	return lipgloss.ColorProfile() != termenv.Ascii
}

// fetchImageBlock downloads an image (through the shared disk cache) and
// returns a ready-to-embed text block sized to fit maxCols×maxRows cells,
// preserving aspect ratio. kittyID > 0 selects full-res kitty graphics
// (transmitting the image as a side effect); otherwise half-block (▀) pixel
// art, whose resolution scales with the space given.
func fetchImageBlock(imgURL string, maxCols, maxRows, kittyID int) (string, error) {
	data, err := cachedGET(imgURL, nil)
	if err != nil {
		return "", err
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	b := src.Bounds()
	cols, rows := fitCells(b.Dx(), b.Dy(), maxCols, maxRows)
	if kittyID > 0 {
		if err := transmitKittyImage(kittyID, src, cols, rows); err != nil {
			return "", err
		}
		return kittyPlaceholderBlock(kittyID, rows, cols), nil
	}
	return renderHalfblockArt(src, cols, rows), nil
}

// fitCells fits an image of srcW×srcH pixels into a box of terminal cells,
// treating a cell as 1×2 pixels of aspect.
func fitCells(srcW, srcH, maxCols, maxRows int) (cols, rows int) {
	maxCols = minInt(maxCols, kittyMaxCells)
	maxRows = minInt(maxRows, kittyMaxRows)
	cols = maxCols
	rows = int(float64(cols)*float64(srcH)/float64(srcW)/2 + 0.5)
	if rows > maxRows {
		rows = maxRows
		cols = int(float64(rows)*2*float64(srcW)/float64(srcH) + 0.5)
	}
	return clamp(cols, 1, maxCols), clamp(rows, 1, maxRows)
}

// renderHalfblockArt renders src as half-block (▀) pixel art of exactly
// cols×rows cells — two pixels per terminal row.
func renderHalfblockArt(src image.Image, cols, rows int) string {
	dst := image.NewNRGBA(image.Rect(0, 0, cols, rows*2))
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Src, nil)

	// Composite transparency onto the modal's panel background.
	var bgR, bgG, bgB int
	fmt.Sscanf(string(colorBgPanel), "#%02x%02x%02x", &bgR, &bgG, &bgB)
	px := func(x, y int) lipgloss.Color {
		c := dst.NRGBAAt(x, y)
		alpha := int(c.A)
		r := (int(c.R)*alpha + bgR*(255-alpha)) / 255
		g := (int(c.G)*alpha + bgG*(255-alpha)) / 255
		b := (int(c.B)*alpha + bgB*(255-alpha)) / 255
		return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", r, g, b))
	}

	var sb strings.Builder
	for y := 0; y < rows; y++ {
		if y > 0 {
			sb.WriteString("\n")
		}
		for x := 0; x < cols; x++ {
			sb.WriteString(lipgloss.NewStyle().
				Foreground(px(x, y*2)).Background(px(x, y*2+1)).Render("▀"))
		}
	}
	return sb.String()
}
