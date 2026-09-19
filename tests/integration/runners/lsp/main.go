package main

import (
	"encoding/json"
	"regexp"
	"slices"
	"strings"

	"github.com/mikn/rules_typescript/tests/integration/harness"
)

type tsconfig struct {
	CompilerOptions struct {
		Paths map[string][]string `json:"paths"`
	} `json:"compilerOptions"`
	Exclude []string `json:"exclude"`
}

func readTsconfig(it *harness.IT, path string) tsconfig {
	it.RequireFile(path, "%s was not generated", path)
	parsed := tsconfig{}
	if err := json.Unmarshal([]byte(it.Read(path)), &parsed); err != nil {
		it.Fail("%s is not valid JSON: %v", path, err)
	}
	return parsed
}

func main() {
	harness.Run(harness.Config{
		Name:         "lsp",
		WorkspaceRel: "tests/integration/lsp",
	}, func(it *harness.IT) {
		program := it.Read(it.Path("src/tsconfig.json"))

		it.Install()
		it.Pass("pnpm install")

		it.MustBazel("run", "//:gazelle")
		it.Pass("bazel run //:gazelle")

		// src/tsconfig.json maps @/* to ./* and button.ts imports @/models/user
		// through it; one program, so the alias resolves inside //src.
		build := it.Path("src/BUILD.bazel")
		it.RequireFile(build, "Gazelle did not generate src/BUILD.bazel")
		it.RequireContains(build, `tsconfig = ":tsconfig"`,
			"//src does not name the tsconfig that sets the alias")
		it.RequireContains(build, `"@npm//:zod"`,
			"//src does not depend on zod, which user.ts imports")
		it.RequireNotContains(build, "path_alias",
			"//src restates the alias through an attribute the rule does not have")
		for _, dir := range []string{"src/components", "src/models"} {
			it.RequireNoFile(it.Path(dir, "BUILD.bazel"),
				"Gazelle made %s a package; src/tsconfig.json lists its files", dir)
		}
		it.Pass("//src names src/tsconfig.json, owns both directories and " +
			"depends on zod")

		// refresh_tsconfig reads the @npm BUILD.bazel out of the output base, and
		// //... is what forces the repo rule to write it.
		it.MustBazel("build", "//...")
		it.Pass("bazel build //...: the aliased import resolves inside the program")

		it.MustBazel("run", "//:refresh_tsconfig")
		it.Pass("bazel run //:refresh_tsconfig")

		root := readTsconfig(it, it.Path("tsconfig.json"))
		it.Pass("tsconfig.json generated")

		// An npm package resolves through the checkout's node_modules, as under
		// tsc; a key here would send the editor somewhere the build does not look.
		for key := range root.CompilerOptions.Paths {
			if key == "zod" || strings.HasPrefix(key, "zod/") {
				it.Fail("tsconfig.json names the npm package zod in paths: %q", key)
			}
		}
		it.Pass("tsconfig.json has no paths key for the npm package zod")

		// The package's own tsconfig.json is the editor program for its files:
		// nothing is written over it, and the root program leaves them out.
		if it.Read(it.Path("src/tsconfig.json")) != program {
			it.Fail("refresh_tsconfig rewrote src/tsconfig.json, the program " +
				"//src checks under")
		}
		for _, file := range []string{
			"src/components/button.ts", "src/models/user.ts",
		} {
			if !slices.Contains(root.Exclude, file) {
				it.Fail("tsconfig.json does not exclude %s, which "+
					"src/tsconfig.json checks; exclude = %v", file, root.Exclude)
			}
		}
		for _, dir := range []string{"src/components", "src/models"} {
			it.RequireNoFile(it.Path(dir, "tsconfig.json"),
				"refresh_tsconfig wrote %s/tsconfig.json; src/tsconfig.json "+
					"is the program there", dir)
		}
		it.Pass("src/tsconfig.json is left as written and the root excludes " +
			"the files it checks")

		// ts_pnpm's contract is a pnpm that runs from the workspace root with no
		// pnpm on the host; Install above ran it, and `--version` needs no network.
		out := it.BazelStdout("run", "//:pnpm", "--", "--version")
		version := ""
		for _, line := range strings.Split(out, "\n") {
			if trimmed := strings.TrimSpace(line); trimmed != "" {
				version = trimmed
			}
		}
		if !regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+`).MatchString(version) {
			it.Fail("bazel run //:pnpm -- --version printed %q, want a version number", version)
		}
		it.Pass("bazel run //:pnpm -- --version reported %s", version)

		// ts_add_package wraps the same binary with `add ... --lockfile-only`;
		// running it for real would need the registry.
		if err := it.Bazel("build", "//:add_package"); err != nil {
			it.Fail("//:add_package does not build")
		}
		it.Pass("//:add_package builds")
	})
}
