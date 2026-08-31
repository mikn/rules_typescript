package typescript

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/bazelbuild/bazel-gazelle/config"
	"github.com/bazelbuild/bazel-gazelle/language"
	"github.com/bazelbuild/bazel-gazelle/rule"
)

// generateRolledUp writes a tree whose subdirectories are not packages, and
// generates rules for its root the way index-only mode reaches it.
func generateRolledUp(t *testing.T, files map[string]string, build string) language.GenerateResult {
	t.Helper()
	repoRoot := t.TempDir()
	dir := filepath.Join(repoRoot, "pkg")
	for name, content := range files {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	f, err := rule.LoadData(filepath.Join(dir, "BUILD.bazel"), "pkg", []byte(build))
	if err != nil {
		t.Fatal(err)
	}
	c := &config.Config{RepoRoot: repoRoot, Exts: make(map[string]interface{})}
	configureTsConfig(c, "", nil)
	configureTsConfig(c, "pkg", f)

	return generateRules(language.GenerateArgs{
		Config: c, Dir: dir, Rel: "pkg", File: f, RegularFiles: names,
	})
}

// A subdirectory that is not a package has its TypeScript rolled up into the
// package above, and until now nothing else: the CSS beside that TypeScript
// belonged to no target at all. The side-effect import then resolves to a
// package that does not exist and fails the whole build.
var rolledUpTree = map[string]string{
	"index.ts":                   "export { Checkbox } from './widget/checkbox';\n",
	"widget/checkbox.ts":         "import './checkbox.css';\nimport styles from './checkbox.module.css';\nexport const Checkbox = () => styles;\n",
	"widget/checkbox.css":        ".checkbox { color: red }\n",
	"widget/checkbox.module.css": ".root { color: blue }\n",
	"widget/data.json":           "{\"a\": 1}\n",
}

const indexOnlyBuild = "# gazelle:ts_package_boundary index-only\n"

func TestRolledUp_AssetsBesideRolledUpSourcesGetTargets(t *testing.T) {
	res := generateRolledUp(t, rolledUpTree, indexOnlyBuild)

	kinds := map[string]string{}
	for _, r := range res.Gen {
		for _, s := range r.AttrStrings("srcs") {
			kinds[s] = r.Kind()
		}
	}
	for src, want := range map[string]string{
		"widget/checkbox.css":        "css_library",
		"widget/checkbox.module.css": "css_module",
		"widget/data.json":           "json_library",
	} {
		if got := kinds[src]; got != want {
			t.Errorf("%s: claimed by %q, want %q (all srcs: %v)", src, got, want, kinds)
		}
	}
}

// Two files with the same basename in different rolled-up directories have to
// end up with different target names; Bazel rejects the package otherwise.
func TestRolledUp_SameBasenameInTwoDirectories(t *testing.T) {
	res := generateRolledUp(t, map[string]string{
		"index.ts":     "export const x = 1;\n",
		"a/styles.css": ".a {}\n",
		"b/styles.css": ".b {}\n",
		"a/a.ts":       "import './styles.css';\nexport const a = 1;\n",
		"b/b.ts":       "import './styles.css';\nexport const b = 1;\n",
	}, indexOnlyBuild)

	seen := map[string]int{}
	for _, r := range res.Gen {
		seen[r.Name()]++
	}
	for name, n := range seen {
		if n > 1 {
			t.Errorf("target name %q generated %d times", name, n)
		}
	}
	if len(seen) < 3 {
		t.Errorf("expected a target per rolled-up stylesheet, got %v", seen)
	}
}
