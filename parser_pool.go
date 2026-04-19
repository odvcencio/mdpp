package mdpp

import (
	"sync"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

var parserPools sync.Map

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
