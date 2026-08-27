package typescript

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/bazelbuild/bazel-gazelle/config"
	"github.com/bazelbuild/bazel-gazelle/language"
	"github.com/bazelbuild/bazel-gazelle/rule"
)

// runGenerateNext generates over a workspace root whose files may sit in
// subdirectories, which is the only way to exercise a layout: which route
// directories exist is what the srcs glob and the ts_compile suppression key on.
func runGenerateNext(t *testing.T, files map[string]string) language.GenerateResult {
	t.Helper()
	return runGenerateNextWithPackages(t, files, nil)
}

// runGenerateNextWithPackages is runGenerateNext with a lockfile: npmPackages
// nil means "no lockfile loaded", which filterNpmDeps passes through whole.
func runGenerateNextWithPackages(t *testing.T, files map[string]string, pkgs []string) language.GenerateResult {
	t.Helper()

	repoRoot := t.TempDir()
	var names []string
	for name, content := range files {
		path := filepath.Join(repoRoot, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(name, "/") {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	c := &config.Config{RepoRoot: repoRoot, Exts: make(map[string]interface{})}
	configureTsConfig(c, "", nil)
	if pkgs != nil {
		tc := getConfig(c)
		tc.npmPackages = make(map[string]string, len(pkgs))
		for _, pkg := range pkgs {
			tc.npmPackages[pkg] = npmLabel(pkg)
		}
	}

	return generateRules(language.GenerateArgs{
		Config:       c,
		Dir:          repoRoot,
		Rel:          "",
		RegularFiles: names,
	})
}

// runGenerateNextWithBuild is runGenerateNext over a workspace that already has
// a BUILD file, which is the only way to see the second run.
func runGenerateNextWithBuild(t *testing.T, files map[string]string, build string) (language.GenerateResult, *rule.File) {
	t.Helper()

	repoRoot := t.TempDir()
	var names []string
	for name, content := range files {
		path := filepath.Join(repoRoot, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "/") {
			if err := os.MkdirAll(path, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(name, "/") {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	f, err := rule.LoadData(filepath.Join(repoRoot, "BUILD.bazel"), "", []byte(build))
	if err != nil {
		t.Fatal(err)
	}
	c := &config.Config{RepoRoot: repoRoot, Exts: make(map[string]interface{})}
	configureTsConfig(c, "", nil)

	return generateRules(language.GenerateArgs{
		Config:       c,
		Dir:          repoRoot,
		Rel:          "",
		File:         f,
		RegularFiles: names,
	}), f
}

// nextAppLayout is the App Router layout: routes at the workspace root.
var nextAppLayout = map[string]string{
	"package.json":      nextPackageJSON,
	"tsconfig.json":     `{"compilerOptions":{"strict":true}}`,
	"next.config.mjs":   "export default {};\n",
	"app/page.tsx":      "export default function P() { return null; }\n",
	"app/globals.css":   ".a{color:red}\n",
	"public/logo.png":   "png",
	"middleware.ts":     "export function middleware() {}\n",
	"lib/greeting.ts":   "export const hi = 1;\n",
	"lib/greeting.d.ts": "export const hi: number;\n",
}

func nextRule(t *testing.T, res language.GenerateResult, kind string) *rule.Rule {
	t.Helper()
	for _, r := range res.Gen {
		if r.Kind() == kind {
			return r
		}
	}
	t.Fatalf("no %s rule generated", kind)
	return nil
}

// renderRule is what the BUILD file would actually say. Asserting on attr
// values instead would have missed the bug this pins: a glob emitted as a
// string list comes out as srcs = ["glob([\"app/**\"])"], one literal filename
// that matches nothing, and the generated target cannot build.
func renderRule(t *testing.T, r *rule.Rule) string {
	t.Helper()
	f := rule.EmptyFile("BUILD.bazel", "")
	r.Insert(f)
	f.Sync()
	return string(f.Format())
}

func TestNextBundle_GeneratesBuildAndDevServer(t *testing.T) {
	res := runGenerateNext(t, nextAppLayout)
	byName := generatedNames(t, res)

	assertRule(t, byName, "node_modules", "node_modules")
	assertRule(t, byName, "app", "next_build")
	assertRule(t, byName, "dev", "next_dev_server")
	for name, kind := range byName {
		if kind == "ts_bundle" || kind == "vite_bundler" {
			t.Errorf("emitted %s(name = %q); Next.js is not on the ts_bundle path", kind, name)
		}
	}
}

func TestNextBundle_SrcsIsARealGlobCall(t *testing.T) {
	res := runGenerateNext(t, nextAppLayout)
	text := renderRule(t, nextRule(t, res, "next_build"))

	if !strings.Contains(text, `srcs = glob(`) {
		t.Errorf("srcs is not a glob() call, so it names files that do not exist:\n%s", text)
	}
	if strings.Contains(text, `"glob(`) {
		t.Errorf("srcs came out as a quoted string, which Bazel reads as a filename:\n%s", text)
	}
}

// srcs is the declaration: a file the glob does not cover does not resolve
// inside the staging directory, and the failure is a webpack "can't resolve".
func TestNextBundle_SrcsCoversEveryDirectoryNextJSReads(t *testing.T) {
	res := runGenerateNext(t, nextAppLayout)
	patterns := globPatterns(t, nextRule(t, res, "next_build"))

	for _, want := range []string{"app/**", "public/**", "middleware.ts"} {
		if !slices.Contains(patterns, want) {
			t.Errorf("srcs patterns %v do not cover %q", patterns, want)
		}
	}
	// A pattern matching nothing fails the whole glob: allow_empty defaults to
	// False, so a directory the project does not have must not be named.
	for _, absent := range []string{"pages/**", "src/**"} {
		if slices.Contains(patterns, absent) {
			t.Errorf("srcs names %q, which this layout does not have; the glob would fail", absent)
		}
	}
}

func TestNextBundle_SrcsFollowsTheSrcLayout(t *testing.T) {
	res := runGenerateNext(t, map[string]string{
		"package.json":      nextPackageJSON,
		"next.config.mjs":   "export default {};\n",
		"src/app/page.tsx":  "export default function P() { return null; }\n",
		"src/pages/x.tsx":   "export default function X() { return null; }\n",
		"src/middleware.ts": "export function middleware() {}\n",
	})
	patterns := globPatterns(t, nextRule(t, res, "next_build"))

	if !slices.Contains(patterns, "src/**") {
		t.Errorf("srcs patterns %v do not cover the src/ layout", patterns)
	}
	// src/middleware.ts is already inside src/**; naming it again at the root
	// would be a pattern matching nothing.
	if slices.Contains(patterns, "middleware.ts") {
		t.Errorf("srcs names a root middleware.ts this layout does not have: %v", patterns)
	}
}

// Next.js takes the first of CONFIG_FILES that exists. Naming any other one
// generates a config the build then ignores.
func TestNextBundle_ConfigIsTheOneNextJSWouldLoad(t *testing.T) {
	for _, tt := range []struct {
		name    string
		present []string
		want    string
	}{
		{"mjs", []string{"next.config.mjs"}, "next.config.mjs"},
		{"ts", []string{"next.config.ts"}, "next.config.ts"},
		{"js beats mjs", []string{"next.config.mjs", "next.config.js"}, "next.config.js"},
		{"mjs beats ts", []string{"next.config.ts", "next.config.mjs"}, "next.config.mjs"},
		// A Next.js project without a config file is legal. Naming one anyway
		// declares an input that does not exist, and the action fails on it.
		{"none", nil, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			files := map[string]string{
				"package.json": nextPackageJSON,
				"app/page.tsx": "export default function P() { return null; }\n",
			}
			for _, name := range tt.present {
				files[name] = "export default {};\n"
			}
			res := runGenerateNext(t, files)
			if got := nextRule(t, res, "next_build").AttrString("config"); got != tt.want {
				t.Errorf("config = %q, want %q", got, tt.want)
			}
		})
	}
}

// Without the tsconfig attr Next.js type-checks against its own defaults, so
// the project's path aliases and strictness silently stop applying.
func TestNextBundle_TsconfigIsDeclaredWhenItExists(t *testing.T) {
	res := runGenerateNext(t, nextAppLayout)
	if got := nextRule(t, res, "next_build").AttrString("tsconfig"); got != "tsconfig.json" {
		t.Errorf("tsconfig = %q, want tsconfig.json", got)
	}

	without := map[string]string{}
	for name, content := range nextAppLayout {
		if name != "tsconfig.json" {
			without[name] = content
		}
	}
	res = runGenerateNext(t, without)
	if got := nextRule(t, res, "next_build").AttrString("tsconfig"); got != "" {
		t.Errorf("tsconfig = %q for a project that has none", got)
	}
}

func TestNextBundle_DevServerTakesTheSameNpmTree(t *testing.T) {
	res := runGenerateNext(t, nextAppLayout)
	dev := nextRule(t, res, "next_dev_server")

	if got := dev.Name(); got != "dev" {
		t.Errorf("next_dev_server name = %q, want \"dev\"", got)
	}
	if got := dev.AttrString("node_modules"); got != ":node_modules" {
		t.Errorf("next_dev_server node_modules = %q, want \":node_modules\"", got)
	}
}

// A generated rule whose kind is not in the load list produces a BUILD file
// that fails to load, naming an undefined symbol.
func TestNextBundle_LoadsNameEveryGeneratedKind(t *testing.T) {
	for _, kind := range []string{"next_build", "next_dev_server"} {
		if !slices.Contains(tsDefsSymbols, kind) {
			t.Errorf("%q is generated but not in the //ts:defs.bzl load list", kind)
		}
	}
	lang := &tsLang{}
	for _, kind := range []string{"next_build", "next_dev_server"} {
		if _, ok := lang.Kinds()[kind]; !ok {
			t.Errorf("%q is generated but has no KindInfo, so Gazelle cannot match or merge it", kind)
		}
	}
}

// The merger cannot merge a glob() call: it logs "could not merge expression"
// on every run and keeps whatever is there. Declaring srcs mergeable would buy
// nothing and cost a warning per invocation.
func TestNextBundle_SrcsIsNotMergeable(t *testing.T) {
	if (&tsLang{}).Kinds()["next_build"].MergeableAttrs["srcs"] {
		t.Error("next_build srcs is mergeable, but a glob() attr cannot be merged")
	}
}

// Next.js compiles its own route trees. A BUILD file inside one makes it a
// subpackage, and a glob does not descend into a subpackage -- so the staged
// tree would silently lose exactly the modules the routes import.
func TestNextBundle_RouteTreesGetNoTypeScriptTargets(t *testing.T) {
	tc := &tsConfig{detectedFramework: FrameworkNextJS}
	for _, owned := range []string{
		"app", "app/api/hello", "pages", "pages/api", "src", "src/app/client", "public",
	} {
		if !nextOwnsDir(owned, tc) {
			t.Errorf("%q is not owned by Next.js, so Gazelle would write a BUILD file in it", owned)
		}
	}
	for _, free := range []string{"", "lib", "packages/shared", "apps"} {
		if nextOwnsDir(free, tc) {
			t.Errorf("%q is owned by Next.js, so its shared TypeScript loses ts_compile and ts_test", free)
		}
	}
	if nextOwnsDir("app", &tsConfig{detectedFramework: FrameworkTanStack}) {
		t.Error("app/ is claimed for a framework that is not Next.js")
	}
}

func TestNextBundle_RouteTreeGeneratesNothing(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoRoot, "package.json"), []byte(nextPackageJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &config.Config{RepoRoot: repoRoot, Exts: make(map[string]interface{})}
	configureTsConfig(c, "", nil)
	configureTsConfig(c, "app", nil)

	res := generateRules(language.GenerateArgs{
		Config:       c,
		Dir:          filepath.Join(repoRoot, "app"),
		Rel:          "app",
		RegularFiles: []string{"page.tsx", "globals.css"},
	})
	for _, r := range res.Gen {
		t.Errorf("generated %s(name = %q) inside the App Router tree", r.Kind(), r.Name())
	}
}

// TestNextBundle_ExistingOwnedPackageIsNamed: a BUILD file inside an owned
// directory is emptied, and the emptied file still makes the directory a Bazel
// package the srcs glob will not descend into. The SvelteKit twin names the
// file; this path used to empty it and print nothing.
func TestNextBundle_ExistingOwnedPackageIsNamed(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoRoot, "package.json"), []byte(nextPackageJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(repoRoot, "src", "lib")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	build := `ts_compile(name = "lib", srcs = ["util.ts"])`
	f, err := rule.LoadData(filepath.Join(dir, "BUILD.bazel"), "src/lib", []byte(build))
	if err != nil {
		t.Fatal(err)
	}

	c := &config.Config{RepoRoot: repoRoot, Exts: make(map[string]interface{})}
	configureTsConfig(c, "", nil)
	configureTsConfig(c, "src/lib", f)

	var res language.GenerateResult
	logged := captureLog(t, func() {
		res = generateRules(language.GenerateArgs{
			Config:       c,
			Dir:          dir,
			Rel:          "src/lib",
			File:         f,
			RegularFiles: []string{"util.ts"},
		})
	})

	if len(res.Gen) != 0 {
		t.Errorf("generated %d targets inside an owned directory, want none", len(res.Gen))
	}
	for _, want := range []string{"BUILD.bazel", "src/lib", "Delete the file"} {
		if !strings.Contains(logged, want) {
			t.Errorf("the log does not mention %q, so the file is emptied in silence:\n%s", want, logged)
		}
	}
}

// Gazelle's second run re-emits every target it owns. Skipping the ones already
// in the BUILD file is what froze staging_srcs and the npm tree at whatever the
// first run saw: the merger only reconciles an attribute a candidate carries.
func TestNextBundle_SecondRunStillEmitsEveryTarget(t *testing.T) {
	build := `node_modules(
    name = "node_modules",
    deps = ["@npm//:next"],
)

next_build(
    name = "app",
    srcs = glob(["app/**"]),
    config = "next.config.mjs",
    node_modules = ":node_modules",
)

next_dev_server(
    name = "dev",
    node_modules = ":node_modules",
)
`
	res, _ := runGenerateNextWithBuild(t, map[string]string{
		"package.json": nextPackageJSON,
		"app/":         "",
	}, build)

	byName := generatedNames(t, res)
	assertRule(t, byName, "node_modules", "node_modules")
	assertRule(t, byName, "app", "next_build")
	assertRule(t, byName, "dev", "next_dev_server")
}

// The glob is written straight onto the rule in the BUILD file, because the
// merger cannot merge a call expression. A pattern for a directory added after
// the first run arrives that way or not at all.
func TestNextBundle_ExistingGlobGainsANewDirectory(t *testing.T) {
	build := `next_build(
    name = "app",
    srcs = glob(["app/**"]),
    node_modules = ":node_modules",
)
`
	_, f := runGenerateNextWithBuild(t, map[string]string{
		"package.json": nextPackageJSON,
		"app/":         "",
		"pages/":       "",
	}, build)

	if got := globPatterns(t, mustFileRuleNamed(t, f, "app")); !slices.Equal(got, []string{"app/**", "pages/**"}) {
		t.Errorf("srcs patterns = %v, want [app/** pages/**]; the Pages Router never reaches the build", got)
	}
}

// A srcs the user wrote by hand is not a glob Gazelle computed, so it is left
// alone and named instead of silently replaced.
func TestNextBundle_HandWrittenSrcsIsNamedNotReplaced(t *testing.T) {
	build := `next_build(
    name = "app",
    srcs = _APP_SRCS,
    node_modules = ":node_modules",
)
`
	var f *rule.File
	logged := captureLog(t, func() {
		_, f = runGenerateNextWithBuild(t, map[string]string{
			"package.json": nextPackageJSON,
			"app/":         "",
		}, build)
	})

	if got := renderRule(t, mustFileRuleNamed(t, f, "app")); !strings.Contains(got, "srcs = _APP_SRCS") {
		t.Errorf("the hand-written srcs was replaced:\n%s", got)
	}
	if !strings.Contains(logged, "not a glob()") {
		t.Errorf("nothing said the hand-written srcs was left alone:\n%s", logged)
	}
}

// A rule in the file carrying no srcs at all is not a srcs to leave alone --
// the merger copies the generated glob into an attribute the rule lacks.
func TestNextBundle_ExistingRuleWithoutSrcsGetsTheGlob(t *testing.T) {
	build := `next_build(
    name = "app",
    node_modules = ":node_modules",
)
`
	var res language.GenerateResult
	logged := captureLog(t, func() {
		res, _ = runGenerateNextWithBuild(t, map[string]string{
			"package.json": nextPackageJSON,
			"app/":         "",
		}, build)
	})

	if got := globPatterns(t, nextRule(t, res, "next_build")); !slices.Contains(got, "app/**") {
		t.Errorf("srcs patterns = %v, so the routes reach no build", got)
	}
	if strings.Contains(logged, "could not merge expression") {
		t.Errorf("an absent srcs was reported as an expression Gazelle gave up on:\n%s", logged)
	}
}

func mustFileRuleNamed(t *testing.T, f *rule.File, name string) *rule.Rule {
	t.Helper()
	for _, r := range f.Rules {
		if r.Name() == name {
			return r
		}
	}
	t.Fatalf("no rule named %q in %s", name, f.Path)
	return nil
}

func globPatterns(t *testing.T, r *rule.Rule) []string {
	t.Helper()
	glob, ok := rule.ParseGlobExpr(r.Attr("srcs"))
	if !ok {
		t.Fatalf("srcs is not a glob expression:\n%s", renderRule(t, r))
	}
	return glob.Patterns
}

// An empty glob() does not fail only its own target: the error is raised while
// the BUILD file loads, so node_modules and the dev server go undeclared with
// it. A Next.js dependency with no route directory yet gets no build target.
func TestNextBundle_NoRouteDirectoryMeansNoBuildTarget(t *testing.T) {
	var res language.GenerateResult
	logged := captureLog(t, func() {
		res = runGenerateNext(t, map[string]string{
			"package.json":    nextPackageJSON,
			"next.config.mjs": "export default {};\n",
		})
	})

	for _, r := range res.Gen {
		if r.Kind() == "next_build" || r.Kind() == "next_dev_server" {
			t.Errorf("generated %s(name = %q) with nothing for its srcs glob to match",
				r.Kind(), r.Name())
		}
	}
	if !strings.Contains(logged, "no next_build was generated") {
		t.Errorf("nothing logged says why there is no build target; log was %q", logged)
	}
}

// middleware.ts, instrumentation.ts and next.config.ts are Next.js's, not a
// TypeScript package's: a ts_compile over one type-checks it outside the
// Next.js program, where `next/server` does not resolve, and declares the same
// .js next_build's staging already covers.
func TestNextBundle_RootConventionFilesGetNoTsCompile(t *testing.T) {
	res := runGenerateNext(t, map[string]string{
		"package.json":       nextPackageJSON,
		"next.config.ts":     "export default {};\n",
		"middleware.ts":      "export function middleware() {}\n",
		"instrumentation.ts": "export function register() {}\n",
		"next-env.d.ts":      "/// <reference types=\"next\" />\n",
		"app/page.tsx":       "export default function P() { return null; }\n",
	})

	for _, r := range res.Gen {
		if r.Kind() != "ts_compile" && r.Kind() != "ts_test" {
			continue
		}
		t.Errorf("generated %s(name = %q, srcs = %v) over Next.js's own root files",
			r.Kind(), r.Name(), r.AttrStrings("srcs"))
	}
}

// A route importing "../lib/greeting" resolves only if lib/ reaches the
// staging tree, and the srcs glob deliberately covers only what Next.js owns.
// Without staging_srcs the very first build after Gazelle is a webpack
// "Module not found".
func TestNextBundle_StagesTheTypeScriptOutsideTheRouteTrees(t *testing.T) {
	res := runGenerateNext(t, nextAppLayout)
	got := nextRule(t, res, "next_build").AttrStrings("staging_srcs")

	if !slices.Contains(got, "//lib") {
		t.Errorf("staging_srcs = %v, which does not stage lib/; the routes' imports of it do not resolve", got)
	}
	for _, owned := range []string{"//app", "//public"} {
		if slices.Contains(got, owned) {
			t.Errorf("staging_srcs = %v names %q, which Next.js compiles from srcs and has no ts_compile", got, owned)
		}
	}
}

func TestNextBundle_StagesNestedPackagesEachOnItsOwn(t *testing.T) {
	res := runGenerateNext(t, map[string]string{
		"package.json":                 nextPackageJSON,
		"next.config.mjs":              "export default {};\n",
		"app/page.tsx":                 "export default function P() { return null; }\n",
		"packages/shared/src/index.ts": "export const greet = () => \"hi\";\n",
		"packages/ui/button.tsx":       "export const B = () => null;\n",
		"lib/helpers.test.ts":          "export const t = 1;\n",
	})
	got := nextRule(t, res, "next_build").AttrStrings("staging_srcs")

	// One label per package: //packages/shared holds no sources of its own, so
	// there is no ts_compile there to name.
	want := []string{"//packages/shared/src", "//packages/ui"}
	if !slices.Equal(got, want) {
		t.Errorf("staging_srcs = %v, want %v", got, want)
	}
}

// TestNextBundle_StagesTheRootPackageAndItsNonTypeScriptSiblings: the srcs glob
// covers only the directories Next.js owns, so everything a route imports from
// outside them is a staging_srcs label or nothing. Two shapes were missing: a
// module at the workspace root, whose target is the root package's own, and a
// directory holding no TypeScript at all -- pages/_app.tsx importing
// ../styles/globals.css is create-next-app's default.
func TestNextBundle_StagesTheRootPackageAndItsNonTypeScriptSiblings(t *testing.T) {
	res := runGenerateNext(t, map[string]string{
		"package.json":       nextPackageJSON,
		"app/page.tsx":       "",
		"version.ts":         "export const version = \"1\";\n",
		"lib/greeting.ts":    "export const hi = 1;\n",
		"styles/globals.css": ".a{color:red}\n",
		"data/config.json":   "{}\n",
		"img/logo.png":       "png",
	})

	staging := nextRule(t, res, "next_build").AttrStrings("staging_srcs")
	for _, want := range []string{
		":root", "//lib", "//styles:globals_css", "//data:config_json", "//img:logo_png",
	} {
		if !slices.Contains(staging, want) {
			t.Errorf("staging_srcs = %v, missing %q", staging, want)
		}
	}
}

// TestNextBundle_StagingLabelFollowsTheDirectorysOwnDirectives:
// ts_target_name and ts_ignore are directory-scoped, so the root generator has
// to read them where generation there reads them. Guessing the directory
// basename names a target no rule declares, and Bazel then fails to analyse
// every target in the workspace, not just the bundle.
func TestNextBundle_StagingLabelFollowsTheDirectorysOwnDirectives(t *testing.T) {
	res := runGenerateNext(t, map[string]string{
		"package.json":       nextPackageJSON,
		"app/page.tsx":       "",
		"lib/BUILD.bazel":    "# gazelle:ts_target_name shared_lib\n",
		"lib/greeting.ts":    "export const hi = 1;\n",
		"hidden/BUILD.bazel": "# gazelle:ts_ignore\n",
		"hidden/thing.ts":    "export const thing = 1;\n",
	})

	staging := nextRule(t, res, "next_build").AttrStrings("staging_srcs")
	if !slices.Contains(staging, "//lib:shared_lib") {
		t.Errorf("staging_srcs = %v, missing \"//lib:shared_lib\"", staging)
	}
	for _, absent := range []string{"//lib", "//hidden"} {
		if slices.Contains(staging, absent) {
			t.Errorf("staging_srcs = %v names %q, which no rule declares", staging, absent)
		}
	}
}

func TestNextBundle_StagingSkipsBuildOutputAndDependencies(t *testing.T) {
	res := runGenerateNext(t, map[string]string{
		"package.json":              nextPackageJSON,
		"next.config.mjs":           "export default {};\n",
		"app/page.tsx":              "export default function P() { return null; }\n",
		"node_modules/pkg/index.ts": "export const x = 1;\n",
		"dist/bundle.ts":            "export const y = 2;\n",
		".next/types/route.ts":      "export const z = 3;\n",
	})
	if got := nextRule(t, res, "next_build").AttrStrings("staging_srcs"); len(got) > 0 {
		t.Errorf("staging_srcs = %v; none of those directories gets a ts_compile", got)
	}
}

// `next build` shells out to `npm install typescript @types/react @types/node`
// the moment it finds a .ts file and cannot import them, and the action it does
// that in has no network.
func TestNextBundle_NodeModulesCarriesTheTypeScriptToolchain(t *testing.T) {
	res := runGenerateNext(t, nextAppLayout)
	deps := nextRule(t, res, "node_modules").AttrStrings("deps")

	for _, want := range []string{
		"@npm//:typescript", "@npm//:types_react", "@npm//:types_node",
		"@npm//:next", "@npm//:react", "@npm//:react-dom",
	} {
		if !slices.Contains(deps, want) {
			t.Errorf("node_modules deps %v do not carry %q", deps, want)
		}
	}
}

// A package the lockfile does not have cannot be in the tree, so the generator
// says which one is missing rather than leaving `next build` to fail on npm.
func TestNextBundle_MissingTypeScriptDepIsNamed(t *testing.T) {
	var res language.GenerateResult
	logged := captureLog(t, func() {
		res = runGenerateNextWithPackages(t, nextAppLayout,
			[]string{"next", "react", "react-dom"})
	})

	deps := nextRule(t, res, "node_modules").AttrStrings("deps")
	if slices.Contains(deps, "@npm//:typescript") {
		t.Errorf("node_modules deps %v name typescript, which this workspace does not have", deps)
	}
	// The whole diagnostic, not the word: "typescript" alone is satisfied by the
	// package prefix the log carries on every line this generator writes.
	want := "the workspace has no typescript, @types/react, @types/react-dom, @types/node"
	if !strings.Contains(logged, want) {
		t.Errorf("nothing logged names the missing packages;\nwant a line containing %q\ngot %q", want, logged)
	}
}
