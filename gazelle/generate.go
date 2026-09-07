package typescript

import (
	"fmt"
	"log"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/bazelbuild/bazel-gazelle/language"
	"github.com/bazelbuild/bazel-gazelle/rule"
	bzl "github.com/bazelbuild/buildtools/build"

	"github.com/mikn/rules_typescript/ts/tools/tsconfig"
)

// globExprPrefix marks a CodegenPattern.Srcs entry that is a glob() call to be
// rendered as Starlark rather than quoted as a file name.
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

// TypeScript exempts declaration files from the module-ness .mts / .cts force on
// a source, so a .d.mts is read the way a .d.ts is: a script declares globals.
func isDeclarationFile(name string) bool {
	return strings.HasSuffix(name, ".d.ts") || strings.HasSuffix(name, ".d.mts") || strings.HasSuffix(name, ".d.cts")
}

// isCompileSrcFile is isTypeScriptFile widened by the declaration flavours and
// by whatever a ts_js_srcs directive admitted. It answers the srcs question
// only: what makes a file an index stays .ts/.tsx, since an admitted .mjs is
// compiled by the target that claims it and is not a reason for a directory to
// become a package.
func isCompileSrcFile(name string, jsSrcExts []string) bool {
	return isTypeScriptFile(name) || isDeclarationFile(name) ||
		slices.Contains(jsSrcExts, strings.ToLower(path.Ext(name)))
}

// isAmbientDeclaration returns true for a declaration file that declares
// globals rather than exporting a module. Nothing can import one, so srcs
// membership is the only way it reaches a program.
func isAmbientDeclaration(dir, name string) bool {
	if !isDeclarationFile(name) {
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
	base := trimSourceExtension(name)
	return strings.HasSuffix(base, ".test") || strings.HasSuffix(base, ".spec")
}

// trimSourceExtension drops the extension of a file a generated target
// compiles, so .test / .spec / .doc read a .mjs the way they read a .ts.
func trimSourceExtension(name string) string {
	for _, ext := range []string{".tsx", ".ts", ".mjs", ".cjs", ".js"} {
		if strings.HasSuffix(name, ext) {
			return strings.TrimSuffix(name, ext)
		}
	}
	return name
}

// isDocFile returns true for files that document or demonstrate the package.
// Patterns: *.doc.ts, *.doc.tsx, *.stories.ts, *.stories.tsx
func isDocFile(name string) bool {
	base := trimSourceExtension(name)
	return strings.HasSuffix(base, ".doc") || strings.HasSuffix(base, ".stories")
}

// isFrameworkGeneratedFile reports whether the build writes name itself. Only
// the TanStack Start route tree does: the Start Vite plugin regenerates it on
// every build, and the tree it emits imports the router module that imports the
// tree back, which is a cycle between two Bazel packages however the two are
// split.
//
// A checked-in file no rule declares as an output is an ordinary source however
// it is named -- see claimedSrcs, which is what defers to ts_codegen. A
// workspace that wants one out of its source targets anyway names it in
// # gazelle:ts_exclude, which excludeSet.dropsBy reads.
func isFrameworkGeneratedFile(name string) bool {
	base := strings.TrimSuffix(strings.TrimSuffix(name, ".tsx"), ".ts")
	return base == "routeTree.gen"
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

// ---- generate entry point --------------------------------------------------

// generateRules writes one directory: a package's targets, or the withdrawal
// of what an earlier run left where no program is.
func generateRules(args language.GenerateArgs) language.GenerateResult {
	tc := getConfig(args.Config)
	if tc.ignore {
		return emptyResult(args)
	}
	s := tc.programs
	s.visit(args.Rel, args.RegularFiles)
	if root, ok := codegenOutDirOwning(args.Rel, tc); ok {
		return codegenOutDirResult(args, root)
	}
	if s.packages[args.Rel] == nil {
		return nonPackageRules(args, tc)
	}
	return packageRules(args, tc)
}

// packageName is the ts_compile's name in rel: the directory's basename.
func packageName(rel string) string {
	if rel == "" {
		return "root"
	}
	return path.Base(rel)
}

// ownedRuleNames is every kind and name Gazelle writes in rel, which is what
// it withdraws where it writes nothing.
func ownedRuleNames(rel string) []*rule.Rule {
	name := packageName(rel)
	return []*rule.Rule{
		rule.NewRule("ts_compile", name),
		rule.NewRule("ts_test", testTargetName(name)),
		rule.NewRule("ts_lint", name+"_lint"),
		rule.NewRule("ts_config", tsConfigTargetName),
		rule.NewRule("filegroup", vitestConfigTargetName),
	}
}

func emptyResult(args language.GenerateArgs) language.GenerateResult {
	return language.GenerateResult{Empty: ownedRuleNames(args.Rel)}
}

// No program: every rule Gazelle would write is withdrawn and a BUILD file
// holding one named; an extended tsconfig.json and the root's config stay.
func nonPackageRules(args language.GenerateArgs, tc *tsConfig,
) language.GenerateResult {
	var res language.GenerateResult
	var held []string
	for _, r := range ownedRuleNames(args.Rel) {
		switch {
		case r.Kind() == "ts_config" && tc.programs.extended[args.Rel] &&
			handWrittenTsConfigIn(args.Dir, args.Config.RepoRoot) != "":
			res.Gen = append(res.Gen, tsConfigRule(args, tc))
			res.Imports = append(res.Imports, nil)
		case r.Kind() == "filegroup" && args.Rel == "" &&
			exportedVitestConfig(args.Dir, "") != "":
			res.Gen = append(res.Gen, vitestConfigRule(args))
			res.Imports = append(res.Imports, nil)
		default:
			res.Empty = append(res.Empty, r)
			if ruleExists(args, r.Kind(), r.Name()) {
				held = append(held, r.Kind()+"("+r.Name()+")")
			}
		}
	}
	if len(held) > 0 {
		log.Printf("typescript: %s: %s is not a package -- no tsconfig.json "+
			"here lists a first-party file -- so %s is withdrawn; Gazelle "+
			"cannot delete the file, so delete it by hand if nothing else is "+
			"in it", args.File.Path, orRepoRoot(args.Rel), strings.Join(held, ", "))
	}
	reportManagedAttrDrops(args, res.Gen)
	return res
}

// packageRules writes a package: ts_compile, ts_test, the declarations in
// both, the tree's other files in the first that exists, its ts_config.
func packageRules(args language.GenerateArgs, tc *tsConfig,
) language.GenerateResult {
	s, pkg := tc.programs, args.Rel
	name := packageName(pkg)
	set := s.srcs(pkg, tc)
	data := s.dataFiles(pkg, tc)
	codegens := codegenLabels(args.File)
	tsConfigAttr := ":" + tsConfigTargetName
	var res language.GenerateResult
	add := func(r *rule.Rule, imports any) {
		res.Gen = append(res.Gen, r)
		res.Imports = append(res.Imports, imports)
	}
	withdraw := func(kind, name string) {
		res.Empty = append(res.Empty, rule.NewRule(kind, name))
	}

	compile := len(set.library) > 0
	if compile {
		program := packageSrcs(args, set.library, set.declaration)
		r := rule.NewRule("ts_compile", name)
		r.SetAttr("srcs", packageSrcs(args, set.library, set.declaration, data))
		r.SetAttr("tsconfig", tsConfigAttr)
		r.SetAttr("visibility", []string{"//visibility:public"})
		imps := s.compileImports(pkg, set)
		imps.deps = append(imps.deps, codegens...)
		add(r, imps)
		if lint, gone := lintRuleFor(args, tc, name, program); lint != nil {
			add(lint, nil)
		} else {
			res.Empty = append(res.Empty, gone)
		}
	} else {
		withdraw("ts_compile", name)
		withdraw("ts_lint", name+"_lint")
	}

	if len(set.test) > 0 {
		r := rule.NewRule("ts_test", testTargetName(name))
		var extra []string
		if !compile {
			extra = data
		}
		r.SetAttr("srcs", packageSrcs(args, set.test, set.declaration, extra))
		attr, cfg := vitestConfigFor(args, tc)
		if attr != "" {
			r.SetAttr("config", attr)
		}
		r.SetAttr("tsconfig", tsConfigAttr)
		compileLabel := ""
		if compile {
			compileLabel = ":" + name
		}
		imps := s.testImports(args.Config.RepoRoot, tc.lock, pkg, compileLabel,
			cfg, set)
		imps.deps = append(imps.deps, codegens...)
		add(r, imps)
	} else {
		withdraw("ts_test", testTargetName(name))
	}
	if !compile && len(set.test) == 0 {
		s.say("%s: the program lists only declaration files, so no target "+
			"compiles them or stages the other files under %s",
			tsconfigIn(pkg), orRepoRoot(pkg))
	}

	add(tsConfigRule(args, tc), nil)
	if exportedVitestConfig(args.Dir, pkg) != "" {
		add(vitestConfigRule(args), nil)
	} else {
		withdraw("filegroup", vitestConfigTargetName)
	}
	reportManagedAttrDrops(args, res.Gen)
	return res
}

// packageSrcs is the given repository paths as the package's srcs: relative to
// it, once each, sorted, and every name a label can spell.
func packageSrcs(args language.GenerateArgs, lists ...[]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, list := range lists {
		for _, f := range list {
			rel := f
			if args.Rel != "" {
				rel = strings.TrimPrefix(f, args.Rel+"/")
			}
			if seen[rel] {
				continue
			}
			seen[rel] = true
			lbl, ok := srcLabel(rel)
			if !ok {
				reportUnlabelableFile(args, rel)
				continue
			}
			out = append(out, lbl)
		}
	}
	sort.Strings(out)
	return out
}

// A file tsgo may list is a src only when it does; every other regular file
// under the package is one by the walk.
var programExtensions = append([]string{".js", ".jsx", ".mjs", ".cjs"},
	tsSourceExtensions...)

func programCandidate(name string) bool {
	return slices.Contains(programExtensions, strings.ToLower(path.Ext(name)))
}

// dataFiles is every regular file under pkg's tree no listing decides; not a
// deeper package's, an out_dir's, a BUILD file or the ts_config's own src.
func (s *programStore) dataFiles(pkg string, tc *tsConfig) []string {
	var out []string
	for _, dir := range slices.Sorted(maps.Keys(s.files)) {
		if dir != pkg && !dirIsAncestorOf(pkg, dir) {
			continue
		}
		if s.nearestPackage(dir) != pkg {
			continue
		}
		if _, gen := codegenOutDirOwning(dir, tc); gen {
			continue
		}
		for _, f := range s.files[dir] {
			switch {
			case f == "BUILD.bazel", f == "BUILD", programCandidate(f):
			case dir == pkg && f == "tsconfig.json":
			default:
				out = append(out, path.Join(dir, f))
			}
		}
	}
	return out
}

// nearestPackage is the package at or above dir; "" when there is none.
func (s *programStore) nearestPackage(dir string) string {
	for ; ; dir = parentDir(dir) {
		if s.packages[dir] != nil || dir == "" {
			return dir
		}
	}
}

// codegenLabels is every ts_codegen declared in f: a dep of every target there.
func codegenLabels(f *rule.File) []string {
	if f == nil {
		return nil
	}
	var out []string
	for _, r := range f.Rules {
		if r.Kind() == "ts_codegen" {
			out = append(out, ":"+r.Name())
		}
	}
	return out
}

// The ts_lint beside a ts_compile while a linter config is in force and the
// lockfile declares the linter; otherwise the withdrawal of one a run wrote.
func lintRuleFor(args language.GenerateArgs, tc *tsConfig, name string,
	srcs []string) (*rule.Rule, *rule.Rule) {
	gone := rule.NewRule("ts_lint", name+"_lint")
	if tc.linterConfig == "" || tc.linterType == "" {
		return nil, gone
	}
	if !hubCouldDeclare(tc, tc.linterType) {
		reportLinterNotInLockfile(args.Config.RepoRoot, tc)
		return nil, gone
	}
	r := rule.NewRule("ts_lint", name+"_lint")
	r.SetAttr("srcs", srcs)
	r.SetAttr("linter", tc.linterType)
	r.SetAttr("linter_binary", linterBinaryLabel(tc))
	r.SetAttr("config", linterConfigLabel(tc.linterConfig))
	return r, nil
}

// tsConfigRule names the directory's tsconfig.json, with the ts_config of every
// tsconfig.json its extends names as a dep.
func tsConfigRule(args language.GenerateArgs, tc *tsConfig) *rule.Rule {
	r := rule.NewRule("ts_config", tsConfigTargetName)
	r.SetAttr("src", "tsconfig.json")
	var deps []string
	for _, base := range tc.programs.bases[args.Rel] {
		deps = append(deps, "//"+base+":"+tsConfigTargetName)
	}
	if len(deps) > 0 {
		sort.Strings(deps)
		r.SetAttr("deps", deps)
	}
	r.SetAttr("visibility", []string{"//visibility:public"})
	return r
}

// ---- helper functions ------------------------------------------------------

// ruleExists returns true when the BUILD file already contains a rule with the
// given kind and name.
func ruleExists(args language.GenerateArgs, kind, name string) bool {
	return existingRule(args, kind, name) != nil
}

func existingRule(args language.GenerateArgs, kind, name string) *rule.Rule {
	if args.File == nil {
		return nil
	}
	for _, r := range args.File.Rules {
		if r.Kind() == kind && r.Name() == name {
			return r
		}
	}
	return nil
}

// localPackage is what a directory's own BUILD file says about generation there;
// a label computed from another directory reads it or names a target nothing writes.
type localPackage struct {
	tc      *tsConfig
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
		for _, d := range f.Directives {
			switch d.Key {
			case directiveTargetName:
				local.targetName = d.Value
			case directiveIgnore:
				if d.Value != "false" {
					lp.ignored = true
				}
			}
		}
		break
	}
	return lp
}

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
	"ts_compile": true,
	"ts_test":    true,
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

func concatFiles(lists ...[]string) []string {
	var all []string
	for _, list := range lists {
		all = append(all, list...)
	}
	return all
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

// typeReferencesKey carries the names a rule's srcs reference in
// `/// <reference types>` to Resolve, where the lockfile turns each into a dep.
const typeReferencesKey = "_type_references"

func setTypeReferences(r *rule.Rule, dir string, srcs []string) {
	if names := uniqueImports(typeReferencesIn(dir, srcs)); len(names) > 0 {
		r.SetPrivateAttr(typeReferencesKey, names)
	}
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

func tsConfigNameTaken(name string) bool {
	return name == tsConfigTargetName
}

// tsConfigLabel is the label a target generated in args.Rel names for its
// baseline, or "" when there is none it can name.
//
// A tsconfig.json is a source file, so the label that reaches it has to name a
// target in the package that holds it -- and that package exists only if
// Gazelle writes a BUILD file there. Naming one it does not write is a dangling
// label, which fails analysis for the whole workspace and not just for the
// target that named it. So: refuse, and say why.
func tsConfigLabel(args language.GenerateArgs, tc *tsConfig) string {
	if tc.tsConfigFile == "" {
		return ""
	}
	dirRel := path.Dir(tc.tsConfigFile)
	if dirRel == "." {
		dirRel = ""
	}
	// Asked about the directory holding the file, not about this one: that is
	// where an anchored pattern naming it was resolved against.
	if tc.excludesIn(dirRel).drops("tsconfig.json") {
		return ""
	}
	// This package holds the file, and a rule is being generated here, so the
	// BUILD file that makes the label resolve is the one being written.
	if dirRel == args.Rel {
		return ":" + tsConfigTargetName
	}

	reach, detail := tsConfigReachFrom(args, tc, dirRel)
	switch reach {
	case reachIgnored:
		log.Printf("typescript: %s holds the tsconfig.json %s would compile under, but a "+
			"ts_ignore directive stops Gazelle writing anything there, so no target names the "+
			"file and %s keeps the ruleset's baseline. Drop the directive, or set tsconfig by "+
			"hand with a \"# keep\" on its line.", dirRel, args.Rel, args.Rel)
		return ""
	case reachBoundaryUndeclared:
		log.Printf("typescript: a ts_package_boundary directive between %s and %s leaves the "+
			"two disagreeing about whether %s becomes a package, so %s names no tsconfig "+
			"rather than risk a label into a package nothing writes. Declare the mode once, at "+
			"or above %s.", dirRel, args.Rel, dirRel, args.Rel, dirRel)
		return ""
	case reachNameTaken:
		log.Printf("typescript: %s already generates a target named %q, which is one of the "+
			"two names the ts_config beside its tsconfig.json and the filegroup staging the "+
			"declarations it names need, so %s keeps the ruleset's baseline. Rename the "+
			"directory's target with a # gazelle:ts_target_name.",
			dirRel, detail, args.Rel)
		return ""
	}
	return "//" + dirRel + ":" + tsConfigTargetName
}

// tsConfigReach is why a ts_config target in one directory is not a label a
// target generated in another may name.
type tsConfigReach int

const (
	reachOK tsConfigReach = iota
	reachIgnored
	reachBoundaryUndeclared
	reachNameTaken
)

// tsConfigReachFrom asks the one question a label into another directory turns
// on -- will Gazelle write a BUILD file there holding a ts_config -- and
// returns the target name already taken there, which is the one value any of
// the answers' messages names. dirRel is an ancestor of args.Rel, so every
// directive at or above dirRel reaches both.
func tsConfigReachFrom(args language.GenerateArgs, tc *tsConfig, dirRel string) (tsConfigReach, string) {
	repoRoot := args.Config.RepoRoot
	absDir := filepath.Join(repoRoot, filepath.FromSlash(dirRel))
	local := readLocalPackage(absDir, dirRel, tc)

	if local.ignored {
		return reachIgnored, ""
	}
	if !boundaryModeAgreesAt(repoRoot, dirRel, args.Rel) {
		return reachBoundaryUndeclared, ""
	}
	if name := targetNameForDir(local.tc, dirRel); tsConfigNameTaken(name) {
		return reachNameTaken, name
	}
	return reachOK, ""
}

// extendsChainDep is the ts_config target this directory's tsconfig.json
// extends, or "" when Gazelle will not name one. Only a single relative
// specifier naming an ancestor directory's own tsconfig.json qualifies: that is
// the shape a per-directory tsconfig split produces, and the one where the file
// on the other end already has a ts_config target and a label that reaches it.
func extendsChainDep(args language.GenerateArgs, tc *tsConfig) string {
	tsConfigPath := filepath.Join(args.Dir, "tsconfig.json")
	spec, ok := soleRelativeExtends(tsConfigPath)
	if !ok {
		return ""
	}
	basePath, ok := tsconfig.ResolveExtends(args.Dir, spec)
	if !ok || filepath.Base(basePath) != "tsconfig.json" {
		return ""
	}
	baseDir := filepath.Dir(basePath)
	rel, err := filepath.Rel(args.Config.RepoRoot, baseDir)
	if err != nil {
		return ""
	}
	dirRel := filepath.ToSlash(rel)
	if dirRel == "." {
		dirRel = ""
	}
	// A base outside the repository leaves a "../" here, which no package path
	// ever starts with, so this is also what stops a label naming nothing.
	if !dirIsAncestorOf(dirRel, args.Rel) {
		return ""
	}
	if handWrittenTsConfigIn(baseDir, args.Config.RepoRoot) == "" {
		return ""
	}
	if tc.excludesIn(dirRel).drops("tsconfig.json") {
		return ""
	}
	if reach, _ := tsConfigReachFrom(args, tc, dirRel); reach != reachOK {
		log.Printf("typescript: the tsconfig in %s extends %q, and Gazelle writes no ts_config "+
			"in %s that a label can name, so the base is not an input to the type-check here. "+
			"Declare deps by hand, or make %s a package that generates one.",
			args.Rel, spec, dirRel, dirRel)
		return ""
	}
	return "//" + dirRel + ":" + tsConfigTargetName
}

// soleRelativeExtends is the one relative specifier tsConfigPath extends. An
// array states a merge order and not which file Bazel should stage, and a
// package-form specifier resolves through node_modules; both stay the author's.
func soleRelativeExtends(tsConfigPath string) (string, bool) {
	tsc, err := tsconfig.Read(tsConfigPath)
	if err != nil {
		return "", false
	}
	if len(tsc.Extends) != 1 {
		return "", false
	}
	spec := tsc.Extends[0]
	if !strings.HasPrefix(spec, "./") && !strings.HasPrefix(spec, "../") {
		return "", false
	}
	return spec, true
}

func dirIsAncestorOf(ancestor, descendant string) bool {
	if ancestor == descendant {
		return false
	}
	return ancestor == "" || strings.HasPrefix(descendant, ancestor+"/")
}

// ownTsConfigRule is the ts_config target for this directory's own hand-written
// tsconfig.json: what makes the file a label the packages below it can name.
// nil when the directory has none.
func ownTsConfigRule(args language.GenerateArgs, tc *tsConfig) *rule.Rule {
	if tc.excludesIn(args.Rel).drops("tsconfig.json") {
		return nil
	}
	if handWrittenTsConfigIn(args.Dir, args.Config.RepoRoot) == "" {
		return nil
	}
	if tsConfigNameTaken(targetNameForDir(tc, args.Rel)) {
		return nil
	}
	r := rule.NewRule("ts_config", tsConfigTargetName)
	r.SetAttr("src", "tsconfig.json")
	if dep := extendsChainDep(args, tc); dep != "" {
		r.SetAttr("deps", []string{dep})
	}
	r.SetAttr("visibility", []string{"//visibility:public"})
	return r
}

// boundaryModeAgreesAt reports whether dirRel and fromRel are generated under
// the same boundary mode. dirRel is an ancestor of fromRel, so every directive
// at or above dirRel reaches both and the mode inherited at fromRel is dirRel's
// too -- unless a directory in between declares one.
func boundaryModeAgreesAt(repoRoot, dirRel, fromRel string) bool {
	for _, between := range dirsBetween(dirRel, fromRel) {
		absDir := filepath.Join(repoRoot, filepath.FromSlash(between))
		if _, declared := boundaryModeDeclaredIn(absDir, between); declared {
			return false
		}
	}
	return true
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

// boundaryModeInForce is the mode governing rel: the nearest declaration at or
// above it, else the repo default. Resolve gets only the importer's config.
func boundaryModeInForce(repoRoot, rel string) string {
	for dir := path.Clean(rel); ; dir = path.Dir(dir) {
		if dir == "." {
			dir = ""
		}
		absDir := filepath.Join(repoRoot, filepath.FromSlash(dir))
		if mode, declared := boundaryModeDeclaredIn(absDir, dir); declared {
			return mode
		}
		if dir == "" {
			return boundaryEveryDir
		}
	}
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
	return reserved
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

// buildCodegenCompileRule wraps a ts_codegen's TypeScript outs in the ts_compile
// that compiles them for their importers.
func buildCodegenCompileRule(name, codegen string) *rule.Rule {
	r := rule.NewRule("ts_compile", name)
	r.SetAttr("srcs", []string{":" + codegen})
	r.SetAttr("visibility", []string{"//visibility:public"})
	return r
}

// codegenSrcsExpr renders a pattern's srcs as the value of a label_list attr:
// a plain list, a glob() call, or the two summed. A glob entry that does not
// parse as a glob() call is rejected rather than dropped, since Bazel would
// otherwise be handed a target whose generator silently reads fewer inputs
// than the directive named.
func codegenSrcsExpr(srcs []string) (any, bool) {
	var plain []string
	var globs []bzl.Expr
	for _, src := range srcs {
		if !strings.HasPrefix(src, globExprPrefix) {
			plain = append(plain, src)
			continue
		}
		g, err := parseGlobExpr(src)
		if err != nil {
			log.Printf("typescript: ts_codegen srcs entry %q is not a glob() call: %v", src, err)
			return nil, false
		}
		globs = append(globs, g)
	}
	sort.Strings(plain)
	if len(globs) == 0 {
		return plain, true
	}

	terms := globs
	if len(plain) > 0 {
		terms = append([]bzl.Expr{rule.ExprFromValue(plain)}, globs...)
	}
	expr := terms[0]
	for _, term := range terms[1:] {
		expr = &bzl.BinaryExpr{X: expr, Op: "+", Y: term}
	}
	return expr, true
}

// parseGlobExpr reads one glob() call out of a directive field.
func parseGlobExpr(src string) (*bzl.CallExpr, error) {
	f, err := bzl.ParseBuild("srcs", []byte(src))
	if err != nil {
		return nil, err
	}
	if len(f.Stmt) != 1 {
		return nil, fmt.Errorf("want one expression, got %d", len(f.Stmt))
	}
	call, ok := f.Stmt[0].(*bzl.CallExpr)
	if !ok {
		return nil, fmt.Errorf("want a call expression")
	}
	if ident, ok := call.X.(*bzl.Ident); !ok || ident.Name != "glob" {
		return nil, fmt.Errorf("want a call to glob")
	}
	return call, nil
}

// globIncludePatterns returns the patterns a glob() call collects. Its
// excludes are left out: they only ever shrink the match, and a caller asking
// what a glob reaches has to assume the widest answer.
func globIncludePatterns(call *bzl.CallExpr) []string {
	var include bzl.Expr
	for _, arg := range call.List {
		if kw, ok := arg.(*bzl.AssignExpr); ok {
			if ident, ok := kw.LHS.(*bzl.Ident); ok && ident.Name == "include" {
				include = kw.RHS
			}
			continue
		}
		if include == nil {
			include = arg
		}
	}
	list, ok := include.(*bzl.ListExpr)
	if !ok {
		return nil
	}
	var patterns []string
	for _, item := range list.List {
		if str, ok := item.(*bzl.StringExpr); ok {
			patterns = append(patterns, str.Value)
		}
	}
	return patterns
}

// codegenGlobClaims returns the files in rel that an ancestor directory's
// ts_codegen glob collects. A glob is evaluated in the package holding the
// rule, so those files are inputs of that package, and a BUILD file here would
// put them in a different one -- where the glob, which does not descend into a
// subpackage, stops matching them.
func codegenGlobClaims(rel string, files []string, tc *tsConfig) map[string]struct{} {
	claimed := map[string]struct{}{}
	for _, p := range tc.customCodegens {
		if p.Dir == rel || (p.Dir != "" && !strings.HasPrefix(rel, p.Dir+"/")) {
			continue
		}
		sub := strings.TrimPrefix(rel, p.Dir+"/")
		if p.Dir == "" {
			sub = rel
		}
		for _, src := range p.Srcs {
			if !strings.HasPrefix(src, globExprPrefix) {
				continue
			}
			call, err := parseGlobExpr(src)
			if err != nil {
				continue
			}
			for _, pattern := range globIncludePatterns(call) {
				tail, ok := strings.CutPrefix(pattern, sub+"/")
				if !ok || strings.Contains(tail, "/") {
					continue
				}
				for _, f := range files {
					if ok, _ := path.Match(tail, f); ok {
						claimed[f] = struct{}{}
					}
				}
			}
		}
	}
	return claimed
}

// codegenOutDirOwning returns the out_dir root rel is at or below, if any.
func codegenOutDirOwning(rel string, tc *tsConfig) (string, bool) {
	for _, root := range tc.codegenOutDirs {
		if rel == root || strings.HasPrefix(rel, root+"/") {
			return root, true
		}
	}
	return "", false
}

// codegenOutDirsBelow returns the out_dir trees inside the package at rel,
// package-relative: the inherited roots below it plus this run's own patterns.
func codegenOutDirsBelow(rel string, tc *tsConfig, patterns []CodegenPattern) []string {
	var dirs []string
	add := func(dir string) {
		dir = path.Clean(dir)
		if dir != "." && !slices.Contains(dirs, dir) {
			dirs = append(dirs, dir)
		}
	}
	for _, root := range tc.codegenOutDirs {
		if rel == "" {
			add(root)
		} else if sub, ok := strings.CutPrefix(root, rel+"/"); ok {
			add(sub)
		}
	}
	for _, p := range patterns {
		if p.OutDir != "" {
			add(p.OutDir)
		}
	}
	sort.Strings(dirs)
	return dirs
}

// stalePackageCompile stubs the package target whose plain srcs name only files that
// are gone; a declaration a ts_codegen here writes is staged by its label, never listed.
func stalePackageCompile(args language.GenerateArgs, tc *tsConfig) []*rule.Rule {
	if args.File == nil {
		return nil
	}
	name := targetNameForDir(tc, args.Rel)
	declarations := codegenDeclarationOutputs(args.File)
	present := func(src string) bool {
		if _, generated := declarations[src]; generated {
			return false
		}
		return slices.Contains(args.GenFiles, src) || onDisk(args.Dir, src)
	}
	for _, r := range args.File.Rules {
		if r.Kind() == "ts_compile" && r.Name() == name && srcsGone(r, present) {
			return withdrawnCompile(args, name)
		}
	}
	return nil
}

// A "# keep" above the ts_compile holds the ts_lint over the same sources with it.
func withdrawnCompile(args language.GenerateArgs, name string) []*rule.Rule {
	if have := existingRule(args, "ts_compile", name); have != nil && have.ShouldKeep() {
		return nil
	}
	stubs := []*rule.Rule{rule.NewRule("ts_compile", name)}
	if ruleExists(args, "ts_lint", name+"_lint") {
		stubs = append(stubs, rule.NewRule("ts_lint", name+"_lint"))
	}
	return stubs
}

// A label, or a srcs that is not a plain list, is not judged.
func srcsGone(r *rule.Rule, present func(string) bool) bool {
	srcs := r.AttrStrings("srcs")
	if len(srcs) == 0 {
		return false
	}
	for _, src := range srcs {
		if isLabelSrc(src) || present(src) {
			return false
		}
	}
	return true
}

func onDisk(dir, src string) bool {
	_, err := os.Stat(filepath.Join(dir, filepath.FromSlash(src)))
	return err == nil
}

// codegenOutDirResult withdraws what Gazelle generates inside an out_dir. A BUILD
// file left there can be emptied but not deleted, so its package outlives the run.
func codegenOutDirResult(args language.GenerateArgs, root string) language.GenerateResult {
	res := emptyResult(args)
	if args.File == nil {
		return res
	}
	log.Printf("typescript: %s is inside %s, the out_dir of a ts_codegen, so everything in it is "+
		"that target's output and nothing here is a source. Gazelle withdraws the targets it "+
		"generated here and cannot delete the BUILD file, which keeps the directory a package "+
		"of its own; delete it by hand.", args.Rel, root)
	return res
}

// Gazelle can empty a BUILD file it did not write but cannot delete it, so the
// package -- and the hole it puts in the ancestor's glob -- outlives the run.
func warnCodegenGlobbedPackage(args language.GenerateArgs, kept []string) {
	log.Printf("typescript: %s holds files an ancestor's ts_codegen srcs glob collects, and "+
		"%v keep it a Bazel package of its own. glob() does not descend into a subpackage, so "+
		"the ancestor's rule will not see them -- and Bazel refuses to load a package whose "+
		"glob matched nothing. Move those files out, or put the tree under a "+
		"# gazelle:ts_package_boundary tsconfig so it stays one package.",
		args.Rel, kept)
}

// buildCodegenRule converts a CodegenPattern into a Bazel rule.Rule ready for
// inclusion in a GenerateResult. Returns nil when the pattern is malformed.
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

	srcs, ok := codegenSrcsExpr(p.Srcs)
	if !ok {
		return nil
	}
	r.SetAttr("srcs", srcs)

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

// vitestConfigTargetName is the filegroup Gazelle writes beside a vitest config
// for the tests in the packages below it.
const vitestConfigTargetName = "vitest_config"

func hasPackageJSON(dir string) bool {
	st, err := os.Stat(filepath.Join(dir, "package.json"))
	return err == nil && !st.IsDir()
}

// vitestRootAbove is where plain `vitest` reads its config for a test in dir with
// none of its own: the nearest ancestor holding a package.json, else repoRoot.
func vitestRootAbove(repoRoot, dir string) string {
	if dir == repoRoot || hasPackageJSON(dir) {
		return ""
	}
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
		if dir == repoRoot || hasPackageJSON(dir) {
			return dir
		}
	}
}

// exportedVitestConfig is the config the directory at rel makes a label for
// the packages below: beside a package.json or at the root, as vitest reads it.
func exportedVitestConfig(dir, rel string) string {
	if rel != "" && !hasPackageJSON(dir) {
		return ""
	}
	name := vitestConfigIn(dir)
	if name == "" || packageName(rel) == vitestConfigTargetName {
		return ""
	}
	return name
}

// vitestConfigRule is the filegroup over the directory's exported config.
func vitestConfigRule(args language.GenerateArgs) *rule.Rule {
	r := rule.NewRule("filegroup", vitestConfigTargetName)
	name := exportedVitestConfig(args.Dir, args.Rel)
	r.SetAttr("srcs", srcLabels([]string{name}))
	r.SetAttr("visibility", []string{"//visibility:public"})
	return r
}

// vitestConfigFor is a ts_test's config attribute and the file's repository
// path: beside the tests, else the nearest package.json's, itself a package.
func vitestConfigFor(args language.GenerateArgs, tc *tsConfig,
) (attr, cfg string) {
	if name := vitestConfigIn(args.Dir); name != "" {
		return name, path.Join(args.Rel, name)
	}
	repoRoot := args.Config.RepoRoot
	dir := vitestRootAbove(repoRoot, args.Dir)
	if dir == "" {
		return "", ""
	}
	rel, err := filepath.Rel(repoRoot, dir)
	if err != nil {
		return "", ""
	}
	if rel = filepath.ToSlash(rel); rel == "." {
		rel = ""
	}
	name := exportedVitestConfig(dir, rel)
	if name == "" {
		return "", ""
	}
	if rel != "" && tc.programs.packages[rel] == nil {
		log.Printf("typescript: %s: plain vitest reads %s for the tests here, "+
			"but %s is not a package, so no label reaches it and the test "+
			"runs with no config", args.Rel, path.Join(rel, name), rel)
		return "", ""
	}
	return "//" + rel + ":" + vitestConfigTargetName, path.Join(rel, name)
}

// ruleImports is what GenerateRules hands Resolve for one rule: the edges of
// the files it compiles, its vitest config and the deps no edge names.
type ruleImports struct {
	edges  []edge
	config string
	deps   []string
}

// ownedEdges is the edges of pkg's program from the given files, and the
// program's type entries, which are the tsconfig's.
func (s *programStore) ownedEdges(pkg string, files ...[]string) []edge {
	from := map[string]bool{}
	for _, list := range files {
		for _, f := range list {
			from[f] = true
		}
	}
	p := s.programs[pkg]
	var out []edge
	for _, e := range p.edges {
		if from[e.from] {
			out = append(out, e)
		}
	}
	return append(out, p.typeEdges()...)
}

func (s *programStore) compileImports(pkg string, set srcSet) *ruleImports {
	return &ruleImports{edges: s.ownedEdges(pkg, set.library, set.declaration)}
}

// A ts_test's runtime is its deps, so the manifest union joins the edges.
func (s *programStore) testImports(repoRoot string, lock *npmLock,
	pkg, compile, cfg string, set srcSet) *ruleImports {
	imps := &ruleImports{
		edges:  s.ownedEdges(pkg, set.library, set.declaration, set.test),
		config: cfg,
	}
	if compile != "" {
		imps.deps = append(imps.deps, compile)
	}
	if lock != nil {
		m := nearestManifest(repoRoot, pkg)
		imps.deps = append(imps.deps, lock.manifestLabels(m)...)
	}
	if cfg != "" {
		s.vitestConfig(cfg)
	}
	return imps
}
