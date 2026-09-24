package fmt

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestFormatTableAfterContent2048BytesIdempotent guards the v0.4.7 table
// corruption. A document of 2048 bytes or more uses the segmented parse
// path. Its fast table path gave every cell the byte range of the whole
// row, so the table rewriter wrote the row once per column. Pass 1 then
// repeated each row of a four-column table four times, and pass 2 changed
// the file again.
//
// The fixture is 16 lines and exactly 2048 bytes. It holds a long-row
// table after other content, which is the shape that corrupted.
func TestFormatTableAfterContent2048BytesIdempotent(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("testdata", "table_after_content_2048.md"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if len(src) != 2048 {
		t.Fatalf("fixture size = %d bytes, want 2048", len(src))
	}
	if got := bytes.Count(src, []byte("\n")); got != 16 {
		t.Fatalf("fixture line count = %d, want 16", got)
	}

	once, err := Format(src)
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	twice, err := Format(once)
	if err != nil {
		t.Fatalf("Format twice: %v", err)
	}
	if !bytes.Equal(once, twice) {
		t.Errorf("Format is not idempotent: pass 1 wrote %d bytes, pass 2 wrote %d bytes", len(once), len(twice))
	}
	if len(once) > len(src) {
		t.Errorf("Format grew the document from %d bytes to %d bytes", len(src), len(once))
	}

	// Every row must survive exactly once. The v0.4.7 fast table path
	// repeated each row once per column.
	for _, row := range []string{
		"| Component | Status | Evidence | Notes |",
		"forge (integrator)",
		"poryscriptZ (forge 1)",
		"dataforge (forge 2)",
		"patchforge (forge 3)",
		"| MMO plane | dormant | See 2.3 |",
	} {
		if got := bytes.Count(once, []byte(row)); got != 1 {
			t.Errorf("row text %q appears %d times after one pass, want 1", row, got)
		}
	}
}
