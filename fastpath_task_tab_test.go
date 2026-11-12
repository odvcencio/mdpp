package mdpp

import "testing"

// A tab after the checkbox of a task item is a permitted separator. The fast
// path used to build an ordinary list item for it.
func TestTaskItemSeparatedByTab(t *testing.T) {
	cases := []struct {
		src     string
		checked string
	}{
		{"***\n- [x]\ta\n", "true"},
		{"***\n- [X]\ta\n", "true"},
		{"***\n- [ ]\ta\n", "false"},
		{"***\n1. [x]\ta\n", "true"},
		{"***\n- [x] \ta\n", "true"},
		{"- [x]\ta\n", "true"},
	}
	for _, c := range cases {
		for _, fast := range []bool{true, false} {
			root := parseTreeWithFastPath(c.src, fast)
			var item *Node
			var walk func(n *Node)
			walk = func(n *Node) {
				if n.Type == NodeTaskListItem {
					item = n
				}
				for _, ch := range n.Children {
					walk(ch)
				}
			}
			walk(root)
			if item == nil {
				t.Errorf("%q (fast=%v): no task list item:\n%s", c.src, fast, fullNodeDump(root))
				continue
			}
			if item.Attrs["checked"] != c.checked {
				t.Errorf("%q (fast=%v): checked=%q, want %q", c.src, fast, item.Attrs["checked"], c.checked)
			}
		}
	}
}
