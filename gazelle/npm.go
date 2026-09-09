package typescript

import (
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"

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

// label spells name for an import from a file in dir: the importer's own
// package when the nearest importer above dir declares it, else the root's.
func (l *npmLock) label(name, dir string) string {
	if imp := l.importerAbove(dir); imp != "" {
		if _, ok := l.importers[imp].deps[name]; ok {
			return "@npm//" + imp + ":" + npmPackageToLabelName(name)
		}
	}
	return "@npm//:" + npmPackageToLabelName(name)
}

// memberView is the hub's view of the member a bare specifier names, from
// every package but the member's own ts_compile, where it is nothing.
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
	return "@npm//:" + npmPackageToLabelName(name), true
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
// and devDependencies, a member's name as its view, the rest lockfile-gated.
func (l *npmLock) manifestLabels(m *manifest) []string {
	if m == nil {
		return nil
	}
	var labels []string
	for _, name := range m.deps {
		switch _, member := l.members[name]; {
		case member:
			labels = append(labels, "@npm//:"+npmPackageToLabelName(name))
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
