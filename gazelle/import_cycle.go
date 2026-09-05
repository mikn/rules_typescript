package typescript

// A cycle between generated targets is reported here, not fixed here. Under
// per-target compilation it is genuine even when every edge is `import type`:
// each target type-checks its own sources under noEmitOnError, where an
// unresolvable `import type` is a hard TS2307. Merging the cyclic directories
// into one
// target would be ts_package_boundary applied behind the user's back --
// different labels, coarser granularity, no consent -- so this file only says
// what Bazel is about to reject, and where it came from.
//
// One rule decides what an edge is: an import a source of the emitted target
// writes, whose resolved label that target's emitted deps carry. Both halves
// are read off the rule Bazel will load wherever that differs from the one
// Gazelle computed, so an import the emitted deps leave out is no edge, a
// source the emitted srcs leave out writes no edge, a dep no import explains
// is no edge, and a cycle left with too few edges to close is not this file's
// to report.
//
// The message names the component and stops. Naming the import behind an edge
// is the one thing Bazel's own loop of labels cannot do, but it is also a
// claim about which imports carry the cycle and what removing one would
// achieve, and three things falsify such a claim: a held srcs list means the
// named file is not one the target compiles, a held deps list can drop an
// import running between two of the named packages out of the edge list, and a
// held deps list that agrees with the imports means deleting the import leaves
// the label behind. docs/gazelle/overview.md § Import Cycles Between Packages.

import (
	"log"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bazelbuild/bazel-gazelle/config"
	"github.com/bazelbuild/bazel-gazelle/label"
	"github.com/bazelbuild/bazel-gazelle/resolve"
	"github.com/bazelbuild/bazel-gazelle/rule"
)

// cycleNode is one generated target, kept with what re-deriving its edges
// needs: the config its directory was resolved under, the rule its ambient
// module names come from, and the two halves of the edge rule.
type cycleNode struct {
	from label.Label
	c    *config.Config
	r    *rule.Rule
	srcs []string
	deps map[string]bool
}

// cycleGraph holds the candidate graph over generated targets, recorded edge by
// edge as each target's imports resolve. Only Resolve writes to it, and
// cmd/gazelle resolves serially, so it needs no lock.
type cycleGraph struct {
	ix    *resolve.RuleIndex
	nodes map[string]*cycleNode
	order []string
	out   map[string]map[string]bool
}

// note records one target and the targets its imports resolved to. deps are
// import-derived only: a label from ts_ambient_types or runtimeDeps.test is a
// configured dep, not an edge any source file asked for. A recorded edge
// nominates a candidate and nothing more; the edge rule is applied to it in
// confirmedFrom.
func (g *cycleGraph) note(
	c *config.Config,
	ix *resolve.RuleIndex,
	r *rule.Rule,
	from label.Label,
	deps []string,
) {
	if g.nodes == nil {
		g.nodes = map[string]*cycleNode{}
		g.out = map[string]map[string]bool{}
	}
	g.ix = ix

	key := targetKey(from)
	if _, seen := g.nodes[key]; !seen {
		g.order = append(g.order, key)
	}
	g.nodes[key] = &cycleNode{
		from: from,
		c:    c,
		r:    r,
		srcs: emittedSrcs(r),
		deps: emittedDeps(r, from),
	}

	for _, dep := range deps {
		to, ok := sameRepoTargetKey(dep, from)
		if !ok || to == key {
			continue
		}
		if g.out[key] == nil {
			g.out[key] = map[string]bool{}
		}
		g.out[key][to] = true
	}
}

// emittedSrcs and emittedDeps are the two halves of the edge rule, each read
// off the rule Bazel will load wherever that differs from the generated one.
// rule.MergeRules returns on a "# keep"ed rule and skips
// a "# keep"ed assignment, so past either gate the value Gazelle computed
// reaches no BUILD file and the author's list is the whole of it; markKeptAttrs
// hands those held lists here on private attrs, which needs no merge to carry
// them -- cmd/gazelle passes Resolve the generated rule, the object
// markKeptAttrs wrote to, and MergeRules copies private last and returns on a
// kept rule first. Past neither gate the merged value is what Gazelle computed
// plus whatever a "# keep" holds beside it, so the generated rule names no
// source the target will not compile and no dep it will not carry.
func emittedSrcs(r *rule.Rule) []string {
	if kept, held := r.PrivateAttr(keptSrcsAttr).([]string); held {
		return kept
	}
	return r.AttrStrings("srcs")
}

func emittedDeps(r *rule.Rule, from label.Label) map[string]bool {
	deps := r.AttrStrings("deps")
	if kept, held := r.PrivateAttr(keptDepsAttr).([]string); held {
		deps = kept
	}
	emitted := make(map[string]bool, len(deps))
	for _, dep := range deps {
		if to, ok := sameRepoTargetKey(dep, from); ok {
			emitted[to] = true
		}
	}
	return emitted
}

// targetKey is the one spelling of a label this graph keys on: label.String()
// elides the name when it repeats the last path segment, so //src/lib and
// //src/lib:lib are one target under two strings.
func targetKey(l label.Label) string {
	return "//" + l.Pkg + ":" + l.Name
}

// sameRepoTargetKey reads a generated dep string as a target in this repo.
// A label in another repository -- an npm hub, a toolchain -- is not a target
// this extension generates, so it is not a node and cannot be in a cycle.
//
// A bare "//pkg:name" dep carries no repository, while the label Gazelle hands
// the resolver carries the main repo's name (cmd/gazelle builds it from
// c.RepoName, "lovable_repo_root" in the monorepo). Comparing the two verbatim
// makes every generated dep look foreign and the graph edgeless, so an absent
// repository is read as this one.
func sameRepoTargetKey(dep string, from label.Label) (string, bool) {
	l, err := label.Parse(dep)
	if err != nil {
		return "", false
	}
	if l.Relative {
		return targetKey(label.New(from.Repo, from.Pkg, l.Name)), true
	}
	if l.Repo != "" && l.Repo != from.Repo {
		return "", false
	}
	return targetKey(l), true
}

// reportCycles names every cross-package cycle among the recorded targets, once
// each, and empties the graph. A component confined to one directory is left
// alone: the doc and test splits put two targets in one directory, and a cycle
// between either of those and the library goes unreported: see
// docs/gazelle/overview.md § Import Cycles Between Packages.
func (g *cycleGraph) reportCycles() {
	defer func() { *g = cycleGraph{} }()

	for _, candidate := range stronglyConnected(g.order, g.out, g.nodes) {
		if len(candidate) < 2 || len(packagesIn(candidate)) < 2 {
			continue
		}
		g.reportCyclesWithin(candidate)
	}
}

// stronglyConnected is Tarjan over out, visiting in the given order so the
// components and their members come out the same on every run. Only a target in
// nodes is followed: an edge out of the graph is not part of a cycle in it.
func stronglyConnected(
	order []string,
	out map[string]map[string]bool,
	nodes map[string]*cycleNode,
) [][]string {
	const unvisited = 0

	index := map[string]int{}
	low := map[string]int{}
	onStack := map[string]bool{}
	var stack []string
	next := 1
	var components [][]string

	var visit func(n string)
	visit = func(n string) {
		index[n] = next
		low[n] = next
		next++
		stack = append(stack, n)
		onStack[n] = true

		for _, m := range sortedKeys(out[n]) {
			if _, known := nodes[m]; !known {
				continue
			}
			switch {
			case index[m] == unvisited:
				visit(m)
				low[n] = min(low[n], low[m])
			case onStack[m]:
				low[n] = min(low[n], index[m])
			}
		}

		if low[n] != index[n] {
			return
		}
		var component []string
		for {
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			onStack[top] = false
			component = append(component, top)
			if top == n {
				break
			}
		}
		sort.Strings(component)
		components = append(components, component)
	}

	for _, n := range order {
		if index[n] == unvisited {
			visit(n)
		}
	}
	return components
}

func packagesIn(component []string) []string {
	seen := map[string]bool{}
	var pkgs []string
	for _, key := range component {
		pkg := strings.SplitN(strings.TrimPrefix(key, "//"), ":", 2)[0]
		if !seen[pkg] {
			seen[pkg] = true
			pkgs = append(pkgs, pkg)
		}
	}
	sort.Strings(pkgs)
	return pkgs
}

// reportCyclesWithin re-reads the candidate's sources and reports each cycle the
// edge rule leaves closed inside it. The candidate is a superset: a ts_test
// target resolves deps from its package's production and doc sources as well as
// its own, so it holds labels no source of its own imports, and a held srcs or
// deps list can drop an edge the resolver computed.
func (g *cycleGraph) reportCyclesWithin(candidate []string) {
	inCandidate := map[string]bool{}
	for _, key := range candidate {
		inCandidate[key] = true
	}

	confirmed := map[string]map[string]bool{}
	for _, key := range candidate {
		confirmed[key] = g.confirmedFrom(key, inCandidate)
	}

	for _, component := range stronglyConnected(candidate, confirmed, g.nodes) {
		if len(component) < 2 || len(packagesIn(component)) < 2 {
			continue
		}
		log.Printf("typescript: %d packages import each other, and their targets are a "+
			"dependency cycle Bazel rejects: %s. Every edge of it is an import one of "+
			"these targets' own sources writes, resolved to a label that target's deps "+
			"will carry; a deps entry names the label, never the import behind it.",
			len(packagesIn(component)), strings.Join(component, ", "))
	}
}

// confirmedFrom is the candidate's members this target's own sources import and
// its own deps carry -- the edges the report is a claim about. Only a candidate
// pays for the re-read: the recorded graph is enough to find one.
func (g *cycleGraph) confirmedFrom(key string, inCandidate map[string]bool) map[string]bool {
	n := g.nodes[key]
	if n == nil {
		return nil
	}
	tc := getConfig(n.c)
	ambient := ambientModuleNames(n.c, n.r, n.from)
	pkgDir := filepath.Join(n.c.RepoRoot, filepath.FromSlash(n.from.Pkg))

	confirmed := map[string]bool{}
	for _, src := range n.srcs {
		if isLabelSrc(src) || !isScannableSrc(src) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(pkgDir, filepath.FromSlash(src)))
		if err != nil {
			continue
		}
		dirRel := path.Dir(src)
		for _, imp := range ScanImports(string(data)) {
			resolved := resolveImport(
				n.c, g.ix, tc, n.r.Kind(), ambient, rebaseRelative(imp.Specifier, dirRel), n.from)
			if resolved == "" {
				continue
			}
			to, ok := sameRepoTargetKey(resolved, n.from)
			if !ok || to == key || !inCandidate[to] || !n.deps[to] {
				continue
			}
			confirmed[to] = true
		}
	}
	return confirmed
}

// isScannableSrc reports whether a src is source the import scanner reads. A
// .css or .json src of a generated target carries no module specifiers.
func isScannableSrc(src string) bool {
	for _, ext := range []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".mts", ".cts"} {
		if strings.HasSuffix(src, ext) {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
