package mdpp

import (
	"flag"
	"sync"
	"testing"
)

var fastReportingMu sync.Mutex

func TestFastPathMatchesFullParser(t *testing.T) {
	t.Parallel()
	// When the range producer is selected, it owns the differential assertions.
	// Returning immediately leaves a slot for the independent GFM corpus.
	if parallelCorpusSuiteEnabled() && rangeProducerSelected() {
		t.Log("differential assertions delegated to selected range producer")
		return
	}
	fastReportingMu.Lock()
	defer fastReportingMu.Unlock()
	differentialProfile = ProfileMarkdownPlus
	checkFastPathMatchesFullParser(t)
}

// Only delegate in the full suite, where Go schedules the producer. Selective
// runs retain the independent differential check, including -skip runs.
func rangeProducerSelected() bool {
	run, skip := flag.Lookup("test.run"), flag.Lookup("test.skip")
	return run != nil && run.Value.String() == "" && (skip == nil || skip.Value.String() == "")
}
