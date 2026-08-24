package main

import (
	"github.com/mikn/rules_typescript/tests/integration/harness"
)

func main() {
	harness.Run(harness.Config{
		Name:         "vite_bundle",
		WorkspaceRel: "tests/integration/vite_bundle",
		Lockfile:     "tests/npm/pnpm-lock.yaml",
	}, func(it *harness.IT) {
		it.MustBazel("run", "//:gazelle")
		it.Pass("bazel run //:gazelle")

		for _, dir := range []string{"src/lib", "src/app"} {
			it.RequireFile(it.Path(dir, "BUILD.bazel"), "Gazelle did not generate %s/BUILD.bazel", dir)
			it.Pass("%s/BUILD.bazel generated", dir)
		}

		it.MustBazel("build", "//:bundle")
		it.Pass("bazel build //:bundle")

		it.RequireDir(it.Bin("bundle_bundle"), "Vite bundle output directory not found: bundle_bundle/")
		it.Pass("bundle_bundle/ directory exists")

		// Vite lib mode produces a .es.js file named after the bundle target.
		it.RequireFile(it.Bin("bundle_bundle", "bundle.es.js"), "Vite bundle JS not found: bundle_bundle/bundle.es.js")
		it.Pass("bundle_bundle/bundle.es.js exists")
	})
}
