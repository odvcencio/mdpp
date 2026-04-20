package fmt

import (
	"bytes"
	"testing"
)

func TestFormatCanonicalizesHeadingsListsAndDirectives(t *testing.T) {
	src := []byte("Title\n=====\n\n1) item\n\n[[ TOC ]]\n\n[[ Embed:https://example.com/Video?q=A ]]\n")
	got, err := Format(src)
	if err != nil {
		t.Fatal(err)
	}
	want := "# Title\n\n1. item\n\n[[toc]]\n\n[[embed:https://example.com/Video?q=A]]\n"
	if string(got) != want {
		t.Fatalf("Format() = %q, want %q", got, want)
	}
}

func TestFormatPreservesFenceBodyBytes(t *testing.T) {
	src := []byte("```Go\nfmt.Println(\"hi\")  \n\n\t// keep\n```\n")
	got, err := Format(src)
	if err != nil {
		t.Fatal(err)
	}
	want := "``` go\nfmt.Println(\"hi\")  \n\n\t// keep\n```\n"
	if string(got) != want {
		t.Fatalf("Format() = %q, want %q", got, want)
	}
}

func TestFormatCollectsReferenceAndFootnoteDefinitions(t *testing.T) {
	src := []byte("See [B][b] and [A][a]. Note[^z].\n\n[^z]: trailing\n[b]: https://b.example\n[a]: https://a.example\n")
	got, err := Format(src)
	if err != nil {
		t.Fatal(err)
	}
	want := "See [B][b] and [A][a]. Note[^z].\n\n[a]: https://a.example\n[b]: https://b.example\n\n[^z]: trailing\n"
	if string(got) != want {
		t.Fatalf("Format() = %q, want %q", got, want)
	}
}

func TestFormatCanonicalizesListEmphasisAndTasks(t *testing.T) {
	src := []byte("* _one_\n+ [X] __two__\n- [✓] three\n")
	got, err := Format(src)
	if err != nil {
		t.Fatal(err)
	}
	want := "- *one*\n- [x] **two**\n- [x] three\n"
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
	want := "3. three\n4. one\n5. nine\n"
	if string(got) != want {
		t.Fatalf("Format() = %q, want %q", got, want)
	}
}

func TestFormatRewritesHardBreakSpacesToBackslash(t *testing.T) {
	src := []byte("Hard break here.  \nNext line.\n")
	got, err := Format(src)
	if err != nil {
		t.Fatal(err)
	}
	want := "Hard break here.\\\nNext line.\n"
	if string(got) != want {
		t.Fatalf("Format() = %q, want %q", got, want)
	}
}

func TestFormatUnwrapsSimpleParagraphProse(t *testing.T) {
	src := []byte("This is a simple\nwrapped paragraph\nwith no inline markup.\n")
	got, err := Format(src)
	if err != nil {
		t.Fatal(err)
	}
	want := "This is a simple wrapped paragraph with no inline markup.\n"
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

func TestFormatCanonicalizesSafeTildeFence(t *testing.T) {
	src := []byte("~~~Go\nfmt.Println(\"hi\")\n~~~\n")
	got, err := Format(src)
	if err != nil {
		t.Fatal(err)
	}
	want := "``` go\nfmt.Println(\"hi\")\n```\n"
	if string(got) != want {
		t.Fatalf("Format() = %q, want %q", got, want)
	}
}

func TestFormatCanonicalizesTildeFenceWithSingleBacktick(t *testing.T) {
	src := []byte("~~~Text\n`inline`\n~~~\n")
	got, err := Format(src)
	if err != nil {
		t.Fatal(err)
	}
	want := "``` text\n`inline`\n```\n"
	if string(got) != want {
		t.Fatalf("Format() = %q, want %q", got, want)
	}
}

func TestFormatCanonicalizesSimplePipeTables(t *testing.T) {
	src := []byte("|  Name  |  Value  |\n| :---: | ---: |\n|  **a**  |  [b](https://example.com)  |\n")
	got, err := Format(src)
	if err != nil {
		t.Fatal(err)
	}
	want := "| Name | Value |\n|:---:|---:|\n| **a** | [b](https://example.com) |\n"
	if string(got) != want {
		t.Fatalf("Format() = %q, want %q", got, want)
	}
}

func TestFormatNormalizesNestedListIndentation(t *testing.T) {
	src := []byte("- one\n   - two\n      - three\n")
	got, err := Format(src)
	if err != nil {
		t.Fatal(err)
	}
	want := "- one\n  - two\n    - three\n"
	if string(got) != want {
		t.Fatalf("Format() = %q, want %q", got, want)
	}
}

func TestFormatCanonicalizesAdmonitionMarkers(t *testing.T) {
	src := []byte("> [!note]   Heads up\n>    body\n")
	got, err := Format(src)
	if err != nil {
		t.Fatal(err)
	}
	want := "> [!NOTE] Heads up\n> body\n"
	if string(got) != want {
		t.Fatalf("Format() = %q, want %q", got, want)
	}
}

func TestFormatCanonicalizesContainerFences(t *testing.T) {
	src := []byte("::::DETAILS   \"Trace\"\nBody\n::::\n")
	got, err := Format(src)
	if err != nil {
		t.Fatal(err)
	}
	want := "::::details \"Trace\"\nBody\n::::\n"
	if string(got) != want {
		t.Fatalf("Format() = %q, want %q", got, want)
	}
}

func TestFormatCanonicalizesFenceAttrs(t *testing.T) {
	src := []byte("```Go Key=VALUE Foo=Bar\nx\n```\n")
	got, err := Format(src)
	if err != nil {
		t.Fatal(err)
	}
	want := "``` go key=VALUE foo=Bar\nx\n```\n"
	if string(got) != want {
		t.Fatalf("Format() = %q, want %q", got, want)
	}
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
			t.Fatalf("Format(%q) returned error: %v", src, err)
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
