package mdpp

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

const specFailuresPath = "testdata/spec/known-failures.json"

type markdownSpecExample struct {
	Example  int    `json:"example"`
	Section  string `json:"section"`
	Ext      string `json:"ext"`
	Markdown string `json:"markdown"`
	HTML     string `json:"html"`
}

type specSectionCount struct {
	Total  int
	Passed int
}

type specFailuresDocument struct {
	Datasets map[string]specFailureDataset `json:"datasets"`
}

type specFailureDataset struct {
	Sections map[string]specFailureSection `json:"sections"`
}

type specFailureSection struct {
	Total          int   `json:"total"`
	BaselinePassed int   `json:"baseline_passed"`
	Examples       []int `json:"examples"`
}

func summarizeSpecSections(examples []markdownSpecExample) map[string]*specSectionCount {
	counts := make(map[string]*specSectionCount)
	for _, example := range examples {
		if example.Ext == "disabled" {
			continue
		}
		count := counts[example.Section]
		if count == nil {
			count = &specSectionCount{}
			counts[example.Section] = count
		}
		count.Total++
	}
	return counts
}

func compareSpecFailures(known, actual []int) (newlyPassing, newlyFailing []int) {
	knownSet := make(map[int]struct{}, len(known))
	actualSet := make(map[int]struct{}, len(actual))
	for _, example := range known {
		knownSet[example] = struct{}{}
	}
	for _, example := range actual {
		actualSet[example] = struct{}{}
	}
	for example := range knownSet {
		if _, remains := actualSet[example]; !remains {
			newlyPassing = append(newlyPassing, example)
		}
	}
	for example := range actualSet {
		if _, wasKnown := knownSet[example]; !wasKnown {
			newlyFailing = append(newlyFailing, example)
		}
	}
	sort.Ints(newlyPassing)
	sort.Ints(newlyFailing)
	return newlyPassing, newlyFailing
}

func readSpecFailures() (specFailuresDocument, error) {
	data, err := os.ReadFile(specFailuresPath)
	if err != nil {
		return specFailuresDocument{}, err
	}
	var known specFailuresDocument
	if err := json.Unmarshal(data, &known); err != nil {
		return specFailuresDocument{}, fmt.Errorf("decode %s: %w", specFailuresPath, err)
	}
	return known, nil
}

func writeSpecFailures(file string, counts map[string]*specSectionCount, actual map[string][]int) error {
	known, err := readSpecFailures()
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if known.Datasets == nil {
		known.Datasets = make(map[string]specFailureDataset)
	}
	dataset := specFailureDataset{Sections: make(map[string]specFailureSection, len(counts))}
	if existing, ok := known.Datasets[file]; ok {
		dataset = existing
		if dataset.Sections == nil {
			dataset.Sections = make(map[string]specFailureSection)
		}
	}
	for section, count := range counts {
		baseline, exists := dataset.Sections[section]
		if !exists {
			baseline.Total = count.Total
			baseline.BaselinePassed = count.Passed
		}
		baseline.Examples = append([]int(nil), actual[section]...)
		sort.Ints(baseline.Examples)
		dataset.Sections[section] = baseline
	}
	known.Datasets[file] = dataset
	data, err := json.MarshalIndent(known, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(specFailuresPath, data, 0o644)
}

func assertKnownSpecFailures(file string, actual map[string][]int) error {
	known, err := readSpecFailures()
	if err != nil {
		return fmt.Errorf("read known spec failures: %w", err)
	}
	dataset, ok := known.Datasets[file]
	if !ok {
		return fmt.Errorf("known spec failures missing dataset %q", file)
	}
	var issues []string
	for section, actualExamples := range actual {
		expected, ok := dataset.Sections[section]
		if !ok {
			issues = append(issues, fmt.Sprintf("%s: no known-failure entry", section))
			continue
		}
		newlyPassing, newlyFailing := compareSpecFailures(expected.Examples, actualExamples)
		if len(newlyPassing) != 0 || len(newlyFailing) != 0 {
			issues = append(issues, fmt.Sprintf("%s: newly passing %v, newly failing %v", section, newlyPassing, newlyFailing))
		}
	}
	for section, expected := range dataset.Sections {
		if _, exists := actual[section]; !exists && len(expected.Examples) != 0 {
			issues = append(issues, fmt.Sprintf("%s: known failures %v no longer fail", section, expected.Examples))
		}
	}
	if len(issues) == 0 {
		return nil
	}
	sort.Strings(issues)
	return fmt.Errorf("%s does not match known spec failures: %v; refresh with -update-spec-failures after reviewing fixes", file, issues)
}
