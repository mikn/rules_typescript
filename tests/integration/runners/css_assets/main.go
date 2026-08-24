package main

import (
	"github.com/mikn/rules_typescript/tests/integration/harness"
)

func main() {
	harness.Run(harness.Config{
		Name:         "css_assets",
		WorkspaceRel: "tests/integration/css_assets",
	}, func(it *harness.IT) {
		it.MustBazel("run", "//:gazelle")
		it.Pass("bazel run //:gazelle")

		build := it.Path("src/components/BUILD.bazel")
		it.RequireFile(build, "Gazelle did not generate src/components/BUILD.bazel")
		it.Pass("src/components/BUILD.bazel generated")
		it.Dump(build)

		it.RequireContains(build, `name = "button_module_css"`,
			"src/components/BUILD.bazel does not contain css_module rule button_module_css")
		it.Pass("src/components/BUILD.bazel has css_module target")

		it.RequireContains(build, `name = "logo_svg"`,
			"src/components/BUILD.bazel does not contain asset_library rule logo_svg")
		it.Pass("src/components/BUILD.bazel has asset_library target")

		it.RequireContains(build, `name = "config_json"`,
			"src/components/BUILD.bazel does not contain json_library rule config_json")
		it.Pass("src/components/BUILD.bazel has json_library target")

		// components.css shares its stem with the directory: its css_library must
		// not take the name of the directory's ts_compile target.
		it.RequireContains(build, `name = "components_css"`,
			"src/components/BUILD.bazel does not contain css_library rule components_css")
		it.RequireContains(build, `name = "components"`,
			"src/components/BUILD.bazel lost the ts_compile target named after the directory")
		it.Pass("css_library and ts_compile target names do not collide")

		it.MustBazel("build", "//...")
		it.Pass("bazel build //...")

		cssDTS := it.Bin("src/components/button.module.css.d.ts")
		it.RequireFile(cssDTS, "CSS module .d.ts not found: src/components/button.module.css.d.ts")
		it.Pass("CSS module .d.ts exists: src/components/button.module.css.d.ts")

		it.RequireContains(cssDTS, "container", "CSS module .d.ts missing class 'container'")
		it.RequireContains(cssDTS, "button", "CSS module .d.ts missing class 'button'")
		it.Pass("CSS module .d.ts contains expected class names")

		it.RequireFile(it.Bin("src/components/Button.js"), "Button.js not found (ts_compile did not run)")
		it.Pass("src/components/Button.js exists")
	})
}
