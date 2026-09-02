package main

import (
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"

	xdraw "golang.org/x/image/draw"
)

// Kitty graphics protocol support via Unicode placeholders (U+10EEEE): the
// image data is transmitted once, then drawn wherever placeholder cells
// appear. Because placeholders are ordinary text cells, they survive Bubble
// Tea's line-based repaints — the terminal keeps the image glued to them.

// kittyGraphicsOK reports whether the terminal understands the kitty
// graphics protocol with Unicode placeholders. That protocol is the only
// image standard whose images anchor to text cells (sixel and iTerm2's
// protocol draw at the cursor, which a repainting TUI clobbers), but it's
// spoken by more than kitty: ghostty, WezTerm and recent Konsole too.
func kittyGraphicsOK() bool {
	if os.Getenv("KITTY_WINDOW_ID") != "" {
		return true
	}
	term := os.Getenv("TERM")
	if strings.Contains(term, "kitty") || strings.Contains(term, "ghostty") {
		return true
	}
	if os.Getenv("TERM_PROGRAM") == "WezTerm" || strings.Contains(term, "wezterm") {
		return true
	}
	// Konsole ships kitty-graphics support with placeholder handling from
	// 23.04 (six-digit versions compare fine as strings).
	if v := os.Getenv("KONSOLE_VERSION"); len(v) == 6 && v >= "230400" {
		return true
	}
	return false
}

// kittyDiacritics is kitty's official rowcolumn-diacritics table (from
// gen/rowcolumn-diacritics.txt in the kitty repo); entry n marks placeholder
// row/column n. Placements are capped to its length.
var kittyDiacritics = []rune{
	0x0305, 0x030D, 0x030E, 0x0310, 0x0312, 0x033D, 0x033E, 0x033F,
	0x0346, 0x034A, 0x034B, 0x034C, 0x0350, 0x0351, 0x0352, 0x0357,
	0x035B, 0x0363, 0x0364, 0x0365, 0x0366, 0x0367, 0x0368, 0x0369,
	0x036A, 0x036B, 0x036C, 0x036D, 0x036E, 0x036F, 0x0483, 0x0484,
	0x0485, 0x0486, 0x0487, 0x0592, 0x0593, 0x0594, 0x0595, 0x0597,
	0x0598, 0x0599, 0x059C, 0x059D, 0x059E, 0x059F, 0x05A0, 0x05A1,
	0x05A8, 0x05A9, 0x05AB, 0x05AC, 0x05AF, 0x05C4, 0x0610, 0x0611,
	0x0612, 0x0613, 0x0614, 0x0615, 0x0616, 0x0617, 0x0657, 0x0658,
	0x0659, 0x065A, 0x065B, 0x065D, 0x065E, 0x06D6, 0x06D7, 0x06D8,
	0x06D9, 0x06DA, 0x06DB, 0x06DC, 0x06DF, 0x06E0, 0x06E1, 0x06E2,
	0x06E4, 0x06E7, 0x06E8, 0x06EB, 0x06EC, 0x0730, 0x0732, 0x0733,
	0x0735, 0x0736, 0x073A, 0x073D, 0x073F, 0x0740, 0x0741, 0x0743,
	0x0745, 0x0747, 0x0749, 0x074A, 0x07EB, 0x07EC, 0x07ED, 0x07EE,
	0x07EF, 0x07F0, 0x07F1, 0x07F3, 0x0816, 0x0817, 0x0818, 0x0819,
	0x081B, 0x081C, 0x081D, 0x081E, 0x081F, 0x0820, 0x0821, 0x0822,
	0x0823, 0x0825, 0x0826, 0x0827, 0x0829, 0x082A, 0x082B, 0x082C,
	0x082D, 0x0951, 0x0953, 0x0954, 0x0F82, 0x0F83, 0x0F86, 0x0F87,
	0x135D, 0x135E, 0x135F, 0x17DD, 0x193A, 0x1A17, 0x1A75, 0x1A76,
	0x1A77, 0x1A78, 0x1A79, 0x1A7A, 0x1A7B, 0x1A7C, 0x1B6B, 0x1B6D,
	0x1B6E, 0x1B6F, 0x1B70, 0x1B71, 0x1B72, 0x1B73, 0x1CD0, 0x1CD1,
	0x1CD2, 0x1CDA, 0x1CDB, 0x1CE0, 0x1DC0, 0x1DC1, 0x1DC3, 0x1DC4,
	0x1DC5, 0x1DC6, 0x1DC7, 0x1DC8, 0x1DC9, 0x1DCB, 0x1DCC, 0x1DD1,
	0x1DD2, 0x1DD3, 0x1DD4, 0x1DD5, 0x1DD6, 0x1DD7, 0x1DD8, 0x1DD9,
	0x1DDA, 0x1DDB, 0x1DDC, 0x1DDD, 0x1DDE, 0x1DDF, 0x1DE0, 0x1DE1,
	0x1DE2, 0x1DE3, 0x1DE4, 0x1DE5, 0x1DE6, 0x1DFE, 0x20D0, 0x20D1,
	0x20D4, 0x20D5, 0x20D6, 0x20D7, 0x20DB, 0x20DC, 0x20E1, 0x20E7,
	0x20E9, 0x20F0, 0x2CEF, 0x2CF0, 0x2CF1, 0x2DE0, 0x2DE1, 0x2DE2,
	0x2DE3, 0x2DE4, 0x2DE5, 0x2DE6, 0x2DE7, 0x2DE8, 0x2DE9, 0x2DEA,
	0x2DEB, 0x2DEC, 0x2DED, 0x2DEE, 0x2DEF, 0x2DF0, 0x2DF1, 0x2DF2,
	0x2DF3, 0x2DF4, 0x2DF5, 0x2DF6, 0x2DF7, 0x2DF8, 0x2DF9, 0x2DFA,
	0x2DFB, 0x2DFC, 0x2DFD, 0x2DFE, 0x2DFF, 0xA66F, 0xA67C, 0xA67D,
	0xA6F0, 0xA6F1, 0xA8E0, 0xA8E1, 0xA8E2, 0xA8E3, 0xA8E4, 0xA8E5,
	0xA8E6, 0xA8E7, 0xA8E8, 0xA8E9, 0xA8EA, 0xA8EB, 0xA8EC, 0xA8ED,
	0xA8EE, 0xA8EF, 0xA8F0, 0xA8F1, 0xAAB0, 0xAAB2, 0xAAB3, 0xAAB7,
	0xAAB8, 0xAABE, 0xAABF, 0xAAC1, 0xFE20, 0xFE21, 0xFE22, 0xFE23,
	0xFE24, 0xFE25, 0xFE26, 0x10A0F, 0x10A38, 0x1D185, 0x1D186, 0x1D187,
	0x1D188, 0x1D189, 0x1D1AA, 0x1D1AB, 0x1D1AC, 0x1D1AD, 0x1D242, 0x1D243,
	0x1D244,
}

// kittyMaxCells caps placement columns to the diacritics we can address.
var kittyMaxCells = len(kittyDiacritics)

// kittyMaxRows caps placement rows to diacritics that go-runewidth agrees
// are zero-width (index ≥35 measures 1 there) — row markers appear in every
// line, so they must not skew width math.
const kittyMaxRows = 30

// kittyIDBase namespaces our image ids so reruns in the same terminal
// window don't collide with a previous process's images.
var kittyIDBase = ((os.Getpid() & 0x7F) + 1) << 16

// transmitKittyImage sends img to the terminal as PNG under the given id and
// creates a rows×cols virtual placement for it.
//
// The pixels travel via a temp file (t=t — the terminal reads and deletes
// it), NOT inline: an inline transmission is a 100KB+ escape sequence, and
// the kernel splits writes that large into chunks which can interleave with
// the renderer's concurrent frame writes, slicing sequences mid-stream and
// spraying frame fragments across the screen. The file variant keeps the
// terminal-bound sequence ~100 bytes — one atomic, race-proof write.
func transmitKittyImage(id int, img image.Image, cols, rows int) error {
	// Downscale large sources — the terminal only has cols×rows cells to
	// fill, so shipping multi-megapixel screenshots is wasted transfer.
	b := img.Bounds()
	if maxPx := 960; b.Dx() > maxPx {
		h := b.Dy() * maxPx / b.Dx()
		dst := image.NewNRGBA(image.Rect(0, 0, maxPx, h))
		xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, b, xdraw.Src, nil)
		img = dst
	}
	f, err := os.CreateTemp("", "packwiz-tui-img-*.png")
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		os.Remove(f.Name())
		return err
	}
	f.Close()
	payload := base64.StdEncoding.EncodeToString([]byte(f.Name()))
	seq := fmt.Sprintf("\x1b_Gf=100,t=t,a=t,i=%d,q=2;%s\x1b\\\x1b_Ga=p,U=1,i=%d,r=%d,c=%d,q=2\x1b\\",
		id, payload, id, rows, cols)
	_, err = os.Stdout.WriteString(seq)
	return err
}

// RunImgTest prints alignment diagnostics straight to the terminal — a
// generated gradient via kitty placeholders and via half-blocks, each boxed
// between text markers. If the "|" markers line up with the rulers, the
// terminal agrees with our cell math; if not, the offset shows by how much.
func RunImgTest() {
	fmt.Printf("TERM=%q KITTY_WINDOW_ID=%q → kitty graphics: %v\n\n",
		os.Getenv("TERM"), os.Getenv("KITTY_WINDOW_ID"), kittyGraphicsOK())

	img := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 4), G: uint8(y * 4), B: 128, A: 255})
		}
	}
	ruler := "0123456789.........0.........0.........0"

	printBlock := func(title, block string) {
		fmt.Println(title)
		fmt.Println(ruler)
		for _, l := range strings.Split(block, "\n") {
			fmt.Println("|" + l + "|←cell 21")
		}
		fmt.Println(ruler)
		fmt.Println()
	}
	if kittyGraphicsOK() {
		id := kittyIDBase + 9999
		if err := transmitKittyImage(id, img, 20, 10); err == nil {
			printBlock("kitty placeholders (20 cols × 10 rows):", kittyPlaceholderBlock(id, 10, 20))
		} else {
			fmt.Println("kitty transmit failed:", err)
		}
	}
	printBlock("half-blocks (20 cols × 10 rows):", renderHalfblockArt(img, 20, 10))
}

// kittyPlaceholderBlock returns rows lines of placeholder cells for the
// image id; embed them like any other text. The cell foreground encodes the
// id; only each row's FIRST cell carries diacritics (row marker + column 0)
// — the terminal infers the rest as previous-column+1. That isn't just
// brevity: go-runewidth counts 120 of the 297 rowcolumn diacritics as
// width 1 (the terminal renders them all zero-width), so any row using
// higher-index column diacritics measures wider than it renders and every
// padding calculation downstream drifts. Bare cells sidestep the whole
// class.
func kittyPlaceholderBlock(id, rows, cols int) string {
	fg := fmt.Sprintf("\x1b[38;2;%d;%d;%dm", (id>>16)&0xFF, (id>>8)&0xFF, id&0xFF)
	var sb strings.Builder
	for r := 0; r < rows; r++ {
		if r > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(fg)
		sb.WriteRune('\U0010EEEE')
		sb.WriteRune(kittyDiacritics[r])
		sb.WriteRune(kittyDiacritics[0])
		for c := 1; c < cols; c++ {
			sb.WriteRune('\U0010EEEE')
		}
		sb.WriteString("\x1b[39m")
	}
	return sb.String()
}
