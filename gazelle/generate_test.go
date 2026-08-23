package typescript

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/bazelbuild/bazel-gazelle/config"
	"github.com/bazelbuild/bazel-gazelle/language"
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
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
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
