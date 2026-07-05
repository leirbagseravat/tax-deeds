// Package pdfconvert renders PDF pages to PNG images. The Converter interface
// lets tests run without poppler installed.
package pdfconvert

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"time"
)

// Converter renders every page of the PDF at pdfPath into outDir and returns
// the resulting image paths ordered by page number.
type Converter interface {
	Convert(ctx context.Context, pdfPath, outDir string) ([]string, error)
}

// Poppler shells out to pdftoppm.
type Poppler struct {
	// Timeout bounds a single conversion; guards against PDF bombs.
	Timeout time.Duration
	// MaxPages rejects documents producing more pages than this.
	MaxPages int
	// DPI for rendering; 200 is a good OCR baseline.
	DPI int
}

// Convert implements Converter using `pdftoppm -png`.
func (p *Poppler) Convert(ctx context.Context, pdfPath, outDir string) ([]string, error) {
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	dpi := p.DPI
	if dpi <= 0 {
		dpi = 200
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "pdftoppm", "-png", "-r", fmt.Sprint(dpi), pdfPath, filepath.Join(outDir, "page"))
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("pdftoppm: %w: %s", err, out)
	}

	// pdftoppm pads page numbers to a uniform width, so lexical order is page order.
	paths, err := filepath.Glob(filepath.Join(outDir, "page-*.png"))
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("pdftoppm produced no pages")
	}
	if p.MaxPages > 0 && len(paths) > p.MaxPages {
		return nil, fmt.Errorf("document has %d pages, more than the allowed %d", len(paths), p.MaxPages)
	}
	sort.Strings(paths)
	return paths, nil
}
