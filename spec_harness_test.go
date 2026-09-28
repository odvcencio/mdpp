package mdpp

import (
	"reflect"
	"testing"
)

func TestCompareSpecFailures(t *testing.T) {
	want := []int{2, 5, 9}
	got := []int{2, 9, 11}
	newlyPassing, newlyFailing := compareSpecFailures(want, got)
	if !reflect.DeepEqual(newlyPassing, []int{5}) {
		t.Fatalf("newly passing examples = %v, want [5]", newlyPassing)
	}
	if !reflect.DeepEqual(newlyFailing, []int{11}) {
		t.Fatalf("newly failing examples = %v, want [11]", newlyFailing)
	}
}

func TestSpecSectionBaselineCounts(t *testing.T) {
	counts := summarizeSpecSections([]markdownSpecExample{
		{Example: 1, Section: "Lists"},
		{Example: 2, Section: "Lists"},
		{Example: 3, Section: "Links", Ext: "disabled"},
	})
	if got := counts["Lists"].Total; got != 2 {
		t.Fatalf("Lists total = %d, want 2", got)
	}
	if _, ok := counts["Links"]; ok {
		t.Fatal("disabled Links example included")
	}
}
