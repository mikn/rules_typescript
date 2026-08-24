package npm_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

// Found by shape rather than by path: both npm layouts are valid -- packages may
// share one external repo or each have their own -- and the runner's file name
// follows the target name plus whichever launcher the rule emits, whose config
// sidecar sits beside it.
func TestBinRunnerRuns(t *testing.T) {
	tree := verify.New(t)

	runner := tree.FoundFile("*/nanoid_bin*", "*.json")
	if !runner.Exists() {
		t.FailNow()
	}

	out, err := exec.Command(runner.Abs()).CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v\n%s", runner.Name(), err, out)
	}

	id := strings.TrimSpace(string(out))
	if len(id) < 10 {
		t.Errorf("%s printed %q (%d chars), want a nanoid of at least 10", runner.Name(), id, len(id))
	}
}
