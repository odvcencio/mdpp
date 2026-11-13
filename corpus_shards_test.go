package mdpp

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The tests using this helper access process-global parser controls. Each
// worker therefore runs the same race-instrumented test binary in isolation.
const corpusShards = 2
const corpusShardEnv = "MDPP_INTERNAL_CORPUS_SHARD"
const corpusDirEnv = "MDPP_INTERNAL_CORPUS_DIR"

type corpusItem struct {
	Index    int
	Problems []string
}
type corpusResult struct {
	Items  []corpusItem
	Digest string
}

func corpusShardMap(t *testing.T, inputs []string, check func(int, string) ([]string, error)) ([][]string, bool) {
	t.Helper()
	if shard := os.Getenv(corpusShardEnv); shard != "" {
		id, err := strconv.Atoi(shard)
		if err != nil || id < 0 || id >= corpusShards {
			t.Fatalf("invalid corpus shard %q", shard)
		}
		var result corpusResult
		h := sha256.New()
		for i, in := range inputs {
			if i%corpusShards != id {
				continue
			}
			fmt.Fprintf(h, "%d:%d:", i, len(in))
			h.Write([]byte(in))
			problems, err := check(i, in)
			if err != nil {
				t.Fatalf("input %d: %v", i, err)
			}
			result.Items = append(result.Items, corpusItem{Index: i, Problems: problems})
		}
		result.Digest = fmt.Sprintf("%x", h.Sum(nil))
		b, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(os.Getenv(corpusDirEnv), shard), b, 0600); err != nil {
			t.Fatal(err)
		}
		return nil, true
	}
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	if deadline, ok := t.Deadline(); ok {
		cancel()
		ctx, cancel = context.WithDeadline(context.Background(), deadline.Add(-time.Second))
	}
	defer cancel()
	path := strings.Split(t.Name(), "/")
	for i := range path {
		path[i] = "^" + regexp.QuoteMeta(path[i]) + "$"
	}
	run := strings.Join(path, "/")
	failures := make([]error, corpusShards)
	var wg sync.WaitGroup
	for id := 0; id < corpusShards; id++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			c := exec.CommandContext(ctx, os.Args[0], "-test.run="+run, "-test.timeout=0")
			c.Env = append(os.Environ(), fmt.Sprintf("%s=%d", corpusShardEnv, id), corpusDirEnv+"="+dir)
			if out, err := c.CombinedOutput(); err != nil {
				failures[id] = fmt.Errorf("shard %d: %w\n%s", id, err, out)
			}
		}(id)
	}
	wg.Wait()
	for _, err := range failures {
		if err != nil {
			t.Error(err)
		}
	}
	if t.Failed() {
		t.FailNow()
	}
	seen := make([]bool, len(inputs))
	results := make([][]string, len(inputs))
	for id := 0; id < corpusShards; id++ {
		b, err := os.ReadFile(filepath.Join(dir, strconv.Itoa(id)))
		if err != nil {
			t.Fatal(err)
		}
		var result corpusResult
		if err := json.Unmarshal(b, &result); err != nil {
			t.Fatal(err)
		}
		h := sha256.New()
		previous := -1
		for _, item := range result.Items {
			i := item.Index
			if i < 0 || i >= len(inputs) || i%corpusShards != id || seen[i] || i <= previous {
				t.Fatalf("shard %d has invalid index %d", id, i)
			}
			previous = i
			seen[i] = true
			in := inputs[i]
			fmt.Fprintf(h, "%d:%d:", i, len(in))
			h.Write([]byte(in))
			results[i] = item.Problems
		}
		if got := fmt.Sprintf("%x", h.Sum(nil)); got != result.Digest {
			t.Fatalf("shard %d input digest mismatch", id)
		}
		t.Logf("shard %d: %d inputs, digest %s", id, len(result.Items), result.Digest)
	}
	for i, ok := range seen {
		if !ok {
			t.Fatalf("input index %d missing from shards", i)
		}
	}
	return results, false
}
