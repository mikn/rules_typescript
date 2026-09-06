package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The chain the testdata captures were printed over, with
// `tsgo --showConfig -p <root>/pkg/tsconfig.json` (tsgo 7.0.2): a base in
// another directory sets paths, target and jsx; a leaf sets types, or nothing.
const baseConfig = `{
  // A comment: tsconfig.json is JSONC.
  "compilerOptions": {
    "target": "es2017",
    "jsx": "react-jsx",
    "jsxImportSource": "preact",
    "paths": { "@app/*": ["src/*"], "#lib": ["lib/index.ts"] },
    "strict": true,
    "typeRoots": ["./typings"],
  }
}
`

const chainLeaf = `{
  "extends": "../base/tsconfig.base.json",
  "compilerOptions": {
    "types": ["./globals.d.ts", "./generated.d.ts", "node", "@cloudflare/workers-types"],
    "lib": ["es2022"]
  },
  "include": ["src"]
}
`

const noTypesLeaf = `{
  "extends": "../base/tsconfig.base.json",
  "compilerOptions": { "target": "esnext", "jsx": "preserve", "module": "nodenext" }
}
`

const binDir = "bazel-out/k8-fastbuild/bin"

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readTestdata(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// fakeTool writes an executable that records its arguments, then runs script.
func fakeTool(t *testing.T, root, name, script string) (bin, argv string) {
	t.Helper()
	argv = filepath.Join(root, name+".argv")
	bin = filepath.Join(root, name)
	writeFile(t, bin, "#!/bin/sh\nprintf '%s\\n' \"$@\" > "+argv+"\n"+script)
	if err := os.Chmod(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, argv
}

func recordedArgs(t *testing.T, argv string) []string {
	t.Helper()
	data, err := os.ReadFile(argv)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

type execroot struct {
	tsgo string
	argv string
}

// newExecroot lays the action's sandbox out under a temp dir and makes it the
// working directory: the chain and its srcs, one generated declaration and the
// forest under the bin dir, and a tsgo whose --showConfig prints showConfig.
func newExecroot(t *testing.T, leaf, showConfig string) *execroot {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"base/tsconfig.base.json":                           baseConfig,
		"pkg/tsconfig.json":                                 leaf,
		"pkg/src/a.ts":                                      "export const a = 1;\n",
		"pkg/globals.d.ts":                                  "declare const G: number;\n",
		binDir + "/pkg/generated.d.ts":                      "declare const generated: string;\n",
		binDir + "/pkg/node_modules/react/index.d.ts":       "export {};\n",
		binDir + "/pkg/node_modules/@types/node/index.d.ts": "declare const process: unknown;\n",
		binDir + "/pkg/pkg.tsconfig_baseline.json":          `{"compilerOptions": {"strict": true}}`,
		"showconfig.json":                                   showConfig,
	}
	for rel, body := range files {
		writeFile(t, filepath.Join(root, rel), body)
	}
	tsgo, argv := fakeTool(t, root, "tsgo", "cat "+filepath.Join(root, "showconfig.json")+"\n")
	t.Chdir(root)
	return &execroot{tsgo: tsgo, argv: argv}
}

func (e *execroot) tsconfigArgs(extra ...string) []string {
	args := []string{
		"-tsgo=" + e.tsgo,
		"-tsconfig=pkg/tsconfig.json",
		"-baseline=" + binDir + "/pkg/pkg.tsconfig_baseline.json",
		"-out=" + binDir + "/pkg/pkg.tsconfig.json",
		"-options=" + binDir + "/pkg/pkg.options.json",
		"-bin_dir=" + binDir,
	}
	args = append(args, extra...)
	return append(args, "pkg/src/a.ts", "pkg/globals.d.ts")
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("%s: %v\n%s", path, err, data)
	}
	return out
}

// assertJSON compares two documents structurally: both are re-encoded with
// sorted keys, so the expectation reads as the JSON it stands for.
func assertJSON(t *testing.T, what string, got any, want string) {
	t.Helper()
	var decoded any
	if err := json.Unmarshal([]byte(want), &decoded); err != nil {
		t.Fatalf("the expectation for %s is not JSON: %v", what, err)
	}
	g, _ := json.MarshalIndent(got, "", "  ")
	w, _ := json.MarshalIndent(decoded, "", "  ")
	if string(g) != string(w) {
		t.Errorf("%s:\n%s\nwant:\n%s", what, g, w)
	}
}

func mustWriteTsconfig(t *testing.T, args []string) {
	t.Helper()
	if err := writeTsconfig(args); err != nil {
		t.Fatalf("tsaction tsconfig: %v", err)
	}
}

// tsgo prints the merged compilerOptions with every enum as its lowercase
// name, so target and jsx reach oxc as the strings tsgo printed.
func TestDecodeShowConfig_EnumsAreNames(t *testing.T) {
	got, err := decodeShowConfig([]byte(readTestdata(t, "showconfig-chain.json")))
	if err != nil {
		t.Fatal(err)
	}
	want := &effectiveOptions{
		Target:          "es2017",
		Jsx:             "react-jsx",
		JsxImportSource: "preact",
		Types:           &[]string{"./globals.d.ts", "./generated.d.ts", "node", "@cloudflare/workers-types"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("decodeShowConfig = %+v, want %+v", got, want)
	}

	noTypes, err := decodeShowConfig([]byte(readTestdata(t, "showconfig-no-types.json")))
	if err != nil {
		t.Fatal(err)
	}
	if noTypes.Types != nil {
		t.Errorf("Types = %v, want nil: the leaf sets no types key", *noTypes.Types)
	}
	if noTypes.Target != "esnext" || noTypes.Jsx != "preserve" || noTypes.JsxImportSource != "preact" {
		t.Errorf("Target, Jsx, JsxImportSource = %q, %q, %q; want esnext, preserve, preact",
			noTypes.Target, noTypes.Jsx, noTypes.JsxImportSource)
	}
}

func TestDecodeShowConfig_DiagnosticsAreNotAConfig(t *testing.T) {
	_, err := decodeShowConfig([]byte("error TS5023: Unknown compiler option 'x'.\n"))
	if err == nil || !strings.Contains(err.Error(), "TS5023") {
		t.Errorf("decodeShowConfig(diagnostic) = %v, want an error quoting the diagnostic", err)
	}
}

// The written config extends the baseline and the user's file, owns the keys
// that encode the sandbox, and rewrites the two the user's chain answers:
// paths from the directory of the base that set them with a bin-dir twin per
// value, and types with each path-shaped entry rebased to the staged source or
// bin-dir file. No key names a forest package and typeRoots stays unset: react
// and @types/node resolve through node_modules alone. showConfig reads the
// config itself, written first with its extends alone, so the baseline's
// defaults are in what it prints.
func TestTsconfigStep_WritesTheForestShapedConfig(t *testing.T) {
	capture := readTestdata(t, "showconfig-chain.json")
	e := newExecroot(t, chainLeaf, capture)

	mustWriteTsconfig(t, e.tsconfigArgs("-declaration_map", "-types_dep=node"))

	if got, want := recordedArgs(t, e.argv), []string{"--showConfig", "-p", binDir + "/pkg/pkg.tsconfig.json"}; !reflect.DeepEqual(got, want) {
		t.Errorf("tsgo ran with %q, want %q", got, want)
	}
	assertJSON(t, "pkg.tsconfig.json", readJSON(t, binDir+"/pkg/pkg.tsconfig.json"), `{
  "extends": ["./pkg.tsconfig_baseline.json", "../../../../pkg/tsconfig.json"],
  "compilerOptions": {
    "composite": false,
    "declaration": true,
    "declarationMap": true,
    "emitDeclarationOnly": true,
    "incremental": false,
    "paths": {
      "#lib": ["../../../../base/lib/index.ts", "../base/lib/index.ts"],
      "@app/*": ["../../../../base/src/*", "../base/src/*"]
    },
    "preserveSymlinks": true,
    "rootDir": "../../../..",
    "rootDirs": ["../../../..", ".."],
    "types": ["../../../../pkg/globals.d.ts", "./generated.d.ts", "node", "@cloudflare/workers-types"]
  },
  "include": ["../../../../pkg/src/a.ts", "../../../../pkg/globals.d.ts"],
  "files": [],
  "exclude": [],
  "references": []
}`)
	assertJSON(t, "pkg.options.json", readJSON(t, binDir+"/pkg/pkg.options.json"),
		`{"target": "es2017", "jsx": "react-jsx", "jsxImportSource": "preact"}`)
}

// A chain that sets no types would let tsgo include every package under
// typeRoots, transitive @types included. The direct @types deps are written
// instead -- and an empty list when there are none -- so the roots never
// auto-include.
func TestTsconfigStep_NoTypesWritesTheDirectTypesDeps(t *testing.T) {
	capture := readTestdata(t, "showconfig-no-types.json")
	e := newExecroot(t, noTypesLeaf, capture)

	mustWriteTsconfig(t, e.tsconfigArgs("-types_dep=node", "-types_dep=react"))
	config := readJSON(t, binDir+"/pkg/pkg.tsconfig.json")
	assertJSON(t, "types", config["compilerOptions"].(map[string]any)["types"], `["node", "react"]`)
	assertJSON(t, "pkg.options.json", readJSON(t, binDir+"/pkg/pkg.options.json"),
		`{"target": "esnext", "jsx": "preserve", "jsxImportSource": "preact"}`)

	mustWriteTsconfig(t, e.tsconfigArgs())
	config = readJSON(t, binDir+"/pkg/pkg.tsconfig.json")
	assertJSON(t, "types with no @types dep", config["compilerOptions"].(map[string]any)["types"], `[]`)
}

// A target with no tsconfig extends the baseline alone: no chain, no paths.
func TestTsconfigStep_NoTsconfigExtendsTheBaselineAlone(t *testing.T) {
	e := newExecroot(t, noTypesLeaf, `{"compilerOptions": {"target": "es2022", "jsx": "react-jsx"}}`)
	args := []string{
		"-tsgo=" + e.tsgo,
		"-baseline=" + binDir + "/pkg/pkg.tsconfig_baseline.json",
		"-out=" + binDir + "/pkg/pkg.tsconfig.json",
		"-options=" + binDir + "/pkg/pkg.options.json",
		"-bin_dir=" + binDir,
		"-types_dep=node",
		"pkg/src/a.ts",
	}
	mustWriteTsconfig(t, args)

	config := readJSON(t, binDir+"/pkg/pkg.tsconfig.json")
	assertJSON(t, "extends", config["extends"], `["./pkg.tsconfig_baseline.json"]`)
	opts := config["compilerOptions"].(map[string]any)
	if _, ok := opts["paths"]; ok {
		t.Errorf("paths = %v, want none: no chain sets one", opts["paths"])
	}
	assertJSON(t, "types", opts["types"], `["node"]`)
	assertJSON(t, "pkg.options.json", readJSON(t, binDir+"/pkg/pkg.options.json"), `{"target": "es2022", "jsx": "react-jsx"}`)
}

// A JavaScript src is in include, and tsgo reads it only under allowJs.
func TestTsconfigStep_JavaScriptSrcSetsAllowJs(t *testing.T) {
	e := newExecroot(t, noTypesLeaf, readTestdata(t, "showconfig-no-types.json"))
	writeFile(t, "pkg/src/b.js", "export const b = 1;\n")

	mustWriteTsconfig(t, e.tsconfigArgs())
	if got := readJSON(t, binDir+"/pkg/pkg.tsconfig.json")["compilerOptions"].(map[string]any)["allowJs"]; got != nil {
		t.Errorf("allowJs = %v with no JavaScript src, want unset", got)
	}

	mustWriteTsconfig(t, append(e.tsconfigArgs(), "pkg/src/b.js"))
	if got := readJSON(t, binDir+"/pkg/pkg.tsconfig.json")["compilerOptions"].(map[string]any)["allowJs"]; got != true {
		t.Errorf("allowJs = %v with a JavaScript src, want true", got)
	}
}

func TestTsconfigStep_EmitShape(t *testing.T) {
	e := newExecroot(t, noTypesLeaf, readTestdata(t, "showconfig-no-types.json"))

	mustWriteTsconfig(t, e.tsconfigArgs(
		"-emit", "-out_dir="+binDir+"/pkg", "-root_dir=pkg", "-isolated_declarations", "-lib_check",
	))
	opts := readJSON(t, binDir+"/pkg/pkg.tsconfig.json")["compilerOptions"].(map[string]any)
	for key, want := range map[string]any{
		"noEmit":               false,
		"noEmitOnError":        true,
		"outDir":               ".",
		"declarationDir":       ".",
		"rootDir":              "../../../../pkg",
		"declarationMap":       false,
		"isolatedDeclarations": true,
		"skipLibCheck":         false,
	} {
		if got := opts[key]; !reflect.DeepEqual(got, want) {
			t.Errorf("compilerOptions.%s = %v, want %v", key, got, want)
		}
	}
}

// A path-shaped types entry that no input sits at would be TS2688 from tsgo,
// naming no label; the step fails first, naming both places it looked, and
// writes nothing.
func TestTsconfigStep_TypesEntryNowhereFails(t *testing.T) {
	e := newExecroot(t, chainLeaf, `{"compilerOptions": {"types": ["./missing.d.ts", "node"]}}`)

	err := writeTsconfig(e.tsconfigArgs())
	if err == nil {
		t.Fatal("writeTsconfig: want an error for a types entry nothing stages")
	}
	for _, want := range []string{`"./missing.d.ts"`, "pkg/missing.d.ts", binDir} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
	if _, statErr := os.Stat(binDir + "/pkg/pkg.tsconfig.json"); statErr == nil {
		t.Error("a config was written although the types entry failed")
	}
}

func TestTsconfigStep_ShowConfigFailureIsTheError(t *testing.T) {
	e := newExecroot(t, chainLeaf, "")
	writeFile(t, e.tsgo, "#!/bin/sh\necho \"error TS5023: Unknown compiler option 'x'.\"\nexit 1\n")

	err := writeTsconfig(e.tsconfigArgs())
	if err == nil || !strings.Contains(err.Error(), "TS5023") || !strings.Contains(err.Error(), "--showConfig") {
		t.Errorf("writeTsconfig = %v, want tsgo's diagnostic and the command that printed it", err)
	}
}

// The oxc step appends what the options file carries to the command line it is
// handed, so oxc transforms with the target and jsx tsgo checked under; a key
// the tsconfig leaves unset is not passed, and oxc's own default stands.
func TestOxcStep_AppendsTheOptions(t *testing.T) {
	root := t.TempDir()
	oxc, argv := fakeTool(t, root, "oxc-bazel", "")
	options := filepath.Join(root, "pkg.options.json")

	writeFile(t, options, `{"target": "es2017", "jsx": "react-jsx", "jsxImportSource": "preact"}`)
	if err := runOxc([]string{"-options=" + options, "--", oxc, "--files", "a.ts", "--out-dir", "out"}); err != nil {
		t.Fatal(err)
	}
	want := []string{"--files", "a.ts", "--out-dir", "out", "--target", "es2017", "--jsx", "react-jsx", "--jsx-import-source", "preact"}
	if got := recordedArgs(t, argv); !reflect.DeepEqual(got, want) {
		t.Errorf("oxc ran with %q, want %q", got, want)
	}

	writeFile(t, options, `{}`)
	if err := runOxc([]string{"-options=" + options, "--", oxc, "--files", "a.ts"}); err != nil {
		t.Fatal(err)
	}
	if got, want := recordedArgs(t, argv), []string{"--files", "a.ts"}; !reflect.DeepEqual(got, want) {
		t.Errorf("oxc ran with %q, want %q", got, want)
	}
}

func TestOxcStep_ExitCodeIsTheTools(t *testing.T) {
	root := t.TempDir()
	oxc, _ := fakeTool(t, root, "oxc-bazel", "exit 3\n")
	options := filepath.Join(root, "pkg.options.json")
	writeFile(t, options, `{"target": "es2022"}`)

	err := runOxc([]string{"-options=" + options, "--", oxc, "--files", "a.ts"})
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 3 {
		t.Errorf("runOxc = %v, want oxc's exit status 3", err)
	}
}

// The spelling _generate_tsconfig used: "." for the same directory, no
// leading "./" -- and "" or "." both name the exec root.
func TestRelativePath(t *testing.T) {
	cases := []struct{ from, to, want string }{
		{binDir + "/pkg", "", "../../../.."},
		{binDir + "/pkg", ".", "../../../.."},
		{binDir + "/pkg", binDir, ".."},
		{binDir + "/pkg", binDir + "/pkg", "."},
		{binDir + "/pkg", binDir + "/pkg/node_modules", "node_modules"},
		{"pkg", "other/dir", "../other/dir"},
		{".", "pkg/src", "pkg/src"},
	}
	for _, tc := range cases {
		if got := relativePath(tc.from, tc.to); got != tc.want {
			t.Errorf("relativePath(%q, %q) = %q, want %q", tc.from, tc.to, got, tc.want)
		}
	}
	for in, want := range map[string]string{"node_modules": "./node_modules", "../x": "../x", ".": ".", "/abs": "/abs", "./y": "./y"} {
		if got := explicitlyRelative(in); got != want {
			t.Errorf("explicitlyRelative(%q) = %q, want %q", in, got, want)
		}
	}
}
