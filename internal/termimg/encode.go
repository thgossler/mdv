package termimg

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"

	"github.com/mattn/go-sixel"
)

// RenderResult is a rendered image ready to be placed into terminal output.
type RenderResult struct {
	// Text is the full escape/glyph sequence. For the half-block protocol it is
	// several newline-separated lines; for pixel protocols it is the protocol's
	// escape sequence already followed by the cursor movement needed to land on
	// a fresh line below the image.
	Text string
	// Cols and Rows are the cell footprint of the rendered image, used by
	// callers to center it within a content column.
	Cols int
	Rows int
}

// alphaThreshold is the minimum alpha (0-255) for a pixel to count as opaque in
// the half-block renderer; below it the terminal's own background shows through.
const alphaThreshold = 128

// maxBlockRows caps the height of a half-block image so a tall picture cannot
// flood the viewport; width is reduced to keep the aspect ratio within the cap.
const maxBlockRows = 40

// cellAspect is the assumed height:width ratio of a character cell, used to size
// pixel-protocol images in cells while keeping the picture's aspect ratio.
const cellAspect = 2.0

// Render draws img for protocol p, fitting it within maxCols character columns.
func Render(img image.Image, p Protocol, maxCols int) (RenderResult, error) {
	if maxCols < 1 {
		maxCols = 1
	}
	switch p {
	case ProtocolBlocks:
		return renderBlocks(img, maxCols), nil
	case ProtocolITerm2:
		return renderITerm2(img, maxCols)
	case ProtocolKitty:
		return renderKitty(img, maxCols)
	case ProtocolSixel:
		return renderSixel(img, maxCols)
	default:
		return RenderResult{}, fmt.Errorf("no image protocol")
	}
}

// renderBlocks renders img as Unicode half-block "pixels". Each character cell
// shows two vertical pixels: the upper via the glyph's foreground color and the
// lower via its background, using the upper-half block ▀ (or lower-half ▄ when
// only the bottom pixel is opaque) so transparency reveals the terminal's own
// background.
func renderBlocks(img image.Image, maxCols int) RenderResult {
	b := img.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if sw <= 0 || sh <= 0 {
		return RenderResult{}
	}

	cols := maxCols
	// Two source pixels map to one cell row, and a cell is ~cellAspect times as
	// tall as it is wide, so a cols-wide image needs this many rows to keep its
	// aspect ratio.
	rows := int(float64(cols)*float64(sh)/float64(sw)/cellAspect + 0.5)
	if rows < 1 {
		rows = 1
	}
	if rows > maxBlockRows {
		rows = maxBlockRows
		cols = int(float64(rows)*cellAspect*float64(sw)/float64(sh) + 0.5)
		if cols < 1 {
			cols = 1
		}
		if cols > maxCols {
			cols = maxCols
		}
	}

	scaled := resize(img, cols, rows*2)
	px := func(x, y int) (uint8, uint8, uint8, uint8) {
		r, g, bl, a := scaled.At(x, y).RGBA()
		return uint8(r >> 8), uint8(g >> 8), uint8(bl >> 8), uint8(a >> 8)
	}

	var sb strings.Builder
	for row := 0; row < rows; row++ {
		yTop := row * 2
		yBot := yTop + 1
		for x := 0; x < cols; x++ {
			tr, tg, tb, ta := px(x, yTop)
			br, bg, bb, ba := px(x, yBot)
			topOpaque := ta >= alphaThreshold
			botOpaque := ba >= alphaThreshold
			switch {
			case !topOpaque && !botOpaque:
				sb.WriteString("\x1b[0m ")
			case topOpaque && botOpaque:
				fmt.Fprintf(&sb, "\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm\u2580", tr, tg, tb, br, bg, bb)
			case topOpaque: // bottom transparent: upper half block, default bg
				fmt.Fprintf(&sb, "\x1b[49m\x1b[38;2;%d;%d;%dm\u2580", tr, tg, tb)
			default: // top transparent: lower half block, default bg
				fmt.Fprintf(&sb, "\x1b[49m\x1b[38;2;%d;%d;%dm\u2584", br, bg, bb)
			}
		}
		sb.WriteString("\x1b[0m")
		if row < rows-1 {
			sb.WriteByte('\n')
		}
	}
	return RenderResult{Text: sb.String(), Cols: cols, Rows: rows}
}

// composeRow lays out several half-block image results side by side, wrapping
// onto further visual rows when they would exceed width columns, mirroring how
// inline images flow and wrap in the GUI. The whole block is centered.
func composeRow(imgs []RenderResult, width int) string {
	const gap = 2 // blank columns between adjacent images
	var lines []string
	for i := 0; i < len(imgs); {
		// Greedily pack images into one visual row.
		rowW := imgs[i].Cols
		j := i + 1
		for j < len(imgs) {
			if rowW+gap+imgs[j].Cols > width {
				break
			}
			rowW += gap + imgs[j].Cols
			j++
		}
		lines = append(lines, composeOneRow(imgs[i:j], width, gap)...)
		i = j
	}
	return strings.Join(lines, "\n")
}

// composeOneRow renders a single visual row of images, top-aligned and centered
// within width columns.
func composeOneRow(imgs []RenderResult, width, gap int) []string {
	height, total := 0, 0
	grids := make([][]string, len(imgs))
	for k, im := range imgs {
		if im.Rows > height {
			height = im.Rows
		}
		if k > 0 {
			total += gap
		}
		total += im.Cols
		grids[k] = strings.Split(im.Text, "\n")
	}
	pad := (width - total) / 2
	if pad < 0 {
		pad = 0
	}
	prefix := strings.Repeat(" ", pad)
	gapStr := strings.Repeat(" ", gap)

	out := make([]string, height)
	for row := 0; row < height; row++ {
		var sb strings.Builder
		sb.WriteString(prefix)
		for k := range imgs {
			if k > 0 {
				sb.WriteString(gapStr)
			}
			if row < len(grids[k]) {
				sb.WriteString(grids[k][row])
			} else {
				// Pad shorter images with blank cells to keep alignment.
				sb.WriteString(strings.Repeat(" ", imgs[k].Cols))
			}
		}
		out[row] = sb.String()
	}
	return out
}

// cellFootprint converts a pixel image into a cell footprint that fits within
// maxCols columns while preserving aspect ratio.
func cellFootprint(img image.Image, maxCols int) (cols, rows int) {
	b := img.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if sw <= 0 || sh <= 0 {
		return 1, 1
	}
	cols = maxCols
	rows = int(float64(cols)*float64(sh)/float64(sw)/cellAspect + 0.5)
	if rows < 1 {
		rows = 1
	}
	return cols, rows
}

// encodePNG encodes img as PNG bytes for transmission to graphics terminals.
func encodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// renderITerm2 emits an OSC 1337 inline image sized to a cell box. iTerm2 leaves
// the cursor to the right of the image's last row, so a trailing CR+LF lands the
// next output on the line below the picture.
func renderITerm2(img image.Image, maxCols int) (RenderResult, error) {
	cols, rows := cellFootprint(img, maxCols)
	data, err := encodePNG(img)
	if err != nil {
		return RenderResult{}, err
	}
	b64 := base64.StdEncoding.EncodeToString(data)
	seq := fmt.Sprintf(
		"\x1b]1337;File=inline=1;size=%d;width=%d;height=%d;preserveAspectRatio=1:%s\x07\r\n",
		len(data), cols, rows, b64,
	)
	return RenderResult{Text: seq, Cols: cols, Rows: rows}, nil
}

// renderKitty emits a kitty graphics-protocol image. kitty does not move the
// cursor when placing an image, so the sequence is followed by as many newlines
// as the image is tall to continue output below it.
func renderKitty(img image.Image, maxCols int) (RenderResult, error) {
	cols, rows := cellFootprint(img, maxCols)
	data, err := encodePNG(img)
	if err != nil {
		return RenderResult{}, err
	}
	b64 := base64.StdEncoding.EncodeToString(data)

	var sb strings.Builder
	// Transmit-and-display (a=T) a PNG (f=100), constrained to c×r cells.
	const chunk = 4096
	first := true
	for len(b64) > 0 {
		n := len(b64)
		if n > chunk {
			n = chunk
		}
		piece := b64[:n]
		b64 = b64[n:]
		more := 0
		if len(b64) > 0 {
			more = 1
		}
		if first {
			fmt.Fprintf(&sb, "\x1b_Ga=T,f=100,c=%d,r=%d,m=%d;%s\x1b\\", cols, rows, more, piece)
			first = false
		} else {
			fmt.Fprintf(&sb, "\x1b_Gm=%d;%s\x1b\\", more, piece)
		}
	}
	sb.WriteString(strings.Repeat("\n", rows))
	sb.WriteByte('\r')
	return RenderResult{Text: sb.String(), Cols: cols, Rows: rows}, nil
}

// renderSixel emits a sixel image. The image is scaled to the cell box in pixels
// using an approximate cell pixel size, since sixel addresses pixels directly.
func renderSixel(img image.Image, maxCols int) (RenderResult, error) {
	cols, rows := cellFootprint(img, maxCols)
	// Approximate cell size in pixels for rasterization.
	const cellW, cellH = 8, 16
	scaled := fit(img, cols*cellW, rows*cellH)
	scaled = ensureOpaque(scaled)

	var buf bytes.Buffer
	enc := sixel.NewEncoder(&buf)
	if err := enc.Encode(scaled); err != nil {
		return RenderResult{}, err
	}
	return RenderResult{Text: buf.String() + "\r\n", Cols: cols, Rows: rows}, nil
}

// ensureOpaque composites img over black so palette-based encoders (sixel) do
// not render transparent regions unpredictably.
func ensureOpaque(img image.Image) image.Image {
	b := img.Bounds()
	out := image.NewRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, a := img.At(x, y).RGBA()
			if a == 0xffff {
				out.Set(x, y, img.At(x, y))
				continue
			}
			af := float64(a) / 0xffff
			out.Set(x, y, color.RGBA{
				R: uint8(float64(r>>8) * af),
				G: uint8(float64(g>>8) * af),
				B: uint8(float64(bl>>8) * af),
				A: 0xff,
			})
		}
	}
	return out
}
