package typescript

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/bazelbuild/bazel-gazelle/config"
	"github.com/bazelbuild/bazel-gazelle/language"
	"github.com/bazelbuild/bazel-gazelle/rule"
)

// runGenerate writes files into <tmp>/<rel> and runs the rule generator over
// that directory, the way Gazelle does when it walks a repository.
func runGenerate(t *testing.T, rel string, files map[string]string) language.GenerateResult {
	t.Helper()

	repoRoot := t.TempDir()
	dir := filepath.Join(repoRoot, rel)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var names []string
	for name, content := range files {
		full := filepath.Join(dir, name)
		// A trailing slash declares a directory the generator has to see on
		// disk without putting a file in RegularFiles.
		if strings.HasSuffix(name, "/") {
			if err := os.MkdirAll(full, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	sort.Strings(names)

	c := &config.Config{RepoRoot: repoRoot, Exts: make(map[string]interface{})}
	configureTsConfig(c, "", nil)
	configureTsConfig(c, rel, nil)

	return generateRules(language.GenerateArgs{
		Config:       c,
		Dir:          dir,
		Rel:          rel,
		RegularFiles: names,
	})
}

// generatedNames maps rule name → kind for every generated rule, failing the
// test when two rules share a name (Bazel rejects such a package outright).
func generatedNames(t *testing.T, res language.GenerateResult) map[string]string {
	t.Helper()
	byName := make(map[string]string, len(res.Gen))
	for _, r := range res.Gen {
		if kind, dup := byName[r.Name()]; dup {
			t.Errorf("duplicate target name %q: %s conflicts with existing %s", r.Name(), r.Kind(), kind)
		}
		byName[r.Name()] = r.Kind()
	}
	return byName
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

// TestGenerate_CSSAndTSWithSameStem covers the observed crash: a directory
// input/ holding both input.css and input.tsx produced css_library(name="input")
// alongside ts_compile(name="input").
func TestGenerate_CSSAndTSWithSameStem(t *testing.T) {
	res := runGenerate(t, "input", map[string]string{
		"input.css": ".input {}\n",
		"input.tsx": "export const Input = () => null;\n",
	})

	byName := generatedNames(t, res)
	assertRule(t, byName, "input", "ts_compile")
	assertRule(t, byName, "input_css", "css_library")
}

// TestGenerate_SameStemAcrossAssetKinds pins the other collisions the old
// stem-only scheme allowed: logo.svg vs logo.json, and a CSS module whose stem
// matches a plain CSS file.
func TestGenerate_SameStemAcrossAssetKinds(t *testing.T) {
	res := runGenerate(t, "logo", map[string]string{
		"logo.svg":        "<svg/>\n",
		"logo.json":       "{}\n",
		"logo.css":        ".logo {}\n",
		"logo.module.css": ".logo {}\n",
		"logo.tsx":        "export const Logo = () => null;\n",
	})

	byName := generatedNames(t, res)
	assertRule(t, byName, "logo", "ts_compile")
	assertRule(t, byName, "logo_svg", "asset_library")
	assertRule(t, byName, "logo_json", "json_library")
	assertRule(t, byName, "logo_css", "css_library")
	assertRule(t, byName, "logo_module_css", "css_module")
}

// TestGenerate_TestTargetNameNotTakenByAsset guards the ts_test and ts_lint
// names too: a directory app/ with app_test.css must not claim "app_test".
func TestGenerate_TestTargetNameNotTakenByAsset(t *testing.T) {
	res := runGenerate(t, "app", map[string]string{
		"app.ts":        "export const a = 1;\n",
		"app.test.ts":   "export const t = 1;\n",
		"app_test.css":  ".a {}\n",
		"app_lint.json": "{}\n",
	})

	byName := generatedNames(t, res)
	assertRule(t, byName, "app", "ts_compile")
	assertRule(t, byName, "app_test", "ts_test")
	assertRule(t, byName, "app_test_css", "css_library")
	assertRule(t, byName, "app_lint_json", "json_library")
}

func TestAssetTargetNames_NumericSuffixOnRemainingTie(t *testing.T) {
	reserved := map[string]struct{}{"dir": {}}
	got := assetTargetNames(reserved, []string{"a.b.css", "a_b.css"})
	if got["a.b.css"] == got["a_b.css"] {
		t.Fatalf("names collided: %v", got)
	}
	if got["a.b.css"] != "a_b_css" || got["a_b.css"] != "a_b_css_2" {
		t.Errorf("assetTargetNames: got %v", got)
	}
}

func TestAssetTargetNames_AvoidsReservedTSNames(t *testing.T) {
	reserved := reservedTSTargetNames(&tsConfig{targetName: "logo_svg"}, "logo")
	got := assetTargetNames(reserved, []string{"logo.svg"})
	if got["logo.svg"] == "logo_svg" {
		t.Errorf("asset name collided with ts_compile target name: %v", got)
	}
}

func TestGenerate_RuleNamesUnchangedForPlainTSPackage(t *testing.T) {
	res := runGenerate(t, "src/lib", map[string]string{
		"index.ts":      "export const a = 1;\n",
		"index.test.ts": "export const t = 1;\n",
	})

	byName := generatedNames(t, res)
	assertRule(t, byName, "lib", "ts_compile")
	assertRule(t, byName, "lib_test", "ts_test")
}

// runGenerateWithBuild is runGenerate with a BUILD file in the directory, so
// that both its directives and its existing rules reach the generator.
func runGenerateWithBuild(t *testing.T, rel, build string, files map[string]string) language.GenerateResult {
	t.Helper()

	repoRoot := t.TempDir()
	dir := filepath.Join(repoRoot, rel)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var names []string
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	sort.Strings(names)

	f, err := rule.LoadData(filepath.Join(dir, "BUILD.bazel"), rel, []byte(build))
	if err != nil {
		t.Fatal(err)
	}

	c := &config.Config{RepoRoot: repoRoot, Exts: make(map[string]interface{})}
	configureTsConfig(c, "", nil)
	configureTsConfig(c, rel, f)

	return generateRules(language.GenerateArgs{
		Config:       c,
		Dir:          dir,
		Rel:          rel,
		File:         f,
		RegularFiles: names,
	})
}

func generatedRule(res language.GenerateResult, name string) *rule.Rule {
	for _, r := range res.Gen {
		if r.Name() == name {
			return r
		}
	}
	return nil
}

// Two ts_compile targets over one source declare the same .js and .d.ts, which
// Bazel rejects as conflicting actions rather than as a duplicate.
func TestGenerate_LeavesWhatAnExistingTargetAlreadyCompiles(t *testing.T) {
	res := runGenerateWithBuild(t, "widget", `
ts_compile(
    name = "hand_written",
    srcs = ["Button.tsx"],
)

css_library(
    name = "hand_written_css",
    srcs = ["styles.css"],
)
`, map[string]string{
		"Button.tsx": "export const Button = () => null;\n",
		"styles.css": ".b {}\n",
	})

	byName := generatedNames(t, res)
	if _, ok := byName["widget"]; ok {
		t.Errorf("generated a ts_compile over a claimed src; generated %v", byName)
	}
	if len(byName) != 0 {
		t.Errorf("every src is claimed, so nothing should be generated; got %v", byName)
	}
}

// A ts_test's setup_files are compiled by the macro and Gazelle never writes
// that attribute, so they stay claimed even on the ts_test Gazelle owns.
func TestGenerate_LeavesTheSetupFilesOfItsOwnTsTest(t *testing.T) {
	res := runGenerateWithBuild(t, "attrs", `
ts_test(
    name = "attrs_test",
    srcs = ["attrs.test.ts"],
    setup_files = ["setup.ts"],
)
`, map[string]string{
		"attrs.test.ts": "export const t = 1;\n",
		"setup.ts":      "export const s = 1;\n",
	})

	byName := generatedNames(t, res)
	if _, ok := byName["attrs"]; ok {
		t.Errorf("generated a ts_compile over a setup_files src; generated %v", byName)
	}
	assertRule(t, byName, "attrs_test", "ts_test")
}

// One tsconfig `paths` map serves a whole workspace, and ts_compile rejects an
// alias whose files are none of its inputs, so a target that uses none must
// carry none.
func TestGenerate_NoPathAliasesWhenTheSourcesUseNone(t *testing.T) {
	res := runGenerateWithBuild(t, "app", `
# gazelle:ts_path_alias @/ app/src/
# gazelle:ts_path_alias ~ui/ packages/ui/src/
`, map[string]string{
		"main.ts": "import { z } from 'zod';\nexport const y = z;\n",
	})

	r := generatedRule(res, "app")
	if r == nil {
		t.Fatal("no ts_compile named app")
	}
	if r.Attr("path_aliases") != nil {
		t.Error("path_aliases set on a target whose sources use no alias")
	}
}

func TestUsedPathAliases_PicksOnlyTheMatchedKeys(t *testing.T) {
	c := emptyConfig()
	c.Exts[languageName] = makeConfig("", []rule.Directive{
		directive("ts_path_alias", "@/ src/"),
		directive("ts_path_alias", "~ui/ packages/ui/src/"),
	})
	tc := getConfig(c)

	got := usedPathAliases(tc, "app", []string{"main.ts"}, []string{"@/x", "zod", "./local"})
	want := map[string]string{"@/": "src/"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("usedPathAliases = %v, want %v", got, want)
	}
	if got := usedPathAliases(tc, "app", []string{"main.ts"}, []string{"zod"}); len(got) != 0 {
		t.Errorf("usedPathAliases with no alias import = %v, want empty", got)
	}
}

// A path alias reaches the IDE tsconfig only through a target that declares it,
// and the package it points into is the one target that can: ts_compile accepts
// an alias whose directory holds that target's own sources.
func TestUsedPathAliases_KeepsTheAliasOverThePackageItPointsInto(t *testing.T) {
	c := emptyConfig()
	c.Exts[languageName] = makeConfig("", []rule.Directive{
		directive("ts_path_alias", "@/ src/"),
		directive("ts_path_alias", "@ui/ packages/ui/src/"),
		directive("ts_path_alias", "@lib lib/index"),
	})
	tc := getConfig(c)

	// Nothing here imports through any alias: "@/" survives because
	// src/components/button.ts lives under src/.
	got := usedPathAliases(tc, "src/components", []string{"button.ts"}, nil)
	want := map[string]string{"@/": "src/"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("usedPathAliases = %v, want %v", got, want)
	}

	// "@lib" points at a file stem, not a directory -- the same thing
	// ts_compile refuses, so Gazelle must not write it either.
	if got := usedPathAliases(tc, "lib", []string{"index.ts"}, nil); len(got) != 0 {
		t.Errorf("usedPathAliases over a stem alias = %v, want empty", got)
	}

	// ts_refresh_tsconfig writes a paths entry for every module_name in the
	// graph. Reading one of those back is not a declaration, so it must not
	// come back as an attr on the package it names.
	tc.aliasesFromDirectives = false
	if got := usedPathAliases(tc, "src/components", []string{"button.ts"}, nil); len(got) != 0 {
		t.Errorf("usedPathAliases over an echoed alias = %v, want empty", got)
	}
}

// `outs` is mergeable, so an empty stub strips it from whatever it matches. Only
// the names the built-in detectors write are Gazelle's to clean up.
func TestGenerate_KeepsAHandWrittenTsCodegen(t *testing.T) {
	res := runGenerateWithBuild(t, "codegen", `
ts_codegen(
    name = "generated_ts",
    srcs = ["input.ts"],
    outs = ["generated.ts"],
    generator = ":test_generator",
)

ts_codegen(
    name = "route_tree",
    srcs = ["input.ts"],
    outs = ["routeTree.gen.ts"],
    generator = "@npm//:tsr_bin",
)
`, map[string]string{
		"input.ts": "export const a = 1;\n",
	})

	var emptied []string
	for _, r := range res.Empty {
		if r.Kind() == "ts_codegen" {
			emptied = append(emptied, r.Name())
		}
	}
	sort.Strings(emptied)
	if len(emptied) != 1 || emptied[0] != "route_tree" {
		t.Errorf("ts_codegen cleanup stubs = %v, want [route_tree]", emptied)
	}
}

// The macro has no default hub, so a generated :add_package with no pnpm_lock
// is a BUILD file that does not load.
func TestGenerate_AddPackageNamesTheRootLockfile(t *testing.T) {
	res := runGenerate(t, "", map[string]string{
		"pnpm-lock.yaml": "lockfileVersion: '9.0'\n",
		"index.ts":       "export const x = 1\n",
	})

	var addPackage *rule.Rule
	for _, r := range res.Gen {
		if r.Kind() == "ts_add_package" {
			addPackage = r
		}
	}
	if addPackage == nil {
		t.Fatalf("no ts_add_package generated beside a root pnpm-lock.yaml; got %v", generatedNames(t, res))
	}
	if got := addPackage.AttrString("pnpm_lock"); got != "//:pnpm-lock.yaml" {
		t.Errorf("pnpm_lock = %q, want %q", got, "//:pnpm-lock.yaml")
	}
}

// A vitest config beside the tests is not decoration: it names the pool, the
// environment and the deps to inline. Dropped, the tests run in plain Node --
// a worker's `defineWorkersConfig` pool becomes no pool at all, and a
// dependency that only resolves through Vite fails at import time.
func TestGenerate_VitestConfigBesideTestsReachesTheTestTarget(t *testing.T) {
	for _, name := range []string{"vitest.config.mts", "vitest.config.ts", "vitest.config.mjs"} {
		t.Run(name, func(t *testing.T) {
			res := runGenerate(t, "pkg", map[string]string{
				"index.ts":      "export const x = 1;\n",
				"index.test.ts": "import { x } from './index';\n",
				name:            "import { defineWorkersConfig } from '@cloudflare/vitest-pool-workers/config';\nexport default defineWorkersConfig({});\n",
			})
			var test *rule.Rule
			for _, r := range res.Gen {
				if r.Kind() == "ts_test" {
					test = r
				}
			}
			if test == nil {
				t.Fatalf("no ts_test generated: %v", generatedNames(t, res))
			}
			if got := test.AttrString("config"); got != name {
				t.Errorf("ts_test config = %q, want %q", got, name)
			}
			// The config is a module the runner imports; what it imports is a dep
			// of the test like any other.
			found := false
			for _, imp := range res.Imports[indexOfRule(res, test)].([]string) {
				if imp == "@cloudflare/vitest-pool-workers/config" {
					found = true
				}
			}
			if !found {
				t.Errorf("the config's own imports never reached the test target")
			}
		})
	}
}

// No config, no attribute: an empty string would name a file that is not there.
func TestGenerate_NoVitestConfigLeavesTheAttributeUnset(t *testing.T) {
	res := runGenerate(t, "pkg", map[string]string{
		"index.ts":      "export const x = 1;\n",
		"index.test.ts": "import { x } from './index';\n",
	})
	for _, r := range res.Gen {
		if r.Kind() == "ts_test" && r.Attr("config") != nil {
			t.Errorf("ts_test config = %q, want unset", r.AttrString("config"))
		}
	}
}

func indexOfRule(res language.GenerateResult, want *rule.Rule) int {
	for i, r := range res.Gen {
		if r == want {
			return i
		}
	}
	return -1
}
