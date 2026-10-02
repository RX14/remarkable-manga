// wrap.go: trivial PDF assembly — one image per page, page = image, 1:1.
package main

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

// wrapPDF assembles the given images into a single PDF: one image per page, in
// order, each page taking its image's exact dimensions (pdfcpu's default "full"
// anchor enforces page dims = image dims). No resizing, no re-encoding knobs —
// the trivial wrap.
func wrapPDF(ctx context.Context, pages [][]byte) ([]byte, error) {
	// Stateless: never read or create ~/.config/pdfcpu (pdfcpu's own prescribed
	// switch for this; we need no user fonts and no custom configuration).
	api.DisableConfigDir()
	readers := make([]io.Reader, len(pages))
	for i, p := range pages {
		readers[i] = bytes.NewReader(p)
	}
	var buf bytes.Buffer
	if err := api.ImportImages(ctx, nil, &buf, readers, nil, nil); err != nil {
		return nil, fmt.Errorf("wrap pdf: %w", err)
	}
	return buf.Bytes(), nil
}
