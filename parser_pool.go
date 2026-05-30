package mdpp

import (
	"sync"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// glrInlineTimeoutMicros is the per-parse timeout applied to the inline
// markdown GLR parser. The inline grammar's parse-stack can grow super-linearly
// on a long, break-free span of inline-ambiguous characters (e.g. a 12 KB JSON
// blob on a single line with no spaces). 2 000 000 µs (2 s) is generous for
// any legitimate inline span — real authored content parses in milliseconds —
// but tight enough to abort pathological inputs before they consume significant
// CPU or memory.
//
// The existing length pre-guard (maxGLRLineBytes = 16 384) handles inputs above
// ~16 KB before they reach inline parsing. This timeout is the backstop for
// sub-threshold inputs that are still dense enough to grind the GLR engine.
const glrInlineTimeoutMicros = 2_000_000 // 2 seconds

var parserPools sync.Map

// inlinePool is the parser pool used exclusively for the markdown_inline
// grammar.  It is configured with a per-parse timeout so pathologically
// ambiguous inline spans terminate quickly instead of grinding for minutes.
var (
	inlinePoolOnce sync.Once
	inlinePool     *gotreesitter.ParserPool
)

// inlineParserPool returns (creating on first call) the timeout-configured
// pool for the markdown inline language.
func inlineParserPool() *gotreesitter.ParserPool {
	inlinePoolOnce.Do(func() {
		lang := inlineLang()
		if lang != nil {
			inlinePool = gotreesitter.NewParserPool(lang,
				gotreesitter.WithParserPoolTimeoutMicros(glrInlineTimeoutMicros),
			)
		}
	})
	return inlinePool
}

func parserPoolFor(lang *gotreesitter.Language) *gotreesitter.ParserPool {
	if lang == nil {
		return nil
	}
	if pool, ok := parserPools.Load(lang); ok {
		return pool.(*gotreesitter.ParserPool)
	}
	pool := gotreesitter.NewParserPool(lang)
	actual, _ := parserPools.LoadOrStore(lang, pool)
	return actual.(*gotreesitter.ParserPool)
}

func parsePooled(lang *gotreesitter.Language, entry *grammars.LangEntry, source []byte) (*gotreesitter.Tree, error) {
	pool := parserPoolFor(lang)
	if pool == nil {
		return nil, gotreesitter.ErrNoLanguage
	}
	if entry != nil && entry.TokenSourceFactory != nil {
		ts := entry.TokenSourceFactory(source, lang)
		return pool.ParseWithTokenSource(source, ts)
	}
	return pool.Parse(source)
}

// parsePooledInline parses source using the timeout-configured inline language
// pool.  It returns the tree and whether the parse was stopped early due to
// complexity (timeout or iteration limit).  On early stop the tree may be nil
// or partial; callers must fall back to raw text.
//
// Two stop reasons both indicate "GLR engine aborted early":
//   - ParseStopTimeout: the per-parse wall-clock timeout fired.
//   - ParseStopIterationLimit: gotreesitter's built-in iteration cap fired
//     (sourceLen*30 iterations).  For the inline grammar this is reached only
//     on pathologically dense, space-free input; legitimate markdown parses
//     well within the limit.
//
// Both cases produce a partial tree that does not cover the full input, so
// both are routed to the raw-text fallback in parseInlineWithRecoveryAt.
func parsePooledInline(source []byte) (*gotreesitter.Tree, bool, error) {
	pool := inlineParserPool()
	if pool == nil {
		return nil, false, gotreesitter.ErrNoLanguage
	}
	entry := mdInlineEntry
	var tree *gotreesitter.Tree
	var err error
	if entry != nil && entry.TokenSourceFactory != nil {
		ts := entry.TokenSourceFactory(source, inlineLang())
		tree, err = pool.ParseWithTokenSource(source, ts)
	} else {
		tree, err = pool.Parse(source)
	}
	if err != nil {
		return nil, false, err
	}
	if tree != nil {
		switch tree.ParseStopReason() {
		case gotreesitter.ParseStopTimeout, gotreesitter.ParseStopIterationLimit:
			return tree, true, nil
		}
	}
	return tree, false, nil
}

// parseIncrementalFromTree re-parses source using oldTree as a starting point.
// oldTree must already have Edit() applied for each edit.
func parseIncrementalFromTree(lang *gotreesitter.Language, entry *grammars.LangEntry, source []byte, oldTree *gotreesitter.Tree) (*gotreesitter.Tree, error) {
	if lang == nil {
		return nil, gotreesitter.ErrNoLanguage
	}
	// Use a fresh parser — ParserPool parsers get reset on release, which
	// would otherwise interfere with incremental state tracking.
	parser := gotreesitter.NewParser(lang)
	if entry != nil && entry.TokenSourceFactory != nil {
		ts := entry.TokenSourceFactory(source, lang)
		return parser.ParseIncrementalWithTokenSource(source, oldTree, ts)
	}
	return parser.ParseIncremental(source, oldTree)
}
