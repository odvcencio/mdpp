package mdpp

import "bytes"

// OriginalRange maps a Range of this document to byte offsets in original, the
// input that was given to Parse. Parse reads CRLF and lone CR as LF, so the
// byte offsets of every node, diagnostic and uncertified range index
// Document.Source and not the input. With original, the offsets that this
// returns can be used on the input directly: for a heading read from
// "# h\r\n" the range ends at byte 4 of Source and at byte 5 of the input.
//
// A range that ends just after a line end that was CRLF in the input includes
// both bytes. The offsets are clamped to len(original) (Parse adds a final
// line feed to a source that lacks one). ok is false when the range has no
// position, or when original is not the input this document was parsed from.
// Line and column numbers are not mapped; they count lines the same way in both
// texts, and a column counts bytes of Source.
func (d *Document) OriginalRange(original []byte, r Range) (start, end int, ok bool) {
	if d == nil || r.StartLine == 0 || r.StartByte < 0 || r.EndByte < r.StartByte || r.EndByte > len(d.Source) {
		return 0, 0, false
	}
	normalized := normalizeLineEndings(original)
	if len(d.Source) < len(normalized) || !bytes.Equal(d.Source[:len(normalized)], normalized) {
		return 0, 0, false
	}
	if len(normalized) == len(original) {
		return min(r.StartByte, len(original)), min(r.EndByte, len(original)), true
	}
	start, end = -1, -1
	if r.StartByte == 0 {
		start = 0
	}
	if r.EndByte == 0 {
		end = 0
	}
	k, i := 0, 0
	for i < len(original) && (start < 0 || end < 0) {
		width := 1
		if original[i] == '\r' && i+1 < len(original) && original[i+1] == '\n' {
			width = 2
		}
		// original[i:i+width] is the source of byte k of the normalized text.
		if k == r.StartByte {
			start = i
		}
		k++
		i += width
		if k == r.EndByte {
			end = i
		}
	}
	if start < 0 {
		start = len(original)
	}
	if end < 0 {
		end = len(original)
	}
	return start, end, true
}
