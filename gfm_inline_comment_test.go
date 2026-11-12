package mdpp

func init() {
	// The inline comment cases run in the GFM profile too (GFM example 645).
	inlineCommentParsers = append(inlineCommentParsers, struct {
		name string
		fn   func([]byte) (*Document, error)
	}{"GFM profile", func(src []byte) (*Document, error) {
		return ParseWithOptions(src, ParseOptions{Profile: ProfileGFM})
	}})
}
