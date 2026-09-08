package typescript

import (
	"log"
	"maps"
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
func importsForRule(_ *config.Config, r *rule.Rule, f *rule.File) []resolve.ImportSpec {
	switch r.Kind() {
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
	for dir := path.Dir(imp); ; dir = path.Dir(dir) {
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
	edges := imps.edges
	if imps.config != "" {
		configEdges := tc.programs.configEdges(c.RepoRoot, imps.config)
		edges = append(edges, configEdges...)
		srcs := configSrcLabels(tc.programs.configSrcs(c.RepoRoot, imps.config),
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
	for _, e := range edges {
		if dep := edgeDep(c, ix, tc, r.Kind(), e, from, reported); dep != "" {
			deps[dep] = true
		}
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

// configSrcLabels spells the modules cfg reaches from the test's package: by
// path when the config is its own, else as the config's package's files.
func configSrcLabels(files []string, cfg string, from label.Label) []string {
	dir := parentDir(cfg)
	var out []string
	for _, f := range files {
		rel, under := strings.CutPrefix(f, dir+"/")
		if dir == "" {
			rel, under = f, true
		}
		if !under {
			log.Printf("typescript: %s: %s imports %s, outside the config's package "+
				"%s; a config's modules are staged from its package alone, so no "+
				"config_srcs entry", from, cfg, f, orRepoRoot(dir))
			continue
		}
		if dir == from.Pkg {
			if lbl, ok := srcLabel(rel); ok {
				out = append(out, lbl)
			}
			continue
		}
		out = append(out, "//"+dir+":"+rel)
	}
	return out
}

func edgeDep(c *config.Config, ix *resolve.RuleIndex, tc *tsConfig, kind string,
	e explainfiles.Edge, from label.Label, reported map[string]bool) string {
	s := tc.programs
	if !firstParty(e.To) {
		if npmPackageName(e.To) == "" {
			return ""
		}
		if tc.lock == nil {
			if !s.noLockSaid {
				s.noLockSaid = true
				log.Printf("typescript: no %s at the repository root, so no hub "+
					"declares an npm package; npm imports get no dep", pnpmLockfileName)
			}
			return ""
		}
		return tc.lock.edgeLabel(e, from.Pkg, kind)
	}
	spec := resolve.ImportSpec{Lang: languageName, Imp: e.To}
	if lbl, ok := resolve.FindRuleWithOverride(c, spec, languageName); ok {
		return lbl.Rel(from.Repo, from.Pkg).String()
	}
	if codegen, ok := tc.codegenOuts[e.To]; ok {
		return codegen.Rel(from.Repo, from.Pkg).String()
	}
	if lbl := resolveCodegenTree(ix, e.To, from); lbl != "" {
		return lbl
	}
	if tc.lock != nil {
		if lbl, ok := tc.lock.memberView(e.Specifier, from.Pkg, kind); ok {
			return lbl
		}
	}
	if lbl, held := ruleHolding(ix, spec, from); held {
		return lbl
	}
	if owner := s.owner(e.To); owner == "" {
		reportEdge(from, e, s.whyUnowned(e.To), reported)
	} else {
		reportEdge(from, e, tsconfigIn(owner)+" lists it and no rule there has it "+
			"in srcs", reported)
	}
	return ""
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
