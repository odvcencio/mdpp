package mdpp

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

func TestConformancePDFCorpus(t *testing.T) {
	root := filepath.Join("examples", "conformance")
	cases, err := pdfConformanceCases(root)
	if err != nil {
		t.Fatalf("list pdf conformance corpus: %v", err)
	}
	if len(cases) == 0 {
		t.Skip("no PDF conformance fixtures present")
	}

	skipIfNoLocalChrome(t)
	if _, err := exec.LookPath("pdftoppm"); err != nil {
		t.Skip("pdftoppm not available")
	}

	for _, name := range cases {
		name := name
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(root, name)
			inputPath := filepath.Join(dir, "input.md")
			readmePath := filepath.Join(dir, "README.md")
			expectedPNGPath := filepath.Join(dir, "expected.pdf.png")

			requireRegularFile(t, inputPath)
			requireRegularFile(t, readmePath)
			requireRegularFile(t, expectedPNGPath)

			input, err := os.ReadFile(inputPath)
			if err != nil {
				t.Fatalf("read input: %v", err)
			}

			pdf, err := RenderPDF(MustParse(input), PDFOptions{
				RenderOptions: RenderOptions{},
				Timeout:       15 * time.Second,
				SettleDelay:   10 * time.Millisecond,
				Background:    true,
			})
			if err != nil {
				t.Fatalf("render pdf: %v", err)
			}

			gotPNG, err := rasterizePDFFirstPage(pdf)
			if err != nil {
				t.Fatalf("rasterize pdf: %v", err)
			}

			if *updateConformance {
				if err := os.WriteFile(expectedPNGPath, gotPNG, 0o644); err != nil {
					t.Fatalf("update expected pdf png: %v", err)
				}
			}

			wantPNG, err := os.ReadFile(expectedPNGPath)
			if err != nil {
				t.Fatalf("read expected pdf png: %v", err)
			}
			comparePNGImages(t, gotPNG, wantPNG)
		})
	}
}

func pdfConformanceCases(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}

	var cases []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if optionalRegularFile(filepath.Join(root, entry.Name(), "expected.pdf.png")) {
			cases = append(cases, entry.Name())
		}
	}
	sort.Strings(cases)
	return cases, nil
}

func rasterizePDFFirstPage(pdf []byte) ([]byte, error) {
	dir, err := os.MkdirTemp("", "mdpp-pdf-raster-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	pdfPath := filepath.Join(dir, "doc.pdf")
	if err := os.WriteFile(pdfPath, pdf, 0o644); err != nil {
		return nil, err
	}

	prefix := filepath.Join(dir, "page")
	cmd := exec.Command("pdftoppm", "-singlefile", "-png", "-r", "96", pdfPath, prefix)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("pdftoppm: %w\n%s", err, output)
	}

	got, err := os.ReadFile(prefix + ".png")
	if err != nil {
		return nil, err
	}
	return got, nil
}

func comparePNGImages(t *testing.T, got, want []byte) {
	t.Helper()

	gotImg := decodePNGImage(t, got)
	wantImg := decodePNGImage(t, want)

	if gotImg.Bounds() != wantImg.Bounds() {
		t.Fatalf("png bounds mismatch: got %v want %v", gotImg.Bounds(), wantImg.Bounds())
	}

	gotRGBA := image.NewRGBA(gotImg.Bounds())
	draw.Draw(gotRGBA, gotRGBA.Bounds(), gotImg, gotImg.Bounds().Min, draw.Src)
	wantRGBA := image.NewRGBA(wantImg.Bounds())
	draw.Draw(wantRGBA, wantRGBA.Bounds(), wantImg, wantImg.Bounds().Min, draw.Src)

	if bytes.Equal(gotRGBA.Pix, wantRGBA.Pix) {
		return
	}

	x, y, gotPix, wantPix := firstPNGDiff(gotRGBA, wantRGBA)
	t.Fatalf("png pixels differ at (%d,%d): got=%v want=%v", x, y, gotPix, wantPix)
}

func decodePNGImage(t *testing.T, data []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode png: %v", err)
	}
	return img
}

func firstPNGDiff(got, want *image.RGBA) (int, int, [4]uint8, [4]uint8) {
	bounds := got.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			gOff := got.PixOffset(x, y)
			wOff := want.PixOffset(x, y)
			if bytes.Equal(got.Pix[gOff:gOff+4], want.Pix[wOff:wOff+4]) {
				continue
			}
			var gotPix, wantPix [4]uint8
			copy(gotPix[:], got.Pix[gOff:gOff+4])
			copy(wantPix[:], want.Pix[wOff:wOff+4])
			return x, y, gotPix, wantPix
		}
	}
	return bounds.Min.X, bounds.Min.Y, [4]uint8{}, [4]uint8{}
}

func optionalRegularFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
