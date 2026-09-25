package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/bazelbuild/rules_go/go/runfiles"

	"github.com/mikn/rules_typescript/tests/integration/harness"
)

var nodeRlocationpath, tsgoRlocationpath string

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
		Renames:      map[string]string{"generated/BUILD.bazel.tpl": "generated/BUILD.bazel"},
	}, func(it *harness.IT) {
		workspace := os.Getenv("TEST_WORKSPACE")
		if workspace == "" {
			workspace = "_main"
		}
		node := it.Runfile(nodeRlocationpath)
		tsgo := it.Runfile(tsgoRlocationpath)
		verifier := it.Runfile(workspace + "/tools/native/verify-editor-cli.mjs")
		it.RequireExecutable(node, "selected Node executable is unavailable: %s", node)
		it.RequireExecutable(tsgo, "selected TypeScript executable is unavailable: %s", tsgo)
		it.RequireFile(verifier, "editor verifier is unavailable: %s", verifier)
		runfilesEnv, err := runfiles.Env()
		if err != nil {
			it.Fail("cannot resolve tool runfiles environment: %v", err)
		}
		for i, entry := range runfilesEnv {
			key, value, _ := strings.Cut(entry, "=")
			absolute, err := filepath.Abs(value)
			if err != nil {
				it.Fail("cannot resolve %s: %v", key, err)
			}
			runfilesEnv[i] = key + "=" + absolute
		}
		editorEnv := append(runfilesEnv, "BUILD_WORKSPACE_DIRECTORY="+it.WorkspaceDir)
		queryTests := it.Runfile(workspace + "/tools/native/query-editor.test.mjs")
		it.RequireFile(queryTests, "editor lifecycle tests unavailable: %s", queryTests)
		queryLog, err := it.Exec("editor-query", editorEnv, node, "--test", queryTests)
		queryLog.Dump()
		if err != nil {
			it.Fail("native query lifecycle failed: %v", err)
		}
		program := it.Read(it.Path("src/tsconfig.json"))

		it.Install()
		it.Pass("pnpm install")

		refreshGenerated := func() {
			it.MustBazel("run", "--run_validations=false", "--output_groups=-_validation", "//generated:refresh_generated")
		}
		buildConfig := it.Read(it.Path("generated/tsconfig.build.json"))
		solutionConfig := it.Read(it.Path("generated/tsconfig.json"))
		it.Write(it.Path("generated/generated/value.ts"), "export const generatedValue: number = 1;\n")
		stale := it.Read(it.Path("generated/generated/value.ts"))
		it.Write(it.Path("generated/generated/types/removed.d.ts"), "export declare const removed: string;\n")
		refreshGenerated()
		for _, path := range []string{"generated/generated/value.ts", "generated/generated/types/first.d.ts", "generated/generated/types/removed.d.ts"} {
			it.RequireFile(filepath.Join(it.BazelBin(), path), "refresh did not materialize %s", path)
		}
		if it.Read(it.Path("generated/generated/value.ts")) != stale {
			it.Fail("refresh changed existing source generator output")
		}
		if it.Read(it.Path("generated/tsconfig.build.json")) != buildConfig || it.Read(it.Path("generated/tsconfig.json")) != solutionConfig {
			it.Fail("refresh changed authored configuration")
		}
		probes := []map[string]string{{"file": "generated/consumer.ts", "symbol": "#generated/value", "definition": "bazel-bin/generated/generated/value.ts", "project": "generated/.bazel/tsconfig/generated_test.json"}, {"file": "generated/consumer.ts", "symbol": "#types/first", "definition": "bazel-bin/generated/generated/types/first.d.ts", "project": "generated/.bazel/tsconfig/generated_test.json"}, {"file": "generated/standalone.test.ts", "symbol": "#generated/value.js", "definition": "bazel-bin/generated/generated/value.ts", "project": "generated/.bazel/tsconfig/generated_test.json"}}
		probes = append(probes,
			map[string]string{"file": "generated/consumer.ts", "symbol": "#ordered/value", "definition": "generated/authored/value.ts", "project": "generated/.bazel/tsconfig/generated_test.json"},
			map[string]string{"file": "generated/consumer.ts", "symbol": "#specific/value", "definition": "generated/override.ts", "project": "generated/.bazel/tsconfig/generated_test.json"},
		)
		raw, err := json.Marshal(probes)
		if err != nil {
			it.Fail("encode editor probes: %v", err)
		}
		probesPath := it.Scratch("editor-probes.json")
		it.Write(probesPath, string(raw))
		verify := func(name string) {
			log, err := it.Exec(name, editorEnv, node, verifier, tsgo, probesPath)
			log.Dump()
			if err != nil {
				it.Fail("editor discovery/generated resolution failed: %v", err)
			}
		}
		verify("editor-first.json")
		queryLauncher := it.Scratch("query editor é")
		it.MustBazel("run", "--script_path="+queryLauncher, "@rules_typescript//tools:query_editor")
		it.RequireExecutable(queryLauncher, "Bazel did not materialize query launcher: %s", queryLauncher)
		queryArgs, err := json.Marshal(map[string]any{"file": "generated/consumer.ts", "operation": "diagnostics"})
		if err != nil {
			it.Fail("encode editor query: %v", err)
		}
		queryCommand := it.Command(editorEnv, queryLauncher, tsgo, it.WorkspaceDir, string(queryArgs))
		queryCommand.Stderr = os.Stderr
		queryOutput, err := queryCommand.Output()
		it.Write(it.Scratch("editor-query-launcher.json"), string(queryOutput))
		fmt.Print(string(queryOutput))
		if err != nil {
			it.Fail("Bazel-owned query launcher failed: %v", err)
		}
		var queryResult struct {
			Project struct {
				ConfigFilePath string `json:"configFilePath"`
			} `json:"project"`
			Result []json.RawMessage `json:"result"`
		}
		if err := json.Unmarshal(queryOutput, &queryResult); err != nil {
			it.Fail("decode editor query: %v", err)
		}
		if len(queryResult.Result) != 0 || queryResult.Project.ConfigFilePath != it.Path("generated/.bazel/tsconfig/generated_test.json") {
			it.Fail("query launcher selected incorrect project or diagnostics: %s", queryOutput)
		}
		nativeQueryFixture := it.Runfile(workspace + "/tools/native/query-editor-native.mjs")
		it.RequireFile(nativeQueryFixture, "native query fixture unavailable: %s", nativeQueryFixture)
		nativeQueryLog, err := it.Exec("editor-native-queries.json", editorEnv, node, nativeQueryFixture, tsgo, it.WorkspaceDir, "generated/consumer.ts", "generated/.bazel/tsconfig/generated_test.json", "bazel-bin/generated/generated/value.ts")
		nativeQueryLog.Dump()
		if err != nil {
			it.Fail("native query operations or cancellation failed: %v", err)
		}
		it.Pass("Bazel-owned query entry forwards arguments and native queries preserve project and process ownership")

		liveFixture := it.Runfile(workspace + "/tools/native/live-editor-native.mjs")
		it.RequireFile(liveFixture, "live editor fixture unavailable: %s", liveFixture)
		liveLog, err := it.Exec("editor-live-generated.json", editorEnv, node, liveFixture, tsgo, it.WorkspaceDir, it.BazelExecutable())
		liveLog.Dump()
		if err != nil {
			it.Fail("live generated editor operations failed: %v", err)
		}

		initialGenerated := it.Read(filepath.Join(it.BazelBin(), "generated/generated/value.ts"))
		it.Write(it.Path("generated/names.json"), "[\"first\"]\n")
		refreshGenerated()
		it.RequireNoFile(filepath.Join(it.BazelBin(), "generated/generated/types/removed.d.ts"), "deleted generated tree child remains")
		verify("editor-refreshed.json")
		if it.Read(filepath.Join(it.BazelBin(), "generated/generated/value.ts")) == initialGenerated {
			it.Fail("generator input edit did not refresh output")
		}
		it.MustBazel("build", "//generated:generated", "//generated:generated_test")
		verify("editor-after-build.json")
		originalConsumer := it.Read(it.Path("generated/consumer.ts"))
		it.Write(it.Path("generated/consumer.ts"), originalConsumer+"\nimport type { removed } from '#types/removed';\nexport const missing: typeof removed = 'gone';\n")
		refreshGenerated()
		deletedProbes := []map[string]any{{"file": "generated/consumer.ts", "symbol": "#types/removed", "project": "generated/.bazel/tsconfig/generated_test.json", "definitionAbsent": true, "diagnosticCodes": []int{2307}}}
		deletedBytes, err := json.Marshal(deletedProbes)
		if err != nil {
			it.Fail("encode deleted-child probe: %v", err)
		}
		it.Write(probesPath, string(deletedBytes))
		verify("editor-deleted-tree-child.json")

		it.Write(it.Path("generated/consumer.ts"), originalConsumer+"\nimport { generatedValue as relativeValue } from './generated/value';\nexport const relative: string = relativeValue;\n")
		refreshGenerated()
		relativeProbes := []map[string]any{{"file": "generated/consumer.ts", "symbol": "./generated/value", "project": "generated/.bazel/tsconfig/generated_test.json", "definition": "generated/generated/value.ts", "diagnosticCodes": []int{2322}}}
		relativeBytes, err := json.Marshal(relativeProbes)
		if err != nil {
			it.Fail("encode relative-import boundary probe: %v", err)
		}
		it.Write(probesPath, string(relativeBytes))
		verify("editor-relative-source-boundary.json")
		it.Write(it.Path("generated/consumer.ts"), originalConsumer)
		refreshGenerated()

		it.Pass("normal solution discovery resolves generated aliases through canonical outputs without changing authored configs or stale source outputs")

		it.MustBazel("run", "//:gazelle")
		it.Pass("bazel run //:gazelle")

		generatedBuild := it.Path("generated/BUILD.bazel")
		it.RequireNotContains(generatedBuild, ".bazel/tsconfig/", "Gazelle included derived editor projects as source inputs")
		convergedBuild := it.Read(generatedBuild)
		refreshGenerated()
		it.MustBazel("run", "//:gazelle")
		if after := it.Read(generatedBuild); after != convergedBuild {
			it.Fail("refresh followed by normal Gazelle changed the converged generated program BUILD")
		}
		it.Pass("editor refresh followed by normal Gazelle preserves the converged input graph")

		owners := strings.Fields(it.BazelStdout("query", `kind("ts_(compile|test) rule", //generated:*)`))
		slices.Sort(owners)
		if !slices.Equal(owners, []string{"//generated:generated", "//generated:generated_test"}) {
			it.Fail("Gazelle changed generated fixture program owners: %v", owners)
		}
		it.RequireContains(it.Path("generated/BUILD.bazel"), `runner = "@rules_typescript//ts/runners:node_test"`, "Gazelle dropped the standalone test runner")
		it.RequireContains(it.Path("generated/BUILD.bazel"), "emit = True", "Gazelle dropped the Node runner's required emitted program")

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
