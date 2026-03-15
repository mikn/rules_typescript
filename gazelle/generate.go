package typescript

import (
	"log"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bazelbuild/bazel-gazelle/language"
	"github.com/bazelbuild/bazel-gazelle/rule"

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
// asset_library (images, SVGs, fonts). NOTE: .json files are NOT included here;
// they are handled by json_library (see isJSONFile).
func isAssetFile(name string) bool {
	ext := strings.ToLower(path.Ext(name))
	switch ext {
	case ".svg", ".png", ".jpg", ".jpeg", ".gif", ".webp",
		".woff", ".woff2", ".ttf", ".eot":
		return true
	}
	return false
}

// isJSONFile returns true for .json files that should be handled by
// json_library (generates a fully-typed .d.ts, not `unknown`).
func isJSONFile(name string) bool {
	return strings.ToLower(path.Ext(name)) == ".json"
}

// isTestFile returns true for files that should be compiled as test targets.
// Patterns: *.test.ts, *.test.tsx, *.spec.ts, *.spec.tsx
func isTestFile(name string) bool {
	base := strings.TrimSuffix(strings.TrimSuffix(name, ".tsx"), ".ts")
	return strings.HasSuffix(base, ".test") || strings.HasSuffix(base, ".spec")
}

// builtinGeneratedSuffixes is the set of name suffixes (after stripping the
// .ts/.tsx extension) that identify generated files that should be excluded
// from source targets. These patterns are always active regardless of any
// gazelle_ts.json configuration.
var builtinGeneratedSuffixes = []string{
	".gen",
	".generated",
	".auto",
}

// isGeneratedFile returns true for files that are generated artefacts and
// should be excluded from source targets. Built-in patterns cover the most
// common code-generation conventions:
//
//   - *.gen.ts / *.gen.tsx  (e.g. routeTree.gen.ts from TanStack Router)
//   - *.generated.ts / *.generated.tsx  (common GraphQL codegen output)
//   - *.auto.ts / *.auto.tsx  (common automatic code generation)
//
// Additional patterns can be supplied via the gazelle_ts.json
// "excludePatterns" field (matched via isConfiguredExclude).
func isGeneratedFile(name string) bool {
	base := strings.TrimSuffix(strings.TrimSuffix(name, ".tsx"), ".ts")
	for _, suffix := range builtinGeneratedSuffixes {
		if strings.HasSuffix(base, suffix) {
			return true
		}
	}
	return false
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
	return name == "index.ts" || name == "index.tsx"
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

	// Collect TypeScript, CSS, and asset source files from the regular files list.
	var (
		srcFiles       []string // non-test, non-generated .ts/.tsx files
		testFiles      []string // *.test.ts, *.spec.ts, etc.
		cssFiles       []string // plain .css source files (side-effect imports)
		cssModuleFiles []string // *.module.css files (default import → typed styles)
		assetFiles     []string // image/font/svg asset files (NOT json)
		jsonFiles      []string // .json data files → json_library (typed .d.ts)
		hasIndex       bool
	)

	for _, f := range args.RegularFiles {
		// Skip well-known config files before the JSON check so that Bazel/npm
		// config files are never classified as json_library sources.
		if f == "package.json" || f == "gazelle_ts.json" || f == "tsconfig.json" {
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
		if isGeneratedFile(f) {
			continue
		}
		if isConfiguredExclude(f, tc.excludePatterns) {
			continue
		}
		if isTestFile(f) {
			testFiles = append(testFiles, f)
			continue
		}
		srcFiles = append(srcFiles, f)
		if isIndexFile(f) {
			hasIndex = true
		}
	}

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
	default: // boundaryEveryDir
		// New default: any directory with .ts files (or the repo root) is a boundary.
		isBoundary = len(srcFiles) > 0 || hasIndex || args.Rel == "" || tc.packageBoundary
	}

	totalNonTS := len(cssFiles) + len(cssModuleFiles) + len(assetFiles) + len(jsonFiles)
	if !isBoundary && len(srcFiles) == 0 && len(testFiles) == 0 && totalNonTS == 0 {
		// No TypeScript, CSS, asset, or JSON files and not a boundary: nothing to do.
		return language.GenerateResult{}
	}

	var gen []*rule.Rule
	var empty []*rule.Rule
	var imports []any

	// ---- css_library targets -----------------------------------------------
	// Generate one css_library rule per plain .css file (side-effect imports).
	// The rule name is the basename without the .css extension.

	sort.Strings(cssFiles)
	for _, f := range cssFiles {
		ruleName := strings.TrimSuffix(f, ".css")
		r := rule.NewRule("css_library", ruleName)
		r.SetAttr("srcs", []string{f})
		r.SetAttr("visibility", []string{"//visibility:public"})
		gen = append(gen, r)
		// css_library targets are indexed by their workspace-relative CSS path
		// so that resolveImports can look them up when a .ts file imports a
		// .css file side-effect style (import "./foo.css").
		imports = append(imports, []string{})
	}

	// ---- css_module targets ------------------------------------------------
	// Generate one css_module rule per *.module.css file (default imports).
	// Rule name: strip ".module.css" from the basename, e.g.
	//   "Button.module.css" → "button_module"
	//   "foo.module.css"    → "foo_module"

	sort.Strings(cssModuleFiles)
	for _, f := range cssModuleFiles {
		// Strip ".module.css" and append "_module" to clearly distinguish these
		// targets from plain css_library targets.
		stem := strings.TrimSuffix(f, ".module.css")
		ruleName := strings.ToLower(stem) + "_module"
		r := rule.NewRule("css_module", ruleName)
		r.SetAttr("srcs", []string{f})
		r.SetAttr("visibility", []string{"//visibility:public"})
		gen = append(gen, r)
		// css_module targets are indexed by their workspace-relative CSS path
		// so that resolveImports can resolve default imports from .module.css.
		imports = append(imports, []string{})
	}

	// ---- asset_library targets ---------------------------------------------
	// Generate one asset_library rule per image/font/SVG file.
	// Rule name: file stem (without extension), with dots replaced by
	// underscores, e.g.:
	//   "logo.svg"   → "logo"
	//   "hero.png"   → "hero"

	sort.Strings(assetFiles)
	for _, f := range assetFiles {
		ext := path.Ext(f)
		stem := strings.TrimSuffix(f, ext)
		// Replace dots in stem (e.g. "logo.2x.png" → "logo_2x") for valid target names.
		ruleName := strings.ReplaceAll(stem, ".", "_")
		r := rule.NewRule("asset_library", ruleName)
		r.SetAttr("srcs", []string{f})
		r.SetAttr("visibility", []string{"//visibility:public"})
		gen = append(gen, r)
		// asset_library targets are indexed by their workspace-relative asset
		// path for import resolution.
		imports = append(imports, []string{})
	}

	// ---- json_library targets ----------------------------------------------
	// Generate one json_library rule per .json file (typed declarations).
	// Rule name: file stem (without extension), with dots replaced by
	// underscores, e.g.:
	//   "config.json"  → "config"
	//   "schema.json"  → "schema"

	sort.Strings(jsonFiles)
	for _, f := range jsonFiles {
		stem := strings.TrimSuffix(f, ".json")
		// Replace dots in stem for valid target names.
		ruleName := strings.ReplaceAll(stem, ".", "_")
		r := rule.NewRule("json_library", ruleName)
		r.SetAttr("srcs", []string{f})
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
		r.SetAttr("srcs", srcFiles)
		r.SetAttr("visibility", []string{"//visibility:public"})

		// When isolated_declarations is disabled via directive, emit the
		// attribute explicitly so the rule falls back to the full
		// ts_compile_legacy behaviour.
		if !tc.isolatedDeclarations {
			r.SetAttr("isolated_declarations", false)
		}

		// Propagate path aliases from tsconfig.json / directives / gazelle_ts.json
		// into the generated ts_compile rule so that tsgo type-checking can resolve
		// source-level path aliases (e.g. "@/components") without emitting
		// false "Cannot find module" errors.
		if len(tc.pathAliases) > 0 {
			r.SetAttr("path_aliases", tc.pathAliases)
		}

		// Collect imports for all src files.
		var allImports []string
		for _, f := range srcFiles {
			filePath := filepath.Join(args.Dir, f)
			imps, err := extractImports(filePath)
			if err != nil {
				log.Printf("typescript: error reading %s: %v", filePath, err)
				continue
			}
			allImports = append(allImports, imps...)
		}

		gen = append(gen, r)
		imports = append(imports, uniqueImports(allImports))

		// ---- ts_lint target (alongside ts_compile when linter is detected) --
		// When an eslint or oxlint config exists in this directory or any
		// ancestor, generate a ts_lint target. The rule name is "<name>_lint".
		// The linter_binary label follows the @npm//:oxlint_bin convention.
		if tc.linterConfig != "" && tc.linterType != "" {
			lintName := name + "_lint"
			lr := rule.NewRule("ts_lint", lintName)
			lr.SetAttr("srcs", srcFiles)
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
		_, hasAppEntry := detectAppEntryPoint(srcFiles)
		hasHTML := hasIndexHTML(args.Dir)
		if hasAppEntry || hasHTML {
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

	// ---- ts_test targets ---------------------------------------------------

	if len(testFiles) > 0 {
		sort.Strings(testFiles)

		// Collect all imports from test files for dep resolution.
		var allImports []string
		for _, f := range testFiles {
			filePath := filepath.Join(args.Dir, f)
			imps, err := extractImports(filePath)
			if err != nil {
				log.Printf("typescript: error reading %s: %v", filePath, err)
				continue
			}
			allImports = append(allImports, imps...)
		}

		// Also collect npm imports from production source files in this package.
		// ts_test auto-generates a node_modules tree from its own @npm// deps, so
		// the deps list must include ALL npm packages the tests need at runtime —
		// both what the test files directly import AND what the production code in
		// this package imports.  Without the production imports, the auto-generated
		// node_modules tree would be missing packages needed by the SUT.
		var allPackageImports []string
		allPackageImports = append(allPackageImports, allImports...)
		for _, f := range srcFiles {
			filePath := filepath.Join(args.Dir, f)
			imps, err := extractImports(filePath)
			if err != nil {
				log.Printf("typescript: error reading %s: %v", filePath, err)
				continue
			}
			allPackageImports = append(allPackageImports, imps...)
		}

		name := testTargetName(targetNameForDir(tc, args.Rel))

		r := rule.NewRule("ts_test", name)
		r.SetAttr("srcs", testFiles)

		// When isolated_declarations is disabled via directive, emit the
		// attribute explicitly so the internal ts_compile inside ts_test also
		// runs in legacy (non-isolated) mode, consistent with ts_compile targets
		// in the same package.
		if !tc.isolatedDeclarations {
			r.SetAttr("isolated_declarations", false)
		}

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
			if hadTestTarget {
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
	if len(testFiles) > 0 && args.File != nil {
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

	// ---- ts_codegen targets ------------------------------------------------
	// Scan the directory for known code generation patterns (TanStack routes,
	// Prisma, GraphQL Codegen, OpenAPI) and emit ts_codegen rules.
	// Custom patterns from # gazelle:ts_codegen directives are also included.
	codegenPatterns := detectCodegen(args.Rel, args.RegularFiles, tc)
	for _, p := range codegenPatterns {
		r := buildCodegenRule(p)
		if r != nil {
			gen = append(gen, r)
			// ts_codegen targets have no import resolution needs.
			imports = append(imports, nil)
		}
	}

	// Emit empty stubs for ts_codegen targets that no longer have a matching
	// pattern but still exist in the current BUILD file. This allows Gazelle
	// to clean up stale auto-generated ts_codegen rules when the trigger files
	// are removed (e.g. schema.prisma deleted).
	if args.File != nil {
		// Build a set of names that were just generated so we know which
		// existing ts_codegen rules are stale.
		generatedNames := make(map[string]bool, len(codegenPatterns))
		for _, p := range codegenPatterns {
			generatedNames[p.Name] = true
		}
		for _, existingRule := range args.File.Rules {
			if existingRule.Kind() == "ts_codegen" && !generatedNames[existingRule.Name()] {
				empty = append(empty, rule.NewRule("ts_codegen", existingRule.Name()))
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
		bundleRules, bundleImports := generateFrameworkBundle(args, tc)
		gen = append(gen, bundleRules...)
		imports = append(imports, bundleImports...)
	}

	// ---- filegroup "sources" for framework staging_srcs --------------------
	// When a framework is detected and this directory is one of the stage dirs,
	// generate a "sources" filegroup that exports all non-test .ts/.tsx files.
	// The root ts_bundle staging_srcs references these filegroups via labels
	// like //src/routes:sources. Only emit when there are actual source files.
	if args.Rel != "" && tc.detectedFramework != FrameworkNone && isStagedDir(args.Rel, tc) && len(srcFiles) > 0 {
		if !ruleExists(args, "filegroup", "sources") {
			if fg := generateSourcesFilegroup(srcFiles); fg != nil {
				gen = append(gen, fg)
				imports = append(imports, nil)
			}
		}
	}

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
		},
	}
}

// generatePnpmTargets generates :pnpm and :add_package macro invocations at
// the workspace root when a pnpm-lock.yaml file is detected.
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
		gen = append(gen, r)
		imports = append(imports, nil)
	}

	return gen, imports
}

// ---- helper functions ------------------------------------------------------

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
