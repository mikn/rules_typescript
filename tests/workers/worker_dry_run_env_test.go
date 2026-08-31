package workers_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

// The bindings wrangler reports are the environment it actually resolved: a
// --env that arrived but selected nothing still prints the top-level ones.
func TestDryRunResolvesTheNamedEnvironment(t *testing.T) {
	tree := verify.New(t)

	runner := tree.FoundFile("*/deploy_dry_run_staging*", "*.json")
	if !runner.Exists() {
		t.FailNow()
	}

	out, err := exec.Command(runner.Abs()).CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v\n%s", runner.Name(), err, out)
	}

	if !strings.Contains(string(out), "staging-only") {
		t.Errorf("the staging environment's own binding never reached the dry run:\n%s", out)
	}
	if strings.Contains(string(out), "top-level") {
		t.Errorf("the top-level bindings were deployed instead of staging's:\n%s", out)
	}
}
