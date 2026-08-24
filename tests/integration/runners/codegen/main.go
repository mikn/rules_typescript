package main

import (
	"github.com/mikn/rules_typescript/tests/integration/harness"
)

func main() {
	harness.Run(harness.Config{
		Name:         "codegen",
		WorkspaceRel: "tests/integration/codegen",
	}, func(it *harness.IT) {
		it.MustBazel("build", "//...")
		it.Pass("bazel build //...")

		generated := it.Bin("generated/record.ts")
		it.RequireFile(generated, "generated TypeScript not found: generated/record.ts")
		it.Pass("generated/record.ts exists")

		it.RequireContains(generated, "GeneratedRecord",
			"generated/record.ts does not contain expected interface 'GeneratedRecord'")
		it.Pass("generated/record.ts contains GeneratedRecord interface")

		for _, rel := range []string{"generated/record.js", "generated/record.d.ts"} {
			it.RequireFile(it.Bin(rel), "expected compiled output not found: %s", rel)
			it.Pass("output file exists: %s", rel)
		}
	})
}
