package mdpp

import (
	"sync"
	"sync/atomic"
	"time"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// glrInlineTimeoutMicros bounds unbudgeted inline-parser calls. Normal document
// parsing uses the deterministic work budget and document-wide deadline.
const glrInlineTimeoutMicros = 2_000_000 // 2 seconds

var parserPools sync.Map

type budgetedParserPoolKey struct {
	language      *gotreesitter.Language
	timeoutMicros uint64
}

// ParserPool accepts a fixed timeout for every checkout. Budgeted parses use
// one-second timeout buckets so their parsers can be reused. Caller-selected
// tighter deadlines and contexts with cancellation use a direct parser so the
// pool's timeout rounding cannot extend the requested deadline.
var budgetedParserPools sync.Map

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

func budgetedParserPoolFor(lang *gotreesitter.Language, timeout time.Duration) *gotreesitter.ParserPool {
	if lang == nil || timeout <= 0 {
		return nil
	}
	seconds := timeout / time.Second
	if timeout%time.Second != 0 {
		seconds++
	}
	if seconds < 1 {
		seconds = 1
	}
	timeoutMicros := uint64(seconds) * 1_000_000
	key := budgetedParserPoolKey{language: lang, timeoutMicros: timeoutMicros}
	if pool, ok := budgetedParserPools.Load(key); ok {
		return pool.(*gotreesitter.ParserPool)
	}
	pool := gotreesitter.NewParserPool(lang, gotreesitter.WithParserPoolTimeoutMicros(timeoutMicros))
	actual, _ := budgetedParserPools.LoadOrStore(key, pool)
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

func parsePooledWithBudget(lang *gotreesitter.Language, entry *grammars.LangEntry, source []byte, budget *parseBudget, timeoutCap time.Duration) (*gotreesitter.Tree, error) {
	if budget == nil {
		return parsePooled(lang, entry, source)
	}
	if lang == nil {
		return nil, gotreesitter.ErrNoLanguage
	}
	if (budget.ctx == nil || budget.ctx.Done() == nil) && !budget.exactDeadline {
		timeout := budget.remaining()
		if timeoutCap > 0 && timeoutCap < timeout {
			timeout = timeoutCap
		}
		if timeoutCap <= 0 && !budget.reserveParserWork(len(source)) {
			return nil, nil
		}
		if timeoutCap <= 0 {
			if timeout <= 0 {
				budget.check()
				return nil, nil
			}
			pool := budgetedParserPoolFor(lang, timeout)
			if pool == nil {
				return nil, gotreesitter.ErrNoLanguage
			}
			if entry != nil && entry.TokenSourceFactory != nil {
				ts := entry.TokenSourceFactory(source, lang)
				return pool.ParseWithTokenSource(source, ts)
			}
			return pool.Parse(source)
		}
	}
	parser := gotreesitter.NewParser(lang)
	return parseWithBudgetParser(parser, entry, source, budget, timeoutCap)
}

func parseWithBudgetParser(parser *gotreesitter.Parser, entry *grammars.LangEntry, source []byte, budget *parseBudget, timeoutCap time.Duration) (*gotreesitter.Tree, error) {
	if budget == nil {
		return nil, gotreesitter.ErrNoLanguage
	}
	if !budget.reserveParserWork(len(source)) {
		return nil, nil
	}
	if parser == nil {
		return nil, gotreesitter.ErrNoLanguage
	}
	stopBudgetWatch := configureParserBudget(parser, budget, timeoutCap)
	defer stopBudgetWatch()
	if entry != nil && entry.TokenSourceFactory != nil {
		ts := entry.TokenSourceFactory(source, parser.Language())
		return parser.ParseWithTokenSource(source, ts)
	}
	return parser.Parse(source)
}

func parseIncrementalFromTreeWithBudget(lang *gotreesitter.Language, entry *grammars.LangEntry, source []byte, oldTree *gotreesitter.Tree, budget *parseBudget) (*gotreesitter.Tree, error) {
	if budget == nil {
		return parseIncrementalFromTree(lang, entry, source, oldTree)
	}
	if !budget.reserveParserWork(len(source)) {
		return nil, nil
	}
	if lang == nil {
		return nil, gotreesitter.ErrNoLanguage
	}
	parser := gotreesitter.NewParser(lang)
	stopBudgetWatch := configureParserBudget(parser, budget, 0)
	defer stopBudgetWatch()
	if entry != nil && entry.TokenSourceFactory != nil {
		ts := entry.TokenSourceFactory(source, lang)
		return parser.ParseIncrementalWithTokenSource(source, oldTree, ts)
	}
	return parser.ParseIncremental(source, oldTree)
}

func configureParserBudget(parser *gotreesitter.Parser, budget *parseBudget, timeoutCap time.Duration) func() {
	if parser == nil || budget == nil {
		return func() {}
	}
	timeout := budget.remaining()
	if timeoutCap > 0 && timeoutCap < timeout {
		timeout = timeoutCap
	}
	if timeout <= 0 {
		budget.check()
		return func() {}
	}
	micros := uint64(timeout / time.Microsecond)
	if micros == 0 {
		micros = 1
	}
	parser.SetTimeoutMicros(micros)
	if budget.ctx == nil || budget.ctx.Done() == nil {
		return func() {}
	}

	var cancelled uint32
	parser.SetCancellationFlag(&cancelled)
	stop := make(chan struct{})
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		select {
		case <-budget.ctx.Done():
			atomic.StoreUint32(&cancelled, 1)
		case <-stop:
		}
	}()
	return func() {
		parser.SetCancellationFlag(nil)
		close(stop)
		<-watchDone
	}
}

// parsePooledInline parses source under the document's deterministic work
// budget and wall-clock backstop. It returns whether parsing stopped early;
// callers must then fall back to raw text.
func parsePooledInline(source []byte, contexts ...*parseCtx) (*gotreesitter.Tree, bool, error) {
	var ctx *parseCtx
	if len(contexts) > 0 {
		ctx = contexts[0]
	}
	if ctx != nil && ctx.budget != nil {
		tree, err := parsePooledWithBudget(inlineLang(), mdInlineEntry, source, ctx.budget, 0)
		if err != nil || tree == nil {
			ctx.budget.check()
			return nil, false, err
		}
		ctx.budget.noteTreeStop(tree)
		if tree.ParseStopReason() == gotreesitter.ParseStopTimeout {
			return tree, true, nil
		}
		if ctx.budget.hit != "" {
			return tree, true, nil
		}
		return tree, false, nil
	}
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
