package mdpp

import (
	"html"
	"io"
	"strings"

	nethtml "golang.org/x/net/html"
)

// URLKind identifies the HTML attribute that will receive a URL.
type URLKind int

const (
	// URLKindLink is an href attribute.
	URLKindLink URLKind = iota
	// URLKindImageSrc is the src attribute on an img element.
	URLKindImageSrc
	// URLKindSrc is a src attribute on a non-image element.
	URLKindSrc
	// URLKindSrcset is one candidate URL in a srcset attribute.
	URLKindSrcset
	// URLKindPoster is a poster attribute.
	URLKindPoster
	// URLKindAction is an action attribute.
	URLKindAction
	// URLKindFormAction is a formaction attribute.
	URLKindFormAction
	// URLKindXLinkHref is an xlink:href attribute.
	URLKindXLinkHref
	// URLKindEmbed is the URL carried by mdpp's embed output.
	URLKindEmbed
)

// defaultURLPolicy checks a URL before writing it into an HTML attribute, so
// it decodes character references once before applying the browser URL rules.
func defaultURLPolicy(kind URLKind, raw string) (string, bool) {
	if !checkDefaultURLPolicy(kind, html.UnescapeString(raw)) {
		return "", false
	}
	return raw, true
}

func defaultParsedURLPolicy(kind URLKind, parsed string) (string, bool) {
	if !checkDefaultURLPolicy(kind, parsed) {
		return "", false
	}
	return parsed, true
}

func checkDefaultURLPolicy(kind URLKind, check string) bool {
	check = stripURLControls(check)
	colon := strings.IndexByte(check, ':')
	if colon <= 0 || !validURLScheme(check[:colon]) {
		return true
	}
	switch asciiLower(check[:colon]) {
	case "javascript", "vbscript", "file":
		return false
	case "data":
		mediaType := check[colon+1:]
		if end := strings.IndexAny(mediaType, ";,"); end >= 0 {
			mediaType = mediaType[:end]
		}
		switch asciiLower(strings.TrimSpace(mediaType)) {
		case "image/png", "image/gif", "image/jpeg", "image/webp", "image/avif":
			if kind == URLKindImageSrc {
				return true
			}
		}
		return false
	default:
		return true
	}
}

func stripURLControls(value string) string {
	start, end := 0, len(value)
	for start < end && value[start] <= 0x20 {
		start++
	}
	for end > start && value[end-1] <= 0x20 {
		end--
	}
	value = value[start:end]
	if !strings.ContainsAny(value, "\t\r\n") {
		return value
	}
	var b strings.Builder
	b.Grow(len(value))
	for i := 0; i < len(value); i++ {
		switch value[i] {
		case '\t', '\r', '\n':
		default:
			b.WriteByte(value[i])
		}
	}
	return b.String()
}

func validURLScheme(scheme string) bool {
	if len(scheme) == 0 || !isASCIIAlpha(scheme[0]) {
		return false
	}
	for i := 1; i < len(scheme); i++ {
		c := scheme[i]
		if !isASCIIAlpha(c) && (c < '0' || c > '9') && c != '+' && c != '.' && c != '-' {
			return false
		}
	}
	return true
}

func isASCIIAlpha(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func asciiLower(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b.WriteByte(c)
	}
	return b.String()
}

func (r *Renderer) filterURL(kind URLKind, raw string) (string, bool) {
	if r != nil && r.unsafeHTML && !r.sanitize {
		return raw, true
	}
	return defaultURLPolicy(kind, raw)
}

func (r *Renderer) filterParsedURL(kind URLKind, parsed string) (string, bool) {
	filtered, ok := defaultParsedURLPolicy(kind, parsed)
	if !ok || filtered == "" || r == nil || r.urlPolicy == nil {
		return filtered, ok
	}
	filtered, ok = r.urlPolicy(kind, filtered)
	if !ok {
		return "", false
	}
	// A custom policy may rewrite a URL, but the built-in policy still gets
	// the final decision so it cannot accidentally re-enable a blocked scheme.
	return defaultParsedURLPolicy(kind, filtered)
}

func (r *Renderer) secureOutput(source string) string {
	if r != nil && r.unsafeHTML && !r.sanitize {
		return source
	}
	if r != nil && r.sanitize {
		return sanitizeHTML(r, source)
	}
	return filterHTMLURLs(r, source)
}

var urlAttributeKinds = map[string]URLKind{
	"href":       URLKindLink,
	"srcset":     URLKindSrcset,
	"poster":     URLKindPoster,
	"action":     URLKindAction,
	"formaction": URLKindFormAction,
	"xlink:href": URLKindXLinkHref,
	"data-src":   URLKindEmbed,
}

func urlKindForAttribute(tag, key string) (URLKind, bool) {
	key = asciiLower(key)
	if key == "src" {
		if asciiLower(tag) == "img" {
			return URLKindImageSrc, true
		}
		return URLKindSrc, true
	}
	kind, ok := urlAttributeKinds[key]
	return kind, ok
}

func filterHTMLURLs(r *Renderer, source string) string {
	z := nethtml.NewTokenizer(strings.NewReader(source))
	var out strings.Builder
	out.Grow(len(source))
	for {
		typ := z.Next()
		raw := string(z.Raw())
		if typ == nethtml.ErrorToken {
			if z.Err() != io.EOF && raw != "" {
				out.WriteString(raw)
			}
			break
		}
		if typ != nethtml.StartTagToken && typ != nethtml.SelfClosingTagToken {
			out.WriteString(raw)
			continue
		}
		token := z.Token()
		changed := false
		for i := range token.Attr {
			kind, isURL := urlKindForAttribute(token.Data, token.Attr[i].Key)
			if !isURL {
				continue
			}
			filtered, ok := filterURLAttribute(r, kind, token.Attr[i].Val)
			if !ok {
				filtered = ""
			}
			if filtered != token.Attr[i].Val {
				token.Attr[i].Val = filtered
				changed = true
			}
		}
		if changed {
			writeHTMLToken(&out, token, typ)
		} else {
			out.WriteString(raw)
		}
	}
	return out.String()
}

func filterURLAttribute(r *Renderer, kind URLKind, raw string) (string, bool) {
	if kind != URLKindSrcset {
		return r.filterParsedURL(kind, raw)
	}
	return filterSrcset(r, raw)
}

func filterSrcset(r *Renderer, srcset string) (string, bool) {
	var candidates []string
	changed := false
	for i := 0; i < len(srcset); {
		for i < len(srcset) && (isHTMLSpace(srcset[i]) || srcset[i] == ',') {
			i++
		}
		if i == len(srcset) {
			break
		}
		start := i
		for i < len(srcset) && !isHTMLSpace(srcset[i]) {
			i++
		}
		urlToken := srcset[start:i]
		urlEnd := len(urlToken)
		for urlEnd > 0 && urlToken[urlEnd-1] == ',' {
			urlEnd--
		}
		url := urlToken[:urlEnd]
		descriptor := ""
		if urlEnd == len(urlToken) {
			for i < len(srcset) && isHTMLSpace(srcset[i]) {
				i++
			}
			descStart := i
			for i < len(srcset) && srcset[i] != ',' {
				i++
			}
			descriptor = strings.TrimSpace(srcset[descStart:i])
		}
		filtered, ok := r.filterParsedURL(URLKindSrcset, url)
		if !ok {
			return "", false
		}
		if filtered != url {
			changed = true
		}
		if descriptor != "" {
			filtered += " " + descriptor
		}
		candidates = append(candidates, filtered)
		if i < len(srcset) && srcset[i] == ',' {
			i++
		}
	}
	if !changed {
		return srcset, true
	}
	return strings.Join(candidates, ", "), true
}

func isHTMLSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f'
}

func writeHTMLToken(out *strings.Builder, token nethtml.Token, typ nethtml.TokenType) {
	if typ == nethtml.EndTagToken {
		out.WriteString("</")
		out.WriteString(token.Data)
		out.WriteByte('>')
		return
	}
	out.WriteByte('<')
	out.WriteString(token.Data)
	for _, attr := range token.Attr {
		out.WriteByte(' ')
		out.WriteString(attr.Key)
		out.WriteString(`="`)
		out.WriteString(html.EscapeString(attr.Val))
		out.WriteByte('"')
	}
	if typ == nethtml.SelfClosingTagToken {
		out.WriteString(" />")
	} else {
		out.WriteByte('>')
	}
}

var sanitizerTags = stringSet(
	"a", "abbr", "b", "blockquote", "br", "caption", "code", "dd", "del", "details", "div", "dl", "dt",
	"em", "figcaption", "figure", "h1", "h2", "h3", "h4", "h5", "h6", "hr", "i", "img", "input", "ins",
	"kbd", "li", "nav", "ol", "p", "pre", "q", "s", "samp", "section", "small", "span", "strike", "strong",
	"sub", "summary", "sup", "table", "tbody", "td", "tfoot", "th", "thead", "tr", "u", "ul", "var",
)

var sanitizerDroppedElements = stringSet("script", "style", "iframe", "object", "embed", "form", "base", "meta", "link")
var sanitizerDroppedContents = stringSet("script", "style", "iframe", "object", "form")
var gfmFilteredTags = stringSet("title", "textarea", "style", "xmp", "iframe", "noembed", "noframes", "script", "plaintext")
var sanitizerEscapedTextTags = stringSet("title", "textarea", "xmp", "noembed", "noframes")

func stringSet(values ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

func hasString(set map[string]struct{}, value string) bool {
	_, ok := set[value]
	return ok
}

func sanitizeHTML(r *Renderer, source string) string {
	z := nethtml.NewTokenizer(strings.NewReader(source))
	var out strings.Builder
	out.Grow(len(source))
	var dropping []string
	var escapedText []string
	plaintext := false
	for {
		typ := z.Next()
		raw := string(z.Raw())
		if typ == nethtml.ErrorToken {
			break
		}
		if typ != nethtml.StartTagToken && typ != nethtml.EndTagToken && typ != nethtml.SelfClosingTagToken {
			if len(dropping) != 0 {
				continue
			}
			if plaintext || len(escapedText) != 0 {
				out.WriteString(strings.ReplaceAll(raw, "<", "&lt;"))
			} else if typ == nethtml.TextToken {
				out.WriteString(raw)
			}
			continue
		}
		token := z.Token()
		name := asciiLower(token.Data)
		isStart := typ == nethtml.StartTagToken || typ == nethtml.SelfClosingTagToken
		if len(dropping) != 0 {
			if isStart && hasString(sanitizerDroppedContents, name) {
				dropping = append(dropping, name)
			} else if typ == nethtml.EndTagToken {
				for i := len(dropping) - 1; i >= 0; i-- {
					if dropping[i] == name {
						dropping = dropping[:i]
						break
					}
				}
			}
			continue
		}
		if typ == nethtml.EndTagToken && len(escapedText) != 0 && escapedText[len(escapedText)-1] == name {
			out.WriteString(escapeTagStart(raw))
			escapedText = escapedText[:len(escapedText)-1]
			continue
		}
		if isStart && hasString(sanitizerDroppedElements, name) {
			if hasString(sanitizerDroppedContents, name) {
				dropping = append(dropping, name)
			}
			continue
		}
		if hasString(gfmFilteredTags, name) {
			out.WriteString(escapeTagStart(raw))
			if isStart && hasString(sanitizerEscapedTextTags, name) {
				escapedText = append(escapedText, name)
			}
			if isStart && name == "plaintext" {
				plaintext = true
			}
			continue
		}
		if !hasString(sanitizerTags, name) {
			continue
		}
		if typ == nethtml.EndTagToken {
			out.WriteString("</")
			out.WriteString(name)
			out.WriteByte('>')
			continue
		}
		filtered := token
		filtered.Attr = nil
		for _, attr := range token.Attr {
			key := asciiLower(attr.Key)
			if strings.HasPrefix(key, "on") || !allowedSanitizerAttribute(name, key) {
				continue
			}
			value := attr.Val
			if kind, isURL := urlKindForAttribute(name, key); isURL {
				var ok bool
				value, ok = filterURLAttribute(r, kind, value)
				if !ok {
					value = ""
				}
			}
			filtered.Attr = append(filtered.Attr, nethtml.Attribute{Key: key, Val: value})
		}
		writeHTMLToken(&out, filtered, typ)
	}
	return out.String()
}

func escapeTagStart(raw string) string {
	if strings.HasPrefix(raw, "<") {
		return "&lt;" + raw[1:]
	}
	return raw
}

func allowedSanitizerAttribute(tag, key string) bool {
	switch key {
	case "class", "id", "title", "role", "aria-label", "aria-hidden", "lang", "dir",
		"data-mdpp-source-start", "data-mdpp-source-end", "data-mdpp-source-line", "data-mdpp-source-col",
		"data-mdpp-source-end-line", "data-mdpp-source-end-col", "data-mdpp-container", "data-diagram-syntax", "data-diagram-kind":
		return true
	case "href":
		return tag == "a"
	case "src", "alt", "width", "height", "srcset":
		return tag == "img"
	case "data-src", "data-provider":
		return tag == "div"
	case "align":
		return tag == "table" || tag == "td" || tag == "th"
	case "scope", "colspan", "rowspan":
		return tag == "td" || tag == "th"
	case "start":
		return tag == "ol"
	case "open":
		return tag == "details"
	case "type", "checked", "disabled":
		return tag == "input"
	default:
		return false
	}
}
