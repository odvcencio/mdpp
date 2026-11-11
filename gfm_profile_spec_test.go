package mdpp

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"testing"
)

// The CommonMark 0.31.2 and GFM 0.29 specification examples, read in the GFM
// profile and compared by rendered HTML, the way TestCommonMarkSpec and
// TestGFMSpec compare the default profile. The examples that fail are listed in
// testdata/spec/gfm-profile-known-failures.json with the section they are in;
// the test fails when the list changes, in either direction.
const gfmProfileFailuresPath = "testdata/spec/gfm-profile-known-failures.json"

type gfmProfileFailures map[string][]int

func gfmProfileSpecResults(t testing.TB, file string) (passBySection map[string][2]int, failing []int) {
	data, err := specFiles.ReadFile("testdata/spec/" + file)
	if err != nil {
		t.Fatal(err)
	}
	var examples []markdownSpecExample
	if err := json.Unmarshal(data, &examples); err != nil {
		t.Fatal(err)
	}
	passBySection = map[string][2]int{}
	for _, example := range examples {
		if example.Ext == "disabled" {
			continue
		}
		counts := passBySection[example.Section]
		counts[1]++
		doc, err := ParseWithOptions([]byte(example.Markdown), ParseOptions{Profile: ProfileGFM})
		ok := err == nil
		if ok {
			options := RenderOptions{UnsafeHTML: true, HeadingIDs: false, ImageTitleAttribute: true}
			if example.Ext == "tagfilter" {
				options = RenderOptions{UnsafeHTML: true, Sanitize: true, ImageTitleAttribute: true}
			}
			var got []byte
			got, err = Render(doc, options)
			ok = err == nil && normalizeSpecHTML(string(got)) == normalizeSpecHTML(example.HTML)
		}
		if ok {
			counts[0]++
		} else {
			failing = append(failing, example.Example)
		}
		passBySection[example.Section] = counts
	}
	sort.Ints(failing)
	return passBySection, failing
}

func TestGFMProfileSpecExamples(t *testing.T) {
	data, err := os.ReadFile(gfmProfileFailuresPath)
	if err != nil {
		t.Fatal(err)
	}
	var known gfmProfileFailures
	if err := json.Unmarshal(data, &known); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"commonmark-0.31.2.json", "gfm-0.29.json"} {
		sections, failing := gfmProfileSpecResults(t, file)
		var names []string
		pass, total := 0, 0
		for name, c := range sections {
			names = append(names, name)
			pass += c[0]
			total += c[1]
		}
		sort.Strings(names)
		for _, name := range names {
			t.Logf("GFMPROFILE %s / %s: pass %d/%d", file, name, sections[name][0], sections[name][1])
		}
		t.Logf("GFMPROFILE %s: pass %d/%d, failing %v", file, pass, total, failing)
		if fmt.Sprint(failing) != fmt.Sprint(known[file]) {
			t.Errorf("%s: failing examples in the GFM profile are %v, the checked-in list is %v; review the change, then update %s",
				file, failing, known[file], gfmProfileFailuresPath)
		}
	}
}
