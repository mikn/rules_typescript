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

// runGenerateRootWithBuild runs the generator over the workspace root with an
// existing BUILD file, the way Gazelle does on a second run.
func runGenerateRootWithBuild(t *testing.T, files map[string]string, build string) language.GenerateResult {
	t.Helper()

	repoRoot := t.TempDir()
	var names []string
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(repoRoot, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	sort.Strings(names)

	var f *rule.File
	if build != "" {
		parsed, err := rule.LoadData(filepath.Join(repoRoot, "BUILD.bazel"), "", []byte(build))
		if err != nil {
			t.Fatal(err)
		}
		f = parsed
	}

	c := &config.Config{RepoRoot: repoRoot, Exts: make(map[string]interface{})}
	configureTsConfig(c, "", nil)

	return generateRules(language.GenerateArgs{
		Config:       c,
		Dir:          repoRoot,
		Rel:          "",
		File:         f,
		RegularFiles: names,
	})
}

func emptyNames(res language.GenerateResult) map[string]string {
	byName := make(map[string]string, len(res.Empty))
	for _, r := range res.Empty {
		byName[r.Name()] = r.Kind()
	}
	return byName
}

const (
	tanstackPackageJSON = `{"dependencies":{"@tanstack/react-router":"1.0.0","react":"19.0.0"}}`
	nextPackageJSON     = `{"dependencies":{"next":"15.0.0","react":"19.0.0"}}`
	remixPackageJSON    = `{"dependencies":{"@remix-run/react":"2.0.0"}}`
)

// TestFrameworkBundle_TargetNames pins the emitted names. The node_modules
// target must be named "node_modules" for every framework: Node resolves a
// module's realpath before its bare imports, so a tree named anything else
// leaves vite's own dependencies (rolldown, …) unresolvable from inside it.
func TestFrameworkBundle_TargetNames(t *testing.T) {
	for _, tt := range []struct {
		name        string
		packageJSON string
		bundleKind  string
		bundleName  string
	}{
		{"tanstack", tanstackPackageJSON, "ts_bundle", "app"},
		{"remix", remixPackageJSON, "ts_bundle", "app_remix"},
		{"sveltekit", `{"devDependencies":{"@sveltejs/kit":"2.0.0"}}`, "ts_bundle", "app_sveltekit"},
		{"solidstart", `{"dependencies":{"@solidjs/start":"1.0.0"}}`, "ts_bundle", "app_solid"},
		{"nextjs", nextPackageJSON, "next_build", "app"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			res := runGenerate(t, "", map[string]string{"package.json": tt.packageJSON})
			byName := generatedNames(t, res)

			assertRule(t, byName, "node_modules", "node_modules")
			assertRule(t, byName, tt.bundleName, tt.bundleKind)
			for name := range byName {
				if name == "app_node_modules" || name == "app_vite" {
					t.Errorf("emitted legacy target %q; generated %v", name, byName)
				}
			}
			if tt.bundleKind == "ts_bundle" {
				assertRule(t, byName, "vite", "vite_bundler")
			}
		})
	}
}

// TestFrameworkBundle_WiresBundlerToNodeModules checks the attrs that carry the
// names, not just the target names themselves.
func TestFrameworkBundle_WiresBundlerToNodeModules(t *testing.T) {
	res := runGenerate(t, "", map[string]string{"package.json": tanstackPackageJSON})

	for _, r := range res.Gen {
		switch r.Kind() {
		case "vite_bundler":
			if got := r.AttrString("node_modules"); got != ":node_modules" {
				t.Errorf("vite_bundler node_modules = %q, want \":node_modules\"", got)
			}
		case "ts_bundle":
			if got := r.AttrString("bundler"); got != ":vite" {
				t.Errorf("ts_bundle bundler = %q, want \":vite\"", got)
			}
		}
	}
}

// TestFrameworkBundle_IdempotentOnHandWrittenTargets covers the second Gazelle
// run over a BUILD file that already carries the correctly-named targets.
func TestFrameworkBundle_IdempotentOnHandWrittenTargets(t *testing.T) {
	build := `node_modules(
    name = "node_modules",
    deps = ["@npm//:vite"],
)

vite_bundler(
    name = "vite",
    node_modules = ":node_modules",
    vite = "@npm//:vite",
)

ts_bundle(
    name = "app",
    bundler = ":vite",
    entry_point = "//src/app:main",
)
`
	res := runGenerateRootWithBuild(t, map[string]string{"package.json": tanstackPackageJSON}, build)

	for _, r := range res.Gen {
		switch r.Name() {
		case "node_modules", "vite", "app", "app_node_modules", "app_vite":
			t.Errorf("regenerated %s(name = %q) over an existing target", r.Kind(), r.Name())
		}
	}
	if kind, ok := emptyNames(res)["node_modules"]; ok {
		t.Errorf("emitted deletion stub %s(name = \"node_modules\")", kind)
	}
}

// TestFrameworkBundle_DeletesOrphanedLegacyTargets covers migration: the
// pre-rename pair is left behind by an older Gazelle and nothing refers to it.
func TestFrameworkBundle_DeletesOrphanedLegacyTargets(t *testing.T) {
	build := `node_modules(
    name = "node_modules",
    deps = ["@npm//:vite"],
)

vite_bundler(
    name = "vite",
    node_modules = ":node_modules",
    vite = "@npm//:vite",
)

ts_bundle(
    name = "app",
    bundler = ":vite",
    entry_point = "//src/app:main",
)

node_modules(
    name = "app_node_modules",
    deps = ["@npm//:vite"],
)

vite_bundler(
    name = "app_vite",
    node_modules = ":app_node_modules",
    vite = "@npm//:vite",
)
`
	res := runGenerateRootWithBuild(t, map[string]string{"package.json": tanstackPackageJSON}, build)

	byName := emptyNames(res)
	if byName["app_node_modules"] != "node_modules" {
		t.Errorf("app_node_modules not deleted; deletions: %v", byName)
	}
	if byName["app_vite"] != "vite_bundler" {
		t.Errorf("app_vite not deleted; deletions: %v", byName)
	}
}

// TestFrameworkBundle_KeepsReferencedLegacyTargets: a legacy target another
// rule still points at is load-bearing, so deleting it would break the build.
func TestFrameworkBundle_KeepsReferencedLegacyTargets(t *testing.T) {
	build := `node_modules(
    name = "app_node_modules",
    deps = ["@npm//:vite"],
)

vite_bundler(
    name = "app_vite",
    node_modules = ":app_node_modules",
    vite = "@npm//:vite",
)

ts_bundle(
    name = "app",
    bundler = ":app_vite",
    entry_point = "//src/app:main",
)
`
	res := runGenerateRootWithBuild(t, map[string]string{"package.json": tanstackPackageJSON}, build)

	for name := range emptyNames(res) {
		if name == "app_vite" || name == "app_node_modules" {
			t.Errorf("deleted %q while ts_bundle still references :app_vite", name)
		}
	}
}

// TestFrameworkBundle_RootTestsKeepNodeModules: the stale-node_modules cleanup
// for ts_test must not delete the framework tree at the workspace root.
func TestFrameworkBundle_RootTestsKeepNodeModules(t *testing.T) {
	files := map[string]string{
		"package.json": tanstackPackageJSON,
		"root.ts":      "export const a = 1;\n",
		"root.test.ts": "export const t = 1;\n",
	}
	build := `node_modules(
    name = "node_modules",
    deps = ["@npm//:vite"],
)
`
	res := runGenerateRootWithBuild(t, files, build)

	if kind, ok := emptyNames(res)["node_modules"]; ok {
		t.Errorf("root ts_test deleted the framework tree: %s(name = \"node_modules\")", kind)
	}
}
