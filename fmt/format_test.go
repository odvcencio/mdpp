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
