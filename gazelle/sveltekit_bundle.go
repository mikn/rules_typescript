package typescript

// Why SvelteKit gets a rule of its own rather than a ts_bundle:
// ts/private/sveltekit_build.bzl.
//
// srcs is a glob() rather than filegroup labels because the build reads
// .svelte and app.html off disk, and Gazelle classifies neither as a source
// kind it would put in a filegroup. rule.GlobValue is what emits a real glob()
// call -- a string attr comes out quoted, which Bazel reads as a filename.
//
// The glob is also why nothing under src/ gets TypeScript targets: a glob does
// not descend into a subpackage, so a BUILD file there would drop exactly the
// modules the app imports. Declining to write one does not remove one already
// there -- hence warnSvelteKitSrcPackage.

import (
	"log"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/bazelbuild/bazel-gazelle/language"
	"github.com/bazelbuild/bazel-gazelle/rule"
)

// svelte is listed explicitly rather than left to the plugin's peer link: the
// tree is what Node resolves `svelte/internal/*` through from compiled output.
var svelteKitNpmDeps = []string{
	"@sveltejs/kit",
	"@sveltejs/vite-plugin-svelte",
	"svelte",
	"vite",
}

const svelteKitSrcTree = "src"

// The default kit.files.assets: read off the cwd like src/app.html, so without it
// in srcs an app builds green and 404s on its favicon. The option relocates it.
const svelteKitStaticTree = "static"

const svelteKitConfigFile = "svelte.config.js"

// svelteKitAssetsExpr reads a string-literal kit.files.assets. The `files`
// block is matched first so an `assets` elsewhere in the config cannot pass.
var svelteKitAssetsExpr = regexp.MustCompile(
	"files\\s*:\\s*\\{[^}]*\\bassets\\s*:\\s*[\"'`]([^\"'`]+)[\"'`]")

// The second return is false when the option is set in a form this cannot read:
// the default then stands, and the caller has to say it is a guess.
func svelteKitAssetsTree(repoRoot string) (tree string, read bool) {
	data, err := os.ReadFile(filepath.Join(repoRoot, svelteKitConfigFile))
	if err != nil {
		return svelteKitStaticTree, true
	}
	text := string(data)
	if m := svelteKitAssetsExpr.FindStringSubmatch(text); m != nil {
		return path.Clean(strings.TrimPrefix(m[1], "./")), true
	}
	return svelteKitStaticTree, !strings.Contains(text, "assets")
}

// svelteKitOwnedTrees are the trees sveltekit_build globs: src/ and whichever
// directory kit.files.assets names.
func svelteKitOwnedTrees(tc *tsConfig) []string {
	assets := tc.svelteKitAssets
	if assets == "" {
		assets = svelteKitStaticTree
	}
	return []string{svelteKitSrcTree, assets}
}

// svelteKitSrcsPatterns globs one pattern per tree the app has, not one per
// extension: a pattern matching nothing fails the glob (allow_empty is False).
func svelteKitSrcsPatterns(dir string, tc *tsConfig) []string {
	patterns := []string{svelteKitSrcTree + "/**"}
	for _, tree := range svelteKitOwnedTrees(tc)[1:] {
		if hasDir(dir, tree) {
			patterns = append(patterns, tree+"/**")
		}
	}
	return patterns
}

// svelteKitOwnsDir reports whether rel is inside a tree sveltekit_build globs,
// and so gets no targets of its own.
func svelteKitOwnsDir(rel string, tc *tsConfig) bool {
	if tc.detectedFramework != FrameworkSvelteKit {
		return false
	}
	for _, tree := range svelteKitOwnedTrees(tc) {
		if rel == tree || strings.HasPrefix(rel, tree+"/") {
			return true
		}
	}
	return false
}

// warnSvelteKitSrcPackage names a BUILD file that already exists inside a
// globbed tree. Emptying it -- all Gazelle can do to a file it did not create --
// leaves the directory a Bazel package, and the srcs glob keeps skipping it.
func warnSvelteKitSrcPackage(args language.GenerateArgs) {
	if args.File == nil {
		return
	}
	log.Printf("typescript: SvelteKit detected: %s is a BUILD file inside %s/, which makes "+
		"%s a Bazel package. sveltekit_build's srcs glob does not descend into a package, so "+
		"every file under it is missing from the staged app. Delete the file -- emptying it "+
		"leaves the package behind.",
		args.File.Path, strings.Split(args.Rel, "/")[0], args.Rel)
}

// generateSvelteKitBundle generates node_modules and sveltekit_build at the
// workspace root. The user hand-authors the two configs; Gazelle wires Bazel.
func generateSvelteKitBundle(
	args language.GenerateArgs,
	tc *tsConfig,
) ([]*rule.Rule, []any) {
	var gen []*rule.Rule
	var imports []any

	log.Printf("typescript: SvelteKit detected: %s/ is compiled by sveltekit_build from srcs, "+
		"so no TypeScript targets are generated inside it. A BUILD file there would make a "+
		"subpackage, which the srcs glob does not descend into. TypeScript outside %s/ keeps "+
		"its ts_compile and reaches the build through staging_srcs.",
		svelteKitSrcTree, svelteKitSrcTree)

	npmDeps := filterNpmDeps(svelteKitNpmDeps, tc)
	nodeModulesName := frameworkNodeModulesName

	nmDeps := make([]string, 0, len(npmDeps))
	for _, pkg := range npmDeps {
		nmDeps = append(nmDeps, npmLabel(pkg))
	}
	sort.Strings(nmDeps)

	nm := rule.NewRule("node_modules", nodeModulesName)
	nm.SetAttr("deps", nmDeps)
	nm.SetAttr("visibility", []string{"//visibility:public"})
	nm.AddComment("# SvelteKit node_modules")
	gen = append(gen, nm)
	imports = append(imports, nil)

	if _, read := svelteKitAssetsTree(args.Dir); !read {
		log.Printf("typescript: SvelteKit detected: %s sets kit.files.assets in a form Gazelle "+
			"cannot read, so srcs globs %s/ and the assets tree may be missing from it -- an app "+
			"whose assets are not staged builds green and 404s on them. Name the tree in srcs "+
			"with a \"# keep\" comment on its pattern.",
			svelteKitConfigFile, svelteKitStaticTree)
	}

	sk := rule.NewRule("sveltekit_build", "app")
	setGeneratedGlob(args, sk, svelteKitSrcsPatterns(args.Dir, tc))
	sk.SetAttr("svelte_config", "svelte.config.js")
	sk.SetAttr("config", "vite.config.mjs")
	sk.SetAttr("node_modules", ":"+nodeModulesName)
	if staging := stagingLabelsOutside(args.Dir, tc, func(rel string) bool {
		return svelteKitOwnsDir(rel, tc)
	}); len(staging) > 0 {
		sk.SetAttr("staging_srcs", rule.SortedStrings(staging))
	}
	sk.AddComment("# SvelteKit application build")
	sk.AddComment("# Pin kit.version.name in svelte.config.js: unpinned it is a build")
	sk.AddComment("# timestamp, hashed into every chunk name, so nothing cache-hits.")
	gen = append(gen, sk)
	imports = append(imports, nil)

	return gen, imports
}
