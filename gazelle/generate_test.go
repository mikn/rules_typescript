package typescript

import (
	"os"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/bazelbuild/bazel-gazelle/config"
	"github.com/bazelbuild/bazel-gazelle/language"
	"github.com/bazelbuild/bazel-gazelle/rule"
)

// Under Bazel the toolchain's tsgo is in the runfiles; a plain go test sets
// TSGO or skips what lists programs.
func requireTsgo(t *testing.T) {
	t.Helper()
	if _, err := newProgramStore().binary(); err != nil {
		t.Skipf("no tsgo binary: %v", err)
	}
}

// writeTree writes a repository-relative tree under a fresh repo root.
func writeTree(t *testing.T, tree map[string]string) string {
	t.Helper()
	root := t.TempDir()
	writeWorkspace(t, root, tree)
	return root
}

// One walk's GenerateRules over every directory of root, in Gazelle's order:
// configured top-down, generated bottom-up. Nothing is merged or resolved.
type generatedTree struct {
	results map[string]language.GenerateResult
	logged  string
}

func generateAll(t *testing.T, root string,
	opts ...func(*config.Config)) generatedTree {
	t.Helper()
	requireTsgo(t)
	out := generatedTree{results: map[string]language.GenerateResult{}}
	var walk func(parent *config.Config, rel string)
	walk = func(parent *config.Config, rel string) {
		dir := filepath.Join(root, filepath.FromSlash(rel))
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		var subdirs, regular []string
		var f *rule.File
		for _, e := range entries {
			name := e.Name()
			switch {
			case strings.HasPrefix(name, "."), strings.HasPrefix(name, "bazel-"):
			case e.IsDir():
				subdirs = append(subdirs, name)
			case name == "BUILD.bazel":
				loaded, err := rule.LoadFile(filepath.Join(dir, name), rel)
				if err != nil {
					t.Fatal(err)
				}
				f = loaded
			default:
				regular = append(regular, name)
			}
		}
		c := parent.Clone()
		configureTsConfig(c, rel, f)
		for _, sub := range subdirs {
			walk(c, path.Join(rel, sub))
		}
		out.results[rel] = generateRules(language.GenerateArgs{
			Config: c, Dir: dir, Rel: rel, File: f, Subdirs: subdirs,
			RegularFiles: regular,
		})
	}
	c := &config.Config{RepoRoot: root, Exts: map[string]any{}}
	for _, opt := range opts {
		opt(c)
	}
	out.logged = captureLog(t, func() { walk(c, "") })
	return out
}

// verbose is -ts_verbose: the run's store says what it lists and leaves out.
func verbose(c *config.Config) {
	tc := defaultTsConfig()
	tc.programs.verbose = true
	c.Exts[languageName] = tc
}

func generatedRule(res language.GenerateResult, name string) *rule.Rule {
	for _, r := range res.Gen {
		if r.Name() == name {
			return r
		}
	}
	return nil
}

// mustRule is the generated rule of that kind and name.
func mustRule(t *testing.T, res language.GenerateResult, kind, name string,
) *rule.Rule {
	t.Helper()
	r := generatedRule(res, name)
	if r == nil || r.Kind() != kind {
		t.Fatalf("no %s %s; generated %v", kind, name, generatedNames(t, res))
	}
	return r
}

// generatedNames maps rule name to kind for every generated rule, failing on a
// name two rules share.
func generatedNames(t *testing.T, res language.GenerateResult,
) map[string]string {
	t.Helper()
	byName := make(map[string]string, len(res.Gen))
	for _, r := range res.Gen {
		if kind, dup := byName[r.Name()]; dup {
			t.Errorf("duplicate target name %q: %s and %s", r.Name(), r.Kind(), kind)
		}
		byName[r.Name()] = r.Kind()
	}
	return byName
}

func kindsOf(rules []*rule.Rule) []string {
	var out []string
	for _, r := range rules {
		out = append(out, r.Kind()+"("+r.Name()+")")
	}
	return out
}

func assertRule(t *testing.T, byName map[string]string, name, kind string) {
	t.Helper()
	got, ok := byName[name]
	if !ok {
		t.Errorf("no target named %q; generated %v", name, byName)
		return
	}
	if got != kind {
		t.Errorf("target %q: got kind %s, want %s", name, got, kind)
	}
}

func wantStrings(t *testing.T, what string, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

func withdraws(res language.GenerateResult, kind, name string) bool {
	for _, r := range res.Empty {
		if r.Kind() == kind && r.Name() == name {
			return true
		}
	}
	return false
}

func hasSrc(srcs []string, want string) bool {
	return slices.Contains(srcs, want)
}

// onDiskRule is the rule of that kind and name in pkg's BUILD file after a run.
func onDiskRule(t *testing.T, root, pkg, kind, name string) *rule.Rule {
	t.Helper()
	r := ruleNamed(loadRules(t, root, pkg), kind, name)
	if r == nil {
		t.Fatalf("no %s(%s) in //%s:\n%s", kind, name, pkg,
			buildFileText(t, root, pkg))
	}
	return r
}

func converge(t *testing.T, tree map[string]string) (root, logged string) {
	t.Helper()
	requireTsgo(t)
	root = writeTree(t, tree)
	logged = captureLog(t, func() { convergeGazelle(t, root) })
	return root, logged
}

const (
	rootManifest = `{"name":"w"}` + "\n"
	includeAll   = `{"include":["**/*"]}` + "\n"
	includeTs    = `{"compilerOptions":{"lib":["es2022"]},"include":["*.ts"]}` +
		"\n"
	includeSrc = `{"compilerOptions":{"lib":["es2022"]},` +
		`"include":["src/**/*"]}` + "\n"
	loadDefs = `load("@rules_typescript//ts:defs.bzl", `
)

// ---- the package ------------------------------------------------------------

// A directory below a package is not one: its files are the package's, and a
// BUILD file an earlier run left there is emptied and named, once.
func TestGenerate_ADirectoryBelowAPackageIsNotAPackage(t *testing.T) {
	g := generateAll(t, writeTree(t, map[string]string{
		"package.json":      rootManifest,
		"pkg/tsconfig.json": includeAll,
		"pkg/src/a.ts":      "export const a = 1;\n",
		"pkg/src/a.test.ts": "export const t = 1;\n",
		"pkg/src/BUILD.bazel": loadDefs + `"ts_compile", "ts_test")

ts_compile(
    name = "src",
    srcs = ["a.ts"],
)

ts_test(
    name = "src_test",
    srcs = ["a.test.ts"],
)
`,
	}))

	below := g.results["pkg/src"]
	if len(below.Gen) != 0 {
		t.Errorf("pkg/src generated %v; it holds no tsconfig.json",
			generatedNames(t, below))
	}
	got := kindsOf(below.Empty)
	sort.Strings(got)
	wantStrings(t, "pkg/src withdraws", got, []string{
		"filegroup(vitest_config)", "filegroup(wrangler_config)",
		"ts_compile(src)", "ts_config(tsconfig)", "ts_test(src_test)"})
	if n := strings.Count(g.logged, "pkg/src/BUILD.bazel"); n != 1 {
		t.Errorf("the BUILD file to delete was named %d times, want once:\n%s",
			n, g.logged)
	}

	pkg := g.results["pkg"]
	compile := mustRule(t, pkg, "ts_compile", "pkg")
	wantStrings(t, "ts_compile srcs", compile.AttrStrings("srcs"),
		[]string{"src/a.ts"})
	test := mustRule(t, pkg, "ts_test", "pkg_test")
	wantStrings(t, "ts_test srcs", test.AttrStrings("srcs"),
		[]string{"src/a.test.ts"})
	for _, r := range []*rule.Rule{compile, test} {
		if got := r.AttrString("tsconfig"); got != ":tsconfig" {
			t.Errorf("%s tsconfig = %q, want :tsconfig", r.Kind(), got)
		}
	}
}

// A rule under # keep in a directory that is no package is the merger's to
// leave, so the run does not say it is withdrawn.
func TestGenerate_AKeptRuleInANonPackageIsNotSaidWithdrawn(t *testing.T) {
	g := generateAll(t, writeTree(t, map[string]string{
		"package.json": rootManifest,
		"fixture/a.ts": "export const a = 1;\n",
		"fixture/BUILD.bazel": loadDefs + `"ts_compile")

# keep
ts_compile(
    name = "fixture",
    srcs = ["a.ts"],
)
`,
	}))
	if !withdraws(g.results["fixture"], "ts_compile", "fixture") {
		t.Errorf("fixture does not withdraw its ts_compile; Empty = %v",
			kindsOf(g.results["fixture"].Empty))
	}
	if strings.Contains(g.logged, "fixture/BUILD.bazel") {
		t.Errorf("the kept rule was said to be withdrawn:\n%s", g.logged)
	}
}

// srcs are the program's files wherever they sit plus the tree's other regular
// files; a deeper package's, an out_dir's and the program's exclusions are not.
func TestGenerate_SrcsAreTheProgramAndTheTreesOtherFiles(t *testing.T) {
	g := generateAll(t, writeTree(t, map[string]string{
		"package.json": rootManifest,
		"pkg/BUILD.bazel": loadDefs + `"ts_codegen")

ts_codegen(
    name = "tree",
    srcs = ["names.txt"],
    generator = "//:gen",
    out_dir = "compiled",
)
`,
		"pkg/tsconfig.json": `{"compilerOptions":{"resolveJsonModule":true,` +
			`"module":"esnext","moduleResolution":"bundler","lib":["es2022"]},` +
			`"include":["src/**/*"],"exclude":["src/skip.ts"]}` + "\n",
		"pkg/package.json": `{"name":"pkg"}` + "\n",
		"pkg/README.md":    "# pkg\n",
		"pkg/names.txt":    "a\n",
		"pkg/legacy.js":    "module.exports = 1;\n",
		"pkg/src/a.ts": "import d from \"./data.json\";\n" +
			"export const a = d;\n",
		"pkg/src/skip.ts":           "export const skipped = 1;\n",
		"pkg/src/data.json":         `{"k":1}` + "\n",
		"pkg/src/deep/b.ts":         "export const b = 1;\n",
		"pkg/src/deep/fixture.snap": "snap\n",
		"pkg/assets/logo.svg":       "<svg/>\n",
		"pkg/tools/gen.ts":          "export const g = 1;\n",
		"pkg/compiled/index.ts":     "export const generated = 1;\n",
		"pkg/compiled/README.md":    "# generated\n",
		"pkg/sub/tsconfig.json":     includeTs,
		"pkg/sub/c.ts":              "export const c = 1;\n",
		"pkg/sub/notes.md":          "notes\n",
	}))

	compile := mustRule(t, g.results["pkg"], "ts_compile", "pkg")
	wantStrings(t, "ts_compile pkg srcs", compile.AttrStrings("srcs"), []string{
		"README.md", "assets/logo.svg", "names.txt", "package.json",
		"src/a.ts", "src/data.json", "src/deep/b.ts", "src/deep/fixture.snap"})
	sub := mustRule(t, g.results["pkg/sub"], "ts_compile", "sub")
	wantStrings(t, "ts_compile sub srcs", sub.AttrStrings("srcs"),
		[]string{"c.ts", "notes.md"})
	for _, rel := range []string{"pkg/compiled", "pkg/tools", "pkg/src"} {
		if res := g.results[rel]; len(res.Gen) != 0 {
			t.Errorf("%s generated %v, want nothing", rel, generatedNames(t, res))
		}
	}
}

// A program with no inputs is no package: its directory's files belong to the
// package above, and a ts_config an earlier run wrote there is withdrawn.
func TestGenerate_AProgramWithNoInputsIsNoPackage(t *testing.T) {
	g := generateAll(t, writeTree(t, map[string]string{
		"package.json":              rootManifest,
		"site/tsconfig.json":        `{"include":["script/**/*","src/**/*"]}` + "\n",
		"site/src/a.ts":             "export const a = 1;\n",
		"site/script/b.ts":          "export const b = 1;\n",
		"site/script/tsconfig.json": `{"include":["nothing/**/*"]}` + "\n",
		"site/script/BUILD.bazel": loadDefs + `"ts_compile", "ts_config")

ts_compile(
    name = "script",
    srcs = ["b.ts"],
    tsconfig = ":tsconfig",
)

ts_config(
    name = "tsconfig",
    src = "tsconfig.json",
)
`,
	}))

	script := g.results["site/script"]
	if len(script.Gen) != 0 {
		t.Errorf("site/script generated %v; TS18003 names no file",
			generatedNames(t, script))
	}
	for _, want := range [][2]string{
		{"ts_compile", "script"}, {"ts_config", "tsconfig"},
	} {
		if !withdraws(script, want[0], want[1]) {
			t.Errorf("site/script does not withdraw %s(%s); Empty = %v",
				want[0], want[1], kindsOf(script.Empty))
		}
	}
	site := mustRule(t, g.results["site"], "ts_compile", "site")
	wantStrings(t, "ts_compile site srcs", site.AttrStrings("srcs"),
		[]string{"script/b.ts", "script/tsconfig.json", "src/a.ts"})
}

// Target names are the directory's; the repository root's is root.
func TestGenerate_TargetNamesAreTheDirectorys(t *testing.T) {
	g := generateAll(t, writeTree(t, map[string]string{
		"package.json":      rootManifest,
		"tsconfig.json":     `{"include":["src/**/*"]}` + "\n",
		"src/a.ts":          "export const a = 1;\n",
		"src/a.test.ts":     "export const t = 1;\n",
		"a/b/tsconfig.json": includeTs,
		"a/b/x.ts":          "export const x = 1;\n",
		"a/b/x.test.ts":     "export const t = 1;\n",
	}))
	root := generatedNames(t, g.results[""])
	assertRule(t, root, "root", "ts_compile")
	assertRule(t, root, "root_test", "ts_test")
	assertRule(t, root, tsConfigTargetName, "ts_config")
	deep := generatedNames(t, g.results["a/b"])
	assertRule(t, deep, "b", "ts_compile")
	assertRule(t, deep, "b_test", "ts_test")
}

// A package whose every owned file is a declaration compiles nothing: no
// target, and -ts_verbose says so.
func TestGenerate_ADeclarationOnlyPackageWritesNoTarget(t *testing.T) {
	g := generateAll(t, writeTree(t, map[string]string{
		"package.json":        rootManifest,
		"types/tsconfig.json": `{"include":["*.d.ts"]}` + "\n",
		"types/globals.d.ts":  "declare const BUILD_ID: string;\n",
	}), verbose)
	got := generatedNames(t, g.results["types"])
	if len(got) != 1 || got[tsConfigTargetName] != "ts_config" {
		t.Errorf("types generated %v, want its ts_config alone", got)
	}
	if !strings.Contains(g.logged, "types/tsconfig.json") {
		t.Errorf("the target-less program was not named:\n%s", g.logged)
	}
}

// tsc drops x.mjs from a program that holds x.d.mts and reads the declaration
// in its place, so the JavaScript a declaration stands for is a src beside it.
func TestGenerate_ADeclarationsJavaScriptTwinIsASrc(t *testing.T) {
	g := generateAll(t, writeTree(t, map[string]string{
		"package.json":      rootManifest,
		"lib/tsconfig.json": includeAll,
		"lib/compile.d.mts": "export declare function compile(v: number): string;\n",
		"lib/compile.mjs":   "export function compile(v) {\n  return `${v}`;\n}\n",
		"lib/compile.test.ts": "import { compile } from \"./compile.mjs\";\n" +
			"export const s = compile(1);\n",
		"lib/index.ts":  "export { compile } from \"./compile.mjs\";\n",
		"lib/legacy.js": "module.exports = 1;\n",
	}))

	compile := mustRule(t, g.results["lib"], "ts_compile", "lib")
	wantStrings(t, "ts_compile srcs", compile.AttrStrings("srcs"),
		[]string{"compile.d.mts", "compile.mjs", "index.ts"})
	test := mustRule(t, g.results["lib"], "ts_test", "lib_test")
	wantStrings(t, "ts_test srcs", test.AttrStrings("srcs"),
		[]string{"compile.d.mts", "compile.test.ts"})
}

// ---- the worker shape -------------------------------------------------------

var workerTree = map[string]string{
	"package.json": rootManifest,
	"worker/tsconfig.json": `{"compilerOptions":{"lib":["es2022"],` +
		`"types":["./worker-configuration.d.ts"]},` +
		`"include":["src/**/*","worker-configuration.d.ts"]}` + "\n",
	"worker/worker-configuration.d.ts": "interface Env {\n\tKV: string;\n}\n",
	"worker/wrangler.jsonc":            `{"name":"w"}` + "\n",
	"worker/src/index.ts": "export const handler = (env: Env) => " +
		"env.KV;\n",
	"worker/test/tsconfig.json": `{"extends":"../tsconfig.json",` +
		`"compilerOptions":{"types":["../worker-configuration.d.ts"]},` +
		`"include":["**/*.ts"]}` + "\n",
	"worker/test/env.d.ts": "declare const TEST_ENV: string;\n",
	"worker/test/index.spec.ts": "import { handler } from \"../src/index\";\n" +
		"export const t = handler;\nexport const e = TEST_ENV;\n",
}

// The test program owns env.d.ts and index.spec.ts: one ts_test, no ts_compile
// over the declaration alone, deps from the listing and no types attribute.
func TestGenerate_WorkerTestPackageWritesTsTestOnly(t *testing.T) {
	root, _ := converge(t, workerTree)

	rules := loadRules(t, root, "worker/test")
	if r := ruleNamed(rules, "ts_compile", "test"); r != nil {
		t.Errorf("worker/test writes a ts_compile over env.d.ts alone:\n%s",
			buildFileText(t, root, "worker/test"))
	}
	test := onDiskRule(t, root, "worker/test", "ts_test", "test_test")
	wantStrings(t, "ts_test srcs", test.AttrStrings("srcs"),
		[]string{"env.d.ts", "index.spec.ts"})
	wantStrings(t, "ts_test deps", test.AttrStrings("deps"),
		[]string{"//worker"})
	if test.Attr("types") != nil {
		t.Errorf("ts_test carries types = %v; the tsconfig owns it",
			test.Attr("types"))
	}
	wantStrings(t, "worker/test ts_config deps",
		tsConfigDeps(t, root, "worker/test"), []string{"//worker:tsconfig"})

	compile := onDiskRule(t, root, "worker", "ts_compile", "worker")
	wantStrings(t, "ts_compile worker srcs", compile.AttrStrings("srcs"),
		[]string{"src/index.ts", "worker-configuration.d.ts", "wrangler.jsonc"})
	assertNoDanglingLabels(t, root)
	if crossing := crossesPackageBoundary(t, root); len(crossing) > 0 {
		t.Errorf("srcs cross a package boundary:\n%s",
			strings.Join(crossing, "\n"))
	}
}

// A ts_codegen in the package's BUILD file is a dep of every target there.
func TestGenerate_ACodegenInThePackageIsEveryTargetsDep(t *testing.T) {
	root, _ := converge(t, map[string]string{
		"package.json": rootManifest,
		"BUILD.bazel": "filegroup(\n    name = \"gen\",\n" +
			"    srcs = [\"gen.sh\"],\n)\n",
		"gen.sh": "#!/bin/sh\n",
		"worker/BUILD.bazel": loadDefs + `"ts_codegen")

ts_codegen(
    name = "worker_types",
    srcs = ["wrangler.jsonc"],
    outs = ["worker-configuration.d.ts"],
    generator = "//:gen",
)
`,
		"worker/wrangler.jsonc": `{"name":"w"}` + "\n",
		"worker/tsconfig.json": `{"compilerOptions":{"lib":["es2022"],` +
			`"types":["./worker-configuration.d.ts"]},` +
			`"include":["src/**/*"]}` + "\n",
		"worker/src/index.ts": "export const handler = (env: Env) => " +
			"env.KV;\n",
		"worker/src/index.test.ts": "export const t = 1;\n",
	})
	compile := onDiskRule(t, root, "worker", "ts_compile", "worker")
	wantStrings(t, "ts_compile worker deps", compile.AttrStrings("deps"),
		[]string{":worker_types"})
	test := onDiskRule(t, root, "worker", "ts_test", "worker_test")
	wantStrings(t, "ts_test worker_test deps", test.AttrStrings("deps"),
		[]string{":worker", ":worker_types"})
	assertNoDanglingLabels(t, root)
}

// The ts_compile takes the directory's name; a hand-written rule of another
// kind holding it keeps the merger from writing the compile; the run says so.
func TestGenerate_AGeneratedNameAHandWrittenRuleHoldsIsSaid(t *testing.T) {
	g := generateAll(t, writeTree(t, map[string]string{
		"package.json": rootManifest,
		"worker/BUILD.bazel": loadDefs + `"ts_codegen")

ts_codegen(
    name = "worker",
    srcs = ["wrangler.jsonc"],
    outs = ["worker-configuration.d.ts"],
    generator = "//:gen",
)
`,
		"worker/wrangler.jsonc": `{"name":"w"}` + "\n",
		"worker/tsconfig.json": `{"compilerOptions":{"lib":["es2022"],` +
			`"types":["./worker-configuration.d.ts"]},` +
			`"include":["src/**/*"]}` + "\n",
		"worker/src/index.ts": "export const handler = (env: Env) => " +
			"env.KV;\n",
	}))
	mustRule(t, g.results["worker"], "ts_compile", "worker")
	for _, want := range []string{"ts_codegen(worker)", "ts_compile"} {
		if !strings.Contains(g.logged, want) {
			t.Errorf("the taken name was not said with %q:\n%s", want, g.logged)
		}
	}
}

// A BUILD file an earlier run left inside an out_dir is emptied and named.
func TestGenerate_APackageLeftUnderAnOutDirIsEmptied(t *testing.T) {
	g := generateAll(t, writeTree(t, map[string]string{
		"package.json": rootManifest,
		"web/BUILD.bazel": loadDefs + `"ts_codegen")

ts_codegen(
    name = "messages",
    srcs = ["names.txt"],
    generator = "//:gen",
    out_dir = "compiled",
)
`,
		"web/names.txt":     "hello\n",
		"web/tsconfig.json": includeTs,
		"web/app.ts":        "export const x = 1;\n",
		"web/compiled/BUILD.bazel": loadDefs + `"ts_compile")

ts_compile(
    name = "compiled",
    srcs = ["index.ts"],
)
`,
		"web/compiled/index.ts": "export const generated = 1;\n",
	}))
	res := g.results["web/compiled"]
	if len(res.Gen) != 0 || !withdraws(res, "ts_compile", "compiled") {
		t.Errorf("web/compiled: generated %v, Empty %v; want the withdrawal",
			generatedNames(t, res), kindsOf(res.Empty))
	}
	if !strings.Contains(g.logged, "web/compiled") {
		t.Errorf("web/compiled was not named as a package in an out_dir:\n%s",
			g.logged)
	}
	web := mustRule(t, g.results["web"], "ts_compile", "web")
	if srcs := web.AttrStrings("srcs"); hasSrc(srcs, "compiled/index.ts") {
		t.Errorf("web's srcs %v hold a file under the out_dir", srcs)
	}
}

// ---- ts_config --------------------------------------------------------------

// A ts_config is written for every package and for a tsconfig.json a program's
// chain extends; one nothing extends and no program lists gets nothing.
func TestGenerate_TsConfigForTheRootAProgramExtends(t *testing.T) {
	g := generateAll(t, writeTree(t, map[string]string{
		"package.json":  rootManifest,
		"tsconfig.json": `{"compilerOptions":{"strict":true}}` + "\n",
		"scripts/tsconfig.json": `{"extends":"../tsconfig.json",` +
			`"include":["*.ts"]}` + "\n",
		"scripts/run.ts":       "export const run = 1;\n",
		"unused/tsconfig.json": `{"compilerOptions":{"strict":true}}` + "\n",
		"unused/README.md":     "nothing here\n",
		"unused/BUILD.bazel": loadDefs + `"ts_config")

ts_config(
    name = "tsconfig",
    src = "tsconfig.json",
)
`,
	}))

	rootRes := g.results[""]
	cfg := mustRule(t, rootRes, "ts_config", tsConfigTargetName)
	if got := cfg.AttrString("src"); got != "tsconfig.json" {
		t.Errorf("root ts_config src = %q, want tsconfig.json", got)
	}
	if cfg.Attr("deps") != nil {
		t.Errorf("root ts_config deps = %v, want none", cfg.Attr("deps"))
	}
	if r := generatedRule(rootRes, "root"); r != nil {
		t.Errorf("the root writes a %s: its tsconfig.json is no program", r.Kind())
	}

	scripts := mustRule(t, g.results["scripts"], "ts_config", tsConfigTargetName)
	wantStrings(t, "scripts ts_config deps", scripts.AttrStrings("deps"),
		[]string{"//:tsconfig"})

	unused := g.results["unused"]
	if len(unused.Gen) != 0 {
		t.Errorf("unused generated %v; nothing extends its tsconfig.json",
			generatedNames(t, unused))
	}
	if !withdraws(unused, "ts_config", tsConfigTargetName) {
		t.Errorf("unused keeps its stale ts_config; Empty = %v",
			kindsOf(unused.Empty))
	}
}

// Every base of an extends array is a dep; a base that is no tsconfig.json has
// no ts_config to name, so the run says so and writes nothing for it.
func TestGenerate_TsConfigDepsAreTheChainsBases(t *testing.T) {
	g := generateAll(t, writeTree(t, map[string]string{
		"package.json":           rootManifest,
		"base/tsconfig.json":     `{"compilerOptions":{"lib":["es2022"]}}` + "\n",
		"base/README.md":         "a base\n",
		"strict/tsconfig.json":   `{"compilerOptions":{"strict":true}}` + "\n",
		"strict/README.md":       "a base\n",
		"app/tsconfig.base.json": `{"compilerOptions":{"noEmit":true}}` + "\n",
		"app/tsconfig.json": `{"extends":["../base/tsconfig.json",` +
			`"../strict/tsconfig.json","./tsconfig.base.json"],` +
			`"include":["*.ts"]}` + "\n",
		"app/a.ts": "export const a = 1;\n",
	}))
	cfg := mustRule(t, g.results["app"], "ts_config", tsConfigTargetName)
	wantStrings(t, "app ts_config deps", cfg.AttrStrings("deps"),
		[]string{"//base:tsconfig", "//strict:tsconfig"})
	for _, base := range []string{"base", "strict"} {
		if generatedRule(g.results[base], tsConfigTargetName) == nil {
			t.Errorf("%s writes no ts_config; app's chain extends it", base)
		}
	}
	if !strings.Contains(g.logged, "tsconfig.base.json") {
		t.Errorf("the base with no ts_config was not named:\n%s", g.logged)
	}
}

// ---- deps -------------------------------------------------------------------

const zodLock = `lockfileVersion: '9.0'

importers:

  .:
    dependencies:
      zod:
        specifier: ^3.24.2
        version: 3.24.2

packages:

  zod@3.24.2:
    resolution: {integrity: sha512-aaa}

snapshots:

  zod@3.24.2: {}
`

// The listing is the one source of deps: a specifier the lexer would read but
// tsgo did not resolve -- the package is not installed -- yields no label.
func TestGenerate_DepsComeFromTheListingAlone(t *testing.T) {
	root, _ := converge(t, map[string]string{
		"package.json": `{"name":"w","dependencies":{"zod":"3.24.2"}}` +
			"\n",
		pnpmLockfileName:             zodLock,
		"node_modules/.modules.yaml": "layoutVersion: 5\n",
		"pkg/tsconfig.json":          includeTs,
		"pkg/a.ts": "import { z } from \"zod\";\n" +
			"export const s = z;\n",
	})
	r := onDiskRule(t, root, "pkg", "ts_compile", "pkg")
	if deps := r.AttrStrings("deps"); len(deps) != 0 {
		t.Errorf("deps = %v, want none: no listing names zod", deps)
	}
}

// ---- the vitest config ------------------------------------------------------

// The config beside the tests is the test's, and what it imports is a dep.
func TestGenerate_VitestConfigBesideTheTestsIsTheTests(t *testing.T) {
	root, _ := converge(t, map[string]string{
		"package.json":            rootManifest,
		"shared/tsconfig.json":    includeTs,
		"shared/vitest.shared.ts": "export const shared = true;\n",
		"pkg/tsconfig.json":       includeSrc,
		"pkg/src/a.ts":            "export const a = 1;\n",
		"pkg/src/a.test.ts": "import { a } from \"./a\";\n" +
			"export const t = a;\n",
		"pkg/vitest.config.mts": "import { shared } from " +
			"\"../shared/vitest.shared\";\n" +
			"export default { test: { globals: shared } };\n",
	})
	test := onDiskRule(t, root, "pkg", "ts_test", "pkg_test")
	if got := test.AttrString("config"); got != "vitest.config.mts" {
		t.Errorf("config = %q, want vitest.config.mts", got)
	}
	wantStrings(t, "ts_test deps", test.AttrStrings("deps"),
		[]string{":pkg", "//shared"})
	assertNoDanglingLabels(t, root)
}

// With no vitest.config.*, plain vitest reads vite.config.*: the test's config.
func TestGenerate_ViteConfigIsTheTestsWhenNoVitestConfig(t *testing.T) {
	g := generateAll(t, writeTree(t, map[string]string{
		"package.json":       rootManifest,
		"pkg/tsconfig.json":  includeSrc,
		"pkg/src/a.test.ts":  "export const t = 1;\n",
		"pkg/vite.config.ts": "export default { define: { __A__: \"1\" } };\n",
	}))
	test := mustRule(t, g.results["pkg"], "ts_test", "pkg_test")
	if got := test.AttrString("config"); got != "vite.config.ts" {
		t.Errorf("config = %q, want vite.config.ts", got)
	}
}

// Beside a vite.config.*, the vitest.config.* is the one vitest reads.
func TestGenerate_VitestConfigWinsOverViteConfig(t *testing.T) {
	g := generateAll(t, writeTree(t, map[string]string{
		"package.json":         rootManifest,
		"pkg/tsconfig.json":    includeSrc,
		"pkg/src/a.test.ts":    "export const t = 1;\n",
		"pkg/vite.config.ts":   "export default {};\n",
		"pkg/vitest.config.ts": "export default { test: { globals: true } };\n",
	}))
	test := mustRule(t, g.results["pkg"], "ts_test", "pkg_test")
	if got := test.AttrString("config"); got != "vitest.config.ts" {
		t.Errorf("config = %q, want vitest.config.ts", got)
	}
}

// Plain vitest reads the config beside the nearest package.json; a test a
// package down names it by label, and that package writes the filegroup.
func TestGenerate_VitestConfigAtThePackageRootReachesATestBelow(t *testing.T) {
	g := generateAll(t, writeTree(t, map[string]string{
		"package.json":              rootManifest,
		"worker/package.json":       `{"name":"worker"}` + "\n",
		"worker/vitest.config.mts":  "export default { test: { globals: true } };\n",
		"worker/tsconfig.json":      `{"include":["src/**/*"]}` + "\n",
		"worker/src/index.ts":       "export const w = 1;\n",
		"worker/test/tsconfig.json": includeTs,
		"worker/test/index.test.ts": "export const t = 1;\n",
	}))
	test := mustRule(t, g.results["worker/test"], "ts_test", "test_test")
	if got := test.AttrString("config"); got != "//worker:vitest_config" {
		t.Errorf("config = %q, want //worker:vitest_config", got)
	}
	fg := mustRule(t, g.results["worker"], "filegroup", vitestConfigTargetName)
	wantStrings(t, "filegroup srcs", fg.AttrStrings("srcs"),
		[]string{"vitest.config.mts"})
	wantStrings(t, "filegroup visibility", fg.AttrStrings("visibility"),
		[]string{"//visibility:public"})
}

// A config beside a package.json in a directory that is no package is a label
// nothing writes: the test gets no config and the run says which file.
func TestGenerate_VitestConfigInANonPackageIsSaid(t *testing.T) {
	g := generateAll(t, writeTree(t, map[string]string{
		"package.json":           rootManifest,
		"app/package.json":       `{"name":"app"}` + "\n",
		"app/vitest.config.mts":  "export default { test: { globals: true } };\n",
		"app/test/tsconfig.json": includeTs,
		"app/test/a.test.ts":     "export const t = 1;\n",
	}))
	test := mustRule(t, g.results["app/test"], "ts_test", "test_test")
	if test.Attr("config") != nil {
		t.Errorf("config = %q, want unset: app is no package",
			test.AttrString("config"))
	}
	if !strings.Contains(g.logged, "app/vitest.config.mts") {
		t.Errorf("the unreachable config was not named:\n%s", g.logged)
	}
	if generatedRule(g.results["app"], vitestConfigTargetName) != nil {
		t.Errorf("app writes a filegroup in a directory that is no package")
	}
}

// The repository root always has a BUILD file, so its config is its label.
func TestGenerate_VitestConfigAtTheRepoRootIsTheRootLabel(t *testing.T) {
	g := generateAll(t, writeTree(t, map[string]string{
		"package.json":           rootManifest,
		"vitest.config.ts":       "export default { test: { globals: true } };\n",
		"pkg/test/tsconfig.json": includeTs,
		"pkg/test/index.test.ts": "export const t = 1;\n",
	}))
	test := mustRule(t, g.results["pkg/test"], "ts_test", "test_test")
	if got := test.AttrString("config"); got != "//:vitest_config" {
		t.Errorf("config = %q, want //:vitest_config", got)
	}
	mustRule(t, g.results[""], "filegroup", vitestConfigTargetName)
}

// The config went; the filegroup goes with it.
func TestGenerate_WithdrawsTheVitestConfigFilegroupWithTheFile(t *testing.T) {
	g := generateAll(t, writeTree(t, map[string]string{
		"package.json":      rootManifest,
		"pkg/package.json":  `{"name":"pkg"}` + "\n",
		"pkg/tsconfig.json": includeTs,
		"pkg/a.ts":          "export const a = 1;\n",
		"pkg/BUILD.bazel": `filegroup(
    name = "vitest_config",
    srcs = ["vitest.config.ts"],
    visibility = ["//visibility:public"],
)
`,
	}))
	if !withdraws(g.results["pkg"], "filegroup", vitestConfigTargetName) {
		t.Errorf("the filegroup outlived its file; Empty = %v",
			kindsOf(g.results["pkg"].Empty))
	}
}

// ---- the Workers pool -------------------------------------------------------

// A pool config as a worker writes it, naming its wrangler config through
// `wrangler.configPath`.
func poolConfig(configPath string) string {
	return "import { cloudflareTest } from " +
		"\"@cloudflare/vitest-pool-workers\";\n" +
		"export default { plugins: [cloudflareTest({ wrangler: { configPath: " +
		"\"" + configPath + "\" } })] };\n"
}

// The wrangler config a package-root vitest config names in a string literal
// is a label beside vitest_config, the file's name as configPath spells it.
func TestGenerate_WranglerConfigTheConfigNamesIsAFilegroup(t *testing.T) {
	g := generateAll(t, writeTree(t, map[string]string{
		"package.json":               rootManifest,
		"worker/package.json":        `{"name":"worker"}` + "\n",
		"worker/vitest.config.mts":   poolConfig("./wrangler.test.jsonc"),
		"worker/wrangler.test.jsonc": `{"main":"src/index.ts"}` + "\n",
		"worker/tsconfig.json":       includeSrc,
		"worker/src/index.ts":        "export const w = 1;\n",
		"worker/test/tsconfig.json":  includeTs,
		"worker/test/index.test.ts":  "export const t = 1;\n",
	}))
	fg := mustRule(t, g.results["worker"], "filegroup", "wrangler_config")
	wantStrings(t, "filegroup srcs", fg.AttrStrings("srcs"),
		[]string{"wrangler.test.jsonc"})
	wantStrings(t, "filegroup visibility", fg.AttrStrings("visibility"),
		[]string{"//visibility:public"})
	mustRule(t, g.results["worker"], "filegroup", vitestConfigTargetName)
}

// The pool reads a wrangler config through configPath alone, so a config naming
// none gets no filegroup, whatever sits beside it; a stale one is withdrawn.
func TestGenerate_AConfigNamingNoWranglerConfigWritesNoFilegroup(t *testing.T) {
	g := generateAll(t, writeTree(t, map[string]string{
		"package.json":        rootManifest,
		"worker/package.json": `{"name":"worker"}` + "\n",
		"worker/vitest.config.mts": "import { cloudflareTest } from " +
			"\"@cloudflare/vitest-pool-workers\";\n" +
			"export default { plugins: [cloudflareTest({ miniflare: {} })] };\n",
		"worker/wrangler.jsonc":     `{"main":"src/index.ts"}` + "\n",
		"worker/tsconfig.json":      includeSrc,
		"worker/src/index.ts":       "export const w = 1;\n",
		"worker/test/tsconfig.json": includeTs,
		"worker/test/index.test.ts": "export const t = 1;\n",
		"worker/BUILD.bazel": `filegroup(
    name = "wrangler_config",
    srcs = ["wrangler.jsonc"],
    visibility = ["//visibility:public"],
)
`,
	}))
	if !withdraws(g.results["worker"], "filegroup", "wrangler_config") {
		t.Errorf("a filegroup for a file no configPath names; Gen = %v, Empty = %v",
			generatedNames(t, g.results["worker"]),
			kindsOf(g.results["worker"].Empty))
	}
}

// The file the literal names went: the filegroup goes with it, and the run
// says which file the config still names.
func TestGenerate_WithdrawsTheWranglerConfigFilegroupWithTheFile(t *testing.T) {
	g := generateAll(t, writeTree(t, map[string]string{
		"package.json":             rootManifest,
		"worker/package.json":      `{"name":"worker"}` + "\n",
		"worker/vitest.config.mts": poolConfig("./wrangler.jsonc"),
		"worker/tsconfig.json":     includeSrc,
		"worker/src/index.ts":      "export const w = 1;\n",
		"worker/BUILD.bazel": `filegroup(
    name = "wrangler_config",
    srcs = ["wrangler.jsonc"],
    visibility = ["//visibility:public"],
)
`,
	}))
	if !withdraws(g.results["worker"], "filegroup", "wrangler_config") {
		t.Errorf("the filegroup outlived its file; Empty = %v",
			kindsOf(g.results["worker"].Empty))
	}
	if !strings.Contains(g.logged, "worker/wrangler.jsonc") {
		t.Errorf("the missing file was not named:\n%s", g.logged)
	}
}

// The pooled shape end to end: the config's edge names the pool, so the test
// names the filegroup, runs istanbul coverage and carries its package.
func TestGenerate_APooledTestNamesTheWranglerConfigAndIstanbul(t *testing.T) {
	root, _ := converge(t, map[string]string{
		"package.json":               rootManifest,
		pnpmLockfileName:             poolLock,
		"node_modules/.modules.yaml": "layoutVersion: 5\n",
		"node_modules/@cloudflare/vitest-pool-workers/package.json": `{"name":` +
			`"@cloudflare/vitest-pool-workers","version":"0.18.4",` +
			`"types":"index.d.ts"}` + "\n",
		"node_modules/@cloudflare/vitest-pool-workers/index.d.ts": "export " +
			"declare function cloudflareTest(o: unknown): unknown;\n",
		"worker/package.json": `{"name":"worker","devDependencies":` +
			`{"@cloudflare/vitest-pool-workers":"0.18.4"}}` + "\n",
		"worker/vitest.config.mts":  poolConfig("./wrangler.jsonc"),
		"worker/wrangler.jsonc":     `{"main":"src/index.ts"}` + "\n",
		"worker/tsconfig.json":      includeSrc,
		"worker/src/index.ts":       "export const w = 1;\n",
		"worker/test/tsconfig.json": includeTs,
		"worker/test/index.test.ts": "import { w } from \"../src/index\";\n" +
			"export const t = w;\n",
	})
	test := onDiskRule(t, root, "worker/test", "ts_test", "test_test")
	if got := test.AttrString("wrangler_config"); got != "//worker:"+
		wranglerConfigTargetName {
		t.Errorf("wrangler_config = %q, want //worker:wrangler_config", got)
	}
	if got := test.AttrString("coverage_provider"); got != "istanbul" {
		t.Errorf("coverage_provider = %q, want istanbul", got)
	}
	wantStrings(t, "ts_test deps", test.AttrStrings("deps"), []string{
		"//worker", "@npm//:vitest_coverage-istanbul",
		"@npm//worker:cloudflare_vitest-pool-workers"})
	fg := onDiskRule(t, root, "worker", "filegroup", "wrangler_config")
	wantStrings(t, "filegroup srcs", fg.AttrStrings("srcs"),
		[]string{"wrangler.jsonc"})
	assertNoDanglingLabels(t, root)
}

// worker declares the pool; istanbul is in the lockfile and in no manifest,
// so its label is the root's.
const poolLock = `lockfileVersion: '9.0'

importers:

  .: {}

  worker:
    devDependencies:
      '@cloudflare/vitest-pool-workers':
        specifier: 0.18.4
        version: 0.18.4

packages:

  '@cloudflare/vitest-pool-workers@0.18.4':
    resolution: {integrity: sha512-aaa}

  '@vitest/coverage-istanbul@4.1.11':
    resolution: {integrity: sha512-bbb}

snapshots:

  '@cloudflare/vitest-pool-workers@0.18.4': {}

  '@vitest/coverage-istanbul@4.1.11': {}
`

// ---- the .d.mts / .d.cts flavours ------------------------------------------

// A declaration is a declaration whatever its extension: it rides in every
// target, and the JavaScript it stands for is a src beside it, listed or not.
func TestGenerate_DeclarationFlavoursRideInEveryTarget(t *testing.T) {
	g := generateAll(t, writeTree(t, map[string]string{
		"package.json": rootManifest,
		"pkg/tsconfig.json": `{"compilerOptions":{"lib":["es2022"]},` +
			`"include":["*.ts","*.mts"]}` + "\n",
		"pkg/entry.ts":    "export const e = 1;\n",
		"pkg/compile.mjs": "export function compile(v) { return `${v}`; }\n",
		"pkg/compile.d.mts": "export declare function compile(v: number): " +
			"string;\n",
		"pkg/globals.d.mts": "declare const BUILD_ID: string;\n",
		"pkg/compile.test.ts": "import { compile } from \"./compile.mjs\";\n" +
			"export const t = compile(1);\n",
	}))
	res := g.results["pkg"]
	compile := mustRule(t, res, "ts_compile", "pkg")
	wantStrings(t, "ts_compile srcs", compile.AttrStrings("srcs"),
		[]string{"compile.d.mts", "compile.mjs", "entry.ts", "globals.d.mts"})
	test := mustRule(t, res, "ts_test", "pkg_test")
	wantStrings(t, "ts_test srcs", test.AttrStrings("srcs"),
		[]string{"compile.d.mts", "compile.test.ts", "globals.d.mts"})
}
