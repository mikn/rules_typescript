package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/bazelbuild/rules_go/go/runfiles"
	"github.com/mikn/rules_typescript/ts/tools/explainfiles"
	"github.com/mikn/rules_typescript/ts/tools/tsconfig"
)

func TestEditorWatcherSuitePreservesNativeCoverage(t *testing.T) {
	tools := map[string]string{}
	for _, name := range []string{"NODE_RLOCATION", "WATCHER_TEST_RLOCATION"} {
		file, err := runfiles.Rlocation(os.Getenv(name))
		if err != nil {
			t.Fatal(err)
		}
		tools[name], err = filepath.Abs(file)
		if err != nil {
			t.Fatal(err)
		}
	}
	output, err := exec.CommandContext(t.Context(), tools["NODE_RLOCATION"], "--test", "--test-reporter=tap", tools["WATCHER_TEST_RLOCATION"]).CombinedOutput()
	t.Logf("native watcher suite:\n%s", output)
	if err != nil {
		t.Fatalf("native watcher suite failed: %v", err)
	}
}

func TestEditorUnobservedSiblingsNeverReadStaleSource(t *testing.T) {
	chain := &tsconfig.Resolved{Paths: map[string][]string{"#generated/*": {"generated/*"}}, PathsDir: "app"}
	files := []string{"app/generated/value.ts", "app/generated/value.d.ts", "app/generated/value.js"}
	for range 2 {
		paths := map[string][]string{}
		projectGeneratedPaths(paths, chain, ".bazel/tsconfig", nil, files)
		if got := paths["#generated/value.js"]; !reflect.DeepEqual(got, []string{"../../bazel-bin/app/generated/value.js"}) {
			t.Fatalf("unobserved siblings chose a source or an arbitrary File: %v", got)
		}
		slices.Reverse(files)
	}
}

func TestEditorDoesNotCollapseImporterSpecificResolution(t *testing.T) {
	chain := &tsconfig.Resolved{Paths: map[string][]string{"#generated/*": {"generated/*"}}, PathsDir: "app"}
	files := []string{"app/generated/value.ts", "app/generated/value.d.ts"}
	edges := []explainfiles.Edge{{Kind: explainfiles.Import, From: "app/first.ts", Specifier: "#generated/value.js", To: files[0]}, {Kind: explainfiles.Import, From: "app/second.ts", Specifier: "#generated/value.js", To: files[1]}}
	for range 2 {
		paths := map[string][]string{}
		projectGeneratedPaths(paths, chain, ".bazel/tsconfig", nil, files)
		projectEditorEdges(paths, chain, ".bazel/tsconfig", files, edges)
		if got := paths["#generated/value.js"]; !reflect.DeepEqual(got, []string{"../../bazel-bin/app/generated/value.js"}) {
			t.Fatalf("importer-specific edges selected an arbitrary winner: %v", got)
		}
		slices.Reverse(edges)
	}
}

func TestEditorAuthoredImporterPreventsGeneratedOverride(t *testing.T) {
	chain := &tsconfig.Resolved{Paths: map[string][]string{"#generated/*": {"generated/*"}}, PathsDir: "app"}
	files := []string{"app/generated/value.ts"}
	edges := []explainfiles.Edge{{Kind: explainfiles.Import, From: "app/first.ts", Specifier: "#generated/value.js", To: files[0]}, {Kind: explainfiles.Import, From: "app/second.ts", Specifier: "#generated/value.js", To: "app/authored/value.ts"}}
	for range 2 {
		paths := map[string][]string{"#generated/value.js": {"unchanged"}}
		projectEditorEdges(paths, chain, ".bazel/tsconfig", files, edges)
		if got := paths["#generated/value.js"]; !reflect.DeepEqual(got, []string{"unchanged"}) {
			t.Fatalf("authored importer was discarded before agreement: %v", got)
		}
		slices.Reverse(edges)
	}
}

func TestEditorCompilerSelectionSurvivesEmittedSiblingsAndBrokenImports(t *testing.T) {
	compiler, err := runfiles.Rlocation(os.Getenv("TSGO_RLOCATION"))
	if err != nil {
		t.Fatal(err)
	}
	compiler, err = filepath.Abs(compiler)
	if err != nil {
		t.Fatal(err)
	}
	version, err := exec.Command(compiler, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("compiler identity: %v: %s", err, version)
	}
	t.Logf("selected compiler: %s", version)
	t.Chdir(t.TempDir())
	files := []string{}
	for _, stem := range []string{"value", "module", "common"} {
		extensions := map[string][]string{"value": {".ts", ".d.ts", ".js"}, "module": {".mts", ".d.mts", ".mjs"}, "common": {".cts", ".d.cts", ".cjs"}}[stem]
		for i, ext := range extensions {
			file := "generated/" + stem + ext
			body := "export const value = 'selected';\n"
			if i == 1 {
				body = "export declare const value: number;\n"
			}
			if i == 2 {
				body = "export const value = 1;\n"
			}
			if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, []byte(body), 0644); err != nil {
				t.Fatal(err)
			}
			files = append(files, file)
		}
	}
	files = append(files, "generated/only.d.cts")
	if err := os.WriteFile("generated/only.d.cts", []byte("export declare const value: string;\n"), 0644); err != nil {
		t.Fatal(err)
	}
	project := `{"compilerOptions":{"strict":true,"module":"preserve","moduleResolution":"bundler","paths":{"#generated/*":["./generated/*"]}},"files":["consumer.ts"]}`
	if err := os.WriteFile("tsconfig.json", []byte(project), 0644); err != nil {
		t.Fatal(err)
	}
	imports := "import { value as a } from '#generated/value.js';\nimport { value as b } from '#generated/module.mjs';\nimport { value as c } from '#generated/common.cjs';\nimport { value as d } from '#generated/only.cjs';\n"
	if err := os.WriteFile("consumer.ts", []byte(imports), 0644); err != nil {
		t.Fatal(err)
	}
	chain, err := tsconfig.Resolve("tsconfig.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, broken := range []bool{false, true} {
		paths := map[string][]string{}
		projectGeneratedPaths(paths, chain, ".bazel/tsconfig", nil, files)
		if err := writeJSON("template.json", map[string]any{"compilerOptions": map[string]any{"paths": paths}}); err != nil {
			t.Fatal(err)
		}
		if broken {
			if err := os.WriteFile("consumer.ts", []byte(imports+"import '#generated/missing';\nconst invalid: number = 'broken';\n"), 0644); err != nil {
				t.Fatal(err)
			}
		}
		command := []string{compiler, "-p", "tsconfig.json", "--noEmit", "--listFilesOnly", "--explainFiles", "--pretty", "false"}
		direct, err := exec.Command(command[0], command[1:]...).CombinedOutput()
		if err != nil {
			t.Fatalf("direct compiler discovery: %v: %s", err, direct)
		}
		listing, err := explainfiles.Parse(string(direct))
		if err != nil {
			t.Fatal(err)
		}
		selected := map[string]string{}
		for _, edge := range listing.Edges {
			if edge.Kind == explainfiles.Import && edge.From == "consumer.ts" && slices.Contains(files, edge.To) {
				selected[edge.Specifier] = edge.To
			}
		}
		t.Logf("direct compiler selected imports: %v", selected)
		if err := editorRun(".", command, "tsconfig.json", "template.json", "editor.json", ".bazel/tsconfig/project.json", files); err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile("editor.json")
		if err != nil {
			t.Fatal(err)
		}
		var editor struct {
			CompilerOptions struct {
				Paths map[string][]string `json:"paths"`
			} `json:"compilerOptions"`
		}
		if err := json.Unmarshal(raw, &editor); err != nil {
			t.Fatal(err)
		}
		for _, specifier := range []string{"value.js", "module.mjs", "common.cjs", "only.cjs"} {
			file, ok := selected["#generated/"+specifier]
			if !ok {
				t.Fatalf("direct compiler omitted declared scalar edge for %s: %s", specifier, direct)
			}
			want := []string{"../../bazel-bin/" + file}
			if got := editor.CompilerOptions.Paths["#generated/"+specifier]; !reflect.DeepEqual(got, want) {
				t.Errorf("%s=%v, want compiler-selected %v", specifier, got, want)
			}
		}
		t.Logf("compiler-selected aliases with broken imports=%v: %s", broken, raw)
		slices.Reverse(files)
	}
}

func TestEditorModuleSuffixSelectionRefreshesWhenAuthoredFallbackChanges(t *testing.T) {
	compiler, err := runfiles.Rlocation(os.Getenv("TSGO_RLOCATION"))
	if err != nil {
		t.Fatal(err)
	}
	compiler, err = filepath.Abs(compiler)
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	for file, body := range map[string]string{
		"generated/value.native.ts": "export const value: string = 'generated';\n",
		"consumer.ts":               "import { value } from '#generated/value';\nexport const result: string = value;\n",
		"tsconfig.json":             `{"compilerOptions":{"strict":true,"module":"preserve","moduleResolution":"bundler","moduleSuffixes":[".native",""],"paths":{"#generated/*":["./authored/*","./generated/*"]}},"files":["consumer.ts"]}`,
	} {
		if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll("authored", 0755); err != nil {
		t.Fatal(err)
	}
	chain, err := tsconfig.Resolve("tsconfig.json")
	if err != nil {
		t.Fatal(err)
	}
	files := []string{"generated/value.native.ts"}
	command := []string{compiler, "-p", "tsconfig.json", "--noEmit", "--listFilesOnly", "--explainFiles", "--pretty", "false"}
	for _, authored := range []bool{false, true, false} {
		if authored {
			if err := os.WriteFile("authored/value.native.ts", []byte("export const value: string = 'authored';\n"), 0644); err != nil {
				t.Fatal(err)
			}
		} else if err := os.Remove("authored/value.native.ts"); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		direct, err := exec.Command(command[0], command[1:]...).CombinedOutput()
		if err != nil {
			t.Fatalf("direct compiler discovery: %v: %s", err, direct)
		}
		listing, err := explainfiles.Parse(string(direct))
		if err != nil {
			t.Fatal(err)
		}
		selected := ""
		for _, edge := range listing.Edges {
			if edge.Kind == explainfiles.Import && edge.From == "consumer.ts" && edge.Specifier == "#generated/value" {
				selected = edge.To
			}
		}
		want := files[0]
		if authored {
			want = "authored/value.native.ts"
		}
		if selected != want {
			t.Fatalf("direct compiler selected %q, want %q: %s", selected, want, direct)
		}
		paths := map[string][]string{"#generated/*": {"../../authored/*", "../../bazel-bin/generated/*"}}
		projectGeneratedPaths(paths, chain, ".bazel/tsconfig", nil, files)
		if _, ok := paths["#generated/value"]; ok {
			t.Fatal("fixture unexpectedly inferred the moduleSuffixes spelling")
		}
		if err := writeJSON("template.json", map[string]any{"compilerOptions": map[string]any{"paths": paths}}); err != nil {
			t.Fatal(err)
		}
		if err := editorRun(".", command, "tsconfig.json", "template.json", "editor.json", ".bazel/tsconfig/project.json", files); err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile("editor.json")
		if err != nil {
			t.Fatal(err)
		}
		var editor struct {
			CompilerOptions struct {
				Paths map[string][]string `json:"paths"`
			} `json:"compilerOptions"`
		}
		if err := json.Unmarshal(raw, &editor); err != nil {
			t.Fatal(err)
		}
		if authored {
			if !reflect.DeepEqual(editor.CompilerOptions.Paths, paths) {
				t.Fatalf("refresh retained a generated override after authored fallback appeared: %s", raw)
			}
		} else if got := editor.CompilerOptions.Paths["#generated/value"]; !reflect.DeepEqual(got, []string{"../../bazel-bin/" + selected}) {
			t.Fatalf("moduleSuffixes edge was not projected: %v; direct=%s", got, selected)
		}
		t.Logf("authored fallback present=%v, direct=%s, editor=%s", authored, selected, raw)
	}
}

func TestEditorPlacementPreservesLeafAmbientTypesWithSharedAuthoredConfig(t *testing.T) {
	compiler, err := runfiles.Rlocation(os.Getenv("TSGO_RLOCATION"))
	if err != nil {
		t.Fatal(err)
	}
	compiler, err = filepath.Abs(compiler)
	if err != nil {
		t.Fatal(err)
	}
	tools := map[string]string{}
	for _, name := range []string{"NODE_RLOCATION", "VERIFIER_RLOCATION"} {
		file, err := runfiles.Rlocation(os.Getenv(name))
		if err != nil {
			t.Fatal(err)
		}
		tools[name], err = filepath.Abs(file)
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, test := range []struct {
		name, types, roots, source, selected, diagnostic string
	}{
		{"nested", `["choice"]`, "", `export const value: "nested" = ambient;`, "pkg/nested/node_modules/@types/choice/index.d.ts", ""},
		{"empty", `[]`, "", `export {};`, "", ""},
		{"missing", `["absent"]`, "", `export {};`, "", "TS2688"},
		{"custom", `["choice"]`, `,"typeRoots":["./typings"]`, `export const value: "custom" = ambient;`, "shared/typings/choice/index.d.ts", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			write := func(name, contents string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(name, []byte(contents), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			write("node_modules/@types/choice/index.d.ts", `declare const ambient: "root";`)
			write("pkg/nested/node_modules/@types/choice/index.d.ts", `declare const ambient: "nested";`)
			write("shared/typings/choice/index.d.ts", `declare const ambient: "custom";`)
			write("shared/tsconfig.json", `{"compilerOptions":{"strict":true,"types":`+test.types+test.roots+`}}`)
			write("baseline.json", `{}`)
			write("pkg/nested/input.ts", test.source)
			write("pkg/nested/reference.json", `{"extends":"../../shared/tsconfig.json","files":["input.ts"]}`)
			check := func(project string) string {
				t.Helper()
				output, err := exec.Command(compiler, "--project", project, "--noEmit", "--pretty", "false", "--listFiles").CombinedOutput()
				text := filepath.ToSlash(string(output))
				if test.diagnostic == "" && err != nil || test.diagnostic != "" && (err == nil || !strings.Contains(text, test.diagnostic)) {
					t.Fatalf("%s: %v\n%s", project, err, output)
				}
				if test.selected != "" && !strings.Contains(text, test.selected) {
					t.Fatalf("%s lost selected ambient file %s:\n%s", project, test.selected, text)
				}
				if test.name == "empty" && strings.Contains(text, "/@types/choice/") {
					t.Fatalf("empty types gained ambient package: %s", text)
				}
				t.Logf("project=%s\n%s", project, text)
				return text
			}
			check("pkg/nested/reference.json")
			var types []string
			if err := json.Unmarshal([]byte(test.types), &types); err != nil {
				t.Fatal(err)
			}
			for _, target := range []string{"library", "tests"} {
				a := actionConfig{project: "shared/tsconfig.json", baseline: "baseline.json", out: "bazel-out/test/bin/pkg/nested/" + target + ".tsconfig.json", binDir: "bazel-out/test/bin", editorPath: "pkg/nested/.bazel/tsconfig/" + target + ".json"}
				config := &tsconfigFile{CompilerOptions: map[string]any{"types": types}, Files: []string{fileRelative(filepath.ToSlash(filepath.Dir(a.out)), "pkg/nested/input.ts")}}
				editor := a.editorConfig(config, nil)
				raw, err := json.Marshal(editor)
				if err != nil {
					t.Fatal(err)
				}
				write(a.editorPath, string(raw))
				check(a.editorPath)
				if test.selected != "" {
					write("pkg/nested/tsconfig.json", `{"files":[],"include":[],"references":[{"path":"./.bazel/tsconfig/`+target+`.json"}]}`)
					probe := []map[string]string{{"file": "pkg/nested/input.ts", "symbol": "ambient", "definition": test.selected, "project": a.editorPath}}
					raw, err := json.Marshal(probe)
					if err != nil {
						t.Fatal(err)
					}
					write("probes.json", string(raw))
					output, err := exec.Command(tools["NODE_RLOCATION"], tools["VERIFIER_RLOCATION"], compiler, "probes.json").CombinedOutput()
					t.Logf("ambient native verifier %s/%s:\n%s", test.name, target, output)
					if err != nil {
						t.Fatalf("native ambient project discovery: %v\n%s", err, output)
					}
				}
			}
		})
	}
}
