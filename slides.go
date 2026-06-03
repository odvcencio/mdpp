package mdpp

import "strings"

// --- gosx-slides parse seams (Track D) ---
//
// These passes run post-parse and reinterpret already-parsed nodes rather than
// changing the grammar, which keeps the parser fully backward-compatible:
// ordinary Markdown (a < b, lowercase <div>, literal braces) is never
// reinterpreted.
//
// Slide splitting changes the top-level document shape, so unlike the inline
// component/expression passes it is NOT part of the default postProcess
// pipeline — it is opt-in via SplitSlides / Document.Slides. This guarantees
// the default Parse AST is byte-identical to before for existing consumers
// (hyphae, docs, the gosx content pipeline) while the deck compiler gets the
// NodeSlide layer on demand.

// SplitSlides reinterprets a parsed document into a deck: it groups the
// document's top-level children into NodeSlide containers (see processSlides).
// It mutates and returns the same Document for chaining, and is idempotent — a
// second call on an already-split document is a no-op (it does not re-wrap the
// existing slides), so SplitSlides and Document.Slides are safe to mix on the
// same document.
//
// A leading YAML/YML fenced code block on a slide is lifted into that slide's
// per-slide frontmatter (Attrs["frontmatter"]) and removed from its rendered
// children; deck-level frontmatter stays a document-level NodeFrontmatter
// sibling ahead of the first slide. See processSlides for the full rules.
func SplitSlides(doc *Document) *Document {
	if doc == nil || doc.Root == nil {
		return doc
	}
	if !documentHasSlides(doc.Root) {
		processSlides(doc.Root)
	}
	return doc
}

// Slides returns the deck's top-level NodeSlide nodes, splitting the document
// in place on first use. If the document has already been split it returns the
// existing slides without re-splitting. As with SplitSlides, a leading YAML/YML
// fence on a slide is lifted into Attrs["frontmatter"] (see processSlides).
func (d *Document) Slides() []*Node {
	if d == nil || d.Root == nil {
		return nil
	}
	if !documentHasSlides(d.Root) {
		processSlides(d.Root)
	}
	var out []*Node
	for _, c := range d.Root.Children {
		if c != nil && c.Type == NodeSlide {
			out = append(out, c)
		}
	}
	return out
}

// documentHasSlides reports whether the document body already carries a slide
// layer, so Slides() is idempotent.
func documentHasSlides(root *Node) bool {
	if root == nil {
		return false
	}
	for _, c := range root.Children {
		if c != nil && c.Type == NodeSlide {
			return true
		}
	}
	return false
}

// processSlides groups a document's top-level children into NodeSlide
// containers, splitting at each top-level NodeThematicBreak (`---`). A `---`
// inside a fenced code block is part of that CodeBlock node and is never a
// top-level ThematicBreak, so it does not split.
//
// Document frontmatter (a leading NodeFrontmatter) is left as a document-level
// sibling ahead of the first slide so deck-level metadata is not swallowed into
// slide one. A YAML/YML fenced code block that is the first content node of a
// slide is lifted onto that slide as its per-slide frontmatter (stored in
// Attrs["frontmatter"]).
//
// If the document contains no thematic breaks the whole body still becomes a
// single slide, so downstream deck consumers always see a uniform NodeSlide
// layer.
func processSlides(root *Node) {
	if root == nil || root.Type != NodeDocument {
		return
	}

	// Preserve a leading document-level frontmatter block as a sibling.
	lead := 0
	var preamble []*Node
	for lead < len(root.Children) {
		c := root.Children[lead]
		if c != nil && c.Type == NodeFrontmatter {
			preamble = append(preamble, c)
			lead++
			continue
		}
		break
	}

	body := root.Children[lead:]

	// Group the body into slides, splitting on top-level thematic breaks.
	var slides []*Node
	cur := newSlide()
	for _, child := range body {
		if child != nil && child.Type == NodeThematicBreak {
			slides = append(slides, finalizeSlide(cur))
			cur = newSlide()
			continue
		}
		cur.Children = append(cur.Children, child)
	}
	// Always emit the trailing slide. A document that is only frontmatter
	// (no body) yields a single empty slide, keeping the layer uniform; a
	// trailing separator likewise yields a final (empty) slide so authors can
	// end a deck on a blank slide deliberately.
	slides = append(slides, finalizeSlide(cur))

	root.Children = append(preamble, slides...)
}

// newSlide allocates an empty slide container.
func newSlide() *Node {
	return &Node{Type: NodeSlide}
}

// finalizeSlide lifts a leading YAML fence into the slide's frontmatter attr
// and returns the slide.
func finalizeSlide(s *Node) *Node {
	if s == nil || len(s.Children) == 0 {
		return s
	}
	first := s.Children[0]
	if first == nil || first.Type != NodeCodeBlock {
		return s
	}
	lang := strings.ToLower(strings.TrimSpace(first.Attr("language")))
	if lang != "yaml" && lang != "yml" {
		return s
	}
	if s.Attrs == nil {
		s.Attrs = make(map[string]string)
	}
	s.Attrs["frontmatter"] = first.Literal
	s.Children = s.Children[1:]
	return s
}
