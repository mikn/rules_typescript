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

// emitRoot lays an exec root out under a temp dir and makes it the working
// directory: two srcs under pkg/, one under bazel-out, and fake tools.
type emitRoot struct {
	oxc, oxcArgv, tsgo, tsgoArgv string
}

func newEmitRoot(t *testing.T, module string) *emitRoot {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"pkg/src/a.ts":                    "export const a: number = 1;\n",
		"pkg/src/view.tsx":                "export const v = <div />;\n",
		binDir + "/pkg/gen.ts":            "export const g = 2;\n",
		binDir + "/pkg/pkg.tsconfig.json": "{}",
		binDir + "/pkg/pkg.options.json": `{"target": "es2017", "jsx": "preserve",` +
			` "module": "` + module + `"}`,
		binDir + "/pkg/pkg/node_modules/x/i": "",
	}
	for rel, body := range files {
		writeFile(t, filepath.Join(root, rel), body)
	}
	// The fake tsgo writes what tsc would into its --outDir: the maps' sources
	// are relative to the map's directory, seven below the exec root here.
	tsgoScript := `out=""; prev=""
for a in "$@"; do
  if [ "$prev" = "--outDir" ]; then out="$a"; fi; prev="$a"
done
mkdir -p "$out/src"
for f in src/a.js src/a.d.ts src/view.jsx src/view.d.ts; do
  echo "$f" > "$out/$f"
done
up=../../../../../../..
cat > "$out/src/a.js.map" <<EOF
{"version":3,"file":"a.js","sourceRoot":"","names":[],"mappings":";;;AAAa",
"sources":["$up/pkg/src/a.ts"],"sourcesContent":["export const a = 1;\\n"]}
EOF
cat > "$out/src/view.jsx.map" <<EOF
{"version":3,"file":"view.jsx","sourceRoot":"","names":[],"mappings":";;;AAAa",
"sources":["$up/pkg/src/view.tsx"],"sourcesContent":["export const v = 1;\\n"]}
EOF
`
	e := &emitRoot{}
	e.oxc, e.oxcArgv = fakeTool(t, root, "oxc-bazel", "")
	e.tsgo, e.tsgoArgv = fakeTool(t, root, "tsgo", tsgoScript)
	t.Chdir(root)
	return e
}

func (e *emitRoot) args(extra ...string) []string {
	args := []string{
		"-options=" + binDir + "/pkg/pkg.options.json",
		"-tsconfig=" + binDir + "/pkg/pkg.tsconfig.json",
		"-node_modules=" + binDir + "/pkg/pkg/node_modules",
		"-scratch=" + binDir + "/pkg/pkg.emit",
		"-out_dir=" + binDir + "/pkg",
		"-oxc=" + e.oxc,
		"-tsgo=" + e.tsgo,
	}
	return append(args, extra...)
}

// An ES module kind is oxc's: one run per root with the options' flags, and
// the root stripped so the emit lands at the src's package-relative path.
func TestEmitStep_ESModulesGoToOxcPerRoot(t *testing.T) {
	e := newEmitRoot(t, "esnext")
	err := runEmit(e.args("-root=pkg", "-root="+binDir+"/pkg", "-source_map",
		"-declarations", "pkg/src/a.ts", binDir+"/pkg/gen.ts", "pkg/src/view.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	// The last run recorded is the sorted last root, pkg.
	want := []string{
		"--files", "pkg/src/a.ts", "pkg/src/view.tsx", "--out-dir", binDir + "/pkg",
		"--strip-dir-prefix", "pkg", "--source-map",
		"--declaration", "--isolated-declarations",
		"--target", "es2017", "--jsx", "preserve",
	}
	if got := recordedArgs(t, e.oxcArgv); !reflect.DeepEqual(got, want) {
		t.Errorf("oxc ran with %q, want %q", got, want)
	}
	if _, err := os.Stat(e.tsgoArgv); err == nil {
		t.Error("tsgo ran for an ES module program")
	}
}

// commonjs is tsgo's: --noCheck from the program root, the emit shape on the
// command line, and each src's outputs moved from the scratch outDir.
func TestEmitStep_CommonJSGoesToTsgo(t *testing.T) {
	e := newEmitRoot(t, "commonjs")
	err := runEmit(e.args("-root=pkg", "-source_map", "-declarations",
		"pkg/src/a.ts", "pkg/src/view.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"--project", binDir + "/pkg/pkg.tsconfig.json", "--noCheck",
		"--noEmit", "false", "--emitDeclarationOnly", "false",
		"--declaration", "true", "--declarationMap", "false", "--sourceMap", "true",
		"--inlineSources", "true",
		"--outDir", binDir + "/pkg/pkg.emit/out", "--rootDir", "pkg",
	}
	if got := recordedArgs(t, e.tsgoArgv); !reflect.DeepEqual(got, want) {
		t.Errorf("tsgo ran with %q, want %q", got, want)
	}
	for _, out := range []string{
		"src/a.js", "src/a.js.map", "src/a.d.ts",
		"src/view.jsx", "src/view.jsx.map", "src/view.d.ts",
	} {
		if _, err := os.Stat(filepath.Join(binDir, "pkg", out)); err != nil {
			t.Errorf("%s was not moved into the output directory: %v", out, err)
		}
	}
	for m, want := range map[string]string{
		"src/a.js.map": "pkg/src/a.ts", "src/view.jsx.map": "pkg/src/view.tsx",
	} {
		got := mapSources(t, filepath.Join(binDir, "pkg", m))
		if !reflect.DeepEqual(got, []string{want}) {
			t.Errorf("%s names sources %q, want %q", m, got, want)
		}
	}
	_, err = os.Stat(filepath.Join(binDir, "pkg", "pkg.emit"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the scratch directory survived the step: %v", err)
	}
	if _, err := os.Stat(e.oxcArgv); err == nil {
		t.Error("oxc ran for a commonjs program")
	}
}

// node16 and nodenext decide the format per file from the nearest package.json,
// which only tsgo reads.
func TestEmitsWithOxc(t *testing.T) {
	for module, want := range map[string]bool{
		"preserve": true, "esnext": true, "es2022": true, "es2015": true,
		"commonjs": false, "node16": false, "node18": false, "nodenext": false,
		"": false,
	} {
		if got := emitsWithOxc(module); got != want {
			t.Errorf("emitsWithOxc(%q) = %v, want %v", module, got, want)
		}
	}
}

// One tsgo emit has one rootDir, the rule the declaration emit already states.
func TestEmitStep_CommonJSNeedsOneRoot(t *testing.T) {
	e := newEmitRoot(t, "commonjs")
	err := runEmit(e.args("-root=pkg", "-root="+binDir+"/pkg",
		"pkg/src/a.ts", binDir+"/pkg/gen.ts"))
	if err == nil {
		t.Fatal("want an error for a commonjs program with two roots")
	}
	for _, want := range []string{
		`"commonjs"`, "2 roots", "pkg\n", binDir + "/pkg", "own target",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not say %q", err, want)
		}
	}
}

// mapSources reads a moved map's sources, the exec-root paths of its srcs.
func mapSources(t *testing.T, name string) []string {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Sources []string `json:"sources"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("%s: %v\n%s", name, err, data)
	}
	return m.Sources
}

func TestEmitStep_ExitCodeIsTheTools(t *testing.T) {
	e := newEmitRoot(t, "esnext")
	writeFile(t, e.oxc, "#!/bin/sh\nexit 3\n")
	if err := os.Chmod(e.oxc, 0o755); err != nil {
		t.Fatal(err)
	}
	err := runEmit(e.args("-root=pkg", "pkg/src/a.ts"))
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 3 {
		t.Errorf("runEmit = %v, want oxc's exit status 3", err)
	}
}
