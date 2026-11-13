package mdpp

import (
	"flag"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestFastDelegationSelection(t *testing.T) {
	run, skip := flag.Lookup("test.run"), flag.Lookup("test.skip")
	oldRun, oldSkip := run.Value.String(), skip.Value.String()
	defer func() { _ = run.Value.Set(oldRun); _ = skip.Value.Set(oldSkip) }()
	for _, tc := range []struct {
		run, skip string
		want      bool
	}{
		{"", "", true}, {"TestFastPathMatchesFullParser", "", false},
		{"", "TestNodeRangesPointIntoSource", false}, {"Other", "Other", false},
	} {
		if err := run.Value.Set(tc.run); err != nil {
			t.Fatal(err)
		}
		if err := skip.Value.Set(tc.skip); err != nil {
			t.Fatal(err)
		}
		if got := rangeProducerSelected(); got != tc.want {
			t.Fatalf("run=%q skip=%q: got %v, want %v", tc.run, tc.skip, got, tc.want)
		}
	}
}

func TestFastDelegationFailureReport(t *testing.T) {
	const key = "MDPP_INTERNAL_TEST_DELEGATION_FAILURE"
	if os.Getenv(key) == "1" {
		reportFastPathComparisons(t, []string{"x"}, []string{"x"}, [][]string{{"different"}})
		return
	}
	c := exec.Command(os.Args[0], "-test.run=^TestFastDelegationFailureReport$")
	c.Env = append(os.Environ(), key+"=1")
	out, err := c.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "fast and full readings differ for") {
		t.Fatalf("failed verdict not reported: error=%v output=%s", err, out)
	}
}

func TestFastDelegationInputCoverage(t *testing.T) {
	inputs := []string{"first", "second", "first"}
	unique, verdicts, err := sharedFastResults(inputs, map[string]bool{"first": true, "second": false})
	if err != nil || len(unique) != 2 || unique[0] != "first" || unique[1] != "second" || len(verdicts[0]) != 0 || len(verdicts[1]) != 1 {
		t.Fatalf("unique=%q verdicts=%q err=%v", unique, verdicts, err)
	}
	if _, _, err := sharedFastResults(inputs, map[string]bool{"first": true}); err == nil {
		t.Fatal("missing input verdict was accepted")
	}
}
