package mdpp

import (
	"fmt"
	"strings"
	"testing"
)

// GFM 4.10: a table ends at the first blank line or at the start of another
// block. A header row and delimiter row that follow a table directly are two
// more body rows. The syntax tree starts a second table there; mdpp joins the
// two in its conversion. The expected trees are written from the rule.
func TestHeaderAndDelimiterUnderTableAreBodyRows(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"standalone", "|h|\n|-|\n|h|\n|-|\n", `Document "" [0-16 1:1-5:1]
 Table align="" "" [0-16 1:1-5:1]
  TableRow "" [0-3 1:1-1:4]
   TableCell "" [1-2 1:2-1:3]
    Text "h" [1-2 1:2-1:3]
  TableRow "" [8-11 3:1-3:4]
   TableCell "" [9-10 3:2-3:3]
    Text "h" [9-10 3:2-3:3]
  TableRow "" [12-15 4:1-4:4]
   TableCell "" [13-14 4:2-4:3]
    Text "-" [13-14 4:2-4:3]
`},
		{"after a break, then an indented heading", "***\n|h|\n|-|\n|h|\n|-|\n # h\n", `Document "" [0-25 1:1-7:1]
 ThematicBreak "" [0-4 1:1-2:1]
 Table align="" "" [4-20 2:1-6:1]
  TableRow "" [4-7 2:1-2:4]
   TableCell "" [5-6 2:2-2:3]
    Text "h" [5-6 2:2-2:3]
  TableRow "" [12-15 4:1-4:4]
   TableCell "" [13-14 4:2-4:3]
    Text "h" [13-14 4:2-4:3]
  TableRow "" [16-19 5:1-5:4]
   TableCell "" [17-18 5:2-5:3]
    Text "-" [17-18 5:2-5:3]
 Heading level="1" "" [20-25 6:1-7:1]
  Text "h" [23-24 6:4-6:5]
`},
		{"different cell counts keep the first alignment and width", "|a|b|\n|:-:|-|\n|c|\n|-|\n", `Document "" [0-22 1:1-5:1]
 Table align="center," "" [0-22 1:1-5:1]
  TableRow "" [0-5 1:1-1:6]
   TableCell "" [1-2 1:2-1:3]
    Text "a" [1-2 1:2-1:3]
   TableCell "" [3-4 1:4-1:5]
    Text "b" [3-4 1:4-1:5]
  TableRow "" [14-17 3:1-3:4]
   TableCell "" [15-16 3:2-3:3]
    Text "c" [15-16 3:2-3:3]
   TableCell "" [0-0 0:0-0:0]
  TableRow "" [18-21 4:1-4:4]
   TableCell "" [19-20 4:2-4:3]
    Text "-" [19-20 4:2-4:3]
   TableCell "" [0-0 0:0-0:0]
`},
		{"inside a quote", "> |h|\n> |-|\n> |h|\n> |-|\n", `Document "" [0-24 1:1-5:1]
 Blockquote "" [0-24 1:1-5:1]
  Table align="" "" [2-24 1:3-5:1]
   TableRow "" [2-5 1:3-1:6]
    TableCell "" [3-4 1:4-1:5]
     Text "h" [3-4 1:4-1:5]
   TableRow "" [14-17 3:3-3:6]
    TableCell "" [15-16 3:4-3:5]
     Text "h" [15-16 3:4-3:5]
   TableRow "" [20-23 4:3-4:6]
    TableCell "" [21-22 4:4-4:5]
     Text "-" [21-22 4:4-4:5]
`},
	}
	for _, tc := range cases {
		for _, fast := range []bool{true, false} {
			if got := parseWithFastPath(tc.src, fast); got != tc.want {
				t.Errorf("%s: %q (fast path %v)\ngot:\n%swant:\n%s", tc.name, tc.src, fast, got, tc.want)
			}
		}
	}
}

// A blank line, a quote line that is blank, or other content between two tables
// keeps them apart.
func TestTablesSeparatedByABlankLineStayTwoTables(t *testing.T) {
	for _, src := range []string{
		"|h|\n|-|\n\n|h|\n|-|\n",
		"|a|\n|-|\nx\n\n|h|\n|-|\n",
		"> |h|\n> |-|\n>\n> |h|\n> |-|\n",
		"***\n|h|\n|-|\n\n|h|\n|-|\n",
		// A line of spaces or tabs is a blank line (Markdown's definition), at
		// the top level and inside containers.
		"|a|\n|-|\n \n|b|\n|-|\n",
		"|a|\n|-|\n\t\n|b|\n|-|\n",
		"- |a|\n  |-|\n \n  |b|\n  |-|\n",
		"- |a|\n  |-|\n\t\n  |b|\n  |-|\n",
		"- |a|\n  |-|\n   \n  |b|\n  |-|\n",
		"1. |a|\n   |-|\n \n   |b|\n   |-|\n",
		"> |a|\n> |-|\n> \n> |b|\n> |-|\n",
		"> |a|\n> |-|\n>\t\n> |b|\n> |-|\n",
		"> - |a|\n>   |-|\n>  \n>   |b|\n>   |-|\n",
	} {
		for _, fast := range []bool{true, false} {
			root := parseTreeWithFastPath(src, fast)
			tables := 0
			walkNodes(root, func(n *Node, parent *Node, index int) bool {
				if n.Type == NodeTable {
					tables++
				}
				return true
			})
			if tables != 2 {
				t.Errorf("%q (fast path %v): %d tables, want 2:\n%s", src, fast, tables, fullNodeDump(root))
			}
		}
	}
}

// Without a line between them, two tables are one table, in a list and in a
// quote as at the top level.
func TestTablesWithoutALineBetweenAreOneTableInContainers(t *testing.T) {
	for _, src := range []string{
		"- |a|\n  |-|\n  |b|\n  |-|\n",
		"> |a|\n> |-|\n> |b|\n> |-|\n",
		"1. |a|\n   |-|\n   |b|\n   |-|\n",
	} {
		for _, fast := range []bool{true, false} {
			root := parseTreeWithFastPath(src, fast)
			tables := 0
			walkNodes(root, func(n *Node, parent *Node, index int) bool {
				if n.Type == NodeTable {
					tables++
				}
				return true
			})
			if tables != 1 {
				t.Errorf("%q (fast path %v): %d tables, want 1:\n%s", src, fast, tables, fullNodeDump(root))
			}
		}
	}
}

// A row that moves into the first table takes the first header's width (GFM
// 4.10): excess cells are dropped, missing cells are empty. A padding cell
// claims no source range; the cells that remain keep theirs.
func TestMergedRowsTakeTheFirstHeaderWidth(t *testing.T) {
	cases := []struct {
		name, src string
		widths    []int  // cells per row of the merged table
		cells     string // text and start byte of every cell that has text, per row
	}{
		{"more cells", "|a|\n|-|\n|b|c|\n|-|-|\n", []int{1, 1, 1}, "a@1 | b@9 | -@15"},
		{"fewer cells", "|a|b|\n|-|-|\n|c|\n|-|\n", []int{2, 2, 2}, "a@1 b@3 | c@13 | -@17"},
		{"second body row", "|a|\n|-|\n|b|\n|-|\n|c|d|\n", []int{1, 1, 1, 1}, "a@1 | b@9 | -@13 | c@17"},
		{"two columns, three cells", "|a|b|\n|-|-|\n|c|d|e|\n|-|-|-|\n", []int{2, 2, 2}, "a@1 b@3 | c@13 d@15 | -@21 -@23"},
		{"in a list", "- |a|\n  |-|\n  |b|c|\n  |-|-|\n", []int{1, 1, 1}, "a@3 | b@15 | -@23"},
	}
	for _, tc := range cases {
		for _, fast := range []bool{true, false} {
			root := parseTreeWithFastPath(tc.src, fast)
			tables := root.Find(NodeTable)
			if len(tables) != 1 {
				t.Errorf("%s (fast path %v): %d tables:\n%s", tc.name, fast, len(tables), fullNodeDump(root))
				continue
			}
			var widths []int
			var rows []string
			for _, row := range tables[0].Children {
				widths = append(widths, len(row.Children))
				var cells []string
				for _, cell := range row.Children {
					if len(cell.Children) == 0 {
						if cell.Range != (Range{}) && cell.Range.EndByte != cell.Range.StartByte {
							t.Errorf("%s (fast path %v): empty cell with range %v", tc.name, fast, cell.Range)
						}
						continue
					}
					cells = append(cells, fmt.Sprintf("%s@%d", cell.Children[0].Literal, cell.Range.StartByte))
				}
				rows = append(rows, strings.Join(cells, " "))
			}
			if fmt.Sprint(widths) != fmt.Sprint(tc.widths) || strings.Join(rows, " | ") != tc.cells {
				t.Errorf("%s (fast path %v): widths %v cells %q, want %v %q:\n%s", tc.name, fast, widths, strings.Join(rows, " | "), tc.widths, tc.cells, fullNodeDump(root))
			}
		}
	}
}
