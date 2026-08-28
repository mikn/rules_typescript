package main

import (
	"github.com/mikn/rules_typescript/tests/integration/harness"
)

func main() {
	harness.Run(harness.Config{
		Name:         "dev_server",
		WorkspaceRel: "tests/integration/dev_server",
	}, func(it *harness.IT) {
		// `bazel run //:dev` is deliberately not run: it would start a real Vite
		// dev server and block.
		it.MustBazel("build", "//:dev")
		it.Pass("bazel build //:dev")

		runner := it.Bin("dev_launcher")
		it.RequireFile(runner, "dev server runner script not found: dev_launcher")
		it.Pass("dev_launcher exists")

		it.RequireExecutable(runner, "dev_launcher is not executable")
		it.Pass("dev_launcher is executable")

		config := it.Bin("dev_dev/vite.config.mjs")
		it.RequireFile(config, "vite.config.mjs not found: dev_dev/vite.config.mjs")
		it.Pass("dev_dev/vite.config.mjs exists")

		it.RequireContains(config, "port: 5173", "vite.config.mjs does not contain 'port: 5173'")
		it.Pass("vite.config.mjs contains port: 5173")

		it.RequireContains(config, "BUILD_WORKSPACE_DIRECTORY", "vite.config.mjs does not reference BUILD_WORKSPACE_DIRECTORY")
		it.Pass("vite.config.mjs references BUILD_WORKSPACE_DIRECTORY")

		it.MustBazel("build", "//...")
		it.Pass("bazel build //...")
	})
}
