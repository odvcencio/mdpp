package mdpp

// Parse diagnostic codes. Each code names one way that a parse departed from
// the reading the source asks for. Every Diagnostic carries the Range of the
// source it affects.
const (
	// DiagPanic: the parser panicked. The whole source is one paragraph of text.
	DiagPanic = "MDPP-PARSE-000"
	// DiagContainerAutoClosed: a ":::" container had no closing fence and was
	// closed at the end of the document. The range runs from the opening line to
	// the end of the document; where the container ends is a guess.
	DiagContainerAutoClosed = "MDPP-PARSE-002"
	// DiagDepthExceeded: nested parsing went too deep. The range, from where it
	// stopped to the end of that part, is kept as text.
	DiagDepthExceeded = "MDPP-PARSE-003"
	// DiagComplexityLimit: a block (a line of the block) exceeded the complexity
	// limit and is kept as paragraph text. The range is that block. A block
	// kept as text has no list items, task items, links or emphasis.
	DiagComplexityLimit = "MDPP-PARSE-004"
	// DiagBudgetExceeded: the work, memory or time budget ran out. The range is
	// the whole source (the source is returned as text), or, for an inline span
	// that ran out, that span, which is kept as text.
	DiagBudgetExceeded = "MDPP-PARSE-005"
	// DiagSyntaxRecovery: the grammar reported a syntax error and the range was
	// kept as text, in whole or in part.
	DiagSyntaxRecovery = "MDPP-PARSE-006"
	// DiagNoTree: the block grammar produced no tree. The whole source is
	// returned as text.
	DiagNoTree = "MDPP-PARSE-007"
)

// StructureUncertain reports whether the diagnostic says that the document
// tree at its Range is not the structural reading of the source: the text
// there was kept as text, cut short, or guessed. Every parse diagnostic that
// the package emits says this. A caller that needs an exact reading must not
// trust the nodes at (or overlapping) the Range, and must not conclude that the
// range holds no heading, list, task item, link or HTML.
func (d Diagnostic) StructureUncertain() bool {
	switch d.Code {
	case DiagPanic, DiagContainerAutoClosed, DiagDepthExceeded, DiagComplexityLimit,
		DiagBudgetExceeded, DiagSyntaxRecovery, DiagNoTree:
		return true
	}
	// An unknown code is treated as uncertain: the safe reading for a caller
	// that needs an exact structure.
	return len(d.Code) >= len("MDPP-PARSE-") && d.Code[:len("MDPP-PARSE-")] == "MDPP-PARSE-"
}

// UncertifiedRanges returns the source ranges where the tree is not the
// structural reading of the source (see Diagnostic.StructureUncertain), in the
// order of the diagnostics. It is empty when the whole tree is certified.
func (d *Document) UncertifiedRanges() []Range {
	if d == nil {
		return nil
	}
	var out []Range
	for _, diagnostic := range d.diagnostics {
		if diagnostic.StructureUncertain() {
			out = append(out, diagnostic.Range)
		}
	}
	return out
}

// StructureCertified reports whether the tree is the structural reading of the
// source everywhere: the parse emitted no diagnostic that says otherwise. A
// caller that needs an exact reading (for example, to find every heading or
// task item) should check it, or UncertifiedRanges, before it trusts the tree.
func (d *Document) StructureCertified() bool {
	return d != nil && len(d.UncertifiedRanges()) == 0
}
