package main

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/mikn/rules_typescript/tests/integration/harness"
)

// A SUBCOMMAND line reads
// "SUBCOMMAND: # ts_compile rule target //src/lib:lib [action 'TsEmit ...".
var subcommandTarget = regexp.MustCompile(`(?m)^SUBCOMMAND:.* target (//[^ ]+) \[action `)

func executedTargets(log *harness.Log) []string {
	seen := map[string]bool{}
	for _, match := range subcommandTarget.FindAllStringSubmatch(log.Text, -1) {
		seen[match[1]] = true
	}
	targets := []string{}
	for target := range seen {
		targets = append(targets, target)
	}
	sort.Strings(targets)
	return targets
}

func executedPackage(targets []string, prefix string) bool {
	for _, target := range targets {
		if strings.HasPrefix(target, prefix) {
			return true
		}
	}
	return false
}

func main() {
	harness.Run(harness.Config{
		Name:         "new_project",
		WorkspaceRel: "tests/integration/new_project",
	}, func(it *harness.IT) {
		it.Write(it.Path("src/app/package.json"), `{"main":"./index.js"}`+"\n")
		it.MustBazel("run", "//:gazelle")
		it.Pass("bazel run //:gazelle")

		for _, dir := range []string{"src/lib", "src/app"} {
			it.RequireFile(it.Path(dir, "BUILD.bazel"), "Gazelle did not generate %s/BUILD.bazel", dir)
			it.Pass("%s/BUILD.bazel generated", dir)
		}

		it.MustBazel("build", "//...", "--output_groups=+declarations")
		it.Pass("bazel build //... --output_groups=+declarations")

		it.Write(it.Path("tools-default/main.js"), "console.log('source launcher works');\n")
		it.Write(it.Path("tools-default/BUILD.bazel"), `load("@rules_typescript//ts:defs.bzl", "ts_binary")

ts_binary(name = "main", entry_point = "main.js")
`)
		launch, err := it.BazelLog("source_launcher.log", "run", "//tools-default:main")
		if err != nil || !launch.Contains("source launcher works") {
			launch.Dump()
			it.Fail("the default consumer launcher did not run the JavaScript program")
		}

		for _, rel := range []string{"src/lib/math.js", "src/lib/math.d.ts", "src/app/index.js", "src/app/index.d.ts"} {
			it.RequireFile(it.Bin(rel), "expected output file not found: %s", rel)
			it.Pass("output file exists: %s", rel)
		}

		// src/app depends on src/lib. An edit to a function BODY leaves src/lib's
		// .d.ts byte-identical, so src/app must not be rebuilt.
		before := it.Read(it.Bin("src/lib/math.d.ts"))
		it.Replace(it.Path("src/lib/math.ts"), "  return a + b;\n", "  const sum: number = a + b;\n  return sum;\n")

		rebuild, err := it.BazelLog("rebuild.log", "build", "//...", "--subcommands",
			"--output_groups=+declarations")
		if err != nil {
			rebuild.Dump()
			it.Fail("the incremental rebuild failed")
		}
		body := executedTargets(rebuild)
		fmt.Printf("INFO: actions executed after the body-only edit:\n%s\n", strings.Join(body, "\n"))

		if !executedPackage(body, "//src/lib") {
			rebuild.Dump()
			it.Fail("editing src/lib/math.ts rebuilt nothing in //src/lib")
		}
		it.Pass("the edited package recompiled")

		if executedPackage(body, "//src/app") {
			rebuild.Dump()
			it.Fail("//src/app recompiled after an implementation-only change to src/lib")
		}
		it.Pass("//src/app did not recompile: the .d.ts boundary held")

		if after := it.Read(it.Bin("src/lib/math.d.ts")); after != before {
			fmt.Printf("--- before ---\n%s--- after ---\n%s", before, after)
			it.Fail("src/lib/math.d.ts changed after an implementation-only edit")
		}
		it.Pass("src/lib/math.d.ts is unchanged")

		// The mirror image: without it, the check above would also pass on a
		// build that has stopped rebuilding anything at all.
		it.Replace(it.Path("src/lib/math.ts"),
			"export function add(a: number, b: number): number {",
			"export function add(a: number, b: number, c: number = 0): number {")

		api, err := it.BazelLog("api_rebuild.log", "build", "//...", "--subcommands",
			"--output_groups=+declarations")
		if err != nil {
			api.Dump()
			it.Fail("the rebuild after the API change failed")
		}
		changed := executedTargets(api)
		fmt.Printf("INFO: actions executed after the signature change:\n%s\n", strings.Join(changed, "\n"))

		if !executedPackage(changed, "//src/app") {
			api.Dump()
			it.Fail("//src/app did not recompile after src/lib's .d.ts changed")
		}
		it.Pass("//src/app recompiled once the .d.ts changed")

		strictDeps(it)
	})
}

// A type-only import through a `paths` alias reaches a file only a dep's dep
// owns; the build names that dep, and declaring it is the fix.
func strictDeps(it *harness.IT) {
	it.Write(it.Path("strict/hidden.ts"), `export interface Hidden {
  id: string;
}
`)
	it.Write(it.Path("strict/leaf.ts"), `import type { Hidden } from "./hidden";

export interface Leaf {
  base: Hidden;
}
`)
	it.Write(it.Path("strict/middle.ts"),
		`import type { Hidden } from "#strict/hidden";
import type { Leaf } from "./leaf";

export interface Middle {
  base: Hidden;
  leaf: Leaf;
}
`)
	it.Write(it.Path("strict/tsconfig.json"), `{
  "compilerOptions": {
    "module": "preserve",
    "moduleResolution": "bundler",
    "target": "es2022",
    "strict": true,
    "paths": { "#strict/*": ["./*"] }
  }
}
`)
	it.Write(it.Path("strict/BUILD.bazel"),
		`load("@rules_typescript//ts:defs.bzl", "ts_compile")

ts_compile(
    name = "hidden",
    emit = True,
    srcs = ["hidden.ts"],
)

ts_compile(
    name = "leaf",
    emit = True,
    srcs = ["leaf.ts"],
    deps = [":hidden"],
)

ts_compile(
    name = "middle",
    emit = True,
    srcs = ["middle.ts"],
    tsconfig = "tsconfig.json",
    deps = [":leaf"],
)
`)

	log, err := it.BazelLog("strict_deps.log", "build", "//strict:middle")
	if err == nil {
		log.Dump()
		it.Fail("//strict:middle built; middle.ts reaches hidden.d.ts " +
			"through :leaf alone")
	}
	it.Pass("//strict:middle failed to build")
	if !log.Contains("add //strict:hidden to deps") {
		log.Dump()
		it.Fail("the failure does not name //strict:hidden as the dep to add")
	}
	var excerpt []string
	for _, line := range log.Lines() {
		if strings.Contains(line, "tsaction:") || strings.HasPrefix(line, "  ") {
			excerpt = append(excerpt, line)
		}
	}
	fmt.Printf("INFO: the failure:\n%s\n", strings.Join(excerpt, "\n"))
	it.Pass("the failure names //strict:hidden")

	it.Replace(it.Path("strict/BUILD.bazel"),
		"    deps = [\":leaf\"],\n",
		"    deps = [\n        \":hidden\",\n        \":leaf\",\n    ],\n")
	it.MustBazel("build", "//strict:middle")
	it.Pass("//strict:middle builds with :hidden declared")
}
