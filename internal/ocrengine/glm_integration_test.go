//go:build ocr_glm_integration

// Run against a live GLM-OCR endpoint (vLLM, SGLang or Ollama):
//
//	GLM_BASE_URL=http://localhost:8080 go test -tags ocr_glm_integration ./internal/ocrengine -run TestRealGLMOCR -v
package ocrengine

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"
	"time"
)

// glyphs is a minimal 5x7 bitmap font, enough to render the test phrase.
var glyphs = map[rune][7]string{
	'M': {"X...X", "XX.XX", "X.X.X", "X...X", "X...X", "X...X", "X...X"},
	'A': {".XXX.", "X...X", "X...X", "XXXXX", "X...X", "X...X", "X...X"},
	'T': {"XXXXX", "..X..", "..X..", "..X..", "..X..", "..X..", "..X.."},
	'R': {"XXXX.", "X...X", "X...X", "XXXX.", "X.X..", "X..X.", "X...X"},
	'I': {"XXXXX", "..X..", "..X..", "..X..", "..X..", "..X..", "XXXXX"},
	'C': {".XXXX", "X....", "X....", "X....", "X....", "X....", ".XXXX"},
	'U': {"X...X", "X...X", "X...X", "X...X", "X...X", "X...X", ".XXX."},
	'L': {"X....", "X....", "X....", "X....", "X....", "X....", "XXXXX"},
	'4': {"...X.", "..XX.", ".X.X.", "X..X.", "XXXXX", "...X.", "...X."},
	'5': {"XXXXX", "X....", "XXXX.", "....X", "....X", "X...X", ".XXX."},
	'6': {".XXX.", "X....", "XXXX.", "X...X", "X...X", "X...X", ".XXX."},
	'7': {"XXXXX", "....X", "...X.", "..X..", "..X..", "..X..", "..X.."},
	'8': {".XXX.", "X...X", "X...X", ".XXX.", "X...X", "X...X", ".XXX."},
	' ': {".....", ".....", ".....", ".....", ".....", ".....", "....."},
}

// renderPNG draws text black-on-white at the given pixel scale.
func renderPNG(t *testing.T, text string, scale int) []byte {
	t.Helper()
	const margin = 4
	w := (len(text)*6 + 2*margin) * scale
	h := (7 + 2*margin) * scale
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.White)
		}
	}
	for i, r := range text {
		glyph, ok := glyphs[r]
		if !ok {
			t.Fatalf("no glyph for %q", r)
		}
		for row, line := range glyph {
			for col, c := range line {
				if c != 'X' {
					continue
				}
				x0 := (margin + i*6 + col) * scale
				y0 := (margin + row) * scale
				for dy := 0; dy < scale; dy++ {
					for dx := 0; dx < scale; dx++ {
						img.Set(x0+dx, y0+dy, color.Black)
					}
				}
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestRealGLMOCR(t *testing.T) {
	baseURL := os.Getenv("GLM_BASE_URL")
	if baseURL == "" {
		t.Skip("GLM_BASE_URL not set")
	}

	e, err := New("glm", Config{
		GLMBaseURL:   baseURL,
		GLMModel:     os.Getenv("GLM_MODEL"),
		GLMAPIKey:    os.Getenv("GLM_API_KEY"),
		ModelTimeout: 2 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}

	img := renderPNG(t, "MATRICULA 45678", 8)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	res, err := e.RecognizePage(ctx, Page{Number: 1, PNG: img})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("recognized: %q", res.Text)
	if res.Text == "" {
		t.Fatal("empty transcription")
	}
}
