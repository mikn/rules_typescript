package typescript

import (
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

	"github.com/mikn/rules_typescript/ts/tools/explainfiles"
)

// generateRules writes one directory: a package's targets, or the withdrawal
// of what an earlier run left where no program is.
func generateRules(args language.GenerateArgs) language.GenerateResult {
	tc := getConfig(args.Config)
	s := tc.programs
	s.visit(args.Rel, args.RegularFiles)
	if root, ok := codegenOutDirOwning(args.Rel, tc); ok {
		return codegenOutDirResult(args, root)
	}
	var res language.GenerateResult
	if s.packages[args.Rel] == nil {
		res = nonPackageRules(args, tc)
	} else {
		res = packageRules(args, tc)
	}
	return withImporterRules(args, tc, res)
}

// withImporterRules adds the lockfile importer's rules for the directory, or
// withdraws them where it is no importer.
func withImporterRules(args language.GenerateArgs, tc *tsConfig,
	res language.GenerateResult) language.GenerateResult {
	var gen []*rule.Rule
	if tc.lock != nil {
		gen = tc.lock.importerRules(args.Rel)
	}
	for _, r := range gen {
		res.Gen = append(res.Gen, r)
		res.Imports = append(res.Imports, nil)
	}
	res.Empty = append(res.Empty, importerEmpties(args.File, gen)...)
	reportManagedAttrDrops(args, gen)
	return res
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
		rule.NewRule("ts_config", tsConfigTargetName),
		rule.NewRule("filegroup", vitestConfigTargetName),
		rule.NewRule("filegroup", wranglerConfigTargetName),
	}
}

func emptyResult(args language.GenerateArgs) language.GenerateResult {
	return language.GenerateResult{Empty: ownedRuleNames(args.Rel)}
}

// No program: every rule Gazelle would write is withdrawn and a BUILD file
// holding one named; an extended tsconfig.json and the root's configs stay.
func nonPackageRules(args language.GenerateArgs, tc *tsConfig,
) language.GenerateResult {
	var res language.GenerateResult
	var held []string
	rootConfig := ""
	if args.Rel == "" {
		rootConfig = exportedVitestConfig(args.Dir, "")
	}
	for _, r := range ownedRuleNames(args.Rel) {
		switch {
		case r.Kind() == "ts_config" && tc.programs.extended[args.Rel] &&
			handWrittenTsConfigIn(args.Dir, args.Config.RepoRoot) != "":
			res.Gen = append(res.Gen, tsConfigRule(args, tc))
			res.Imports = append(res.Imports, nil)
		case r.Kind() == "filegroup" && rootConfig != "":
			if fg := configFilegroup(args, tc, rootConfig, r.Name()); fg != nil {
				res.Gen = append(res.Gen, fg)
				res.Imports = append(res.Imports, nil)
				continue
			}
			fallthrough
		default:
			res.Empty = append(res.Empty, r)
			if have := existingRule(args, r.Kind(), r.Name()); have != nil &&
				!have.ShouldKeep() {
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

	nodeModules := ""
	if tc.lock != nil {
		nodeModules = tc.lock.nodeModulesLabel(pkg)
	}
	compile := len(set.library) > 0
	if compile {
		r := rule.NewRule("ts_compile", name)
		r.SetAttr("srcs", packageSrcs(args, set.library, set.declaration, data))
		r.SetAttr("tsconfig", tsConfigAttr)
		if nodeModules != "" {
			r.SetAttr("node_modules", nodeModules)
		}
		r.SetAttr("visibility", []string{"//visibility:public"})
		imps := s.compileImports(pkg, set)
		imps.deps = append(imps.deps, codegens...)
		add(r, imps)
	} else {
		withdraw("ts_compile", name)
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
		if nodeModules != "" {
			r.SetAttr("node_modules", nodeModules)
		}
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
	cfgName := exportedVitestConfig(args.Dir, pkg)
	for _, fg := range []string{vitestConfigTargetName, wranglerConfigTargetName} {
		var r *rule.Rule
		if cfgName != "" {
			r = configFilegroup(args, tc, cfgName, fg)
		}
		if r != nil {
			add(r, nil)
		} else {
			withdraw("filegroup", fg)
		}
	}
	reportTakenNames(args, res.Gen)
	reportManagedAttrDrops(args, res.Gen)
	return res
}

// The merger drops a generated rule whose name a rule of another kind holds
// without a word, so the run names the rule to rename.
func reportTakenNames(args language.GenerateArgs, gen []*rule.Rule) {
	if args.File == nil {
		return
	}
	for _, want := range gen {
		for _, have := range args.File.Rules {
			if have.Name() != want.Name() || have.Kind() == want.Kind() {
				continue
			}
			log.Printf("typescript: %s: %s(%s) holds the name Gazelle writes "+
				"for the %s of %s, so the %s is not written; rename the %s",
				args.File.Path, have.Kind(), have.Name(), want.Kind(),
				orRepoRoot(args.Rel), want.Kind(), have.Kind())
		}
	}
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

// tsConfigRule names the directory's tsconfig.json, with the ts_config of every
// tsconfig.json its extends names as a dep, its jsx preserve and its module.
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
	own := filepath.Join(args.Config.RepoRoot,
		filepath.FromSlash(tsconfigIn(args.Rel)))
	if programPreservesJsx(own) {
		r.SetAttr("jsx", "preserve")
	}
	if module := programModule(own); module != "" {
		r.SetAttr("module", module)
	}
	r.SetAttr("visibility", []string{"//visibility:public"})
	return r
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

func dirIsAncestorOf(ancestor, descendant string) bool {
	if ancestor == descendant {
		return false
	}
	return ancestor == "" || strings.HasPrefix(descendant, ancestor+"/")
}

func testTargetName(libName string) string {
	return libName + "_test"
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

// vitestConfigNames are the file names vitest itself looks for, in its own
// order of preference.
var vitestConfigNames = []string{
	"vitest.config.ts", "vitest.config.mts", "vitest.config.cts",
	"vitest.config.js", "vitest.config.mjs", "vitest.config.cjs",
	"vite.config.ts", "vite.config.mts", "vite.config.cts",
	"vite.config.js", "vite.config.mjs", "vite.config.cjs",
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

// configFilegroup is the filegroup of that name an exported vitest config puts
// in its directory: over the config, or over the wrangler config it names.
func configFilegroup(args language.GenerateArgs, tc *tsConfig, cfgName,
	name string) *rule.Rule {
	if name == vitestConfigTargetName {
		return vitestConfigRule(args)
	}
	return wranglerConfigRule(args, tc, path.Join(args.Rel, cfgName))
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
	edges  []explainfiles.Edge
	config string
	deps   []string
}

// ownedEdges is the edges of pkg's program from the given files, and the
// program's type entries, which are the tsconfig's.
func (s *programStore) ownedEdges(pkg string, files ...[]string,
) []explainfiles.Edge {
	from := map[string]bool{}
	for _, list := range files {
		for _, f := range list {
			from[f] = true
		}
	}
	p := s.programs[pkg]
	var out []explainfiles.Edge
	for _, e := range p.Edges {
		if from[e.From] {
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
		imps.deps = append(imps.deps, lock.manifestLabels(m, pkg)...)
	}
	if cfg != "" {
		s.vitestConfig(cfg)
	}
	return imps
}
