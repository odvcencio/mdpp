package mdpp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Reuses the -update flag already defined by conformance_test.go.
// Run: go test -run TestCSTSnapshots -update

// TestCSTSnapshots is the per-extension CST shape contract. Every file under
// testdata/cst-snapshots/<extension>/input.md must produce a tree shape
// matching the sibling expected.tree file. A failure means the grammar /
// extension passes produced a different AST shape than the last snapshot.
//
// During the v0.2 grammar-extension migration this is the load-bearing
// regression surface: a snapshot diff is the per-cut review artifact.
func TestCSTSnapshots(t *testing.T) {
	root := filepath.Join("testdata", "cst-snapshots")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read snapshots dir: %v", err)
	}
	if len(entries) == 0 {
		t.Skip("no CST snapshots yet")
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		t.Run(name, func(t *testing.T) {
			inputPath := filepath.Join(root, name, "input.md")
			treePath := filepath.Join(root, name, "expected.tree")

			src, err := os.ReadFile(inputPath)
			if err != nil {
				t.Fatalf("read %s: %v", inputPath, err)
			}
			doc, perr := Parse(src)
			if perr != nil {
				t.Fatalf("parse %s: %v", inputPath, perr)
			}
			got := DumpTreeForSnapshot(doc.AST())

			if *updateConformance {
				if err := os.WriteFile(treePath, []byte(got), 0o644); err != nil {
					t.Fatalf("write golden: %v", err)
				}
				t.Logf("updated %s", treePath)
				return
			}

			want, err := os.ReadFile(treePath)
			if err != nil {
				t.Fatalf("read golden %s: %v (run with -update to create)", treePath, err)
			}
			if got != string(want) {
				t.Errorf("CST shape drifted for %s\n--- want\n%s\n--- got\n%s", name, want, got)
			}
		})
	}
}

// DumpTreeForSnapshot produces a stable, line-oriented dump of a Node tree.
// One line per node:
//
//	  <indent>(<NodeType> [attrs] "<literal-if-leaf>")
//
// Range info, parent pointers, and pointer identities are deliberately
// excluded — those drift across runs without indicating semantic change.
// Snapshots compare shape and content, not memory.
func DumpTreeForSnapshot(n *Node) string {
	if n == nil {
		return "(nil)\n"
	}
	var sb strings.Builder
	dumpNode(&sb, n, 0)
	return sb.String()
}

func dumpNode(sb *strings.Builder, n *Node, depth int) {
	for i := 0; i < depth; i++ {
		sb.WriteString("  ")
	}
	sb.WriteByte('(')
	sb.WriteString(n.Type.String())
	if len(n.Attrs) > 0 {
		// Deterministic key order.
		keys := make([]string, 0, len(n.Attrs))
		for k := range n.Attrs {
			keys = append(keys, k)
		}
		sortStrings(keys)
		for _, k := range keys {
			fmt.Fprintf(sb, " %s=%q", k, n.Attrs[k])
		}
	}
	if n.Literal != "" {
		fmt.Fprintf(sb, " %q", n.Literal)
	}
	sb.WriteByte(')')
	sb.WriteByte('\n')
	for _, c := range n.Children {
		dumpNode(sb, c, depth+1)
	}
}

// sortStrings is a 6-line bubble; pulling in sort.Strings just for one
// snapshot helper isn't worth the import.
func sortStrings(s []string) {
	for i := 0; i < len(s); i++ {
		for j := i + 1; j < len(s); j++ {
			if s[j] < s[i] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
}
