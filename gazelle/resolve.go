package typescript

import (
	"log"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bazelbuild/bazel-gazelle/config"
	"github.com/bazelbuild/bazel-gazelle/label"
	"github.com/bazelbuild/bazel-gazelle/resolve"
	"github.com/bazelbuild/bazel-gazelle/rule"

	"github.com/mikn/rules_typescript/ts/tools/explainfiles"
)

// importsForRule indexes a ts_compile or ts_test by the repository path of
// each src, what an edge target is looked up by, and a ts_codegen by its tree.
func importsForRule(c *config.Config, r *rule.Rule, f *rule.File) []resolve.ImportSpec {
	switch r.Kind() {
	case "ts_config":
		specs := []resolve.ImportSpec{{Lang: languageName, Imp: "ts_config_label:" + label.New(c.RepoName, f.Pkg, r.Name()).String()}}
		src, err := label.Parse(r.AttrString("src"))
		if err != nil || r.AttrString("src") == "" {
			return specs
		}
		src = src.Abs(c.RepoName, f.Pkg)
		if src.Repo == "" {
			src.Repo = c.RepoName
		}
		if src.Repo != c.RepoName {
			return specs
		}
		return append(specs, resolve.ImportSpec{Lang: languageName, Imp: "ts_config_source:" + path.Join(src.Pkg, src.Name)})
	case "ts_proto_library":
		return protoImportsForRule(c, r, f)
	case "ts_codegen":
		return codegenTreeSpecs(r, f.Pkg)
	case "ts_compile", "ts_test":
	default:
		return nil
	}
	var specs []resolve.ImportSpec
	for _, src := range r.AttrStrings("srcs") {
		// A ":" pins a file's name to this package (srcLabel) unless it names
		// a rule here; "//" and "@" open a label, which is no file.
		name, pinned := strings.CutPrefix(src, ":")
		if strings.HasPrefix(src, "//") || strings.HasPrefix(src, "@") ||
			(pinned && namesRule(f, name)) {
			continue
		}
		specs = append(specs, resolve.ImportSpec{Lang: languageName,
			Imp: path.Join(f.Pkg, name)})
	}
	return specs
}

func namesRule(f *rule.File, name string) bool {
	for _, r := range f.Rules {
		if r.Name() == name {
			return true
		}
	}
	return false
}

// codegenTreeSpecs returns the ImportSpecs an out_dir ts_codegen answers to:
// the tree fills at build time, so its root is indexed for resolveCodegenTree.
func codegenTreeSpecs(r *rule.Rule, pkg string) []resolve.ImportSpec {
	outDir := r.AttrString("out_dir")
	if outDir == "" {
		return nil
	}
	root := path.Join(pkg, outDir)
	return []resolve.ImportSpec{
		{Lang: languageName, Imp: root},
		{Lang: languageName, Imp: codegenTreeKey(root)},
	}
}

// codegenTreeKey namespaces a codegen tree root, so the ancestor walk in
// resolveCodegenTree reaches a tree and nothing else indexed under the path.
func codegenTreeKey(root string) string {
	return "ts_codegen_tree:" + root
}

// resolveCodegenTree is the out_dir ts_codegen whose tree imp sits under;
// deepest root first, so a nested tree answers for its own subtree.
func resolveCodegenTree(ix *resolve.RuleIndex, imp string, from label.Label) string {
	for dir := imp; ; dir = path.Dir(dir) {
		if dir == "" || dir == "." || dir == "/" || dir == ".." {
			return ""
		}
		spec := resolve.ImportSpec{Lang: languageName, Imp: codegenTreeKey(dir)}
		for _, r := range ix.FindRulesByImport(spec, languageName) {
			if r.IsSelfImport(from) {
				return ""
			}
			return r.Label.Rel(from.Repo, from.Pkg).String()
		}
	}
}

// resolveEdges is deps from the listing: one label per edge target, by where
// it sits and who owns it. Core # gazelle:resolve names a file no rule indexes.
func resolveEdges(c *config.Config, ix *resolve.RuleIndex, r *rule.Rule,
	imps *ruleImports, from label.Label) {
	tc := getConfig(c)
	tc.programs.requireInstall(c.RepoRoot)
	deps := map[string]bool{}
	for _, dep := range imps.deps {
		deps[dep] = true
	}
	addCandidates := func(candidates []resolutionCandidate) {
		for _, candidate := range candidates {
			if firstParty(candidate.path) {
				if dep := resolveCodegenTree(ix, candidate.path, from); dep != "" {
					deps[dep] = true
				}
			}
		}
	}
	addCandidates(imps.candidates)
	var configEdges []explainfiles.Edge
	if imps.config != "" {
		configEdges = tc.programs.configEdges(c.RepoRoot, imps.config)
		srcs := configSrcLabels(c, tc, tc.programs.configSrcs(c.RepoRoot, imps.config),
			imps.config, from)
		if len(srcs) > 0 {
			r.SetAttr("config_srcs", srcs)
		}
		dep := workersPoolAttrs(c, tc, r, imps.config, configEdges, from)
		if dep != "" {
			deps[dep] = true
		}
	}
	reported := map[string]bool{}
	srcs := map[string]bool{}
	for _, src := range r.AttrStrings("srcs") {
		srcs[src] = true
	}
	program := tc.programs.programs[from.Pkg]
	byFrom := program.edgesBySource()
	reached := map[string]bool{}
	queue := slices.Clone(imps.edges)
	for len(queue) > 0 {
		e := queue[0]
		queue = queue[1:]
		dep, source := edgeDep(c, ix, tc, e, from, reported, srcs)
		if dep != "" {
			deps[dep] = true
		}
		if source && !reached[e.To] {
			reached[e.To] = true
			if program != nil {
				addCandidates(tc.programs.ownedCandidates(from.Pkg, []string{e.To}))
			}
			queue = append(queue, sourceEdges(byFrom, []string{e.To})...)
			if twin := javaScriptTwin(e.To, func(file string) bool {
				st, err := os.Stat(filepath.Join(c.RepoRoot, filepath.FromSlash(file)))
				return err == nil && st.Mode().IsRegular()
			}); twin != "" {
				queue = append(queue, explainfiles.Edge{From: e.From, To: twin, Specifier: e.Specifier, Kind: e.Kind})
			}
		}
	}
	for _, e := range configEdges {
		if dep, _ := edgeDep(c, ix, tc, e, from, reported, nil); dep != "" {
			deps[dep] = true
		}
	}
	if len(srcs) > 0 {
		r.SetAttr("srcs", slices.Sorted(maps.Keys(srcs)))
	}
	if imps.reportSrcDrops != nil {
		imps.reportSrcDrops(r)
	}
	// A types entry naming a codegen out that is not in the checkout (D9).
	own := filepath.Join(c.RepoRoot, filepath.FromSlash(from.Pkg), "tsconfig.json")
	for _, file := range typesEntryFiles(own, from.Pkg) {
		if codegen, ok := tc.codegenOuts[file]; ok {
			deps[codegen.Rel(from.Repo, from.Pkg).String()] = true
		}
	}
	if len(deps) > 0 {
		r.SetAttr("deps", slices.Sorted(maps.Keys(deps)))
	}
}

func configSrcLabels(c *config.Config, tc *tsConfig, files []string, cfg string, from label.Label) []string {
	dir := parentDir(cfg)
	var out []string
	for _, f := range files {
		_, under := strings.CutPrefix(f, dir+"/")
		if dir == "" {
			under = true
		}
		if !under {
			log.Printf("typescript: %s: %s imports %s, outside the config's package "+
				"%s; a config's modules are its package's files, so no "+
				"config_srcs entry", from, cfg, f, orRepoRoot(dir))
			continue
		}
		pkg := configSrcPackage(c, tc, f, dir)
		rel := strings.TrimPrefix(f, pkg+"/")
		if pkg == from.Pkg {
			if lbl, ok := srcLabel(rel); ok {
				out = append(out, lbl)
			}
			continue
		}
		out = append(out, "//"+pkg+":"+rel)
	}
	return out
}

func configSrcPackage(c *config.Config, tc *tsConfig, file, configDir string) string {
	buildRoot := c.RepoRoot
	if c.ReadBuildFilesDir != "" {
		buildRoot = c.ReadBuildFilesDir
	}
	for dir := parentDir(file); dir != configDir; dir = parentDir(dir) {
		for _, name := range c.ValidBuildFileNames {
			if st, err := os.Stat(filepath.Join(buildRoot, dir, name)); err == nil && !st.IsDir() {
				return dir
			}
		}
		if tc.programs.generatedPackages[dir] {
			return dir
		}
	}
	return configDir
}

func edgeDep(c *config.Config, ix *resolve.RuleIndex, tc *tsConfig,
	e explainfiles.Edge, from label.Label, reported, srcs map[string]bool) (dep string, source bool) {
	s := tc.programs
	if !firstParty(e.To) {
		if npmPackageName(e.To) == "" {
			return "", false
		}
		if tc.lock == nil {
			if !s.noLockSaid {
				s.noLockSaid = true
				log.Printf("typescript: no %s at the repository root, so no hub "+
					"declares an npm package; npm imports get no dep", pnpmLockfileName)
			}
			return "", false
		}
		return tc.lock.edgeLabel(e, from.Pkg), false
	}
	spec := resolve.ImportSpec{Lang: languageName, Imp: e.To}
	if lbl, ok := resolve.FindRuleWithOverride(c, spec, languageName); ok {
		return lbl.Rel(from.Repo, from.Pkg).String(), false
	}
	if codegen, ok := tc.codegenOuts[e.To]; ok {
		return codegen.Rel(from.Repo, from.Pkg).String(), false
	}
	if lbl := resolveProtoOutput(c, ix, e.To, from); lbl != "" {
		return lbl, false
	}
	if lbl := resolveCodegenTree(ix, e.To, from); lbl != "" {
		return lbl, false
	}
	if tc.lock != nil {
		if lbl, ok := tc.lock.memberView(e.Specifier, e.From, from.Pkg); ok {
			return lbl, false
		}
	}
	if lbl, held := ruleHolding(ix, spec, from); held {
		return lbl, false
	}
	if owner := s.owner(e.To); owner == "" {
		if srcs != nil && (slices.Contains(tsSourceExtensions, path.Ext(e.To)) ||
			slices.Contains([]string{".js", ".jsx", ".mjs", ".cjs", ".json"}, path.Ext(e.To))) {
			if st, err := os.Stat(filepath.Join(c.RepoRoot, filepath.FromSlash(e.To))); err == nil && st.Mode().IsRegular() {
				pkg := configSrcPackage(c, tc, e.To, "")
				name := e.To
				if pkg != "" {
					name = strings.TrimPrefix(name, pkg+"/")
				}
				if _, ok := srcLabel(name); !ok {
					reportEdge(from, e, "file name contains ':'; no Bazel label can name it", reported)
					return "", false
				}
				srcs[label.New(from.Repo, pkg, name).Rel(from.Repo, from.Pkg).String()] = true
				return "", true
			}
		}
		reportEdge(from, e, s.whyUnowned(e.To), reported)
	} else {
		reportEdge(from, e, tsconfigIn(owner)+" lists it and no rule there has it "+
			"in srcs", reported)
	}
	return "", false
}

// ruleHolding is the rule whose srcs hold the file, relative to from; a rule of
// the importing package -- its own or the package's other one -- is nothing.
func ruleHolding(ix *resolve.RuleIndex, spec resolve.ImportSpec,
	from label.Label) (lbl string, held bool) {
	for _, r := range ix.FindRulesByImport(spec, languageName) {
		if r.Label.Repo == from.Repo && r.Label.Pkg == from.Pkg {
			return "", true
		}
		return r.Label.Rel(from.Repo, from.Pkg).String(), true
	}
	return "", false
}

func reportEdge(from label.Label, e explainfiles.Edge, why string,
	said map[string]bool) {
	if said[e.To] {
		return
	}
	said[e.To] = true
	log.Printf("typescript: %s: %s imports %s: %s; no dep",
		from, e.From, e.To, why)
}

func configTypeEdges(c *config.Config, ix *resolve.RuleIndex, selected string, owner string) []explainfiles.Edge {
	configLabel, err := label.Parse(selected)
	if err != nil {
		return nil
	}
	configLabel = configLabel.Abs(c.RepoName, owner)
	if configLabel.Repo == "" {
		configLabel.Repo = c.RepoName
	}
	if configLabel.Repo != c.RepoName {
		return nil
	}
	configs := ix.FindRulesByImport(resolve.ImportSpec{Lang: languageName, Imp: "ts_config_label:" + configLabel.String()}, languageName)
	for _, p := range getConfig(c).programs.programs {
		if p.manifest || p.refused != "" {
			continue
		}
		source := tsconfigIn(p.dir)
		if len(configs) == 0 && path.Join(configLabel.Pkg, configLabel.Name) == source {
			return p.typeEdges()
		}
		for _, match := range ix.FindRulesByImport(resolve.ImportSpec{Lang: languageName, Imp: "ts_config_source:" + source}, languageName) {
			if match.Label == configLabel {
				return p.typeEdges()
			}
		}
	}
	return nil
}
