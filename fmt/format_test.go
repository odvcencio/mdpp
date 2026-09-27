package fmt

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"m31labs.dev/mdpp"
)

func TestFormatCanonicalizesSetextHeadingsAndPreservesDirectiveLikeText(t *testing.T) {
	src := []byte("Title\n=====\n\n1) item\n\n[[ TOC ]]\n\n[[ Embed:https://example.com/Video?q=A ]]\n")
	got, err := Format(src)
	if err != nil {
		t.Fatal(err)
	}
	want := "# Title\n\n1) item\n\n[[ TOC ]]\n\n[[ Embed:https://example.com/Video?q=A ]]\n"
	if string(got) != want {
		t.Fatalf("Format() = %q, want %q", got, want)
	}
}

func TestFormatPreservesFencedCodeBytes(t *testing.T) {
	src := []byte("```Go\nfmt.Println(\"hi\")  \n\n\t// keep\n```\n")
	got, err := Format(src)
	if err != nil {
		t.Fatal(err)
	}
	want := string(src)
	if string(got) != want {
		t.Fatalf("Format() = %q, want %q", got, want)
	}
}

func TestFormatPreservesReferenceAndFootnoteDefinitionOrder(t *testing.T) {
	src := []byte("See [B][b] and [A][a]. Note[^z].\n\n[^z]: trailing\n[b]: https://b.example\n[a]: https://a.example\n")
	got, err := Format(src)
	if err != nil {
		t.Fatal(err)
	}
	want := string(src)
	if string(got) != want {
		t.Fatalf("Format() = %q, want %q", got, want)
	}
}

func TestFormatPreservesEmphasisAndListMarkers(t *testing.T) {
	src := []byte("* _one_\n+ [X] __two__\n- [✓] three\n")
	got, err := Format(src)
	if err != nil {
		t.Fatal(err)
	}
	want := string(src)
	if string(got) != want {
		t.Fatalf("Format() = %q, want %q", got, want)
	}
}

func TestFormatRenumbersOrderedListItems(t *testing.T) {
	src := []byte("3) three\n1) one\n9) nine\n")
	got, err := Format(src)
	if err != nil {
		t.Fatal(err)
	}
	want := "3) three\n4) one\n5) nine\n"
	if string(got) != want {
		t.Fatalf("Format() = %q, want %q", got, want)
	}
}

func TestFormatPreservesHardBreakSpaces(t *testing.T) {
	src := []byte("Hard break here.  \nNext line.\n")
	got, err := Format(src)
	if err != nil {
		t.Fatal(err)
	}
	want := string(src)
	if string(got) != want {
		t.Fatalf("Format() = %q, want %q", got, want)
	}
}

func TestFormatPreservesWrappedParagraphProse(t *testing.T) {
	src := []byte("This is a simple\nwrapped paragraph\nwith no inline markup.\n")
	got, err := Format(src)
	if err != nil {
		t.Fatal(err)
	}
	want := string(src)
	if string(got) != want {
		t.Fatalf("Format() = %q, want %q", got, want)
	}
}

func TestFormatIdempotent(t *testing.T) {
	once, err := Format([]byte("# Title #\n\n\nText  \n"))
	if err != nil {
		t.Fatal(err)
	}
	twice, err := Format(once)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(once, twice) {
		t.Fatalf("Format not idempotent:\nonce:  %q\ntwice: %q", once, twice)
	}
}

func TestFormatEmptyHeadingIsIdempotent(t *testing.T) {
	once, err := Format([]byte("#\n00"))
	if err != nil {
		t.Fatal(err)
	}
	twice, err := Format(once)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(once, twice) {
		t.Fatalf("Format not idempotent:\nonce:  %q\ntwice: %q", once, twice)
	}
	if want := "#\n00\n"; string(once) != want {
		t.Fatalf("Format() = %q, want %q", once, want)
	}
}

func TestFormatHashPrefixedParagraphIsIdempotent(t *testing.T) {
	once, err := Format([]byte("#0\n000000000000000"))
	if err != nil {
		t.Fatal(err)
	}
	twice, err := Format(once)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(once, twice) {
		t.Fatalf("Format not idempotent:\nonce:  %q\ntwice: %q", once, twice)
	}
	if want := "#0\n000000000000000\n"; string(once) != want {
		t.Fatalf("Format() = %q, want %q", once, want)
	}
}

func TestFormatPreservesUnwrappedListMarker(t *testing.T) {
	once, err := Format([]byte("* 00\n0"))
	if err != nil {
		t.Fatal(err)
	}
	twice, err := Format(once)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(once, twice) {
		t.Fatalf("Format not idempotent:\nonce:  %q\ntwice: %q", once, twice)
	}
	if want := "* 00\n0\n"; string(once) != want {
		t.Fatalf("Format() = %q, want %q", once, want)
	}
}

func TestFormatPreservesSafeTildeFence(t *testing.T) {
	src := []byte("~~~Go\nfmt.Println(\"hi\")\n~~~\n")
	got, err := Format(src)
	if err != nil {
		t.Fatal(err)
	}
	want := string(src)
	if string(got) != want {
		t.Fatalf("Format() = %q, want %q", got, want)
	}
}

func TestFormatPreservesTildeFenceWithSingleBacktick(t *testing.T) {
	src := []byte("~~~Text\n`inline`\n~~~\n")
	got, err := Format(src)
	if err != nil {
		t.Fatal(err)
	}
	want := string(src)
	if string(got) != want {
		t.Fatalf("Format() = %q, want %q", got, want)
	}
}

func TestFormatPreservesPipeTableSpacing(t *testing.T) {
	src := []byte("|  Name  |  Value  |\n| :---: | ---: |\n|  **a**  |  [b](https://example.com)  |\n")
	got, err := Format(src)
	if err != nil {
		t.Fatal(err)
	}
	want := string(src)
	if string(got) != want {
		t.Fatalf("Format() = %q, want %q", got, want)
	}
}

func TestFormatPreservesNestedListIndentation(t *testing.T) {
	src := []byte("- one\n   - two\n      - three\n")
	got, err := Format(src)
	if err != nil {
		t.Fatal(err)
	}
	want := string(src)
	if string(got) != want {
		t.Fatalf("Format() = %q, want %q", got, want)
	}
}

func TestFormatPreservesLowercaseAdmonitionLikeText(t *testing.T) {
	src := []byte("> [!note]   Heads up\n>    body\n")
	got, err := Format(src)
	if err != nil {
		t.Fatal(err)
	}
	want := string(src)
	if string(got) != want {
		t.Fatalf("Format() = %q, want %q", got, want)
	}
}

func TestFormatPreservesContainerDirectiveBytes(t *testing.T) {
	src := []byte("::::DETAILS   \"Trace\"\nBody\n::::\n")
	got, err := Format(src)
	if err != nil {
		t.Fatal(err)
	}
	want := string(src)
	if string(got) != want {
		t.Fatalf("Format() = %q, want %q", got, want)
	}
}

func TestFormatPreservesFenceInfoBytes(t *testing.T) {
	src := []byte("```Go Key=VALUE Foo=Bar\nx\n```\n")
	got, err := Format(src)
	if err != nil {
		t.Fatal(err)
	}
	want := string(src)
	if string(got) != want {
		t.Fatalf("Format() = %q, want %q", got, want)
	}
}

func TestFormatPreservesMeaning(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("testdata", "meaning", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("meaning corpus is empty")
	}
	for _, path := range paths {
		path := path
		t.Run(filepath.Base(path), func(t *testing.T) {
			source, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			assertFormatMeaning(t, source)
		})
	}

	data, err := os.ReadFile(filepath.Join("..", "testdata", "spec", "commonmark-0.31.2.json"))
	if err != nil {
		t.Fatal(err)
	}
	var examples []struct {
		Example  int    `json:"example"`
		Section  string `json:"section"`
		Markdown string `json:"markdown"`
	}
	if err := json.Unmarshal(data, &examples); err != nil {
		t.Fatalf("decode CommonMark examples: %v", err)
	}
	if len(examples) < 600 {
		t.Fatalf("CommonMark examples = %d, want the complete 0.31.2 set", len(examples))
	}
	for _, example := range examples {
		example := example
		name := strings.ReplaceAll(example.Section, "/", "-")
		t.Run(filepath.Join("commonmark", name, formatExampleNumber(example.Example)), func(t *testing.T) {
			assertFormatMeaning(t, []byte(example.Markdown))
		})
	}

	if corpus := os.Getenv("MDPP_FMT_CORPUS"); corpus != "" {
		var corpusPaths []string
		err := filepath.WalkDir(corpus, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !entry.IsDir() && strings.EqualFold(filepath.Ext(path), ".md") {
				corpusPaths = append(corpusPaths, path)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk MDPP_FMT_CORPUS: %v", err)
		}
		sort.Strings(corpusPaths)
		for _, path := range corpusPaths {
			path := path
			t.Run(filepath.Join("external", filepath.Base(path)), func(t *testing.T) {
				source, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				assertFormatMeaning(t, source)
			})
		}
	}
}

func TestFormatMeaningGuardReturnsOriginal(t *testing.T) {
	source := []byte("---\ntitle: Example\n---")
	formatted, err := Format(source)
	if !errors.Is(err, ErrMeaningChanged) {
		t.Fatalf("Format error = %v, want ErrMeaningChanged", err)
	}
	if !bytes.Equal(formatted, source) {
		t.Fatalf("Format output = %q, want original input %q", formatted, source)
	}
	if !strings.Contains(err.Error(), "frontmatter bytes changed") {
		t.Fatalf("Format error = %q, want first difference named", err)
	}
}

func TestFormatUsesParserFrontmatterRange(t *testing.T) {
	source := []byte("---\ntitle: Archive\nbody: |\n  ---\n  key: value\n  ---\n  content\n---\n\n# End\n")
	doc, err := mdpp.Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	if got := doc.Frontmatter()["title"]; got != "Archive" {
		t.Fatalf("frontmatter title = %v, want Archive", got)
	}
	body, ok := doc.Frontmatter()["body"].(string)
	if !ok || !strings.Contains(body, "---\nkey: value\n---\ncontent") {
		t.Fatalf("frontmatter body did not retain its indented fence lines")
	}
	fm := doc.AST().Find(mdpp.NodeFrontmatter)
	if len(fm) != 1 || fm[0].Range.StartByte != 0 || fm[0].Range.EndByte > len(source) {
		t.Fatalf("frontmatter AST range = %+v, want one leading source range", fm)
	}
	if got := string(source[fm[0].Range.StartByte:fm[0].Range.EndByte]); !strings.HasSuffix(got, "---\n") {
		t.Fatalf("frontmatter AST range does not include the closing fence")
	}
}

func assertFormatMeaning(t *testing.T, source []byte) {
	t.Helper()
	formatted, err := Format(source)
	if err != nil {
		if !errors.Is(err, ErrMeaningChanged) {
			t.Fatalf("Format: %v", err)
		}
		if !bytes.Equal(formatted, source) {
			t.Fatal("meaning guard returned output different from its input")
		}
	}
	if difference := meaningDifference(source, formatted); difference != "" {
		t.Fatalf("formatted output changed meaning: %s", difference)
	}
	twice, err := Format(formatted)
	if err != nil {
		if !errors.Is(err, ErrMeaningChanged) {
			t.Fatalf("Format second pass: %v", err)
		}
		if !bytes.Equal(twice, formatted) {
			t.Fatal("meaning guard returned output different from its input on second pass")
		}
	}
	if !bytes.Equal(formatted, twice) {
		t.Fatal("Format is not idempotent")
	}
}

func formatExampleNumber(n int) string {
	return strconv.Itoa(n)
}

func FuzzFormat(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte("# Title #\n\nText  \n"),
		[]byte("|  A  |  B  |\n|---|---|\n| x | y |\n"),
		[]byte("- one\n   - two\n"),
		[]byte("> [!WARNING]  Heads up\n>  body\n"),
		[]byte(":::DETAILS \"Trace\"\nBody\n:::\n"),
		[]byte("```Go Key=VALUE\nx\n```\n"),
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, src []byte) {
		if len(src) > 8192 {
			return
		}
		got, err := Format(src)
		if err != nil {
			if !errors.Is(err, ErrMeaningChanged) {
				t.Fatalf("Format(%q) returned error: %v", src, err)
			}
			if !bytes.Equal(got, src) {
				t.Fatalf("Format changed input after meaning guard failed:\ninput: %q\noutput: %q", src, got)
			}
			return
		}
		if difference := meaningDifference(src, got); difference != "" {
			t.Fatalf("Format changed meaning (%s):\ninput: %q\noutput: %q", difference, src, got)
		}
		got2, err := Format(got)
		if err != nil {
			t.Fatalf("Format(idempotence pass) returned error: %v", err)
		}
		if !bytes.Equal(got, got2) {
			t.Fatalf("Format not idempotent:\nfirst:  %q\nsecond: %q", got, got2)
		}
	})
}
