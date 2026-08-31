package typescript

import (
	"log"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/bazelbuild/bazel-gazelle/language"
	"github.com/bazelbuild/bazel-gazelle/rule"

	"github.com/mikn/rules_typescript/gazelle/remix"
	"github.com/mikn/rules_typescript/gazelle/tanstack"
)

// globExpr is a sentinel prefix used in CodegenPattern.Srcs to indicate that
// the entry should be emitted as a Bazel glob() expression rather than a plain
// string. The value is stripped before the glob is rendered.
const globExprPrefix = "glob("

// builtinExcludeDirs is the set of directory basenames that are always
// excluded from Gazelle TypeScript rule generation. These are framework and
// toolchain output directories that should never be scanned for sources.
var builtinExcludeDirs = map[string]bool{
	".next":        true,
	".nuxt":        true,
	".svelte-kit":  true,
	"dist":         true,
	"build":        true,
	"node_modules": true,
}

// ---- file classification ---------------------------------------------------

// isTypeScriptFile returns true for .ts and .tsx source files.
func isTypeScriptFile(name string) bool {
	return strings.HasSuffix(name, ".ts") || strings.HasSuffix(name, ".tsx")
}

// isCSSFile returns true for .css source files (including .module.css).
func isCSSFile(name string) bool {
	return strings.HasSuffix(name, ".css")
}

// isCSSModuleFile returns true for CSS Module files (*.module.css).
// These are handled by the css_module rule rather than css_library.
func isCSSModuleFile(name string) bool {
	return strings.HasSuffix(name, ".module.css")
}

// isAssetFile returns true for static asset files that should be handled by
// asset_library (images, SVGs, fonts, text). NOTE: .json files are NOT included
// here; they are handled by json_library (see isJSONFile).
//
// .jsonc is an asset and not JSON here: no bundler parses that extension as
// JSON, so the import yields a URL rather than a value.
func isAssetFile(name string) bool {
	ext := strings.ToLower(path.Ext(name))
	switch ext {
	case ".svg", ".png", ".jpg", ".jpeg", ".gif", ".webp",
		".woff", ".woff2", ".ttf", ".eot",
		".md", ".txt", ".jsonc":
		return true
	}
	return false
}

// isJSONFile returns true for .json files that should be handled by
// json_library (generates a fully-typed .d.ts, not `unknown`).
func isJSONFile(name string) bool {
	return strings.ToLower(path.Ext(name)) == ".json"
}

// isAmbientDeclaration returns true for a .d.ts that declares globals rather
// than exporting a module. Nothing can import one, so srcs membership is the
// only way it reaches a program.
func isAmbientDeclaration(dir, name string) bool {
	if !strings.HasSuffix(name, ".d.ts") {
		return false
	}
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return false
	}
	return !hasModuleSyntax(string(data))
}

// isTestFile returns true for files that should be compiled as test targets.
// Patterns: *.test.ts, *.test.tsx, *.spec.ts, *.spec.tsx
func isTestFile(name string) bool {
	base := strings.TrimSuffix(strings.TrimSuffix(name, ".tsx"), ".ts")
	return strings.HasSuffix(base, ".test") || strings.HasSuffix(base, ".spec")
}

// isDocFile returns true for files that document or demonstrate the package.
// Patterns: *.doc.ts, *.doc.tsx, *.stories.ts, *.stories.tsx
func isDocFile(name string) bool {
	base := strings.TrimSuffix(strings.TrimSuffix(name, ".tsx"), ".ts")
	return strings.HasSuffix(base, ".doc") || strings.HasSuffix(base, ".stories")
}

// isFrameworkGeneratedFile reports whether the build writes name itself. Only
// the TanStack Start route tree does: the Start Vite plugin regenerates it into
// the staging tree on every bundle, and the tree it emits imports the router
// module that imports the tree back, which is a cycle between two Bazel
// packages however the two are split.
//
// A checked-in file no rule declares as an output is an ordinary source however
// it is named -- see claimedSrcs, which is what defers to ts_codegen. A
// workspace that wants one out of its source targets anyway names it in
// # gazelle:ts_exclude (isConfiguredExclude).
func isFrameworkGeneratedFile(name string) bool {
	base := strings.TrimSuffix(strings.TrimSuffix(name, ".tsx"), ".ts")
	return base == "routeTree.gen"
}

// isConfiguredExclude returns true when a file's basename matches any of the
// exclude patterns configured in gazelle_ts.json. Patterns use
// filepath.Match semantics.
func isConfiguredExclude(name string, patterns []string) bool {
	for _, pattern := range patterns {
		matched, err := filepath.Match(pattern, name)
		if err == nil && matched {
			return true
		}
	}
	return false
}

// isExcludedDir returns true when the given directory basename should be
// excluded from Gazelle TypeScript rule generation. Checks both the
// built-in exclude set and any additional dirs from the configuration.
func isExcludedDir(basename string, additionalDirs []string) bool {
	if builtinExcludeDirs[basename] {
		return true
	}
	for _, d := range additionalDirs {
		if d == basename {
			return true
		}
	}
	return false
}

// isIndexFile returns true for files that define a package public API.
func isIndexFile(name string) bool {
	// Base, not the whole string: a rolled-up src reaches here as the path
	// src/index.ts, and it is as much an index file as index.ts is.
	base := path.Base(name)
	return base == "index.ts" || base == "index.tsx"
}

// ---- app entry point detection ---------------------------------------------

// appEntryFileNames is the ordered list of TypeScript file names that indicate
// an application entry point suitable for a ts_dev_server target.
// The first match wins.
var appEntryFileNames = []string{
	"main.tsx",
	"main.ts",
	"app.tsx",
	"app.ts",
}

// isAppEntryPoint returns true when the given file is a known application entry
// point. This is used to decide whether to generate a ts_dev_server target.
func isAppEntryPoint(name string) bool {
	lower := strings.ToLower(name)
	for _, n := range appEntryFileNames {
		if lower == n {
			return true
		}
	}
	return false
}

// detectAppEntryPoint scans srcFiles for a known app entry point file.
// Returns the matched source file name and true if one is found.
func detectAppEntryPoint(srcFiles []string) (string, bool) {
	for _, want := range appEntryFileNames {
		for _, f := range srcFiles {
			if strings.ToLower(f) == want {
				return f, true
			}
		}
	}
	return "", false
}

// hasIndexHTML returns true when the directory contains an index.html file,
// which is a strong signal that this is an application package.
func hasIndexHTML(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "index.html"))
	return err == nil
}

// ---- generate entry point --------------------------------------------------

// generateRules is the core generation logic invoked by tsLang.GenerateRules.
func generateRules(args language.GenerateArgs) language.GenerateResult {
	tc := getConfig(args.Config)

	// If this directory is explicitly ignored, emit empty rules to delete any
	// stale targets that might have been left from a previous run.
	if tc.ignore {
		return emptyResult(args)
	}

	// SvelteKit's route tree is the framework's own, not a TypeScript package.
	if svelteKitOwnsDir(args.Rel, tc) {
		warnSvelteKitSrcPackage(args)
		return emptyResult(args)
	}

	// Next.js's route trees likewise: next_build stages them by glob, and a
	// BUILD file inside one would make a subpackage the glob cannot see into.
	if nextOwnsDir(args.Rel, tc) {
		warnNextOwnedPackage(args)
		return emptyResult(args)
	}

	// Collect TypeScript, CSS, and asset source files from the regular files list.
	var (
		srcFiles       []string // non-test, non-generated .ts/.tsx files
		testFiles      []string // *.test.ts, *.spec.ts, etc.
		docFiles       []string // *.doc.tsx, *.stories.tsx, etc.
		cssFiles       []string // plain .css source files (side-effect imports)
		cssModuleFiles []string // *.module.css files (default import → typed styles)
		assetFiles     []string // image/font/svg asset files (NOT json)
		jsonFiles      []string // .json data files → json_library (typed .d.ts)
		excludedSrcs   []string // .ts/.tsx a ts_exclude directive dropped
		ambientFiles   []string // .d.ts declaring globals, which only srcs membership carries
		hasIndex       bool
	)

	for _, f := range args.RegularFiles {
		// Skip well-known config files before the JSON check so that Bazel/npm
		// config files are never classified as json_library sources.
		if f == "package.json" || f == "gazelle_ts.json" || f == "tsconfig.json" {
			continue
		}
		if _, ok := srcLabel(f); !ok {
			reportUnlabelableFile(args, f)
			continue
		}
		if isJSONFile(f) {
			jsonFiles = append(jsonFiles, f)
			continue
		}
		if isAssetFile(f) {
			assetFiles = append(assetFiles, f)
			continue
		}
		if isCSSFile(f) {
			if isCSSModuleFile(f) {
				cssModuleFiles = append(cssModuleFiles, f)
			} else {
				cssFiles = append(cssFiles, f)
			}
			continue
		}
		if !isTypeScriptFile(f) {
			continue
		}
		if isFrameworkGeneratedFile(f) {
			continue
		}
		if isConfiguredExclude(f, tc.excludePatterns) {
			excludedSrcs = append(excludedSrcs, f)
			continue
		}
		if nextOwnsFile(args.Rel, f, tc) {
			continue
		}
		if isAmbientDeclaration(args.Dir, f) {
			ambientFiles = append(ambientFiles, f)
			continue
		}
		if isTestFile(f) {
			testFiles = append(testFiles, f)
			continue
		}
		if isDocFile(f) {
			docFiles = append(docFiles, f)
			continue
		}
		srcFiles = append(srcFiles, f)
		if isIndexFile(f) {
			hasIndex = true
		}
	}

	// Read before the claim below: what a generator declares is an output of
	// this package, and a file cannot be both that and a source of it.
	codegenPatterns := detectCodegen(args.Rel, args.RegularFiles, tc)

	// Two targets over one source declare the same .js and .d.ts, which Bazel
	// rejects as conflicting actions rather than tolerating as a duplicate.
	srcsBeforeClaim := append([]string(nil), srcFiles...)
	if claimed := claimedSrcs(args, tc, codegenPatterns); len(claimed) > 0 {
		srcFiles = dropClaimed(srcFiles, claimed)
		testFiles = dropClaimed(testFiles, claimed)
		docFiles = dropClaimed(docFiles, claimed)
		ambientFiles = dropClaimed(ambientFiles, claimed)
		cssFiles = dropClaimed(cssFiles, claimed)
		cssModuleFiles = dropClaimed(cssModuleFiles, claimed)
		assetFiles = dropClaimed(assetFiles, claimed)
		jsonFiles = dropClaimed(jsonFiles, claimed)
		hasIndex = false
		for _, f := range srcFiles {
			if isIndexFile(f) {
				hasIndex = true
			}
		}
	}

	srcFiles = append(srcFiles, ambientFiles...)
	sort.Strings(srcFiles)

	// Also check GenFiles: a generated index file counts as a boundary only
	// when there are regular source files present too. Without regular source
	// files the generated index alone would cause an empty ts_compile cleanup
	// rule to be emitted in every directory that has a generated index.
	if len(srcFiles) > 0 {
		for _, f := range args.GenFiles {
			if isTypeScriptFile(f) && isIndexFile(f) {
				hasIndex = true
			}
		}
	}

	// Determine whether this directory is a package boundary.
	//
	// every-dir mode (default): every directory with .ts files is a boundary.
	// index-only mode: only dirs with index.ts/tsx, an explicit
	//   # gazelle:ts_package_boundary directive, or the repo root.
	var isBoundary bool
	switch tc.packageBoundaryMode {
	case boundaryIndexOnly:
		// Old behaviour: require index.ts or explicit directive.
		isBoundary = tc.packageBoundary || hasIndex || args.Rel == ""
	case boundaryTsConfig:
		// One target per TypeScript project, which is the directory holding
		// the tsconfig that names the sources.
		isBoundary = tc.packageBoundary || args.Rel == "" || dirHasTsConfig(args.Dir)
	default: // boundaryEveryDir
		// New default: any directory with .ts files (or the repo root) is a boundary.
		isBoundary = len(srcFiles) > 0 || hasIndex || args.Rel == "" || tc.packageBoundary
	}

	// In index-only mode a plain subdirectory is not a package, so its files
	// belong to this target rather than to one of their own. Rolling them up is
	// what keeps an ordinary shape -- a barrel re-exporting ./rules, and ./rules
	// importing ../utils -- from becoming a cycle between two Bazel packages
	// when at file granularity there is no cycle.
	//
	// A directory that is not a boundary claims nothing at all, so no BUILD file
	// appears in it to make those rolled-up labels cross a package boundary.
	if tc.packageBoundaryMode != boundaryEveryDir {
		if !isBoundary {
			return language.GenerateResult{}
		}
		rolled := rolledUpIn(tc.packageBoundaryMode, args.Dir, tc.excludePatterns)
		// Every kind, not only the TypeScript ones: a declared out that is also
		// checked in below the boundary would otherwise be a source and an
		// output of the same package, whatever kind of file it is.
		if claimed := claimedSrcs(args, tc, codegenPatterns); len(claimed) > 0 {
			rolled.srcs = dropClaimed(rolled.srcs, claimed)
			rolled.tests = dropClaimed(rolled.tests, claimed)
			rolled.docs = dropClaimed(rolled.docs, claimed)
			rolled.ambient = dropClaimed(rolled.ambient, claimed)
			rolled.css = dropClaimed(rolled.css, claimed)
			rolled.cssModules = dropClaimed(rolled.cssModules, claimed)
			rolled.assets = dropClaimed(rolled.assets, claimed)
			rolled.json = dropClaimed(rolled.json, claimed)
		}
		srcFiles = append(srcFiles, rolled.srcs...)
		testFiles = append(testFiles, rolled.tests...)
		docFiles = append(docFiles, rolled.docs...)
		ambientFiles = append(ambientFiles, rolled.ambient...)
		cssFiles = append(cssFiles, rolled.css...)
		cssModuleFiles = append(cssModuleFiles, rolled.cssModules...)
		assetFiles = append(assetFiles, rolled.assets...)
		jsonFiles = append(jsonFiles, rolled.json...)
		sort.Strings(srcFiles)
		sort.Strings(testFiles)
		sort.Strings(docFiles)
	}

	// A generator that named no srcs reads the sources of the target it sits
	// beside: the post-claim list, so its own out is never fed back into it,
	// and post-roll-up, so a mode where one target covers a subtree hands the
	// generator the subtree rather than the one directory the BUILD file is in.
	for i := range codegenPatterns {
		if len(codegenPatterns[i].Srcs) == 0 {
			codegenPatterns[i].Srcs = append([]string(nil), srcFiles...)
		}
	}

	// Read before the guard below: a directory holding nothing but package.json
	// and tsconfig.json -- the standard pnpm workspace-member shape -- classifies
	// no source at all, and returning early there is what would leave the label
	// its subpackages name pointing at a package nothing writes.
	tsConfigRule := ownTsConfigRule(args, tc)

	// A tsconfig.json that has been deleted or moved leaves the ts_config behind,
	// and this directory may hold nothing else for a later run to notice it by.
	var tsConfigEmpty []*rule.Rule
	if tsConfigRule == nil && ruleExists(args, "ts_config", tsConfigTargetName) {
		tsConfigEmpty = append(tsConfigEmpty, rule.NewRule("ts_config", tsConfigTargetName))
	}

	totalNonTS := len(cssFiles) + len(cssModuleFiles) + len(assetFiles) + len(jsonFiles)
	if !isBoundary && len(srcFiles) == 0 && len(testFiles) == 0 && len(docFiles) == 0 &&
		totalNonTS == 0 && len(codegenPatterns) == 0 && tsConfigRule == nil {
		// No TypeScript, CSS, asset, or JSON files and not a boundary: nothing to do.
		// A declared generator is a target of its own, though: a package whose
		// sources are all generated holds nothing else.
		return language.GenerateResult{Empty: tsConfigEmpty}
	}

	var gen []*rule.Rule
	var empty []*rule.Rule
	var imports []any

	// The framework client entry gets its own single-file target, so it leaves
	// the directory-wide one. Only a second compile action is the problem, so
	// srcsWithEntry -- the package as it is on disk -- is what everything else
	// still reads: the staged sources, the linter, and the sibling test's tree.
	srcsWithEntry := srcFiles
	entryFile, entryName, hasEntry := frameworkEntrySrc(args.Rel, tc, srcFiles)
	if hasEntry && entryName == targetNameForDir(tc, args.Rel) {
		hasEntry = reportEntryNameCollision(args, tc, entryName)
	}
	if hasEntry {
		srcFiles = dropClaimed(srcFiles, map[string]struct{}{entryFile: {}})
	} else if claimed, _, ok := frameworkEntrySrc(args.Rel, tc, srcsBeforeClaim); ok {
		// A target that is not Gazelle's compiles the entry, and the staged tree
		// still needs the file itself.
		srcsWithEntry = append(append([]string(nil), srcFiles...), claimed)
	} else {
		reportHandMaintainedEntry(args, tc, excludedSrcs)
	}
	sort.Strings(srcsWithEntry)

	// Assigned up front so the names cannot collide with each other or with
	// the TypeScript targets below.
	reserved := reservedTSTargetNames(tc, args.Rel)
	if hasEntry {
		reserved[entryName] = struct{}{}
	}
	if tsConfigRule != nil {
		reserved[tsConfigRule.Name()] = struct{}{}
	}
	for _, name := range codegenTargetNames(codegenPatterns) {
		reserved[name] = struct{}{}
	}
	libNames := assetTargetNames(reserved, cssFiles, cssModuleFiles, assetFiles, jsonFiles)

	// Resolved once, and only where a target would carry it: a refusal is worth
	// one log line per package that wanted the baseline, not one per directory.
	tsConfigAttr := ""
	if (isBoundary && len(srcFiles) > 0) || len(testFiles) > 0 || len(docFiles) > 0 || hasEntry {
		tsConfigAttr = tsConfigLabel(args, tc)
	}

	// ---- css_library targets -----------------------------------------------
	// Generate one css_library rule per plain .css file (side-effect imports).

	sort.Strings(cssFiles)
	for _, f := range cssFiles {
		r := rule.NewRule("css_library", libNames[f])
		r.SetAttr("srcs", srcLabels([]string{f}))
		r.SetAttr("visibility", []string{"//visibility:public"})
		gen = append(gen, r)
		// css_library targets are indexed by their workspace-relative CSS path
		// so that resolveImports can look them up when a .ts file imports a
		// .css file side-effect style (import "./foo.css").
		imports = append(imports, []string{})
	}

	// ---- css_module targets ------------------------------------------------
	// Generate one css_module rule per *.module.css file (default imports).

	sort.Strings(cssModuleFiles)
	for _, f := range cssModuleFiles {
		r := rule.NewRule("css_module", libNames[f])
		r.SetAttr("srcs", srcLabels([]string{f}))
		r.SetAttr("visibility", []string{"//visibility:public"})
		gen = append(gen, r)
		// css_module targets are indexed by their workspace-relative CSS path
		// so that resolveImports can resolve default imports from .module.css.
		imports = append(imports, []string{})
	}

	// ---- asset_library targets ---------------------------------------------
	// Generate one asset_library rule per image/font/SVG file.

	sort.Strings(assetFiles)
	for _, f := range assetFiles {
		r := rule.NewRule("asset_library", libNames[f])
		r.SetAttr("srcs", srcLabels([]string{f}))
		r.SetAttr("visibility", []string{"//visibility:public"})
		gen = append(gen, r)
		// asset_library targets are indexed by their workspace-relative asset
		// path for import resolution.
		imports = append(imports, []string{})
	}

	// ---- json_library targets ----------------------------------------------
	// Generate one json_library rule per .json file (typed declarations).

	sort.Strings(jsonFiles)
	for _, f := range jsonFiles {
		r := rule.NewRule("json_library", libNames[f])
		r.SetAttr("srcs", srcLabels([]string{f}))
		r.SetAttr("visibility", []string{"//visibility:public"})
		gen = append(gen, r)
		// json_library targets are indexed by their workspace-relative JSON
		// path for import resolution.
		imports = append(imports, []string{})
	}

	// ---- primary ts_compile target -----------------------------------------

	if isBoundary && len(srcFiles) > 0 {
		name := targetNameForDir(tc, args.Rel)
		r := rule.NewRule("ts_compile", name)

		sort.Strings(srcFiles)
		r.SetAttr("srcs", srcLabels(srcFiles))
		r.SetAttr("visibility", []string{"//visibility:public"})

		// Only emit the attribute when it differs from the rule default.
		if tc.declarations != "" && tc.declarations != "tsgo" {
			r.SetAttr("declarations", tc.declarations)
		}

		setTsConfig(r, tsConfigAttr)

		// Collect imports for all src files.
		allImports := importsIn(args.Dir, srcFiles)

		if hasEntry {
			reportEntryImportCycle(args, tc, entryFile, entryName, srcFiles, allImports)
		}

		// Aliases let tsgo resolve source-level specifiers like "@/components".
		// One tsconfig `paths` map serves a whole workspace, so a target takes
		// only the entries it can carry -- see usedPathAliases.
		setPathAliases(args, r, usedPathAliases(tc, args.Rel, srcFiles, allImports))

		gen = append(gen, r)
		imports = append(imports, uniqueImports(allImports))

		// ---- ts_lint target (alongside ts_compile when linter is detected) --
		// When an eslint or oxlint config exists in this directory or any
		// ancestor, generate a ts_lint target. The rule name is "<name>_lint".
		// The linter_binary label follows the @npm//:oxlint_bin convention.
		if tc.linterConfig != "" && tc.linterType != "" {
			lintName := name + "_lint"
			lr := rule.NewRule("ts_lint", lintName)
			lr.SetAttr("srcs", srcLabels(srcsWithEntry))
			lr.SetAttr("linter", tc.linterType)
			if binLabel := linterBinaryLabel(tc.linterType); binLabel != "" {
				lr.SetAttr("linter_binary", binLabel)
			}
			if cfgLabel := linterConfigLabel(tc.linterConfig); cfgLabel != "" {
				lr.SetAttr("config", cfgLabel)
			}
			gen = append(gen, lr)
			// ts_lint has no import resolution needs; placeholder nil keeps
			// len(gen) == len(imports) invariant.
			imports = append(imports, nil)
		}
	} else if isBoundary && len(srcFiles) == 0 {
		// Boundary directory with no source files: emit an empty rule to clean
		// up any stale ts_compile target.
		name := targetNameForDir(tc, args.Rel)
		empty = append(empty, rule.NewRule("ts_compile", name))
		// Clean up any stale ts_lint target too.
		if args.File != nil {
			lintName := name + "_lint"
			for _, existingRule := range args.File.Rules {
				if existingRule.Name() == lintName && existingRule.Kind() == "ts_lint" {
					empty = append(empty, rule.NewRule("ts_lint", lintName))
					break
				}
			}
		}
	}

	// ---- framework client entry target -------------------------------------
	// The single-file ts_compile the generated ts_bundle's entry_point names.

	// Re-emitted on every run: srcs and deps are mergeable, and emitting the
	// candidate is what keeps a single-file target following its own imports.
	if hasEntry {
		entryImports := importsIn(args.Dir, []string{entryFile})
		r := frameworkEntryRule(entryName, entryFile, ambientFiles, tc)
		setTsConfig(r, tsConfigAttr)
		setPathAliases(args, r, usedPathAliases(tc, args.Rel, []string{entryFile}, entryImports))
		gen = append(gen, r)
		imports = append(imports, uniqueImports(entryImports))
	} else if name, ok := frameworkEntryTargetName(args.Rel, tc); ok &&
		ruleExists(args, "ts_compile", name) && !frameworkEntryFileExists(args.RegularFiles, name) {
		// The entry was renamed or moved away, so the rule left behind names a
		// source that is gone -- which fails `bazel build //...` outright.
		empty = append(empty, rule.NewRule("ts_compile", name))
	}

	// ---- ts_dev_server target (app packages only) --------------------------
	// Generate a ts_dev_server target when this directory looks like an
	// application entry point. Detection heuristics (in priority order):
	//   1. One of the source files is a known entry file (main.tsx, main.ts,
	//      app.tsx, app.ts).
	//   2. The directory contains an index.html (strong signal for Vite apps).
	//
	// The generated target:
	//   - name: "dev" (conventional name for dev server targets)
	//   - entry_point: the primary ts_compile target for this directory
	//   - node_modules: ":node_modules" when a node_modules rule is already
	//     generated (or exists in the build file) — omitted otherwise.
	//
	// We only generate the target once: if a ts_dev_server named "dev" already
	// exists in the build file with a non-empty entry_point attr, we leave it
	// alone (Gazelle's merge strategy handles updates to other attrs).

	if isBoundary && len(srcFiles) > 0 {
		_, hasAppEntry := detectAppEntryPoint(srcsWithEntry)
		hasHTML := hasIndexHTML(args.Dir)
		if (hasAppEntry || hasHTML) && !reportUnsupportedDevServer(tc.detectedFramework) {
			libName := targetNameForDir(tc, args.Rel)
			devName := "dev"

			// Only generate if there is no existing ts_dev_server rule named
			// "dev" in the current build file. Gazelle's merge will update
			// attrs on the existing rule, so we only generate when absent.
			existingDevServer := false
			if args.File != nil {
				for _, existingRule := range args.File.Rules {
					if existingRule.Name() == devName && existingRule.Kind() == "ts_dev_server" {
						existingDevServer = true
						break
					}
				}
			}

			if !existingDevServer {
				devR := rule.NewRule("ts_dev_server", devName)
				devR.SetAttr("entry_point", ":"+libName)
				devR.SetAttr("port", 5173)
				// Wire the Bazel-aware Vite plugin by default so that ibazel
				// triggers component-level HMR updates instead of full-page
				// reloads. Consumers can remove this attr if they do not use Vite.
				devR.SetAttr("plugin", "@rules_typescript//vite:vite_plugin_bazel")
				devR.SetAttr("visibility", []string{"//visibility:public"})
				gen = append(gen, devR)
				// ts_dev_server has no import resolution needs.
				imports = append(imports, nil)
			}
		}
	}

	// The framework bundle owns node_modules(name = "node_modules") at the
	// workspace root; the stale-rule cleanup below must not delete it.
	frameworkOwnsNodeModules := args.Rel == "" && tc.detectedFramework != FrameworkNone

	// ---- ts_test targets ---------------------------------------------------

	if len(testFiles) > 0 {
		testSrcs := append(append([]string(nil), testFiles...), ambientFiles...)
		sort.Strings(testSrcs)
		sort.Strings(testFiles)

		// Collect all imports from test files for dep resolution.
		allImports := importsIn(args.Dir, testFiles)

		// Also collect npm imports from production source files in this package.
		// ts_test auto-generates a node_modules tree from its own @npm// deps, so
		// the deps list must include ALL npm packages the tests need at runtime —
		// both what the test files directly import AND what the production code in
		// this package imports.  Without the production imports, the auto-generated
		// node_modules tree would be missing packages needed by the SUT.
		//
		// The doc files too: a test that composes a story runs the story's npm
		// imports, which left this package's sources when the doc target did.
		var allPackageImports []string
		allPackageImports = append(allPackageImports, allImports...)
		allPackageImports = append(allPackageImports, importsIn(args.Dir, srcsWithEntry)...)
		allPackageImports = append(allPackageImports, importsIn(args.Dir, docFiles)...)

		name := testTargetName(targetNameForDir(tc, args.Rel))

		r := rule.NewRule("ts_test", name)
		r.SetAttr("srcs", srcLabels(testSrcs))

		// A vitest config beside the tests names the pool, the environment and
		// the deps to inline. Dropped, the tests run in plain Node: a worker's
		// `defineWorkersConfig` pool becomes no pool, and a dependency that only
		// resolves through Vite fails at import time.
		if cfg := vitestConfigIn(args.Dir); cfg != "" {
			r.SetAttr("config", cfg)
			// The config is a module the runner imports, so what it imports is a
			// dep of the test like any other. `defineWorkersConfig` comes from the
			// pool package, and without it the runner dies before the first test.
			allPackageImports = append(allPackageImports, importsIn(args.Dir, []string{cfg})...)
		}

		// Same emitter as the ts_compile targets in this package, so the internal
		// ts_compile inside ts_test does not disagree with its siblings.
		if tc.declarations != "" && tc.declarations != "tsgo" {
			r.SetAttr("declarations", tc.declarations)
		}

		setTsConfig(r, tsConfigAttr)

		// ts_test auto-builds a node_modules tree from its @npm// deps, so no
		// explicit node_modules rule is generated. The ts_test macro filters deps
		// by @npm// label convention and creates an internal _<name>_node_modules
		// target automatically.
		//
		// Pass allPackageImports (test + production imports) to the resolver so
		// that the generated deps list includes npm packages from production code.

		gen = append(gen, r)
		imports = append(imports, uniqueImports(allPackageImports))
	} else {
		// No test files: only emit cleanup stubs when the stale rules already
		// exist in the current build file. Emitting empty rules unconditionally
		// would cause Gazelle to attempt to delete targets in every directory,
		// even those that never had them.
		if args.File != nil {
			wantName := testTargetName(targetNameForDir(tc, args.Rel))
			hadTestTarget := false
			for _, r := range args.File.Rules {
				if r.Name() == wantName && r.Kind() == "ts_test" {
					hadTestTarget = true
					empty = append(empty, rule.NewRule("ts_test", wantName))
				}
			}
			// Only remove a node_modules(name="node_modules") rule when a ts_test
			// target was also being deleted. This prevents Gazelle from deleting
			// user-managed Vite node_modules targets at the workspace root or in
			// packages that never had ts_test.
			if hadTestTarget && !frameworkOwnsNodeModules {
				for _, r := range args.File.Rules {
					if r.Name() == "node_modules" && r.Kind() == "node_modules" {
						empty = append(empty, rule.NewRule("node_modules", "node_modules"))
						break
					}
				}
			}
		}
	}

	// Clean up any stale node_modules rules left from before ts_test auto-generation.
	// When test files are present, Gazelle no longer emits standalone node_modules rules,
	// so any existing one should be removed. We emit an empty stub to trigger deletion.
	//
	// Exception: if any ts_test rule in this BUILD file has an explicit node_modules
	// attr set, the user is managing node_modules manually and we must not delete it.
	if len(testFiles) > 0 && args.File != nil && !frameworkOwnsNodeModules {
		hasManualNodeModules := false
		for _, existingRule := range args.File.Rules {
			if existingRule.Kind() == "ts_test" && existingRule.Attr("node_modules") != nil {
				hasManualNodeModules = true
				break
			}
		}
		if !hasManualNodeModules {
			for _, r := range args.File.Rules {
				if r.Name() == "node_modules" && r.Kind() == "node_modules" {
					empty = append(empty, rule.NewRule("node_modules", "node_modules"))
					break
				}
			}
		}
	}

	// ---- doc target --------------------------------------------------------
	// A doc file consumes the package rather than belonging to it, so it compiles
	// on its own -- as a ts_compile, since unlike a test there is nothing to run.
	// In the package target, two components demonstrating each other are a cycle
	// between their directories although neither component depends on the other.

	docName := docTargetName(targetNameForDir(tc, args.Rel))
	if len(docFiles) > 0 {
		sort.Strings(docFiles)

		// Nothing imports an ambient .d.ts, so no dep edge carries one into this
		// program: a story reaching a global declared beside it needs it in srcs.
		docSrcs := append(append([]string(nil), docFiles...), ambientFiles...)
		sort.Strings(docSrcs)

		r := rule.NewRule("ts_compile", docName)
		r.SetAttr("srcs", docSrcs)
		r.SetAttr("visibility", []string{"//visibility:public"})
		if tc.declarations != "" && tc.declarations != "tsgo" {
			r.SetAttr("declarations", tc.declarations)
		}

		// A story is TypeScript in this package: it needs the package's own lib,
		// types and strictness for the same reason its sources do, and the same
		// label they name, so a refusal refuses for all of them at once.
		setTsConfig(r, tsConfigAttr)

		docImports := importsIn(args.Dir, docFiles)
		if used := usedPathAliases(tc, args.Rel, docSrcs, docImports); len(used) > 0 {
			r.SetAttr("path_aliases", used)
		}

		gen = append(gen, r)
		imports = append(imports, uniqueImports(docImports))
	} else if ruleExists(args, "ts_compile", docName) {
		empty = append(empty, rule.NewRule("ts_compile", docName))
	}

	// ---- ts_codegen targets ------------------------------------------------
	// The patterns read above: known tools (Prisma, GraphQL Codegen, OpenAPI)
	// plus whatever # gazelle:ts_codegen directives declared here.
	for _, p := range codegenPatterns {
		r := buildCodegenRule(p)
		if r == nil {
			log.Printf("typescript: ts_codegen %q in %q generates nothing: it names no srcs, and the directory has no TypeScript sources to default to. Give the directive a srcs: field naming the generator's inputs.",
				p.Name, args.Rel)
			continue
		}
		gen = append(gen, r)
		// ts_codegen targets have no import resolution needs.
		imports = append(imports, nil)

		if compile, ok := codegenCompileName(p); ok {
			gen = append(gen, buildCodegenCompileRule(compile, p.Name))
			// Nothing to read imports from: the sources do not exist yet.
			imports = append(imports, nil)
		}
	}

	// Emit empty stubs for ts_codegen targets that no longer have a matching
	// pattern but still exist in the current BUILD file. This allows Gazelle
	// to clean up stale auto-generated ts_codegen rules when the trigger files
	// are removed (e.g. schema.prisma deleted).
	//
	// Only the names the built-in detectors use: `outs` is mergeable, so an
	// empty rule strips it from whatever it matches, hand-written or not.
	if args.File != nil {
		generatedNames := make(map[string]bool, len(codegenPatterns))
		for _, p := range codegenPatterns {
			generatedNames[p.Name] = true
		}
		for _, existingRule := range args.File.Rules {
			if existingRule.Kind() != "ts_codegen" {
				continue
			}
			if generatedNames[existingRule.Name()] || !detectorCodegenNames[existingRule.Name()] {
				continue
			}
			empty = append(empty, rule.NewRule("ts_codegen", existingRule.Name()))
			stale := CodegenPattern{Name: existingRule.Name(), Outs: existingRule.AttrStrings("outs")}
			if compile, ok := codegenCompileName(stale); ok {
				empty = append(empty, rule.NewRule("ts_compile", compile))
			}
		}
	}

	// ---- hermetic pnpm targets (root package only) -------------------------
	// Generate :pnpm and :add_package macro invocations at the workspace root.
	// These targets let consumers run `bazel run //:pnpm -- add <pkg>` without
	// requiring a system-level pnpm installation.
	//
	// We only generate these when a pnpm-lock.yaml exists in the workspace root
	// (strong signal that this is a pnpm project).
	if args.Rel == "" {
		pnpmRules, pnpmImports := generatePnpmTargets(args)
		gen = append(gen, pnpmRules...)
		imports = append(imports, pnpmImports...)
	}

	// ---- framework bundle targets (root package only) ---------------------
	// When we are at the workspace root and a framework is detected, generate
	// the framework-appropriate bundle targets:
	//   - Vite-based frameworks: node_modules, vite_bundler, ts_bundle
	//   - Next.js: node_modules, next_build
	// These targets are only emitted at the root; sub-packages handle their
	// own ts_compile targets via the normal path above.
	if args.Rel == "" && tc.detectedFramework != FrameworkNone {
		bundleRules, bundleImports, bundleEmpty := generateFrameworkBundle(args, tc)
		gen = append(gen, bundleRules...)
		imports = append(imports, bundleImports...)
		empty = append(empty, bundleEmpty...)
		reportMissingFrameworkEntry(args, tc)
	}

	// ---- filegroup "sources" for framework staging_srcs --------------------
	// When a framework is detected and this directory is one of the stage dirs,
	// generate a "sources" filegroup that exports all non-test .ts/.tsx files.
	// The root ts_bundle staging_srcs references these filegroups via labels
	// like //src/routes:sources.
	//
	// The directory listing, not srcFiles: the framework's plugin reads the
	// route or entry off the staging tree by name, so ts_exclude and a sibling
	// target's claim -- which decide what compiles here, not what exists --
	// must not take a file out of the staged copy.
	//
	// Re-emitted even when the rule is already in the BUILD file: filegroup.srcs
	// is mergeable, and skipping the existing rule freezes the staged tree at
	// what the first run saw, so a route added later never reaches the bundle.
	if args.Rel != "" && tc.detectedFramework != FrameworkNone && isStagedDir(args.Rel, tc) {
		if fg := generateSourcesFilegroup(stagedSources(args.RegularFiles)); fg != nil {
			gen = append(gen, fg)
			imports = append(imports, nil)
		}
	}

	// ---- ts_config for this package's own tsconfig.json --------------------
	// The baseline every target at or below this directory names, and a source
	// file only becomes a label another package can reach through a target.
	if tsConfigRule != nil {
		gen = append(gen, tsConfigRule)
		imports = append(imports, nil)
	}
	empty = append(empty, tsConfigEmpty...)

	result := language.GenerateResult{
		Gen:     gen,
		Empty:   empty,
		Imports: imports,
	}

	// Post-process with the TanStack plugin when the framework is detected.
	// The plugin adjusts rules for directories inside the routes/ tree:
	// it removes generated files from srcs, annotates rules with route
	// metadata, and adds route pattern comments.
	if tc.detectedFramework == FrameworkTanStack {
		result = tanstack.AdjustGenerateResult(args, result)
	}

	// The Remix plugin does the same for app/: it annotates route targets with
	// the route tree, stages the folder routes the static stage-dir list cannot
	// name, and names the conventions it refuses to apply.
	if tc.detectedFramework == FrameworkRemix {
		result = remix.AdjustGenerateResult(args, result)
	}

	reportManagedAttrDrops(args, result.Gen)

	return result
}

// emptyResult generates empty stubs for all known rule kinds, which causes
// Gazelle to delete them if they exist.
func emptyResult(args language.GenerateArgs) language.GenerateResult {
	tc := getConfig(args.Config)
	name := targetNameForDir(tc, args.Rel)
	return language.GenerateResult{
		Empty: []*rule.Rule{
			rule.NewRule("ts_compile", name),
			rule.NewRule("ts_test", testTargetName(name)),
			rule.NewRule("ts_lint", name+"_lint"),
			rule.NewRule("ts_dev_server", "dev"),
			rule.NewRule("node_modules", "node_modules"),
			rule.NewRule("ts_config", tsConfigTargetName),
		},
	}
}

// generatePnpmTargets generates :pnpm and :add_package macro invocations at
// the workspace root when a pnpm-lock.yaml file is detected. That lockfile is
// the hub :add_package edits; the macro has no default for it, because a
// pnpm add with no hub writes a package.json at the workspace root.
//
// Both targets are generated unconditionally once a lockfile is found: they
// are low-cost no-ops if the user never runs them, and essential for the
// "hermetic pnpm" workflow when they do.
//
// Idempotent: if the rules already exist in the BUILD file they are left as-is
// (Gazelle merges existing rules rather than overwriting them).
func generatePnpmTargets(args language.GenerateArgs) ([]*rule.Rule, []any) {
	// Only generate when pnpm-lock.yaml exists at the workspace root.
	lockfilePath := filepath.Join(args.Dir, "pnpm-lock.yaml")
	if _, err := os.Stat(lockfilePath); err != nil {
		// No lockfile: do not generate pnpm targets.
		return nil, nil
	}

	var gen []*rule.Rule
	var imports []any

	if !ruleExists(args, "ts_pnpm", "pnpm") {
		r := rule.NewRule("ts_pnpm", "pnpm")
		gen = append(gen, r)
		imports = append(imports, nil)
	}

	if !ruleExists(args, "ts_add_package", "add_package") {
		r := rule.NewRule("ts_add_package", "add_package")
		r.SetAttr("pnpm_lock", "//:pnpm-lock.yaml")
		gen = append(gen, r)
		imports = append(imports, nil)
	}

	return gen, imports
}

// ---- helper functions ------------------------------------------------------

// detectorCodegenNames mirrors the names the detectors in codegen.go emit.
// Change one there, change it here.
var detectorCodegenNames = map[string]bool{
	"route_tree":    true,
	"prisma_client": true,
	"graphql_types": true,
	"api_types":     true,
}

// compilingKinds declare a per-source output for every src they list.
var compilingKinds = map[string]bool{
	"ts_compile":    true,
	"ts_test":       true,
	"css_library":   true,
	"css_module":    true,
	"asset_library": true,
	"json_library":  true,
}

// claimedSrcs returns the file names Gazelle must keep out of the targets it
// writes: the srcs of the rules in this build file it is not about to write,
// and every ts_codegen out. A glob() srcs expression reads as no names.
//
// The outs matter because a declared output that is also checked in would
// otherwise be a source and an output of the same package, which Bazel rejects
// as a conflicting declaration.
func claimedSrcs(args language.GenerateArgs, tc *tsConfig, patterns []CodegenPattern) map[string]struct{} {
	claimed := make(map[string]struct{})
	for _, out := range codegenOuts(patterns) {
		claimed[out] = struct{}{}
	}
	if args.File == nil {
		return claimed
	}
	ours := reservedTSTargetNames(tc, args.Rel)
	for _, r := range args.File.Rules {
		if r.Kind() == "ts_codegen" {
			for _, out := range r.AttrStrings("outs") {
				claimed[out] = struct{}{}
			}
			continue
		}
		// Gazelle writes neither attribute, so what they name survives the merge
		// and is compiled -- including on the ts_test Gazelle owns.
		if r.Kind() == "ts_test" {
			for _, attr := range []string{"setup_files", "global_setup"} {
				for _, src := range r.AttrStrings(attr) {
					claimed[src] = struct{}{}
				}
			}
		}
		if !compilingKinds[r.Kind()] {
			continue
		}
		if _, mine := ours[r.Name()]; mine {
			continue
		}
		for _, src := range r.AttrStrings("srcs") {
			claimed[src] = struct{}{}
		}
	}
	return claimed
}

func dropClaimed(files []string, claimed map[string]struct{}) []string {
	var kept []string
	for _, f := range files {
		if _, taken := claimed[f]; !taken {
			kept = append(kept, f)
		}
	}
	return kept
}

// usedPathAliases narrows the inherited alias map to the entries this target can
// carry: the ones its own imports resolve through, plus the ones whose directory
// holds its own sources.
//
// The second set carries the alias into the IDE tsconfig, which only learns an
// alias exists from the targets that declare it. It covers directive-declared
// aliases only: one read back out of a generated tsconfig is an echo, not a
// declaration.
func usedPathAliases(tc *tsConfig, rel string, srcs, imports []string) map[string]string {
	if len(tc.pathAliases) == 0 {
		return nil
	}
	used := make(map[string]string)
	for _, imp := range imports {
		if m, ok := matchPathAlias(tc, imp); ok {
			used[m.prefix] = m.dir
		}
	}
	for prefix, dir := range tc.pathAliases {
		if tc.aliasesFromDirectives && aliasCoversSrcs(dir, rel, srcs) {
			used[prefix] = dir
		}
	}
	return used
}

// aliasCoversSrcs mirrors ts_compile's _validate_path_aliases: an alias holds only
// when one of the target's own sources sits at or under its directory.
func aliasCoversSrcs(dir, rel string, srcs []string) bool {
	norm := strings.TrimSuffix(dir, "/")
	if norm == "" {
		return false
	}
	prefix := norm + "/"
	for _, src := range srcs {
		p := path.Join(rel, src)
		if p == norm || strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return false
}

// ---- the compilerOptions baseline ------------------------------------------

// setTsConfig names the compilerOptions baseline for a generated target, so it
// compiles under the package's own lib / types / jsx / strictness instead of
// only the ruleset's defaults.
func setTsConfig(r *rule.Rule, label string) {
	if label != "" {
		r.SetAttr("tsconfig", label)
	}
}

// tsConfigLabel is the label a target generated in args.Rel names for its
// baseline, or "" when there is none it can name.
//
// A tsconfig.json is a source file, so the label that reaches it has to name a
// target in the package that holds it -- and that package exists only if
// Gazelle writes a BUILD file there. Naming one it does not write is a dangling
// label, which fails analysis for the whole workspace and not just for the
// target that named it. That is generationCanStage's question asked about a
// different directory, and it is answered the same way: refuse, and say why.
func tsConfigLabel(args language.GenerateArgs, tc *tsConfig) string {
	if tc.tsConfigFile == "" || isConfiguredExclude("tsconfig.json", tc.excludePatterns) {
		return ""
	}
	dirRel := path.Dir(tc.tsConfigFile)
	if dirRel == "." {
		dirRel = ""
	}
	// This package holds the file, and a rule is being generated here, so the
	// BUILD file that makes the label resolve is the one being written.
	if dirRel == args.Rel {
		return ":" + tsConfigTargetName
	}

	repoRoot := args.Config.RepoRoot
	absDir := filepath.Join(repoRoot, filepath.FromSlash(dirRel))
	local := readLocalPackage(absDir, dirRel, tc)

	if local.ignored {
		log.Printf("typescript: %s holds the tsconfig.json %s would compile under, but a "+
			"ts_ignore directive stops Gazelle writing anything there, so no target names the "+
			"file and %s keeps the ruleset's baseline. Drop the directive, or set tsconfig by "+
			"hand with a \"# keep\" on its line.", dirRel, args.Rel, args.Rel)
		return ""
	}
	if nextOwnsDir(dirRel, tc) || svelteKitOwnsDir(dirRel, tc) {
		log.Printf("typescript: %s detected: %s holds the tsconfig.json %s would compile "+
			"under, but the framework stages that tree by glob and a BUILD file there would "+
			"make a package the glob cannot descend into. %s keeps the ruleset's baseline.",
			frameworkName(tc.detectedFramework), dirRel, args.Rel, args.Rel)
		return ""
	}
	mode, agreed := boundaryModeAt(repoRoot, dirRel, args.Rel, tc.packageBoundaryMode)
	if !agreed {
		log.Printf("typescript: a ts_package_boundary directive between %s and %s leaves the "+
			"two disagreeing about whether %s becomes a package, so %s names no tsconfig "+
			"rather than risk a label into a package nothing writes. Declare the mode once, at "+
			"or above %s.", dirRel, args.Rel, dirRel, args.Rel, dirRel)
		return ""
	}
	if tsConfigPackageCosts(mode, absDir) {
		log.Printf("typescript: %s holds the tsconfig.json %s would compile under, but %s "+
			"package boundaries roll the sources below %s into the nearest package above it, "+
			"and a BUILD file written there just to hold the ts_config target would drop every "+
			"one of them. %s keeps the ruleset's baseline; make %s a package of its own with an "+
			"index file or a # gazelle:ts_package_boundary true.",
			dirRel, args.Rel, mode, dirRel, args.Rel, dirRel)
		return ""
	}
	if targetNameForDir(local.tc, dirRel) == tsConfigTargetName {
		log.Printf("typescript: %s already generates a target named %q, so there is no name "+
			"left for the ts_config beside its tsconfig.json and %s keeps the ruleset's "+
			"baseline. Rename the directory's target with a # gazelle:ts_target_name.",
			dirRel, tsConfigTargetName, args.Rel)
		return ""
	}
	return "//" + dirRel + ":" + tsConfigTargetName
}

// ownTsConfigRule is the ts_config target for this directory's own hand-written
// tsconfig.json: what makes the file a label the packages below it can name.
// nil when the directory has none, or when becoming a package would cost the
// package above it sources -- tsConfigLabel logs that case from the other side.
func ownTsConfigRule(args language.GenerateArgs, tc *tsConfig) *rule.Rule {
	if isConfiguredExclude("tsconfig.json", tc.excludePatterns) {
		return nil
	}
	if handWrittenTsConfigIn(args.Dir, args.Config.RepoRoot) == "" {
		return nil
	}
	if tsConfigPackageCosts(tc.packageBoundaryMode, args.Dir) {
		return nil
	}
	if targetNameForDir(tc, args.Rel) == tsConfigTargetName {
		return nil
	}
	r := rule.NewRule("ts_config", tsConfigTargetName)
	r.SetAttr("src", "tsconfig.json")
	r.SetAttr("visibility", []string{"//visibility:public"})
	return r
}

// tsConfigPackageCosts reports whether writing a BUILD file into absDir would
// take sources away from the package above it. In every-dir mode nothing is
// rolled up, so a new package costs nothing; in a roll-up mode the walk stops
// at every directory that is a package of its own, and one that becomes a
// package for the sake of a ts_config target takes the whole subtree beneath it
// out of the target that was compiling it.
func tsConfigPackageCosts(mode, absDir string) bool {
	return mode != boundaryEveryDir && !dirIsItsOwnPackageIn(mode, absDir)
}

// boundaryModeAt is the boundary mode dirRel is generated under, and whether
// fromRel is generated under the same one. dirRel is an ancestor of fromRel, so
// every directive at or above dirRel reaches both and the mode inherited at
// fromRel is dirRel's too -- unless a directory in between declares one, which
// is what the second return value reports.
func boundaryModeAt(repoRoot, dirRel, fromRel, inherited string) (string, bool) {
	for _, between := range dirsBetween(dirRel, fromRel) {
		absDir := filepath.Join(repoRoot, filepath.FromSlash(between))
		if _, declared := boundaryModeDeclaredIn(absDir, between); declared {
			return inherited, false
		}
	}
	return inherited, true
}

// dirsBetween lists every directory below ancestor down to and including
// descendant. A directive in any of them is one dirRel never saw.
func dirsBetween(ancestor, descendant string) []string {
	if descendant == ancestor {
		return nil
	}
	rest := descendant
	if ancestor != "" {
		rest = strings.TrimPrefix(descendant, ancestor+"/")
	}
	var out []string
	current := ancestor
	for _, part := range strings.Split(rest, "/") {
		current = path.Join(current, part)
		out = append(out, current)
	}
	return out
}

// boundaryModeDeclaredIn returns the boundary mode dir's own BUILD file
// declares. `# gazelle:ts_package_boundary true` marks that one directory and
// leaves the mode alone, so it declares none.
func boundaryModeDeclaredIn(absDir, rel string) (string, bool) {
	for _, buildName := range []string{"BUILD.bazel", "BUILD"} {
		f, err := rule.LoadFile(filepath.Join(absDir, buildName), rel)
		if err != nil {
			continue
		}
		for _, d := range f.Directives {
			if d.Key != directivePackageBoundary {
				continue
			}
			switch strings.TrimSpace(d.Value) {
			case "", boundaryEveryDir:
				return boundaryEveryDir, true
			case boundaryIndexOnly:
				return boundaryIndexOnly, true
			case boundaryTsConfig:
				return boundaryTsConfig, true
			}
		}
		return "", false
	}
	return "", false
}

// targetNameForDir returns the Bazel target name for the primary ts_compile
// rule in a directory. Uses the configured override if present, otherwise the
// directory basename. Falls back to "root" for the repository root.
func targetNameForDir(tc *tsConfig, rel string) string {
	if tc.targetName != "" {
		return tc.targetName
	}
	if rel == "" {
		return "root"
	}
	return path.Base(rel)
}

// testTargetName returns the conventional name for a ts_test target associated
// with a given library target name.
func testTargetName(libName string) string {
	return libName + "_test"
}

// docTargetName returns the conventional name for the ts_compile target holding
// a package's doc and story files.
func docTargetName(libName string) string {
	return libName + "_doc"
}

// reservedTSTargetNames returns the target names the TypeScript rules in a
// directory own. Non-TypeScript libraries must avoid these.
func reservedTSTargetNames(tc *tsConfig, rel string) map[string]struct{} {
	name := targetNameForDir(tc, rel)
	reserved := map[string]struct{}{
		name:                 {},
		name + "_lint":       {},
		testTargetName(name): {},
		docTargetName(name):  {},
		"dev":                {},
		"node_modules":       {},
	}
	// The framework entry target: leaving it out let claimedSrcs read Gazelle's
	// own rule as a hand-written claim, freezing that rule after run 1.
	if entry, ok := frameworkEntryTargetName(rel, tc); ok {
		reserved[entry] = struct{}{}
	}
	return reserved
}

// assetTargetNames names every css/asset/json source file in a package. Keeping
// the extension ("logo.svg" → "logo_svg") is what stops these targets colliding
// with the directory-named ts_compile target and with each other; a numeric
// suffix breaks any tie that survives that.
func assetTargetNames(reserved map[string]struct{}, groups ...[]string) map[string]string {
	var all []string
	for _, g := range groups {
		all = append(all, g...)
	}
	sort.Strings(all)

	names := make(map[string]string, len(all))
	used := make(map[string]struct{}, len(reserved)+len(all))
	for n := range reserved {
		used[n] = struct{}{}
	}
	for _, f := range all {
		// A rolled-up file arrives as a path, and two directories can hold the
		// same basename.
		base := strings.ReplaceAll(strings.ReplaceAll(f, "/", "_"), ".", "_")
		name := base
		for i := 2; ; i++ {
			if _, taken := used[name]; !taken {
				break
			}
			name = base + "_" + strconv.Itoa(i)
		}
		used[name] = struct{}{}
		names[f] = name
	}
	return names
}

// uniqueImports deduplicates and returns sorted import specifiers. The sorted
// order makes generated BUILD files deterministic.
func uniqueImports(imps []string) []string {
	seen := make(map[string]struct{}, len(imps))
	for _, imp := range imps {
		seen[imp] = struct{}{}
	}
	result := make([]string, 0, len(seen))
	for imp := range seen {
		result = append(result, imp)
	}
	sort.Strings(result)
	return result
}

// buildCodegenCompileRule wraps a ts_codegen's output in the ts_compile that
// makes it importable: ts_compile deps take JsInfo, which ts_codegen does not
// return, so the generated source has to arrive through srcs. oxc emits the
// declarations because tsgo's emit would put its outDir where the generated
// source already is, and TypeScript excludes outDir from the program.
func buildCodegenCompileRule(name, codegen string) *rule.Rule {
	r := rule.NewRule("ts_compile", name)
	r.SetAttr("srcs", []string{":" + codegen})
	r.SetAttr("declarations", "oxc")
	r.SetAttr("visibility", []string{"//visibility:public"})
	return r
}

// buildCodegenRule converts a CodegenPattern into a Bazel rule.Rule ready for
// inclusion in a GenerateResult. Returns nil when the pattern is malformed.
//
// The function handles three srcs cases:
//  1. Entries that start with "glob(" are emitted as-is (Bazel glob expressions).
//  2. Plain strings are emitted as a string list.
//  3. Mixed lists are flattened — plain strings remain strings, glob entries
//     are rendered inline.
//
// When CodegenPattern.OutDir is set, an "out_dir" string attr is emitted
// instead of "outs" (the ts_codegen rule then uses declare_directory).
func buildCodegenRule(p CodegenPattern) *rule.Rule {
	if p.Name == "" || p.Generator == "" {
		return nil
	}
	if len(p.Outs) == 0 && p.OutDir == "" {
		return nil
	}
	if len(p.Srcs) == 0 {
		return nil
	}

	r := rule.NewRule("ts_codegen", p.Name)

	// Comment (optional).
	if p.Comment != "" {
		r.AddComment(p.Comment)
	}

	// srcs: entries prefixed with "glob(" are raw Bazel glob expressions;
	// plain strings are regular file names. The rule.Rule API only supports
	// string-list attrs, so globs cannot be emitted natively here — instead
	// we mark them for the Gazelle starlark printer by emitting them as-is.
	// For now, if ALL srcs are plain strings we emit a string list; if any
	// entry is a glob expression we fall back to the glob string itself.
	// This matches the common pattern where detectors use a glob expression
	// as the sole entry.
	hasGlob := false
	for _, s := range p.Srcs {
		if strings.HasPrefix(s, globExprPrefix) {
			hasGlob = true
			break
		}
	}
	if hasGlob && len(p.Srcs) == 1 {
		// Single glob: emit as a raw Bazel expression string. Gazelle's
		// rule.SetAttr with a string value will write it verbatim when the
		// string looks like a function call (starts with "glob("). Unfortunately
		// the rule API does not natively support non-string-list exprs for srcs,
		// so we use a string list containing the raw glob text as a workaround.
		// The resulting BUILD file will contain:  srcs = glob(["*.tsx"])
		// This is achieved by wrapping in a special single-element list where
		// the sole element is the raw expression.
		//
		// NOTE: Gazelle's rule package renders []string attrs as Starlark lists.
		// A glob expression therefore needs to be emitted as a select/function
		// outside of a list. The idiomatic approach for Gazelle extensions is to
		// emit the raw string and rely on buildifier to format it. We use the
		// rule.SetPrivateAttr mechanism to pass a raw expression through, but
		// since that only works with the custom printer, we instead emit the
		// glob expression directly as a string attr value (non-list), which
		// the Bazel BUILD printer will output as a bare expression assignment.
		r.SetAttr("srcs", p.Srcs[0]) // raw glob string, printed as expression
	} else {
		// Collect only plain strings (strip any glob prefix if somehow mixed).
		var plain []string
		for _, s := range p.Srcs {
			if !strings.HasPrefix(s, globExprPrefix) {
				plain = append(plain, s)
			}
		}
		sort.Strings(plain)
		r.SetAttr("srcs", plain)
	}

	// outs or out_dir.
	if p.OutDir != "" {
		r.SetAttr("out_dir", p.OutDir)
	} else {
		sort.Strings(p.Outs)
		r.SetAttr("outs", p.Outs)
	}

	r.SetAttr("generator", p.Generator)

	if len(p.Args) > 0 {
		r.SetAttr("args", p.Args)
	}

	if p.NodeModules {
		r.SetAttr("node_modules", ":node_modules")
	}

	r.SetAttr("visibility", []string{"//visibility:public"})

	return r
}

// vitestConfigNames are the file names vitest itself looks for, in its own
// order of preference.
var vitestConfigNames = []string{
	"vitest.config.ts", "vitest.config.mts", "vitest.config.cts",
	"vitest.config.js", "vitest.config.mjs", "vitest.config.cjs",
}

// vitestConfigIn returns the vitest config file in dir, or "" when there is
// none.
func vitestConfigIn(dir string) string {
	for _, name := range vitestConfigNames {
		if st, err := os.Stat(filepath.Join(dir, name)); err == nil && !st.IsDir() {
			return name
		}
	}
	return ""
}
