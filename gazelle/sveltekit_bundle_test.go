package typescript

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bazelbuild/bazel-gazelle/config"
	"github.com/bazelbuild/bazel-gazelle/language"
	"github.com/bazelbuild/bazel-gazelle/rule"
)

// TestSvelteKitBundle_GeneratesTheBuildRule: SvelteKit is no longer refused.
// It gets a node_modules and a sveltekit_build, and no ts_bundle -- ts_bundle
// cannot host it, because SvelteKit resolves everything against process.cwd()
// and the plugin forces `root: cwd` back over any override.
func TestSvelteKitBundle_GeneratesTheBuildRule(t *testing.T) {
	res := runGenerate(t, "", map[string]string{"package.json": svelteKitPackageJSON})
	byName := generatedNames(t, res)

	assertRule(t, byName, "node_modules", "node_modules")
	assertRule(t, byName, "app", "sveltekit_build")
	for name, kind := range byName {
		if kind == "ts_bundle" || kind == "vite_bundler" {
			t.Errorf("emitted %s(name = %q); SvelteKit is not on the ts_bundle path", kind, name)
		}
	}
}

// TestSvelteKitBundle_NoRefusalIsLogged: the refusal used to be the whole
// answer for SvelteKit. Now that a build works, saying bundling is unsupported
// would send users away from a rule that serves them.
func TestSvelteKitBundle_NoRefusalIsLogged(t *testing.T) {
	logged := captureLog(t, func() {
		runGenerate(t, "", map[string]string{"package.json": svelteKitPackageJSON})
	})
	if strings.Contains(logged, "unsupported") {
		t.Errorf("still logs a bundling refusal for SvelteKit: %q", logged)
	}
	if _, refused := unsupportedBundling[FrameworkSvelteKit]; refused {
		t.Error("SvelteKit is still registered in unsupportedBundling")
	}
}

// TestSvelteKitBundle_SrcsGlobCarriesTheRouteTree: SvelteKit scans src/routes
// and reads src/app.html off disk rather than through imports, so both have to
// reach the staging root through srcs. app.html especially: nothing imports it,
// and the build fails with "src/app.html does not exist" without it.
func TestSvelteKitBundle_SrcsGlobCarriesTheRouteTree(t *testing.T) {
	sk := svelteKitRule(t)

	// A real glob() call, not a string: a string attr comes out quoted, and
	// Bazel then reads the whole expression as one filename.
	glob, ok := rule.ParseGlobExpr(sk.Attr("srcs"))
	if !ok {
		t.Fatalf("srcs is not a glob() call: %v", sk.Attr("srcs"))
	}
	// One pattern over the whole tree. Per-extension patterns fail the glob
	// outright when a project has no file of that extension, and app.html and
	// the route tree have to reach the build whatever they are called.
	if !slices.Contains(glob.Patterns, "src/**") {
		t.Errorf("srcs glob %v does not collect the whole src/ tree", glob.Patterns)
	}
	if len(glob.Patterns) != 1 {
		t.Errorf("srcs glob %v has per-extension patterns, which fail when one kind is absent", glob.Patterns)
	}
}

// TestSvelteKitBundle_SrcsGlobCarriesTheStaticTree: static/ is kit.files.assets,
// read off the cwd like the route tree, and nothing imports it. Without it in
// srcs `bazel build` is green, server/manifest.js carries an empty asset set,
// and the app 404s on its own favicon.
func TestSvelteKitBundle_SrcsGlobCarriesTheStaticTree(t *testing.T) {
	res := runGenerate(t, "", map[string]string{
		"package.json":       svelteKitPackageJSON,
		"src/app.html":       "",
		"static/favicon.svg": "",
	})
	var sk *rule.Rule
	for _, r := range res.Gen {
		if r.Kind() == "sveltekit_build" {
			sk = r
		}
	}
	if sk == nil {
		t.Fatal("no sveltekit_build rule generated")
	}
	glob, ok := rule.ParseGlobExpr(sk.Attr("srcs"))
	if !ok {
		t.Fatalf("srcs is not a glob() call: %v", sk.Attr("srcs"))
	}
	if !slices.Equal(glob.Patterns, []string{"src/**", "static/**"}) {
		t.Errorf("srcs glob %v does not carry static/, so no asset reaches the build", glob.Patterns)
	}
}

// TestSvelteKitBundle_RelocatedAssetsTreeIsGlobbed: kit.files.assets is a
// documented relocatable option, so "static" is a default rather than a name.
// An app that moved it gets the same green-build/404-at-runtime failure the
// default case was fixed for -- plus orphan targets in the moved directory,
// which svelteKitOwnsDir has to claim.
func TestSvelteKitBundle_RelocatedAssetsTreeIsGlobbed(t *testing.T) {
	files := map[string]string{
		"package.json":       svelteKitPackageJSON,
		"svelte.config.js":   "export default { kit: { files: { assets: \"public\" } } };\n",
		"src/app.html":       "",
		"public/favicon.svg": "",
		"static/stale.svg":   "",
	}
	res := runGenerate(t, "", files)

	var sk *rule.Rule
	for _, r := range res.Gen {
		if r.Kind() == "sveltekit_build" {
			sk = r
		}
	}
	if sk == nil {
		t.Fatal("no sveltekit_build rule generated")
	}
	glob, ok := rule.ParseGlobExpr(sk.Attr("srcs"))
	if !ok {
		t.Fatalf("srcs is not a glob() call: %v", sk.Attr("srcs"))
	}
	if !slices.Equal(glob.Patterns, []string{"src/**", "public/**"}) {
		t.Errorf("srcs glob %v does not carry the relocated assets tree, so no asset reaches "+
			"the build and the app 404s on its own favicon", glob.Patterns)
	}

	tc := &tsConfig{detectedFramework: FrameworkSvelteKit, svelteKitAssets: "public"}
	if !svelteKitOwnsDir("public", tc) {
		t.Error("public/ is not claimed, so Gazelle writes an orphan asset_library there that " +
			"nothing references and a package the srcs glob cannot descend into")
	}
	if svelteKitOwnsDir("static", tc) {
		t.Error("static/ is still claimed even though the config moved the assets tree")
	}
}

// TestSvelteKitBundle_UnreadableAssetsOptionIsNamed: an assets option Gazelle
// cannot parse falls back to the default, which is a guess. Saying so is the
// difference between a wrong glob and a wrong glob nobody hears about.
func TestSvelteKitBundle_UnreadableAssetsOptionIsNamed(t *testing.T) {
	logged := captureLog(t, func() {
		runGenerate(t, "", map[string]string{
			"package.json":     svelteKitPackageJSON,
			"svelte.config.js": "const assets = process.env.ASSETS;\nexport default { kit: { files: { assets } } };\n",
			"src/app.html":     "",
		})
	})
	for _, want := range []string{"kit.files.assets", "# keep"} {
		if !strings.Contains(logged, want) {
			t.Errorf("the log does not mention %q, so the fallback is a silent guess:\n%s", want, logged)
		}
	}
}

// TestSvelteKitBundle_ConfigNamesAreTheOnesSvelteKitLooksFor: sveltekit_build
// stages the svelte config as svelte.config.js, and load_config() globs
// svelte.config.{js,ts} at its cwd -- so a generated svelte.config.mjs would
// build under Bazel and nowhere else, and sveltekit_build rejects one.
func TestSvelteKitBundle_ConfigNamesAreTheOnesSvelteKitLooksFor(t *testing.T) {
	sk := svelteKitRule(t)

	if got := sk.AttrString("svelte_config"); got != "svelte.config.js" {
		t.Errorf("svelte_config = %q, want svelte.config.js", got)
	}
	if got := sk.AttrString("config"); got == "" {
		t.Error("config is unset, so no Vite config carries SvelteKit's plugin")
	}
	if got := sk.AttrString("node_modules"); got != ":node_modules" {
		t.Errorf("node_modules = %q, want :node_modules", got)
	}
}

// TestSvelteKitBundle_NpmDepsNameSvelteItself: the compiled components import
// `svelte/internal/*`, which Node resolves through the tree rather than through
// the plugin's peer link.
func TestSvelteKitBundle_NpmDepsNameSvelteItself(t *testing.T) {
	res := runGenerate(t, "", map[string]string{"package.json": svelteKitPackageJSON})

	for _, r := range res.Gen {
		if r.Kind() != "node_modules" {
			continue
		}
		deps := r.AttrStrings("deps")
		for _, want := range []string{
			"@npm//:sveltejs_kit",
			"@npm//:sveltejs_vite-plugin-svelte",
			"@npm//:svelte",
			"@npm//:vite",
		} {
			if !slices.Contains(deps, want) {
				t.Errorf("node_modules deps %v is missing %q", deps, want)
			}
		}
		return
	}
	t.Fatal("no node_modules rule generated")
}

// TestSvelteKitBundle_StagesNothing: sveltekit_build takes a glob, not
// filegroup labels, so the per-directory "sources" filegroups that feed a
// ts_bundle must not be generated for a SvelteKit workspace.
func TestSvelteKitBundle_StagesNothing(t *testing.T) {
	tc := &tsConfig{detectedFramework: FrameworkSvelteKit}
	for _, dir := range []string{"src/routes", "src/lib", "src"} {
		if isStagedDir(dir, tc) {
			t.Errorf("%q is a stage dir, but sveltekit_build takes a glob", dir)
		}
	}
}

// TestSvelteKitBundle_SrcTreeGetsNoTargets: a BUILD file anywhere under src/
// makes a subpackage, and glob() does not descend into one -- so the staged tree
// would silently lose the very modules the app imports, and a route ts_compile
// could not typecheck anyway (route modules import ./$types, generated under
// .svelte-kit/types). Nothing outside src/ is claimed.
func TestSvelteKitBundle_SrcTreeGetsNoTargets(t *testing.T) {
	sk := &tsConfig{detectedFramework: FrameworkSvelteKit}
	for _, dir := range []string{"src", "src/routes", "src/lib", "src/routes/blog/[slug]", "static", "static/img"} {
		if !svelteKitOwnsDir(dir, sk) {
			t.Errorf("%q is not recognised as a tree sveltekit_build globs", dir)
		}
	}
	for _, dir := range []string{"", "packages/ui", "srcs", "source", "statics"} {
		if svelteKitOwnsDir(dir, sk) {
			t.Errorf("%q was taken for SvelteKit's src/ tree", dir)
		}
	}

	other := &tsConfig{detectedFramework: FrameworkTanStack}
	if svelteKitOwnsDir("src/routes", other) {
		t.Error("src/routes is claimed for a framework that is not SvelteKit")
	}
}

// TestSvelteKitBundle_ExistingSrcPackageIsNamed: the src/ tree gets no targets,
// but declining to create a BUILD file there does not remove one that is
// already there -- Gazelle empties such a file and the directory stays a Bazel
// package, which the srcs glob keeps skipping. The name of the file is the
// whole diagnosis, so it has to reach the log.
func TestSvelteKitBundle_ExistingSrcPackageIsNamed(t *testing.T) {
	res, logged := generateInSvelteKitSrcTree(t, "src/lib", `ts_compile(name = "lib")`)

	if len(res.Gen) != 0 {
		t.Errorf("generated %d targets inside the src/ tree, want none", len(res.Gen))
	}
	for _, want := range []string{"BUILD.bazel", "src/lib", "Delete the file"} {
		if !strings.Contains(logged, want) {
			t.Errorf("the log does not mention %q:\n%s", want, logged)
		}
	}
}

// generateInSvelteKitSrcTree runs generation for a directory inside the src/
// tree of a SvelteKit workspace that already has a BUILD file there, and
// returns what was generated alongside what was logged. Detection runs against
// the repo root, so the package.json has to sit there rather than in dir.
func generateInSvelteKitSrcTree(t *testing.T, rel, build string) (language.GenerateResult, string) {
	t.Helper()

	repoRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoRoot, "package.json"), []byte(svelteKitPackageJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(repoRoot, rel)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	f, err := rule.LoadData(filepath.Join(dir, "BUILD.bazel"), rel, []byte(build))
	if err != nil {
		t.Fatal(err)
	}

	c := &config.Config{RepoRoot: repoRoot, Exts: make(map[string]interface{})}
	configureTsConfig(c, "", nil)
	configureTsConfig(c, rel, f)

	var res language.GenerateResult
	logged := captureLog(t, func() {
		res = generateRules(language.GenerateArgs{
			Config:       c,
			Dir:          dir,
			Rel:          rel,
			File:         f,
			RegularFiles: []string{"greeting.ts"},
		})
	})
	return res, logged
}

func svelteKitRule(t *testing.T) *rule.Rule {
	t.Helper()
	res := runGenerate(t, "", map[string]string{"package.json": svelteKitPackageJSON})
	for _, r := range res.Gen {
		if r.Kind() == "sveltekit_build" {
			return r
		}
	}
	t.Fatal("no sveltekit_build rule generated")
	return nil
}
