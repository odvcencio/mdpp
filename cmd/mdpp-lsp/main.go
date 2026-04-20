package main

import (
	"context"
	"fmt"
	"os"

	"github.com/odvcencio/mdpp/lsp"
)

func main() {
	if err := lsp.Serve(context.Background(), os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "mdpp-lsp: %v\n", err)
		os.Exit(1)
	}
}
