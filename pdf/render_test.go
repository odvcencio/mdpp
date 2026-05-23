package pdf

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/odvcencio/mdpp"
)

func TestRenderProducesPDFBytes(t *testing.T) {
	skipIfNoLocalChrome(t)

	doc := mdpp.MustParse([]byte("# PDF Smoke\n\n[[toc]]\n\n## Section\n\nBody with math $x^2$.\n"))
	out, err := Render(doc, Options{
		RenderOptions: mdpp.RenderOptions{HeadingIDs: true, Math: mdpp.MathRaw},
		Timeout:       15 * time.Second,
		SettleDelay:   10 * time.Millisecond,
		Background:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(out, []byte("%PDF-")) {
		t.Fatalf("Render output header = %q", out[:minInt(len(out), 16)])
	}
	if !bytes.Contains(out, []byte("%%EOF")) {
		t.Fatalf("Render output missing EOF marker; length=%d", len(out))
	}
}

func TestRenderCanRasterizeFirstPage(t *testing.T) {
	skipIfNoLocalChrome(t)
	if _, err := exec.LookPath("pdftoppm"); err != nil {
		t.Skip("pdftoppm not available")
	}

	doc := mdpp.MustParse([]byte("# Raster Smoke\n\n![Diagram](images/diagram.png \"Architecture\")\n\n| A | B |\n|---|---|\n| 1 | 2 |\n"))
	out, err := Render(doc, Options{
		RenderOptions: mdpp.RenderOptions{HeadingIDs: true, Math: mdpp.MathRaw},
		Timeout:       15 * time.Second,
		SettleDelay:   10 * time.Millisecond,
		Background:    true,
	})
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "doc.pdf")
	if err := os.WriteFile(pdfPath, out, 0o644); err != nil {
		t.Fatal(err)
	}
	prefix := filepath.Join(dir, "page")
	cmd := exec.Command("pdftoppm", "-singlefile", "-png", "-r", "96", pdfPath, prefix)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rasterize pdf: %v\n%s", err, output)
	}
	png, err := os.ReadFile(prefix + ".png")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(png, []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatalf("rasterized first page header = %q", png[:minInt(len(png), 16)])
	}
	if len(png) < 1024 {
		t.Fatalf("rasterized first page too small: %d bytes", len(png))
	}
}

func TestHelpersDefaultPaperAndMargins(t *testing.T) {
	width, height := paperDimensions(Options{})
	if width != 8.5 || height != 11 {
		t.Fatalf("default paper = %gx%g, want letter", width, height)
	}
	margins := pdfMargins(Margins{})
	if margins.Top != 0.5 || margins.Right != 0.5 || margins.Bottom != 0.5 || margins.Left != 0.5 {
		t.Fatalf("default margins = %+v, want all 0.5", margins)
	}
	width, height = paperDimensions(Options{PaperSize: PaperCustom, PaperWidthInches: 4, PaperHeightInches: 6})
	if width != 4 || height != 6 {
		t.Fatalf("custom paper = %gx%g, want 4x6", width, height)
	}
}

func skipIfNoLocalChrome(t *testing.T) {
	t.Helper()
	for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"} {
		if _, err := exec.LookPath(name); err == nil {
			return
		}
	}
	t.Skip("local Chrome/Chromium not available")
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
