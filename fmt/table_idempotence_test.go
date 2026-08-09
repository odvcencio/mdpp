package fmt

import (
	"bytes"
	"strings"
	"testing"
)

// TestFormatPipeTableIdempotentAndBounded guards the 2026-08-09 table
// explosion: repeated Format passes multiplied table cells until the
// canonical file reached 17 MB. Formatting must converge and never grow
// a table document materially.
func TestFormatPipeTableIdempotentAndBounded(t *testing.T) {
	src := []byte(strings.Join([]string{
		"# Title",
		"",
		"| Piece | Location | Behavior |",
		"| --- | --- | --- |",
		"| `StableStepID` | `pkg/agentloop/durability.go` | Logical identity; doubles as the idempotency key |",
		"| SQLite journal | `pkg/runledger/sqlite.go` | completed steps are immutable and replay |",
		"",
		"Closing prose.",
		"",
	}, "\n"))

	once, err := Format(src)
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	if len(once) > len(src)*2 {
		t.Fatalf("Format grew document from %d to %d bytes", len(src), len(once))
	}
	twice, err := Format(once)
	if err != nil {
		t.Fatalf("Format twice: %v", err)
	}
	if !bytes.Equal(once, twice) {
		t.Fatalf("Format is not idempotent:\nfirst:\n%s\nsecond:\n%s", once, twice)
	}
	if got := bytes.Count(twice, []byte("StableStepID")); got != 1 {
		t.Fatalf("cell content duplicated %d times", got)
	}
}
