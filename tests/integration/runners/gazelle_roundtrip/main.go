package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/mikn/rules_typescript/tests/integration/harness"
)

// A deleted test target satisfies both "the output builds" and "two runs are
// byte-identical", which is how seven hand-written go_tests once disappeared.
func testTargets(it *harness.IT) []string {
	out := strings.Fields(it.BazelStdout("query", "tests(//...)"))
	sort.Strings(out)
	return out
}

func main() {
	harness.Run(harness.Config{
		Name:         "gazelle_roundtrip",
		WorkspaceRel: "tests/integration/gazelle_roundtrip",
	}, func(it *harness.IT) {
		dirs := []string{"src/lib", "src/app"}

		it.MustBazel("run", "//:gazelle")
		it.Pass("gazelle pass 1 complete")

		for _, dir := range dirs {
			it.RequireFile(it.Path(dir, "BUILD.bazel"), "Gazelle did not generate %s/BUILD.bazel", dir)
			it.Pass("%s/BUILD.bazel generated (pass 1)", dir)
		}

		before := testTargets(it)
		if len(before) == 0 {
			it.Fail("no test targets in this workspace: the set comparison below would be vacuous")
		}
		it.Pass("test targets after pass 1: %d", len(before))

		firstPass := map[string]string{}
		for _, dir := range dirs {
			firstPass[dir] = it.Read(it.Path(dir, "BUILD.bazel"))
			if err := os.Remove(it.Path(dir, "BUILD.bazel")); err != nil {
				it.Fail("cannot delete %s/BUILD.bazel: %v", dir, err)
			}
		}
		it.Pass("BUILD files snapshotted and deleted")

		it.MustBazel("run", "//:gazelle")
		it.Pass("gazelle pass 2 complete")

		differs := false
		for _, dir := range dirs {
			second := it.Read(it.Path(dir, "BUILD.bazel"))
			if second != firstPass[dir] {
				fmt.Fprintf(os.Stderr, "--- %s/BUILD.bazel pass 1 ---\n%s--- pass 2 ---\n%s", dir, firstPass[dir], second)
				differs = true
				continue
			}
			it.Pass("%s/BUILD.bazel is identical across both Gazelle runs", dir)
		}
		if differs {
			it.Fail("Gazelle output is not idempotent — see the dumps above")
		}

		it.MustBazel("build", "//...")
		it.Pass("bazel build //...")

		after := testTargets(it)
		if strings.Join(before, "\n") != strings.Join(after, "\n") {
			fmt.Fprintf(os.Stderr, "--- before ---\n%s\n--- after ---\n%s\n",
				strings.Join(before, "\n"), strings.Join(after, "\n"))
			it.Fail("Gazelle changed the test target set: %d before, %d after", len(before), len(after))
		}
		it.Pass("test target set unchanged across a delete-and-regenerate: %d", len(after))

		it.MustBazel("test", "//...")
		it.Pass("bazel test //... on Gazelle's own output")

		for _, rel := range []string{"src/lib/math.js", "src/lib/math.d.ts", "src/app/index.js", "src/app/index.d.ts"} {
			it.RequireFile(it.Bin(rel), "expected output file not found: %s", rel)
			it.Pass("output file exists: %s", rel)
		}
	})
}
