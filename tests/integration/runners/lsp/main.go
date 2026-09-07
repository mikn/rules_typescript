package main

import (
	"encoding/json"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/mikn/rules_typescript/tests/integration/harness"
)

type tsconfig struct {
	Extends         []string `json:"extends"`
	CompilerOptions struct {
		Paths map[string][]string `json:"paths"`
	} `json:"compilerOptions"`
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
		Lockfile:     "tests/npm/pnpm-lock.yaml",
	}, func(it *harness.IT) {
		it.MustBazel("run", "//:gazelle")
		it.Pass("bazel run //:gazelle")

		for _, dir := range []string{"src/models", "src/components"} {
			it.RequireFile(it.Path(dir, "BUILD.bazel"), "Gazelle did not generate %s/BUILD.bazel", dir)
			it.Pass("%s/BUILD.bazel generated", dir)
		}

		// src/tsconfig.json maps @/* to ./* and button.ts imports @/models/user
		// through it; the BUILD file carries the dep the alias resolves to.
		components := it.Path("src/components/BUILD.bazel")
		it.RequireContains(components, `tsconfig = "//src:tsconfig"`,
			"//src/components does not name the tsconfig that sets the alias")
		it.RequireContains(components, `deps = ["//src/models"]`,
			"//src/components does not depend on the target the alias resolves to")
		it.RequireNotContains(components, "path_alias",
			"//src/components restates the alias through an attribute the rule does not have")
		it.Pass("//src/components names src/tsconfig.json and depends on //src/models through @/models/user")

		// refresh_tsconfig reads the @npm BUILD.bazel out of the output base, and
		// //... is what forces the repo rule to write it. @npm//... would drag in
		// the workspace alias for a packages/shared this workspace does not have.
		it.MustBazel("build", "//...")
		it.Pass("bazel build //...: the aliased import resolves through the tsconfig's paths and the dep")

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

		// A package whose targets name a tsconfig gets an editor program of its
		// own extending it, so the alias reaches the editor from the file that sets it.
		for _, pkg := range []string{"src/components", "src/models"} {
			nested := readTsconfig(it, it.Path(pkg, "tsconfig.json"))
			extended := []string{}
			for _, entry := range nested.Extends {
				extended = append(extended, path.Clean(path.Join(pkg, entry)))
			}
			if !slices.Contains(extended, "src/tsconfig.json") {
				it.Fail("%s/tsconfig.json extends %q, want src/tsconfig.json among them: the alias is set there", pkg, nested.Extends)
			}
			if len(nested.CompilerOptions.Paths) != 0 {
				it.Fail("%s/tsconfig.json restates paths %v; the editor inherits them through extends", pkg, nested.CompilerOptions.Paths)
			}
			it.Pass("%s/tsconfig.json extends src/tsconfig.json and restates no paths", pkg)
		}

		// ts_pnpm's contract is a pnpm that runs from the workspace root with no
		// pnpm on the host. `--version` needs neither network nor package.json.
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
