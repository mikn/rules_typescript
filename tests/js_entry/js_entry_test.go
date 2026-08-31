package js_entry_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

// A ts_binary whose entry_point is a .mjs source runs that file, and the
// sibling module it imports is in runfiles beside it.
func TestSourceEntryPointRuns(t *testing.T) {
	tree := verify.New(t)

	runner := tree.File("tests/js_entry/main_launcher")
	if !runner.Exists() {
		t.FailNow()
	}

	out, err := exec.Command(runner.Abs()).CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v\n%s", runner.Name(), err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "hello, runfiles" {
		t.Errorf("printed %q, want %q", got, "hello, runfiles")
	}

	// The data attr is what puts the sibling there. Node also finds it through
	// the entry symlink's realpath in a local tree, so asserting membership is
	// the part that holds for a manifest-only or remote layout.
	tree.File("tests/js_entry/greet.mjs").Exists()
}

// The same ts_binary as a ts_codegen generator: the exec-configuration path,
// where the rule also has to hand it NODE_BINARY and TS_CODEGEN_NODE_MODULES.
func TestSourceEntryPointGenerates(t *testing.T) {
	tree := verify.New(t)

	generated := tree.File("tests/js_entry/generated.ts")
	generated.Exists()
	generated.Contains(
		`export const greeting: string = "hello, codegen";`,
		"export const nodeBinary: boolean = true;",
		"export const nodeModulesDir: boolean = true;",
	)
}
