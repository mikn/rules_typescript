package typescript

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/bazelbuild/bazel-gazelle/config"
	"github.com/bazelbuild/bazel-gazelle/language"
	"github.com/bazelbuild/bazel-gazelle/rule"
)

// A workspace that installed oxlint and nothing else. Every eslint config below
// is a file on disk with no eslint behind it -- the shape of a nested npm
// island's config, or one left over from a package that was never installed.
const oxlintOnlyLock = `lockfileVersion: '9.0'

importers:

  .:
    devDependencies:
      oxlint:
        specifier: ^1.0.0
        version: 1.0.0

packages:

  oxlint@1.0.0:
    resolution: {integrity: sha512-aaa}

snapshots:

  oxlint@1.0.0: {}
`

// lintWorkspace is a workspace whose root holds lock (none when empty), with
// the root directory configured so the lockfile has been read.
func lintWorkspace(t *testing.T, lock string) *config.Config {
	t.Helper()
	root := t.TempDir()
	if lock != "" {
		writeFile(t, filepath.Join(root, pnpmLockfileName), lock)
	}
	c := &config.Config{RepoRoot: root, Exts: make(map[string]interface{})}
	configureTsConfig(c, "", nil)
	return c
}

// generateLintDir writes files into rel, configures it under whatever directory
// was configured last -- its parent, when called in walk order -- and runs the
// generator over it, with build as its existing BUILD file when non-empty.
func generateLintDir(t *testing.T, c *config.Config, rel string, files map[string]string, build string) language.GenerateResult {
	t.Helper()
	dir := filepath.Join(c.RepoRoot, rel)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var names []string
	for name, content := range files {
		writeFile(t, filepath.Join(dir, name), content)
		names = append(names, name)
	}
	sort.Strings(names)

	var f *rule.File
	if build != "" {
		parsed, err := rule.LoadData(filepath.Join(dir, "BUILD.bazel"), rel, []byte(build))
		if err != nil {
			t.Fatal(err)
		}
		f = parsed
	}
	configureTsConfig(c, rel, f)
	return generateRules(language.GenerateArgs{
		Config:       c,
		Dir:          dir,
		Rel:          rel,
		File:         f,
		RegularFiles: names,
	})
}

func lintRule(res language.GenerateResult) *rule.Rule {
	for _, r := range res.Gen {
		if r.Kind() == "ts_lint" {
			return r
		}
	}
	return nil
}

// The defect: eslint.config.mjs on disk was enough for a ts_lint naming
// @npm//:eslint_bin, a target the hub does not declare when the lockfile has no
// eslint. Bazel answers that with `no such target`, which fails analysis for
// every target in the package rather than the lint alone.
func TestGenerate_NoLintTargetForALinterTheLockfileLacks(t *testing.T) {
	c := lintWorkspace(t, oxlintOnlyLock)

	var res language.GenerateResult
	logged := captureLog(t, func() {
		res = generateLintDir(t, c, "pkg", map[string]string{
			"index.ts":          "export const a = 1;\n",
			"eslint.config.mjs": "export default [];\n",
		}, "")
	})

	if lr := lintRule(res); lr != nil {
		t.Errorf("ts_lint(%s) generated with linter_binary %q; the lockfile has no eslint",
			lr.Name(), lr.AttrString("linter_binary"))
	}
	assertRule(t, generatedNames(t, res), "pkg", "ts_compile")
	for _, want := range []string{"pkg/eslint.config.mjs", "@npm//:eslint_bin", pnpmLockfileName} {
		if !strings.Contains(logged, want) {
			t.Errorf("the refusal does not name %q, so the reader cannot tell which config or "+
				"which package to act on:\n%s", want, logged)
		}
	}
}

// The config is inherited by every directory below it, so the reason is said
// once for the file, not once per directory it covers.
func TestGenerate_LinterRefusalIsSaidOnce(t *testing.T) {
	c := lintWorkspace(t, oxlintOnlyLock)

	var outer, inner language.GenerateResult
	logged := captureLog(t, func() {
		outer = generateLintDir(t, c, "pkg", map[string]string{
			"index.ts":          "export const a = 1;\n",
			"eslint.config.mjs": "export default [];\n",
		}, "")
		inner = generateLintDir(t, c, "pkg/sub", map[string]string{
			"index.ts": "export const b = 2;\n",
		}, "")
	})

	if lintRule(outer) != nil || lintRule(inner) != nil {
		t.Errorf("a ts_lint was generated under a config whose linter the lockfile lacks")
	}
	if got := strings.Count(logged, "pkg/eslint.config.mjs"); got != 1 {
		t.Errorf("the refusal for pkg/eslint.config.mjs was printed %d times, want 1:\n%s", got, logged)
	}
}

// A ts_lint an earlier run wrote for that config is the label that fails the
// package now, so it is withdrawn rather than left in place.
func TestGenerate_StaleLintTargetForAMissingLinterIsWithdrawn(t *testing.T) {
	c := lintWorkspace(t, oxlintOnlyLock)

	res := generateLintDir(t, c, "pkg", map[string]string{
		"index.ts":          "export const a = 1;\n",
		"eslint.config.mjs": "export default [];\n",
	}, `load("@rules_typescript//ts:defs.bzl", "ts_compile", "ts_lint")

ts_compile(
    name = "pkg",
    srcs = ["index.ts"],
    visibility = ["//visibility:public"],
)

ts_lint(
    name = "pkg_lint",
    srcs = ["index.ts"],
    config = "//pkg:eslint.config.mjs",
    linter = "eslint",
    linter_binary = "@npm//:eslint_bin",
)
`)

	withdrawn := false
	for _, r := range res.Empty {
		if r.Kind() == "ts_lint" && r.Name() == "pkg_lint" {
			withdrawn = true
		}
	}
	if !withdrawn {
		t.Errorf("ts_lint(pkg_lint) names @npm//:eslint_bin and was not withdrawn; Empty = %v", kindsOf(res.Empty))
	}
	if lintRule(res) != nil {
		t.Errorf("a ts_lint was generated alongside the withdrawal")
	}
}

// The linter the lockfile does have keeps its target, config label and all.
func TestGenerate_LintTargetForTheLinterTheLockfileHas(t *testing.T) {
	c := lintWorkspace(t, oxlintOnlyLock)

	var res language.GenerateResult
	logged := captureLog(t, func() {
		res = generateLintDir(t, c, "pkg", map[string]string{
			"index.ts":    "export const a = 1;\n",
			"oxlint.json": "{}\n",
		}, "")
	})

	lr := lintRule(res)
	if lr == nil {
		t.Fatalf("no ts_lint generated for oxlint.json; oxlint is in the lockfile")
	}
	for attr, want := range map[string]string{
		"linter":        "oxlint",
		"linter_binary": "@npm//:oxlint_bin",
		"config":        "//pkg:oxlint.json",
	} {
		if got := lr.AttrString(attr); got != want {
			t.Errorf("ts_lint.%s = %q, want %q", attr, got, want)
		}
	}
	if logged != "" {
		t.Errorf("a refusal was printed for a linter the lockfile has:\n%s", logged)
	}
}

// A ts_npm_hub tree resolves against a lockfile this reader never saw, so it is
// not refused, and its binary names that hub, as its bare imports' deps do.
func TestGenerate_LintBinaryFollowsTheTreesHub(t *testing.T) {
	c := lintWorkspace(t, oxlintOnlyLock)

	var res language.GenerateResult
	logged := captureLog(t, func() {
		res = generateLintDir(t, c, "pkg", map[string]string{
			"index.ts":          "export const a = 1;\n",
			"eslint.config.mjs": "export default [];\n",
		}, "# gazelle:ts_npm_hub npm_eslint\n")
	})

	lr := lintRule(res)
	if lr == nil {
		t.Fatalf("no ts_lint generated under a ts_npm_hub directive; its lockfile was never read:\n%s", logged)
	}
	if got := lr.AttrString("linter_binary"); got != "@npm_eslint//:eslint_bin" {
		t.Errorf("linter_binary = %q, want the tree's hub, @npm_eslint//:eslint_bin", got)
	}
	if logged != "" {
		t.Errorf("a refusal was printed for a hub whose lockfile was never read:\n%s", logged)
	}
}

// No lockfile is no information, not an empty workspace: the config alone still
// gets its ts_lint, as it did before this gate existed.
func TestGenerate_NoLockfileRefusesNoLint(t *testing.T) {
	c := lintWorkspace(t, "")

	res := generateLintDir(t, c, "pkg", map[string]string{
		"index.ts":          "export const a = 1;\n",
		"eslint.config.mjs": "export default [];\n",
	}, "")

	lr := lintRule(res)
	if lr == nil {
		t.Fatalf("no ts_lint generated with no lockfile to refuse on")
	}
	if got := lr.AttrString("linter_binary"); got != "@npm//:eslint_bin" {
		t.Errorf("linter_binary = %q, want @npm//:eslint_bin", got)
	}
}

func kindsOf(rules []*rule.Rule) []string {
	var out []string
	for _, r := range rules {
		out = append(out, r.Kind()+"("+r.Name()+")")
	}
	return out
}
