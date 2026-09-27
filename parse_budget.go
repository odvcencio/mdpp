package mdpp

import (
	"context"
	"fmt"
	"math"
	"time"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

const (
	parserWorkIterationsPerByte = 30
	defaultParseWorkPerByte     = 256
	minimumParseWorkBudget      = 64_000
	defaultParseDeadlineFloor   = 20 * time.Second
	defaultParseDeadlinePerMiB  = 10 * time.Second
)

// ParseOptions sets deterministic work and wall-clock limits for one parse.
// Zero values select defaults: a work budget of 256 units per input byte
// (with a 64,000-unit floor) and a wall-clock backstop of 20 seconds plus 10
// seconds per MiB. Work units reserve the parser's maximum 30 iterations per
// byte for each tree-sitter parse call. A parse that reaches a work, memory,
// or deadline limit returns the complete source as text with MDPP-PARSE-005.
type ParseOptions struct {
	// WorkBudget is the maximum estimated parser-iteration work. Zero selects
	// the input-size-based default. A budget smaller than the initial block
	// parse estimate returns text before parsing.
	WorkBudget int64
	// Deadline is the maximum wall-clock time for parsing. Zero selects the
	// input-size-based default. ParseContext also honors an earlier context
	// deadline or cancellation.
	Deadline time.Duration
}

type parseBudget struct {
	ctx           context.Context
	deadline      time.Time
	workLimit     int64
	workUsed      int64
	exactDeadline bool
	hit           string
}

func newParseBudget(ctx context.Context, sourceLen int, options ParseOptions) *parseBudget {
	if ctx == nil {
		ctx = context.Background()
	}
	workLimit := options.WorkBudget
	if workLimit <= 0 {
		workLimit = defaultParseWorkBudget(sourceLen)
	}
	defaultDeadline := defaultParseDeadline(sourceLen)
	deadlineAfter := options.Deadline
	exactDeadline := deadlineAfter > 0 && deadlineAfter <= defaultDeadline
	if deadlineAfter <= 0 {
		deadlineAfter = defaultDeadline
	}
	deadline := time.Now().Add(deadlineAfter)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	return &parseBudget{ctx: ctx, deadline: deadline, workLimit: workLimit, exactDeadline: exactDeadline}
}

func defaultParseWorkBudget(sourceLen int) int64 {
	if sourceLen < 0 {
		sourceLen = 0
	}
	if int64(sourceLen) > math.MaxInt64/defaultParseWorkPerByte {
		return math.MaxInt64
	}
	budget := int64(sourceLen) * defaultParseWorkPerByte
	if budget < minimumParseWorkBudget {
		return minimumParseWorkBudget
	}
	return budget
}

func defaultParseDeadline(sourceLen int) time.Duration {
	if sourceLen < 0 {
		sourceLen = 0
	}
	const bytesPerMiB = int64(1 << 20)
	bytes := int64(sourceLen)
	wholeMiB, remainder := bytes/bytesPerMiB, bytes%bytesPerMiB
	floor := int64(defaultParseDeadlineFloor)
	perMiB := int64(defaultParseDeadlinePerMiB)
	if wholeMiB > (math.MaxInt64-floor)/perMiB {
		return time.Duration(math.MaxInt64)
	}
	extra := wholeMiB*perMiB + remainder*perMiB/bytesPerMiB
	return time.Duration(floor + extra)
}

func parserWorkEstimate(sourceLen int) int64 {
	if sourceLen <= 0 {
		return 0
	}
	if int64(sourceLen) > math.MaxInt64/parserWorkIterationsPerByte {
		return math.MaxInt64
	}
	return int64(sourceLen) * parserWorkIterationsPerByte
}

func (b *parseBudget) check() bool {
	if b == nil || b.hit != "" {
		return b == nil
	}
	if err := b.ctx.Err(); err != nil {
		if err == context.DeadlineExceeded {
			b.hit = "context deadline"
		} else {
			b.hit = "context cancellation"
		}
		return false
	}
	if !time.Now().Before(b.deadline) {
		b.hit = "wall-clock deadline"
		return false
	}
	return true
}

func (b *parseBudget) reserveParserWork(sourceLen int) bool {
	if !b.check() {
		return false
	}
	needed := parserWorkEstimate(sourceLen)
	if needed > b.workLimit-b.workUsed {
		b.hit = "parser work budget"
		return false
	}
	b.workUsed += needed
	return true
}

func (b *parseBudget) remaining() time.Duration {
	if b == nil {
		return 0
	}
	remaining := time.Until(b.deadline)
	if remaining < 0 {
		return 0
	}
	return remaining
}

func (b *parseBudget) noteTreeStop(tree *gotreesitter.Tree) {
	if b == nil || tree == nil {
		return
	}
	switch tree.ParseStopReason() {
	case gotreesitter.ParseStopTimeout:
		b.hit = "wall-clock deadline"
	case gotreesitter.ParseStopCancelled:
		if b.ctx.Err() == context.DeadlineExceeded {
			b.hit = "context deadline"
		} else {
			b.hit = "context cancellation"
		}
	case gotreesitter.ParseStopIterationLimit:
		b.hit = "parser work budget"
	case gotreesitter.ParseStopStackDepthLimit:
		b.hit = "parser stack budget"
	case gotreesitter.ParseStopNodeLimit:
		b.hit = "parser node budget"
	case gotreesitter.ParseStopMemoryBudget:
		b.hit = "parser memory budget"
	}
}

func budgetFallback(source []byte, budget *parseBudget) *Document {
	reason := "parse budget"
	if budget != nil && budget.hit != "" {
		reason = budget.hit
	}
	return textFallbackDocument(source, Diagnostic{
		Code:     "MDPP-PARSE-005",
		Severity: SeverityWarning,
		Message:  fmt.Sprintf("parse budget hit (%s); source returned as text", reason),
	})
}

func textFallbackDocument(source []byte, diagnostic Diagnostic) *Document {
	src := normalizeLineEndings(append([]byte(nil), source...))
	unregister := registerSourcePositions(src)
	defer unregister()
	wholeRange := sourceRange(src, 0, len(src))
	if diagnostic.Range == (Range{}) {
		diagnostic.Range = wholeRange
	}
	paragraph := &Node{
		Type:     NodeParagraph,
		Children: []*Node{textNodeRange(string(src), wholeRange)},
		Range:    wholeRange,
	}
	return &Document{
		Root:        &Node{Type: NodeDocument, Children: []*Node{paragraph}, Range: wholeRange},
		Source:      src,
		diagnostics: []Diagnostic{diagnostic},
	}
}

func finishBudgetedParse(doc *Document, tree *gotreesitter.Tree, source []byte, ctx *parseCtx) (*Document, *gotreesitter.Tree) {
	if ctx == nil || ctx.budget == nil {
		return doc, tree
	}
	ctx.budget.check()
	if ctx.budget.hit != "" {
		if tree != nil {
			tree.Release()
		}
		return budgetFallback(source, ctx.budget), nil
	}
	return doc, tree
}
