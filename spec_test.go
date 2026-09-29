package mdpp

import (
	"embed"
	"encoding/json"
	"flag"
	"html"
	mrand "math/rand"
	"regexp"
	"sort"
	"strings"
	"testing"
	"unicode"
)

// The CommonMark spec license and exact upstream JSON fixtures are in testdata/spec.
//
//go:embed testdata/spec/commonmark-0.31.2.json testdata/spec/gfm-0.29.json
var specFiles embed.FS

const (
	// Raise these floors only after measuring the full spec suite.
	commonMarkPassFloor = 468
	gfmPassFloor        = 462
	gfmLostTextCeiling  = 0
)

var updateSpecFailures = flag.Bool("update-spec-failures", false, "refresh the checked-in known spec failures")

var (
	specTagsRE   = regexp.MustCompile(`(?s)<[^>]*>`)
	specWordsRE  = regexp.MustCompile(`[A-Za-z0-9]+`)
	specSpaceRE  = regexp.MustCompile(`\s+`)
	specEntityRE = regexp.MustCompile(`&(#x[0-9A-Fa-f]+|#[0-9]+|[A-Za-z][A-Za-z0-9]+);`)
)

func TestCommonMarkSpec(t *testing.T) {
	runMarkdownSpec(t, "commonmark-0.31.2.json", commonMarkPassFloor, 0)
}

func TestGFMSpec(t *testing.T) {
	runMarkdownSpec(t, "gfm-0.29.json", gfmPassFloor, gfmLostTextCeiling)
}

func runMarkdownSpec(t *testing.T, file string, passFloor, lostTextCeiling int) {
	t.Helper()
	data, err := specFiles.ReadFile("testdata/spec/" + file)
	if err != nil {
		t.Fatal(err)
	}
	var examples []markdownSpecExample
	if err := json.Unmarshal(data, &examples); err != nil {
		t.Fatalf("decode %s: %v", file, err)
	}

	passed := 0
	sections := summarizeSpecSections(examples)
	sectionFailures := make(map[string][]int)
	total := 0
	lostText := 0
	emptyOutput := 0
	var mismatches, lostExamples, emptyExamples []int
	for _, example := range examples {
		disabled := example.Ext == "disabled"
		if !disabled {
			total++
		}
		doc, err := Parse([]byte(example.Markdown))
		if err != nil {
			t.Errorf("example %d: Parse: %v", example.Example, err)
			if !disabled {
				mismatches = append(mismatches, example.Example)
			}
			continue
		}
		assertSpecDiagnosticRanges(t, example, doc)
		options := RenderOptions{UnsafeHTML: true, HeadingIDs: false}
		if example.Ext == "tagfilter" {
			// PR #6 adds the GFM tagfilter in sanitizing mode.
			options = RenderOptions{UnsafeHTML: true, Sanitize: true}
		}
		got, err := Render(doc, options)
		if err != nil {
			t.Errorf("example %d: Render: %v", example.Example, err)
			if !disabled {
				mismatches = append(mismatches, example.Example)
			}
			continue
		}
		if disabled {
			if hasLostSpecWords(example.HTML, string(got)) {
				t.Errorf("disabled example %d lost visible words", example.Example)
			}
			continue
		}
		wantHTML := normalizeSpecHTML(example.HTML)
		gotHTML := normalizeSpecHTML(string(got))
		if gotHTML == wantHTML {
			passed++
			sections[example.Section].Passed++
		} else {
			sectionFailures[example.Section] = append(sectionFailures[example.Section], example.Example)
			mismatches = append(mismatches, example.Example)
		}
		if strings.TrimSpace(string(got)) == "" && strings.TrimSpace(example.HTML) != "" {
			emptyOutput++
			emptyExamples = append(emptyExamples, example.Example)
		}
		if hasLostSpecWords(example.HTML, string(got)) {
			lostText++
			lostExamples = append(lostExamples, example.Example)
		}
	}
	if *updateSpecFailures {
		if err := writeSpecFailures(file, sections, sectionFailures); err != nil {
			t.Fatalf("write known spec failures: %v", err)
		}
	}
	sectionNames := make([]string, 0, len(sections))
	for section := range sections {
		sectionNames = append(sectionNames, section)
	}
	sort.Strings(sectionNames)
	for _, section := range sectionNames {
		count := sections[section]
		t.Logf("%s / %s: pass %d/%d", file, section, count.Passed, count.Total)
	}
	if !*updateSpecFailures {
		if err := assertKnownSpecFailures(file, sectionFailures); err != nil {
			t.Error(err)
		}
	}

	t.Logf("%s: pass %d/%d (floor %d), lost text %d (ceiling %d), empty output %d; mismatches %v",
		file, passed, total, passFloor, lostText, lostTextCeiling, emptyOutput, mismatches)
	if passed < passFloor {
		t.Errorf("%s passed %d examples, below floor %d; failing examples: %v", file, passed, passFloor, mismatches)
	}
	if lostText > lostTextCeiling {
		t.Errorf("%s lost visible words in %d examples, above ceiling %d; examples: %v", file, lostText, lostTextCeiling, lostExamples)
	}
	if len(emptyExamples) > 0 {
		t.Errorf("%s produced empty output for examples: %v", file, emptyExamples)
	}
}

func TestRandomSpecExamplePairsPreserveWordsAndDiagnosticRanges(t *testing.T) {
	examples := loadAllSpecExamples(t)
	random := mrand.New(mrand.NewSource(20260927))
	for pair := 0; pair < 128; pair++ {
		first := examples[random.Intn(len(examples))]
		second := examples[random.Intn(len(examples))]
		source := []byte(first.Markdown + "\n\n" + second.Markdown)
		doc, err := Parse(source)
		if err != nil {
			t.Errorf("pair %d: Parse: %v", pair, err)
			continue
		}
		for _, diagnostic := range doc.Diagnostics() {
			assertDiagnosticRangeWithinSource(t, source, pair, diagnostic)
		}
		options := RenderOptions{UnsafeHTML: true, HeadingIDs: false}
		if first.Ext == "tagfilter" || second.Ext == "tagfilter" {
			options.Sanitize = true
		}
		got, err := Render(doc, options)
		if err != nil {
			t.Errorf("pair %d: Render: %v", pair, err)
			continue
		}
		want := first.HTML + "\n" + second.HTML
		if hasLostSpecWords(want, string(got)) {
			t.Errorf("pair %d lost visible words (examples %d and %d)", pair, first.Example, second.Example)
		}
	}
}

func loadAllSpecExamples(t *testing.T) []markdownSpecExample {
	t.Helper()
	var all []markdownSpecExample
	for _, file := range []string{"commonmark-0.31.2.json", "gfm-0.29.json"} {
		data, err := specFiles.ReadFile("testdata/spec/" + file)
		if err != nil {
			t.Fatal(err)
		}
		var examples []markdownSpecExample
		if err := json.Unmarshal(data, &examples); err != nil {
			t.Fatalf("decode %s: %v", file, err)
		}
		all = append(all, examples...)
	}
	if len(all) == 0 {
		t.Fatal("spec fixtures contain no examples")
	}
	return all
}

func assertSpecDiagnosticRanges(t *testing.T, example markdownSpecExample, doc *Document) {
	t.Helper()
	source := []byte(example.Markdown)
	for _, diagnostic := range doc.Diagnostics() {
		assertDiagnosticRangeWithinSource(t, source, example.Example, diagnostic)
	}
}

func assertDiagnosticRangeWithinSource(t *testing.T, source []byte, example int, diagnostic Diagnostic) {
	t.Helper()
	r := diagnostic.Range
	if r.StartByte < 0 || r.EndByte <= r.StartByte || r.EndByte > len(source) {
		t.Errorf("example %d: %s has an empty or out-of-source byte range %+v for %d bytes", example, diagnostic.Code, r, len(source))
		return
	}
	if r.StartLine < 1 || r.EndLine < r.StartLine || sourceRange(source, r.StartByte, r.EndByte) != r {
		t.Errorf("example %d: %s has invalid source line positions %+v", example, diagnostic.Code, r)
	}
}

func hasLostSpecWords(wantHTML, gotHTML string) bool {
	want := specWordCounts(wantHTML)
	got := specWordCounts(gotHTML)
	for word, count := range want {
		if got[word] < count {
			return true
		}
	}
	return false
}

func specWordCounts(source string) map[string]int {
	plain := html.UnescapeString(specTagsRE.ReplaceAllString(source, " "))
	counts := make(map[string]int)
	for _, word := range specWordsRE.FindAllString(plain, -1) {
		counts[word]++
	}
	return counts
}

// normalizeSpecHTML ports normalize.py's HTMLParser behavior closely enough
// to compare spec renderings while ignoring attribute order and insignificant
// HTML whitespace.
func normalizeSpecHTML(source string) string {
	var out strings.Builder
	last, lastTag := "starttag", ""
	inPre := false
	writeData := func(data string) {
		afterTag := last == "endtag" || last == "starttag"
		afterBlockTag := afterTag && isSpecBlockTag(lastTag)
		if afterTag && lastTag == "br" {
			data = strings.TrimLeft(data, "\n")
		}
		if !inPre {
			data = specSpaceRE.ReplaceAllString(data, " ")
		}
		if afterBlockTag && !inPre {
			if last == "starttag" {
				data = strings.TrimLeftFunc(data, unicode.IsSpace)
			} else if last == "endtag" {
				data = strings.TrimSpace(data)
			}
		}
		out.WriteString(data)
		last = "data"
	}
	writeEntity := func(entity string) {
		decoded := html.UnescapeString(entity)
		if decoded == entity {
			out.WriteString(entity)
		} else {
			for _, r := range decoded {
				switch r {
				case '<':
					out.WriteString("&lt;")
				case '>':
					out.WriteString("&gt;")
				case '&':
					out.WriteString("&amp;")
				case '"':
					out.WriteString("&quot;")
				default:
					out.WriteRune(r)
				}
			}
		}
		last = "ref"
	}

	for i := 0; i < len(source); {
		if strings.HasPrefix(source[i:], "<![CDATA[") {
			if end := strings.Index(source[i+9:], "]]>"); end >= 0 {
				end += i + 9 + 3
				out.WriteString(source[i:end])
				i = end
				continue
			}
		}
		if source[i] == '<' {
			if end := strings.IndexByte(source[i:], '>'); end >= 0 {
				end += i + 1
				token := source[i:end]
				switch {
				case strings.HasPrefix(token, "<!--"):
					out.WriteString(token)
					last = "comment"
				case strings.HasPrefix(token, "</"):
					tag := specTagName(token[2:])
					if tag == "pre" {
						inPre = false
					}
					if isSpecBlockTag(tag) {
						trimBuilderRight(&out)
					}
					out.WriteString("</" + tag + ">")
					lastTag, last = tag, "endtag"
				case strings.HasPrefix(token, "<!"):
					out.WriteString("<!" + strings.TrimSuffix(token[2:], ">") + ">")
					last = "decl"
				case strings.HasPrefix(token, "<?"):
					out.WriteString("<?" + strings.TrimSuffix(token[2:], ">") + ">")
					last = "pi"
				default:
					name := specTagName(strings.TrimLeft(token[1:], "/"))
					if isSpecBlockTag(name) {
						trimBuilderRight(&out)
					}
					if name == "pre" {
						inPre = true
					}
					out.WriteByte('<')
					out.WriteString(name)
					for _, attr := range specAttributes(token, name) {
						out.WriteByte(' ')
						out.WriteString(attr.name)
						if attr.value != nil {
							value := *attr.value
							// Port normalize.py exactly: its URL branch checks the
							// attribute value, not the attribute name.
							if value == "href" || value == "src" {
								value = quoteSpecURL(unquoteSpecURL(value))
							}
							out.WriteString(`="` + escapeSpecAttribute(value) + `"`)
						}
					}
					out.WriteByte('>')
					lastTag, last = name, "starttag"
					if strings.HasSuffix(strings.TrimSpace(token), "/>") {
						last = "endtag"
					}
				}
				i = end
				continue
			}
		}

		end := strings.IndexByte(source[i:], '<')
		if end < 0 {
			end = len(source)
		} else {
			end += i
		}
		textChunk := source[i:end]
		matches := specEntityRE.FindAllStringIndex(textChunk, -1)
		cursor := 0
		for _, match := range matches {
			if match[0] > cursor {
				writeData(textChunk[cursor:match[0]])
			}
			writeEntity(textChunk[match[0]:match[1]])
			cursor = match[1]
		}
		if cursor < len(textChunk) {
			writeData(textChunk[cursor:])
		}
		if len(textChunk) == 0 {
			writeData("<")
			end = i + 1
		}
		i = end
	}
	return out.String()
}

type specAttr struct {
	name  string
	value *string
}

func specAttributes(token, tagName string) []specAttr {
	body := strings.TrimSuffix(token[1+len(tagName):], ">")
	body = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(body), "/"))
	var attrs []specAttr
	for len(body) > 0 {
		body = strings.TrimLeftFunc(body, unicode.IsSpace)
		if body == "" {
			break
		}
		i := 0
		for i < len(body) && !unicode.IsSpace(rune(body[i])) && body[i] != '=' && body[i] != '/' {
			i++
		}
		if i == 0 {
			body = body[1:]
			continue
		}
		name := strings.ToLower(body[:i])
		body = body[i:]
		body = strings.TrimLeftFunc(body, unicode.IsSpace)
		var value *string
		if strings.HasPrefix(body, "=") {
			body = strings.TrimLeftFunc(body[1:], unicode.IsSpace)
			if body != "" {
				quote := body[0]
				if quote == '"' || quote == '\'' {
					body = body[1:]
					end := strings.IndexByte(body, quote)
					if end < 0 {
						end = len(body)
					}
					v := html.UnescapeString(body[:end])
					value = &v
					if end < len(body) {
						body = body[end+1:]
					} else {
						body = ""
					}
				} else {
					end := 0
					for end < len(body) && !unicode.IsSpace(rune(body[end])) && body[end] != '>' {
						end++
					}
					v := html.UnescapeString(body[:end])
					value = &v
					body = body[end:]
				}
			}
		}
		attrs = append(attrs, specAttr{name: name, value: value})
	}
	sort.SliceStable(attrs, func(i, j int) bool {
		if attrs[i].name != attrs[j].name {
			return attrs[i].name < attrs[j].name
		}
		iv, jv := "", ""
		if attrs[i].value != nil {
			iv = *attrs[i].value
		}
		if attrs[j].value != nil {
			jv = *attrs[j].value
		}
		return iv < jv
	})
	return attrs
}

func specTagName(body string) string {
	body = strings.TrimSpace(body)
	if strings.HasPrefix(body, "/") {
		body = body[1:]
	}
	end := 0
	for end < len(body) && !unicode.IsSpace(rune(body[end])) && body[end] != '/' && body[end] != '>' {
		end++
	}
	return strings.ToLower(body[:end])
}

func escapeSpecAttribute(value string) string {
	value = strings.ReplaceAll(value, "&", "&amp;")
	value = strings.ReplaceAll(value, "<", "&lt;")
	value = strings.ReplaceAll(value, ">", "&gt;")
	value = strings.ReplaceAll(value, `"`, "&quot;")
	return strings.ReplaceAll(value, "'", "&#x27;")
}

func unquoteSpecURL(value string) string {
	var out strings.Builder
	for i := 0; i < len(value); i++ {
		if value[i] == '%' && i+2 < len(value) {
			if a, okA := fromHex(value[i+1]); okA {
				if b, okB := fromHex(value[i+2]); okB {
					out.WriteByte(a<<4 | b)
					i += 2
					continue
				}
			}
		}
		out.WriteByte(value[i])
	}
	return out.String()
}

func quoteSpecURL(value string) string {
	const hex = "0123456789ABCDEF"
	var out strings.Builder
	for i := 0; i < len(value); i++ {
		b := value[i]
		if (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || strings.ContainsRune("-_.~/", rune(b)) {
			out.WriteByte(b)
		} else {
			out.WriteByte('%')
			out.WriteByte(hex[b>>4])
			out.WriteByte(hex[b&15])
		}
	}
	return out.String()
}

func fromHex(b byte) (byte, bool) {
	switch {
	case b >= '0' && b <= '9':
		return b - '0', true
	case b >= 'a' && b <= 'f':
		return b - 'a' + 10, true
	case b >= 'A' && b <= 'F':
		return b - 'A' + 10, true
	default:
		return 0, false
	}
}

func trimBuilderRight(builder *strings.Builder) {
	text := strings.TrimRightFunc(builder.String(), unicode.IsSpace)
	builder.Reset()
	builder.WriteString(text)
}

func isSpecBlockTag(tag string) bool {
	switch tag {
	case "article", "header", "aside", "hgroup", "blockquote", "hr", "iframe", "body", "li", "map", "button", "object", "canvas", "ol", "caption", "output", "col", "colgroup", "p", "pre", "dd", "progress", "div", "section", "dl", "table", "td", "dt", "tbody", "embed", "textarea", "fieldset", "tfoot", "figcaption", "th", "figure", "thead", "footer", "tr", "form", "ul", "h1", "h2", "h3", "h4", "h5", "h6", "video", "script", "style":
		return true
	default:
		return false
	}
}
