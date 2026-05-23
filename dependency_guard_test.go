package mdpp

import (
	"os/exec"
	"strings"
	"testing"
)

func TestCoreModuleDoesNotDependOnBrowserPDFDeps(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "./...").CombinedOutput()
	if err != nil {
		t.Fatalf("go list deps: %v\n%s", err, out)
	}
	for _, dep := range strings.Fields(string(out)) {
		if strings.Contains(dep, "chromedp") || strings.Contains(dep, "cdproto") {
			t.Fatalf("core dependency graph includes browser/PDF dependency %q", dep)
		}
	}
}
