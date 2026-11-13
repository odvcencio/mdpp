package mdpp

import (
	"flag"
	"testing"
	"time"
)

func TestSharedRangeProducerCoordination(t *testing.T) {
	oldStart, oldDone := sharedRangeStarted, sharedRangeDone
	defer func() { sharedRangeStarted, sharedRangeDone = oldStart, oldDone }()
	sharedRangeStarted = make(chan struct{})
	sharedRangeDone = make(chan struct{})
	if awaitStartedRangeProducer() {
		t.Fatal("an unscheduled producer must not block or report completion")
	}
	close(sharedRangeStarted)
	ready := make(chan struct{})
	result := make(chan bool, 1)
	go func() { close(ready); result <- awaitStartedRangeProducer() }()
	<-ready
	select {
	case <-result:
		t.Fatal("returned before producer completed")
	case <-time.After(10 * time.Millisecond):
	}
	close(sharedRangeDone)
	select {
	case ok := <-result:
		if !ok {
			t.Fatal("producer completion not observed")
		}
	case <-time.After(time.Second):
		t.Fatal("did not wake after producer completion")
	}
}

func TestSharedCorpusSelectiveScheduling(t *testing.T) {
	names := []string{"test.run", "test.count", "test.skip"}
	previous := make(map[string]string, len(names))
	for _, name := range names {
		f := flag.Lookup(name)
		if f == nil {
			t.Fatalf("missing flag %s", name)
		}
		previous[name] = f.Value.String()
	}
	defer func() {
		for _, name := range names {
			if err := flag.Lookup(name).Value.Set(previous[name]); err != nil {
				t.Error(err)
			}
		}
	}()
	set := func(name, value string) {
		t.Helper()
		if err := flag.Lookup(name).Value.Set(value); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("MDPP_INTERNAL_PARALLEL_SHARE", "")
	set("test.count", "1")
	set("test.skip", "")
	set("test.run", "")
	if !parallelCorpusSuiteEnabled() {
		t.Fatal("full-suite schedule was not enabled")
	}
	set("test.run", "^TestSharedCorpusSelectiveScheduling$")
	if parallelCorpusSuiteEnabled() {
		t.Fatal("a selective run unexpectedly entered the full-suite schedule")
	}
	t.Setenv("MDPP_INTERNAL_PARALLEL_SHARE", "1")
	if !parallelCorpusSuiteEnabled() {
		t.Fatal("explicit diagnostic schedule was not enabled")
	}
	set("test.count", "2")
	if parallelCorpusSuiteEnabled() {
		t.Fatal("repeated runs must not share process-global caches")
	}
}
