package thresholds_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

const (
	metLauncher    = "tests/vitest/thresholds/met/threshold_met_test_test_launcher"
	missedLauncher = "tests/vitest/thresholds/missed/threshold_missed_test_test_launcher"
	unmet          = "Coverage for lines (50%) does not meet global threshold (90%)"
)

func TestAThresholdTheCoverageMissesFailsTheTest(t *testing.T) {
	out, err := runTarget(t, missedLauncher)
	if err == nil {
		t.Fatalf("a target whose coverage misses its threshold passed:\n%s", out)
	}
	if !strings.Contains(out, unmet) {
		t.Errorf("failure does not name the threshold: want %q in\n%s", unmet, out)
	}

	// The assertions ran and passed, so the exit status is the threshold's.
	if !strings.Contains(out, "Tests  1 passed") {
		t.Errorf("the test itself was expected to pass; something else failed:\n%s", out)
	}
}

func TestAThresholdTheCoverageClearsPassesTheTest(t *testing.T) {
	if out, err := runTarget(t, metLauncher); err != nil {
		t.Fatalf("a target whose coverage clears its threshold failed: %v\n%s", err, out)
	}
}

// Both targets run the same compiled test over one library, so the threshold is
// the only difference the exit statuses above can be about.
func TestTheTwoTargetsRunTheSameTest(t *testing.T) {
	tree := verify.New(t)
	met := tree.File("tests/vitest/thresholds/met/partial.test.js").Text()
	missed := tree.File("tests/vitest/thresholds/missed/partial.test.js").Text()
	if met != missed {
		t.Errorf("the two compiled tests differ:\n%s\n---\n%s", met, missed)
	}
}

func runTarget(t *testing.T, launcher string) (string, error) {
	t.Helper()
	target := verify.New(t).File(launcher)
	if !target.Exists() {
		t.Fatalf("%s is not in the runfiles", launcher)
	}
	cmd := exec.Command(target.Abs())
	// NO_COLOR: the assertions above read vitest's own summary lines, which it
	// otherwise breaks up with escape sequences.
	cmd.Env = append(os.Environ(), "TEST_TMPDIR="+t.TempDir(), "NO_COLOR=1")
	out, err := cmd.CombinedOutput()
	return string(out), err
}
