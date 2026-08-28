package typescript

// framework_next.go generates the root-level targets for a Next.js
// application: a node_modules tree, a next_build, and a next_dev_server.
//
// Next.js is not on the ts_bundle path. `next build` is its own compiler,
// bundler and type checker, reached through next_build; the only thing Gazelle
// decides is which files reach its staging directory and which config it reads.
//
// srcs is a glob() rather than filegroup labels, for the same reason
// sveltekit_build's is: the build reads the route tree, the stylesheets and
// public/ off disk rather than through imports, and Gazelle classifies most of
// that as nothing it would put in a filegroup. rule.GlobValue is what emits a
// real glob() call -- a string attr comes out quoted, which Bazel then reads as
// a filename.
//
// The glob is also why nothing inside those directories gets TypeScript
// targets: a BUILD file there would make a subpackage, and a glob does not
// descend into one, so the staged tree would silently lose exactly the modules
// the routes import.

import (
	"log"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/bazelbuild/bazel-gazelle/language"
	"github.com/bazelbuild/bazel-gazelle/rule"
)

// nextJSNpmDeps are the packages next_build's node_modules must carry. The
// Next.js CLI runs out of this tree, and `next build` npm-installs typescript
// and the @types itself when it cannot import them -- with no network to do it.
var nextJSNpmDeps = []string{
	"next",
	"react",
	"react-dom",
	"typescript",
	"@types/react",
	"@types/react-dom",
	"@types/node",
}

// nextOwnedDirs are the directories Next.js compiles itself. `app` and `pages`
// are the two routers, `src` is the same pair one level down (Next.js supports
// either layout), and `public` is served verbatim at request time.
//
// Everything else -- shared TypeScript at the workspace root -- keeps its
// ts_compile and ts_test targets and reaches the build through staging_srcs.
var nextOwnedDirs = []string{"app", "pages", "src", "public"}

// nextRootFiles are the conventional root-level files Next.js reads by name,
// and so the ones next_build's srcs has to cover.
var nextRootFiles = []string{"middleware.ts", "instrumentation.ts"}

// nextConfigFiles is Next.js's own CONFIG_FILES order (next/dist/shared/lib/
// constants.js): it takes the first that exists, so Gazelle has to as well, or
// it would name a config the build ignores.
var nextConfigFiles = []string{"next.config.js", "next.config.mjs", "next.config.ts"}

// nextOwnsFile reports whether a root-level TypeScript file belongs to Next.js
// rather than to a ts_compile target. Compiling one on its own type-checks it
// outside the Next.js program, where `next/server` and the JSX runtime resolve
// through a tsconfig next_build stages and this target does not have.
func nextOwnsFile(rel, name string, tc *tsConfig) bool {
	if tc.detectedFramework != FrameworkNextJS || rel != "" {
		return false
	}
	// next-env.d.ts is written by `next dev`, not by anyone who would compile it.
	return name == "next-env.d.ts" ||
		slices.Contains(nextConfigFiles, name) ||
		slices.Contains(nextRootFiles, name)
}

// Emptying such a file -- all Gazelle can do to one it did not create -- leaves
// the directory a Bazel package, and the srcs glob keeps skipping it.
func warnNextOwnedPackage(args language.GenerateArgs) {
	if args.File == nil {
		return
	}
	log.Printf("typescript: Next.js detected: %s is a BUILD file inside %s/, which makes %s a "+
		"Bazel package. next_build's srcs glob does not descend into a package, so every file "+
		"under it is missing from the staged app and does not resolve. Delete the file -- "+
		"emptying it leaves the package behind.",
		args.File.Path, strings.Split(args.Rel, "/")[0], args.Rel)
}

// nextOwnsDir reports whether rel is inside a directory Next.js compiles, and
// so gets no TypeScript targets of its own.
func nextOwnsDir(rel string, tc *tsConfig) bool {
	if tc.detectedFramework != FrameworkNextJS || rel == "" {
		return false
	}
	for _, dir := range nextOwnedDirs {
		if rel == dir || strings.HasPrefix(rel, dir+"/") {
			return true
		}
	}
	return false
}

// nextSrcsPatterns globs one pattern per directory that exists, not one per
// extension: an extension with no files fails the glob (allow_empty is False).
func nextSrcsPatterns(dir string) []string {
	patterns := []string{}
	for _, owned := range nextOwnedDirs {
		if hasDir(dir, owned) {
			patterns = append(patterns, owned+"/**")
		}
	}
	for _, name := range nextRootFiles {
		if hasFile(dir, name) {
			patterns = append(patterns, name)
		}
	}
	return patterns
}

func hasDir(dir, name string) bool {
	info, err := os.Stat(filepath.Join(dir, name))
	return err == nil && info.IsDir()
}

func hasFile(dir, name string) bool {
	info, err := os.Stat(filepath.Join(dir, name))
	return err == nil && !info.IsDir()
}

// nextOwnedDirsWithSlash names the owned directories the way a user sees them.
func nextOwnedDirsWithSlash() []string {
	named := make([]string, 0, len(nextOwnedDirs))
	for _, dir := range nextOwnedDirs {
		named = append(named, dir+"/")
	}
	return named
}

// nextConfigFile is the config Next.js would load from dir, or "" when the
// project has none -- a legal Next.js layout, and one where naming a config
// anyway declares an input that does not exist.
func nextConfigFile(dir string) string {
	for _, name := range nextConfigFiles {
		if hasFile(dir, name) {
			return name
		}
	}
	return ""
}

// generateNextJSBundle generates node_modules, next_build and next_dev_server
// at the workspace root. The user hand-authors next.config.*; Gazelle generates
// the Bazel wiring.
func generateNextJSBundle(
	args language.GenerateArgs,
	tc *tsConfig,
) ([]*rule.Rule, []any) {
	var gen []*rule.Rule
	var imports []any

	log.Printf("typescript: Next.js detected: %s are compiled by next_build from srcs, "+
		"so no TypeScript targets are generated inside them. A BUILD file there would make "+
		"a subpackage, which the srcs glob does not descend into. TypeScript outside them "+
		"keeps its ts_compile and reaches the build through staging_srcs.",
		strings.Join(nextOwnedDirsWithSlash(), ", "))

	npmDeps := filterNpmDeps(nextJSNpmDeps, tc)
	if missing := missingNpmDeps(nextJSNpmDeps, npmDeps); len(missing) > 0 {
		log.Printf("typescript: Next.js detected, but the workspace has no %s, so the generated "+
			"node_modules cannot carry them. `next build` npm-installs the TypeScript ones "+
			"itself and its action has no network, so add them to package.json and re-run.",
			strings.Join(missing, ", "))
	}
	nodeModulesName := frameworkNodeModulesName

	nmDeps := make([]string, 0, len(npmDeps))
	for _, pkg := range npmDeps {
		nmDeps = append(nmDeps, npmLabel(pkg))
	}
	sort.Strings(nmDeps)

	nm := rule.NewRule("node_modules", nodeModulesName)
	nm.SetAttr("deps", nmDeps)
	nm.SetAttr("visibility", []string{"//visibility:public"})
	nm.AddComment("# Next.js node_modules")
	gen = append(gen, nm)
	imports = append(imports, nil)

	patterns := nextSrcsPatterns(args.Dir)
	if len(patterns) == 0 {
		// An empty glob() does not just fail its own target: the error is raised
		// while the BUILD file is loading, so every target in the package --
		// node_modules and the dev server included -- becomes undeclared.
		log.Printf("typescript: Next.js detected, but none of %s exist yet, "+
			"so no next_build was generated. Create the route directory and re-run Gazelle.",
			strings.Join(append(nextOwnedDirsWithSlash(), nextRootFiles...), ", "))
		return gen, imports
	}

	nb := rule.NewRule("next_build", "app")
	setGeneratedGlob(args, nb, patterns)
	if config := nextConfigFile(args.Dir); config != "" {
		nb.SetAttr("config", config)
	}
	nb.SetAttr("node_modules", ":"+nodeModulesName)
	if staging := stagingLabelsOutside(args.Dir, tc, func(rel string) bool {
		return nextOwnsDir(rel, tc)
	}); len(staging) > 0 {
		nb.SetAttr("staging_srcs", rule.SortedStrings(staging))
	}
	if hasFile(args.Dir, "tsconfig.json") {
		// Without it Next.js type-checks against its own defaults, so the
		// project's path aliases and strictness silently stop applying.
		nb.SetAttr("tsconfig", "tsconfig.json")
	}
	nb.AddComment("# Next.js application build")
	nb.AddComment("# srcs is the declaration: a file that is not staged does not resolve.")
	gen = append(gen, nb)
	imports = append(imports, nil)

	dev := rule.NewRule("next_dev_server", "dev")
	dev.SetAttr("node_modules", ":"+nodeModulesName)
	dev.AddComment("# `bazel run //:dev` runs `next dev` over the source tree.")
	gen = append(gen, dev)
	imports = append(imports, nil)

	return gen, imports
}
