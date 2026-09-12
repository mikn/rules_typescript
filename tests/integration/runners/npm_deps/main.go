package main

import (
	"strings"

	"github.com/mikn/rules_typescript/tests/integration/harness"
)

func main() {
	harness.Run(harness.Config{
		Name:         "npm_deps",
		WorkspaceRel: "tests/integration/npm_deps",
	}, func(it *harness.IT) {
		it.Install()
		it.Pass("pnpm install")

		it.MustBazel("run", "//:gazelle")
		it.Pass("bazel run //:gazelle")

		build := it.Path("src/models/BUILD.bazel")
		it.RequireFile(build, "Gazelle did not generate src/models/BUILD.bazel")
		it.Pass("src/models/BUILD.bazel generated")

		it.RequireContains(build, "@npm//:zod", "src/models/BUILD.bazel does not reference @npm//:zod")
		it.Pass("src/models/BUILD.bazel references @npm//:zod")

		it.MustBazel("build", "//...", "--output_groups=+declarations")
		it.Pass("bazel build //... --output_groups=+declarations")

		for _, rel := range []string{"src/models/user.js", "src/models/user.d.ts"} {
			it.RequireFile(it.Bin(rel), "expected output file not found: %s", rel)
			it.Pass("output file exists: %s", rel)
		}

		it.MustBazel("test", "//...")
		it.Pass("bazel test //...")

		// The positive case (oxlint passing on clean sources) is //tests/lint_real.
		// This is the other half, which needs a Bazel that is allowed to fail.
		lint, err := it.BazelLog("violations.log", "build", "//:violations")
		if err == nil {
			lint.Dump()
			it.Fail("//:violations built; the linter ts.lint() names must fail " +
				"it on `var`")
		}
		it.Pass("//:violations failed to build, as a lint violation must")

		if !strings.Contains(strings.ToLower(lint.Text), "no-var") {
			lint.Dump()
			it.Fail("the build failed, but not with the no-var diagnostic that oxlint.json enables")
		}
		it.Pass("the failure names the no-var rule")
	})
}
