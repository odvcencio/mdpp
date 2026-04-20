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
	src := []byte("```Go\nfmt.Println(\"hi\")  \n\t// keep\n```\n")
	got, err := Format(src)
	if err != nil {
		t.Fatal(err)
	}
	want := "``` go\nfmt.Println(\"hi\")  \n\t// keep\n```\n"
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
