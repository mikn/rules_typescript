package typescript

// framework_bundle.go generates framework-aware bundle targets (node_modules,
// vite_bundler, ts_bundle) at the workspace root BUILD.bazel when a supported
// Vite-based framework is detected from package.json.
//
// User story:
//  1. Write a minimal vite.config.mjs with the framework plugin (3 lines).
//  2. Run Gazelle → it generates node_modules, vite_bundler and ts_bundle here,
//     and the single-file entry target EntryPoint names (framework_entry.go).
//  3. bazel build //:app produces the framework bundle.
//
// Gazelle cannot write arbitrary non-BUILD files, so the vite_config file must
// be hand-authored. We generate a ts_bundle that points at the conventional
// config filename for each framework (e.g. "tanstack-vite.config.mjs").
//
// A framework whose bundling cannot work at all belongs in unsupportedBundling
// instead: Gazelle then says so rather than emitting a target that fails.

import (
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bazelbuild/bazel-gazelle/language"
	"github.com/bazelbuild/bazel-gazelle/rule"
)

// ---- FrameworkBundleConfig -------------------------------------------------

// Node realpaths a module before resolving its bare imports, so only a tree
// whose own directory is named "node_modules" is on that resolution path.
const frameworkNodeModulesName = "node_modules"

// One framework per workspace root, so the bundler can take the bare name.
const frameworkViteBundlerName = "vite"

// FrameworkBundleConfig describes the Bazel targets that should be generated
// at the workspace root when a specific Vite-based framework is detected.
type FrameworkBundleConfig struct {
	// AppName is the base name for the generated targets (e.g. "app") and the
	// stem of the pre-rename names cleanupLegacyFrameworkTargets deletes.
	AppName string

	// NpmDeps is the list of npm package names to include in the node_modules
	// target. Each entry is converted to an @npm//:<label> reference using
	// npmPackageToLabelName.
	NpmDeps []string

	// ViteConfigFile is the expected filename of the user-authored vite config
	// (e.g. "tanstack-vite.config.mjs"). Gazelle sets the vite_config attr of
	// ts_bundle to this filename. The user must create this file manually.
	ViteConfigFile string

	// StageFiles lists workspace-root files, besides HTMLFile, that the
	// framework plugin reads from the staging root rather than from the
	// package source directory.
	StageFiles []string

	// StageDirs are the roots of the trees whose "sources" filegroups
	// ts_bundle.staging_srcs names -- each entry and everything beneath it.
	StageDirs []string

	// EntryPoint is the Bazel label of the primary entry_point for ts_bundle.
	// ts_bundle requires exactly one .js from it, so it names a single-file
	// ts_compile rather than the directory-wide one. framework_entry.go
	// generates that target, reading this label for both the package to write
	// it in and the source file to claim -- see frameworkEntrySrc.
	EntryPoint string

	// HTMLFile is the workspace-relative path to the HTML entry file
	// (typically "index.html"). Used as the html attr of ts_bundle.
	HTMLFile string

	// BundleName is the Bazel target name for the ts_bundle rule.
	// Typically matches AppName (e.g. "app") or a framework-specific name
	// (e.g. "app_remix").
	BundleName string
}

// frameworkConfigs maps each detected Framework to its bundle configuration.
// FrameworkNextJS uses its own next_build rule and has a separate generation
// path. A detected framework absent from BOTH maps gets no bundle targets and
// no explanation, which is the one outcome to avoid.
var frameworkConfigs = map[Framework]FrameworkBundleConfig{
	FrameworkTanStack: {
		AppName:        "app",
		BundleName:     "app",
		ViteConfigFile: "tanstack-vite.config.mjs",
		NpmDeps: []string{
			"vite",
			"react",
			"react-dom",
			"@tanstack/react-start",
			"@tanstack/react-router",
			"zod",
			"h3",
		},
		StageDirs:  []string{"src/routes", "src/app", "src/lib", "src/components"},
		EntryPoint: "//src/app:main",
		HTMLFile:   "index.html",
	},
	FrameworkRemix: {
		AppName:        "app",
		BundleName:     "app_remix",
		ViteConfigFile: "remix-vite.config.mjs",
		NpmDeps: []string{
			"vite",
			"react",
			"react-dom",
			"@remix-run/dev",
			"@remix-run/react",
			"@remix-run/node",
		},
		StageDirs:  []string{"app/routes", "app"},
		StageFiles: []string{"package.json"},
		EntryPoint: "//app:entry_client",
		HTMLFile:   "index.html",
	},
}

// unsupportedBundling carries, for each framework detection recognises but
// bundling cannot serve, why no bundle target is generated for it. A target
// that fails to build is worse than no target, and silence is worse than both.
var unsupportedBundling = map[Framework]string{
	FrameworkSolidStart: "@solidjs/start ships no Vite plugin: defineConfig() returns a vinxi app, " +
		"which ts_bundle's vite_config contract (a default export with a plugins array) cannot consume",
}

// unsupportedDevServer carries, per framework, why ts_dev_server cannot serve
// its app, so Gazelle emits no dev target instead of one that answers 500.
var unsupportedDevServer = map[Framework]string{
	FrameworkTanStack: "its SSR module runner inlines react/jsx-runtime instead of externalising it " +
		"against a node_modules tree that is a build output, and React's CJS entry then " +
		"evaluates `module` in an ESM context",
}

// reportUnsupportedDevServer reports the skipped dev target once, naming why.
func reportUnsupportedDevServer(f Framework) bool {
	reason, ok := unsupportedDevServer[f]
	if !ok {
		return false
	}
	log.Printf("typescript: %s detected: no ts_dev_server generated — %s. "+
		"Build the bundle instead.", frameworkName(f), reason)
	return true
}

// reportUnsupportedBundling names the framework and says bundling for it is
// unsupported, so the missing target is a decision rather than a hole.
func reportUnsupportedBundling(f Framework) {
	name := frameworkName(f)
	reason, ok := unsupportedBundling[f]
	if !ok {
		log.Printf("typescript: %s detected: no bundle target generated, and no reason is registered for it.", name)
		return
	}
	log.Printf("typescript: %s detected: bundling it is unsupported, so no bundle target was generated — %s. "+
		"Your TypeScript still compiles and tests; for a client-only build, declare a ts_bundle by hand with no vite_config.", name, reason)
}

// ---- generation ------------------------------------------------------------

// generateFrameworkBundle generates the root-level bundle targets, every one of
// them on every run: the merger reconciles only an attribute a candidate carries.
func generateFrameworkBundle(
	args language.GenerateArgs,
	tc *tsConfig,
) ([]*rule.Rule, []any, []*rule.Rule) {
	// Next.js uses its own rule (next_build) rather than Vite-based bundling.
	if tc.detectedFramework == FrameworkNextJS {
		gen, imports := generateNextJSBundle(args, tc)
		return gen, imports, nil
	}

	// SvelteKit likewise: it reads process.cwd(), which ts_bundle's staging root
	// cannot become, so sveltekit_build owns the cwd instead.
	if tc.detectedFramework == FrameworkSvelteKit {
		gen, imports := generateSvelteKitBundle(args, tc)
		return gen, imports, nil
	}

	cfg, ok := frameworkConfigs[tc.detectedFramework]
	if !ok {
		reportUnsupportedBundling(tc.detectedFramework)
		return nil, nil, nil
	}

	// Filter out npm deps that are not actually present in the lockfile.
	// When npmPackages is nil (no lockfile), include all configured deps.
	npmDeps := filterNpmDeps(cfg.NpmDeps, tc)

	var gen []*rule.Rule
	var imports []any

	nodeModulesName := frameworkNodeModulesName
	viteTargetName := frameworkViteBundlerName
	empty := cleanupLegacyFrameworkTargets(args, cfg)

	// ---- node_modules target -----------------------------------------------
	nmDeps := make([]string, 0, len(npmDeps))
	for _, pkg := range npmDeps {
		nmDeps = append(nmDeps, npmLabel(pkg))
	}
	sort.Strings(nmDeps)

	nm := rule.NewRule("node_modules", nodeModulesName)
	nm.SetAttr("deps", nmDeps)
	nm.SetAttr("visibility", []string{"//visibility:public"})
	nm.AddComment("# Framework node_modules for " + frameworkName(tc.detectedFramework))
	nm.AddComment("# Name must be \"node_modules\": Node realpaths a module before its imports.")
	gen = append(gen, nm)
	imports = append(imports, nil)

	// ---- vite_bundler target -----------------------------------------------
	vb := rule.NewRule("vite_bundler", viteTargetName)
	vb.SetAttr("vite", "@npm//:vite")
	vb.SetAttr("node_modules", ":"+nodeModulesName)
	gen = append(gen, vb)
	imports = append(imports, nil)

	// ---- ts_bundle target --------------------------------------------------

	// An unresolvable entry_point fails analysis for everything that reaches it,
	// so the bundle waits for the entry. reportMissingFrameworkEntry says so.
	entryPoint := bundleEntryPoint(args, tc, cfg)
	if entryPoint == "" {
		// Only the rule Gazelle itself wrote is withdrawn. An entry_point the
		// user pointed elsewhere is theirs, however this generator reads it.
		if have, ok := existingBundleEntryPoint(args, cfg); ok && (have == "" || have == cfg.EntryPoint) {
			log.Printf("typescript: %s detected: ts_bundle(%s) is being withdrawn -- its "+
				"entry_point %q names a target nothing declares any more, and an unresolvable "+
				"label fails analysis for every target that reaches it. A bundle you maintain "+
				"yourself needs a \"# keep\" comment above the rule to survive this.",
				frameworkName(tc.detectedFramework), cfg.BundleName, have)
			empty = append(empty, rule.NewRule("ts_bundle", cfg.BundleName))
		}
		return gen, imports, empty
	}

	tb := rule.NewRule("ts_bundle", cfg.BundleName)
	tb.SetAttr("mode", "app")
	if cfg.HTMLFile != "" {
		tb.SetAttr("html", cfg.HTMLFile)
	}
	tb.SetAttr("entry_point", entryPoint)
	tb.SetAttr("bundler", ":"+viteTargetName)
	if cfg.ViteConfigFile != "" {
		tb.SetAttr("vite_config", cfg.ViteConfigFile)
	}
	if stagingSrcs := buildStagingSrcs(cfg, args.Config.RepoRoot, tc); len(stagingSrcs) > 0 {
		tb.SetAttr("staging_srcs", rule.SortedStrings(stagingSrcs))
	}
	gen = append(gen, tb)
	imports = append(imports, nil)

	return gen, imports, empty
}

// generateSourcesFilegroup generates a "sources" filegroup rule in a
// sub-package directory, exporting all .ts/.tsx source files for use in
// ts_bundle.staging_srcs at the workspace root.
//
// This is called for each directory that appears in a framework's StageDirs
// list, and is returned alongside the normal ts_compile rules for that directory.
// The filegroup only collects non-generated, non-test TypeScript files.
func generateSourcesFilegroup(srcFiles []string) *rule.Rule {
	if len(srcFiles) == 0 {
		return nil
	}
	fg := rule.NewRule("filegroup", "sources")
	sorted := make([]string, len(srcFiles))
	copy(sorted, srcFiles)
	sort.Strings(sorted)
	fg.SetAttr("srcs", sorted)
	fg.SetAttr("visibility", []string{"//visibility:public"})
	return fg
}

// stagedSources is the subset of a directory's own files that belong in its
// "sources" filegroup: TypeScript, not a test, not generated.
func stagedSources(files []string) []string {
	var out []string
	for _, f := range files {
		if !isTypeScriptFile(f) || strings.HasSuffix(f, ".d.ts") ||
			isGeneratedFile(f) || isTestFile(f) {
			continue
		}
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// The framework compiles what owned() claims off disk; nothing outside those
// directories reaches the build except through a label the app rule names.
func stagingLabelsOutside(dir string, tc *tsConfig, owned func(rel string) bool) []string {
	labels := packageStagingLabels(dir, "", tc)
	var walk func(rel string)
	walk = func(rel string) {
		for _, sub := range subdirsOf(filepath.Join(dir, rel)) {
			subRel := path.Join(rel, sub)
			if skipRolledUpDir(sub) || isExcludedDir(sub, tc.excludeDirs) || owned(subRel) {
				continue
			}
			if isConfiguredExclude(sub, tc.excludePatterns) || isConfiguredExclude(subRel, tc.excludePatterns) {
				continue
			}
			labels = append(labels, packageStagingLabels(filepath.Join(dir, subRel), subRel, tc)...)
			walk(subRel)
		}
	}
	walk("")
	sort.Strings(labels)
	return labels
}

// The classification in generateRules, mirrored: a label naming a target
// nothing writes fails analysis for the whole workspace, not just the bundle.
func packageStagingLabels(absDir, rel string, tc *tsConfig) []string {
	lp := readLocalPackage(absDir, rel, tc)
	local := lp.tc
	if lp.ignored || !dirGetsItsOwnTargets(absDir, local, rel) {
		return nil
	}
	entries, err := os.ReadDir(absDir)
	if err != nil {
		return nil
	}
	var css, cssModules, assets, jsons []string
	hasTS := false
	for _, e := range entries {
		name := e.Name()
		switch {
		case e.IsDir(), isConfiguredExclude(name, lp.dropped):
		case name == "package.json", name == "gazelle_ts.json", name == "tsconfig.json":
		case isJSONFile(name):
			jsons = append(jsons, name)
		case isAssetFile(name):
			assets = append(assets, name)
		case isCSSModuleFile(name):
			cssModules = append(cssModules, name)
		case isCSSFile(name):
			css = append(css, name)
		case !isTypeScriptFile(name), isGeneratedFile(name), isTestFile(name):
		case isConfiguredExclude(name, local.excludePatterns), nextOwnsFile(rel, name, local):
		default:
			hasTS = true
		}
	}

	var labels []string
	if hasTS {
		labels = append(labels, dirTargetLabel(rel, targetNameForDir(local, rel)))
	}
	names := assetTargetNames(reservedTSTargetNames(local, rel), css, cssModules, assets, jsons)
	for _, group := range [][]string{css, cssModules, assets, jsons} {
		for _, f := range group {
			labels = append(labels, dirTargetLabel(rel, names[f]))
		}
	}
	return labels
}

// dirTargetLabel spells a target's label the way generated BUILD files do:
// ":name" at the root, and the "//dir" shorthand when the name is the basename.
func dirTargetLabel(rel, name string) string {
	switch {
	case rel == "":
		return ":" + name
	case name == path.Base(rel):
		return "//" + rel
	default:
		return "//" + rel + ":" + name
	}
}

// In index-only mode a plain subdirectory's sources roll up into its nearest
// package, and generation there emits nothing to name.
func dirGetsItsOwnTargets(absDir string, tc *tsConfig, rel string) bool {
	if rel == "" || tc.packageBoundaryMode != boundaryIndexOnly {
		return true
	}
	return dirIsItsOwnPackage(absDir)
}

// ---- helpers ---------------------------------------------------------------

// filterNpmDeps returns only those package names from pkgs that are present
// in tc.npmPackages. When tc.npmPackages is nil (no lockfile loaded) all
// packages are returned unchanged.
func filterNpmDeps(pkgs []string, tc *tsConfig) []string {
	if tc.npmPackages == nil {
		return pkgs
	}
	out := make([]string, 0, len(pkgs))
	for _, pkg := range pkgs {
		if hasNpmPackage(tc, pkg) {
			out = append(out, pkg)
		}
	}
	// Always include "vite" itself even if not found (it should always be present).
	hasVite := false
	for _, p := range out {
		if p == "vite" {
			hasVite = true
			break
		}
	}
	if !hasVite && len(out) == 0 {
		// No packages found at all — fall back to the full list to avoid
		// generating an empty node_modules target.
		return pkgs
	}
	return out
}

// missingNpmDeps returns the wanted packages filterNpmDeps dropped, so a
// generator can say which dependency the workspace has to add.
func missingNpmDeps(wanted, kept []string) []string {
	have := make(map[string]struct{}, len(kept))
	for _, pkg := range kept {
		have[pkg] = struct{}{}
	}
	var missing []string
	for _, pkg := range wanted {
		if _, ok := have[pkg]; !ok {
			missing = append(missing, pkg)
		}
	}
	return missing
}

// npmLabel converts an npm package name to its @npm//:<label> form.
func npmLabel(pkgName string) string {
	return "@npm//:" + npmPackageToLabelName(pkgName)
}

// frameworkName returns a human-readable string for the framework.
func frameworkName(f Framework) string {
	switch f {
	case FrameworkTanStack:
		return "TanStack Start"
	case FrameworkRemix:
		return "Remix"
	case FrameworkSvelteKit:
		return "SvelteKit"
	case FrameworkSolidStart:
		return "SolidStart"
	case FrameworkNextJS:
		return "Next.js"
	default:
		return "unknown framework"
	}
}

// legacyFrameworkTargetNames maps each pre-rename target name to its kind.
func legacyFrameworkTargetNames(cfg FrameworkBundleConfig) map[string]string {
	return map[string]string{
		cfg.AppName + "_node_modules": "node_modules",
		cfg.AppName + "_vite":         "vite_bundler",
	}
}

// cleanupLegacyFrameworkTargets deletes the pre-rename pair, all of it or none
// of it: the bundler half is the tree half's only consumer.
func cleanupLegacyFrameworkTargets(args language.GenerateArgs, cfg FrameworkBundleConfig) []*rule.Rule {
	if args.File == nil {
		return nil
	}
	legacy := legacyFrameworkTargetNames(cfg)

	var empty []*rule.Rule
	for _, r := range args.File.Rules {
		if kind, ok := legacy[r.Name()]; ok && r.Kind() == kind {
			empty = append(empty, rule.NewRule(kind, r.Name()))
		}
	}
	for _, r := range args.File.Rules {
		if _, isLegacy := legacy[r.Name()]; isLegacy {
			continue
		}
		if referencesAny(r, legacy) {
			return nil
		}
	}
	return empty
}

func referencesAny(r *rule.Rule, names map[string]string) bool {
	referenced := func(v string) bool {
		for name := range names {
			if v == ":"+name || v == "//:"+name {
				return true
			}
		}
		return false
	}
	for _, key := range r.AttrKeys() {
		if referenced(r.AttrString(key)) {
			return true
		}
		for _, v := range r.AttrStrings(key) {
			if referenced(v) {
				return true
			}
		}
	}
	return false
}

// ruleExists returns true when the BUILD file already contains a rule with the
// given kind and name.
func ruleExists(args language.GenerateArgs, kind, name string) bool {
	if args.File == nil {
		return false
	}
	for _, r := range args.File.Rules {
		if r.Kind() == kind && r.Name() == name {
			return true
		}
	}
	return false
}

// The framework's own tree reaches staging through the "sources" filegroups;
// a package outside it, through the targets generation already writes there.
func buildStagingSrcs(cfg FrameworkBundleConfig, repoRoot string, tc *tsConfig) []string {
	var srcs []string

	// HTML file at the workspace root is referenced without a target separator.
	if cfg.HTMLFile != "" {
		srcs = append(srcs, cfg.HTMLFile)
	}

	srcs = append(srcs, cfg.StageFiles...)

	for _, dir := range stagedDirsUnder(repoRoot, cfg.StageDirs, tc) {
		srcs = append(srcs, fmt.Sprintf("//%s:sources", dir))
	}

	if repoRoot != "" {
		srcs = append(srcs, stagingLabelsOutside(repoRoot, tc, func(rel string) bool {
			return isStagedDir(rel, tc)
		})...)
	}

	return srcs
}

// stagedDirsUnder walks, rather than matching StageDirs exactly, to reach the
// routes a static list cannot name: a folder route, or one nested two deep.
// An empty repoRoot is no workspace to walk, so every entry is named.
func stagedDirsUnder(repoRoot string, stageDirs []string, tc *tsConfig) []string {
	if repoRoot == "" {
		return append([]string(nil), stageDirs...)
	}
	// StageDirs nest (Remix stages app/routes and app), so visited comes before
	// the questions: a refusal is otherwise diagnosed once per base.
	visited := map[string]struct{}{}
	seen := map[string]struct{}{}
	for _, base := range stageDirs {
		root := filepath.Join(repoRoot, filepath.FromSlash(base))
		filepath.WalkDir(root, func(p string, entry os.DirEntry, err error) error {
			if err != nil || !entry.IsDir() {
				return nil
			}
			if p != root && (strings.HasPrefix(entry.Name(), ".") || isExcludedDir(entry.Name(), tc.excludeDirs)) {
				return filepath.SkipDir
			}
			rel, err := filepath.Rel(repoRoot, p)
			if err != nil {
				return nil
			}
			slashRel := filepath.ToSlash(rel)
			if _, done := visited[slashRel]; done {
				return nil
			}
			visited[slashRel] = struct{}{}
			if !holdsStagedSource(p, readLocalPackage(p, slashRel, tc).dropped) {
				return nil
			}
			if !generationCanStage(p, slashRel, tc) {
				return nil
			}
			seen[slashRel] = struct{}{}
			return nil
		})
	}
	dirs := make([]string, 0, len(seen))
	for dir := range seen {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	return dirs
}

// Gazelle recomputes staging_srcs on every run, so "stage it by hand" without
// this half is advice that undoes itself: keep.go.
const stageByHandAdvice = "stage the files by hand -- a target of your own, named in " +
	"staging_srcs with a \"# keep\" comment on its line, since Gazelle regenerates " +
	"staging_srcs on every run and drops an entry no \"# keep\" holds."

// generationCanStage reports whether generation in absDir will write the
// filegroup this label would name. Naming one it will not is a dangling label,
// which fails analysis for the whole workspace; saying so is the alternative.
func generationCanStage(absDir, rel string, tc *tsConfig) bool {
	if readLocalPackage(absDir, rel, tc).ignored {
		log.Printf("typescript: %s detected: %s holds staged sources but a ts_ignore directive "+
			"stops Gazelle writing anything there, so no \"sources\" filegroup exists to stage "+
			"them and the bundle does not name the directory. Drop the directive, or %s",
			frameworkName(tc.detectedFramework), rel, stageByHandAdvice)
		return false
	}
	if tc.packageBoundaryMode == boundaryIndexOnly && !dirIsItsOwnPackage(absDir) {
		log.Printf("typescript: %s detected: %s holds staged sources but index-only package "+
			"boundaries roll them into the nearest package, so no \"sources\" filegroup is "+
			"written there and the bundle does not name it. Add an index.ts, a "+
			"# gazelle:ts_package_boundary true, or %s",
			frameworkName(tc.detectedFramework), rel, stageByHandAdvice)
		return false
	}
	return true
}

func holdsStagedSource(absDir string, dropped []string) bool {
	entries, err := os.ReadDir(absDir)
	if err != nil {
		return false
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && !isConfiguredExclude(entry.Name(), dropped) {
			names = append(names, entry.Name())
		}
	}
	return len(stagedSources(names)) > 0
}

// localPackage is what a directory's own BUILD file says about generation there.
// A label computed from the root reads it, or it names a target nothing writes.
type localPackage struct {
	tc      *tsConfig
	file    *rule.File
	dropped []string
	ignored bool
}

func readLocalPackage(absDir, rel string, tc *tsConfig) localPackage {
	local := *tc
	lp := localPackage{tc: &local}
	if rel == "" {
		return lp
	}
	local.targetName = ""
	for _, buildName := range []string{"BUILD.bazel", "BUILD"} {
		f, err := rule.LoadFile(filepath.Join(absDir, buildName), rel)
		if err != nil {
			continue
		}
		lp.file = f
		for _, d := range f.Directives {
			value := strings.TrimSpace(d.Value)
			switch d.Key {
			case directiveTargetName:
				local.targetName = d.Value
			case directiveIgnore:
				if d.Value != "false" {
					lp.ignored = true
				}
			case directiveExclude:
				if value != "" {
					local.excludePatterns = append(append([]string(nil), local.excludePatterns...), value)
				}
			case "exclude":
				if value != "" {
					lp.dropped = append(lp.dropped, value)
				}
			}
		}
		break
	}
	return lp
}

// isStagedDir returns true when rel is a stage directory for the detected
// framework, or a directory beneath one. This is used by generateRules to
// decide whether to emit a filegroup alongside the ts_compile target.
func isStagedDir(rel string, tc *tsConfig) bool {
	cfg, ok := frameworkConfigs[tc.detectedFramework]
	if !ok {
		return false
	}
	for _, d := range cfg.StageDirs {
		if rel == d || strings.HasPrefix(rel, d+"/") {
			return true
		}
	}
	return false
}

// bundleEntryPoint is the framework's conventional entry_point once a target
// carries it, else one already in the BUILD file that resolves, else "".
func bundleEntryPoint(args language.GenerateArgs, tc *tsConfig, cfg FrameworkBundleConfig) string {
	if pkg, target, ok := splitPackageLabel(cfg.EntryPoint); ok && entryTargetIsCovered(args, tc, pkg, target) {
		return cfg.EntryPoint
	}
	if args.File == nil {
		return ""
	}
	for _, r := range args.File.Rules {
		if r.Kind() != "ts_bundle" || r.Name() != cfg.BundleName {
			continue
		}
		have := r.AttrString("entry_point")
		if pkg, target, ok := splitPackageLabel(have); ok && entryTargetIsCovered(args, tc, pkg, target) {
			return have
		}
	}
	return ""
}

// existingBundleEntryPoint is the entry_point of the bundle rule already in the
// BUILD file, and whether that rule is there at all.
func existingBundleEntryPoint(args language.GenerateArgs, cfg FrameworkBundleConfig) (string, bool) {
	if args.File == nil {
		return "", false
	}
	for _, r := range args.File.Rules {
		if r.Kind() == "ts_bundle" && r.Name() == cfg.BundleName {
			return r.AttrString("entry_point"), true
		}
	}
	return "", false
}
