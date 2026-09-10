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

	"github.com/bazelbuild/bazel-gazelle/label"
	"github.com/bazelbuild/bazel-gazelle/rule"

	"github.com/mikn/rules_typescript/ts/tools/explainfiles"
)

// One pnpm importer: the names it declares, each with the version it
// resolved to, and the workspace members its link: entries name.
type pnpmImporter struct {
	deps  map[string]string
	links map[string]string
}

// What pnpm-lock.yaml says the hub declares: every name it mentions, the
// importers by directory ("" is the root) and each member's directory.
type npmLock struct {
	names     map[string]bool
	importers map[string]*pnpmImporter
	members   map[string]string
}

func loadNpmLock(repoRoot string) (*npmLock, error) {
	data, err := os.ReadFile(filepath.Join(repoRoot, pnpmLockfileName))
	if err != nil {
		return nil, err
	}
	return parseNpmLock(repoRoot, strings.Split(string(data), "\n")), nil
}

// The members are the hub's (npm/lazy.bzl): every link: name, then the
// manifest name of every importer but the root that no link names.
func parseNpmLock(repoRoot string, lines []string) *npmLock {
	l := &npmLock{
		names:     parsePnpmLockNames(lines),
		importers: parsePnpmImporters(lines),
		members:   map[string]string{},
	}
	linked := map[string]bool{}
	for _, imp := range l.importers {
		for name := range imp.deps {
			l.names[name] = true
		}
		for name, dir := range imp.links {
			l.members[name] = dir
			linked[dir] = true
		}
	}
	for dir := range l.importers {
		if dir == "" || linked[dir] {
			continue
		}
		if m := readManifest(repoRoot, dir); m != nil && m.name != "" {
			l.members[m.name] = dir
		}
	}
	return l
}

// npmPackageName is the package a listed node_modules path belongs to: the
// segments after its last node_modules/, two when the first is a scope.
func npmPackageName(listed string) string {
	p := "/" + listed
	i := strings.LastIndex(p, "/node_modules/")
	if i < 0 {
		return ""
	}
	parts := strings.SplitN(p[i+len("/node_modules/"):], "/", 3)
	if strings.HasPrefix(parts[0], "@") {
		if len(parts) < 3 {
			return ""
		}
		return parts[0] + "/" + parts[1]
	}
	if len(parts) < 2 {
		return ""
	}
	return parts[0]
}

// npmPackageToLabelName is the hub's target name for a package, the twin of
// _package_name_to_label in npm/private/npm_translate_lock.bzl.
func npmPackageToLabelName(name string) string {
	return strings.ReplaceAll(strings.TrimPrefix(name, "@"), "/", "_")
}

// A bare specifier names an npm package: not relative, not absolute, not a
// package-private "#" import and not a "scheme:" module.
func isBareSpecifier(spec string) bool {
	return spec != "" && !strings.HasPrefix(spec, ".") &&
		!strings.HasPrefix(spec, "/") && !strings.HasPrefix(spec, "#") &&
		!strings.Contains(spec, ":")
}

// importerAbove is the nearest importer at or above dir; "" is the root.
func (l *npmLock) importerAbove(dir string) string {
	for ; dir != ""; dir = parentDir(dir) {
		if _, ok := l.importers[dir]; ok {
			return dir
		}
	}
	return ""
}

// nodeModulesLabel spells, for a target in pkg, the node_modules target of
// the nearest importer at or above it: the chain its npm deps resolve along.
func (l *npmLock) nodeModulesLabel(pkg string) string {
	imp := l.importerAbove(pkg)
	return label.New("", imp, nodeModulesTargetName).Rel("", pkg).String()
}

// label spells name for an import from a file in dir: under the nearest
// importer on the chain above dir that declares it, the root last.
func (l *npmLock) label(name, dir string) string {
	imp := l.importerAbove(dir)
	for ; imp != ""; imp = l.importerAbove(parentDir(imp)) {
		if _, ok := l.importers[imp].deps[name]; ok {
			return "@npm//" + imp + ":" + npmPackageToLabelName(name)
		}
	}
	return "@npm//:" + npmPackageToLabelName(name)
}

// memberView is the member a bare specifier names, from every package but the
// member's own ts_compile, where it is nothing; memberLabel spells it.
func (l *npmLock) memberView(spec, pkg, kind string) (string, bool) {
	if !isBareSpecifier(spec) {
		return "", false
	}
	name := barePackageName(spec)
	dir, ok := l.members[name]
	if !ok {
		return "", false
	}
	if kind == "ts_compile" && dir == pkg {
		return "", true
	}
	return l.memberLabel(name, pkg), true
}

// memberLabel spells member name for a target in pkg: the link target of the
// nearest importer at or above pkg that links it, or "" and a line for none.
func (l *npmLock) memberLabel(name, pkg string) string {
	for dir, more := pkg, true; more; dir, more = parentDir(dir), dir != "" {
		if imp, ok := l.importers[dir]; ok && imp.links[name] != "" {
			return label.New("", dir, "node_modules/"+name).Rel("", pkg).String()
		}
	}
	log.Printf("typescript: %s: the workspace member %q is linked by no "+
		"importer at or above it; no dep", orRepoRoot(pkg), name)
	return ""
}

// edgeLabel is the one label an edge from a file of pkg into node_modules
// takes: the specifier's package when it names one, else the listed file's.
func (l *npmLock) edgeLabel(e explainfiles.Edge, pkg, kind string) string {
	if lbl, ok := l.memberView(e.Specifier, pkg, kind); ok {
		return lbl
	}
	name := npmPackageName(e.To)
	if e.Kind == explainfiles.Import && isBareSpecifier(e.Specifier) {
		name = barePackageName(e.Specifier)
	}
	if !l.names[name] {
		log.Printf("typescript: %s: %q names the npm package %q, which %s does "+
			"not mention; no dep", e.From, e.Specifier, name, pnpmLockfileName)
		return ""
	}
	return l.label(name, parentDir(e.From))
}

// manifestLabels is a ts_test's runtime union: the manifest's dependencies
// and devDependencies, a member's link target spelled from the test's pkg.
func (l *npmLock) manifestLabels(m *manifest, pkg string) []string {
	if m == nil {
		return nil
	}
	var labels []string
	for _, name := range m.deps {
		switch _, member := l.members[name]; {
		case member:
			if lbl := l.memberLabel(name, pkg); lbl != "" {
				labels = append(labels, lbl)
			}
		case l.names[name]:
			labels = append(labels, l.label(name, m.dir))
		default:
			log.Printf("typescript: %s: %s is not in %s; no dep",
				path.Join(m.dir, "package.json"), name, pnpmLockfileName)
		}
	}
	return labels
}

// barePackageName is the package a specifier names: its first segment, two
// when the first is a scope.
func barePackageName(spec string) string {
	if strings.HasPrefix(spec, "@") {
		parts := strings.SplitN(spec[1:], "/", 3)
		if len(parts) >= 2 {
			return "@" + parts[0] + "/" + parts[1]
		}
		return spec
	}
	return strings.SplitN(spec, "/", 2)[0]
}

// An importer's rules: node_modules, a link target per member it links, and
// the store call in the lockfile's package, the root.
const (
	nodeModulesTargetName = "node_modules"
	storeTargetName       = "node_modules/.pnpm"
)

var publicVisibility = []string{"//visibility:public"}

// importerRules is what Gazelle writes in rel for the lockfile's importer
// there, or nothing when rel is no importer.
func (l *npmLock) importerRules(rel string) []*rule.Rule {
	imp, ok := l.importers[rel]
	if !ok {
		return nil
	}
	var out []*rule.Rule
	if rel == "" {
		out = append(out, rule.NewRule("npm_virtual_store", storeTargetName))
	}
	nm := rule.NewRule("node_modules", nodeModulesTargetName)
	var deps []string
	for name := range imp.deps {
		deps = append(deps, l.label(name, rel))
	}
	if len(deps) > 0 {
		sort.Strings(deps)
		nm.SetAttr("deps", deps)
	}
	if rel == "" {
		nm.SetAttr("hoist", ":"+storeTargetName+"/node_modules")
	} else {
		nm.SetAttr("parent", "//"+l.importerAbove(parentDir(rel))+":"+
			nodeModulesTargetName)
	}
	nm.SetAttr("visibility", publicVisibility)
	out = append(out, nm)
	for _, name := range slices.Sorted(maps.Keys(imp.links)) {
		link := rule.NewRule("node_modules_member", "node_modules/"+name)
		link.SetAttr("member", "@npm//:"+npmPackageToLabelName(name))
		link.SetAttr("visibility", publicVisibility)
		out = append(out, link)
	}
	return out
}

// importerEmpties is every importer rule to withdraw from f: the node_modules
// and every node_modules_member not among gen.
func importerEmpties(f *rule.File, gen []*rule.Rule) []*rule.Rule {
	kept := map[string]bool{}
	for _, r := range gen {
		kept[r.Kind()+" "+r.Name()] = true
	}
	var out []*rule.Rule
	if !kept["node_modules "+nodeModulesTargetName] {
		out = append(out, rule.NewRule("node_modules", nodeModulesTargetName))
	}
	if f == nil {
		return out
	}
	for _, r := range f.Rules {
		if r.Kind() == "node_modules_member" && !kept[r.Kind()+" "+r.Name()] {
			out = append(out, rule.NewRule(r.Kind(), r.Name()))
		}
	}
	return out
}
