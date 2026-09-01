package typescript

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/bazelbuild/bazel-gazelle/config"
	"github.com/bazelbuild/bazel-gazelle/label"
	"github.com/bazelbuild/bazel-gazelle/rule"
)

// Seven defects so far are one defect, in two directions: Gazelle wrote a dep
// naming something that cannot exist -- Bazel answers `no such package` /
// `no such target` during ANALYSIS, failing every target in the build -- or a
// guard against that dropped a dep that named something real.
//
// The guards live apart because the specifiers reach the label by different
// routes. The invariant does not: whatever route a dep came by, it must name
// something -- and a specifier whose package is real must still produce one.
// This test is that invariant, run over a corpus holding every shape that has
// broken so far, with the exact deps each one owes.
//
// The npm half of the corpus is checked for a loadable label only. The
// inventory would be the oracle for whether the hub declares a name, and it
// under-claims -- selfImportLock's own `zod` is missing from it -- so a
// membership assertion here would encode the parser's gaps as the contract.
func TestResolveImports_EveryDepNamesSomethingLoadable(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{
		"web/src", "web/shared/lib", "web/shared/public/.well-known", "web/node_modules/acme",
		".github/scripts", "web/tools/.internal",
	} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(dir)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(root, "web/shared/public/.well-known/assetlinks.json"), "{}\n")
	writeFile(t, filepath.Join(root, "web/shared/public/.well-known/apple-app-site-association"), "{}\n")
	writeFile(t, filepath.Join(root, "web/shared/types/mobile.d.ts"), "declare module \"mobile\" {}\n")
	writeFile(t, filepath.Join(root, ".github/scripts/BUILD.bazel"), "")
	writeFile(t, filepath.Join(root, "web/tools/.internal/BUILD.bazel"), "")

	c := &config.Config{RepoRoot: root, Exts: make(map[string]interface{})}
	c.Exts[languageName] = makeConfig("", []rule.Directive{
		directive("ts_path_alias", "@/ web/shared/"),
	})
	ix := buildIndex(t, c,
		indexedRule{kind: "ts_compile", name: "lib", pkg: "web/shared/lib", srcs: []string{"index.ts"}},
	)
	from := label.New("", "web", "web")
	srcs := []string{"src/app.ts", "shared/types/mobile.d.ts"}

	// Each row is a specifier and the exact deps it must produce. Both
	// directions are the same defect: #90 added a guard that made every emitted
	// dep loadable and dropped a checked-in one that already was, so a row
	// asserting "no dep" is worth no more than a row asserting a specific one.
	for _, tt := range []struct {
		imp  string
		want []string
	}{
		// The query is dropped and the JSON's package is real, so this one is a
		// dep -- the loadability-only version of this test never checked that.
		{"./shared/lib/config.json?raw", []string{"//web/shared/lib"}},
		{"./shared/lib/notes.rst", nil},                          // unclassified extension
		{"./shared/i18n/compiled/messages", nil},                 // directory not on disk
		{"./shared/public/.well-known/assetlinks.json?raw", nil}, // dot-directory, no BUILD file
		{"./shared/public/.well-known", nil},                     // the same, imported bare
		{"./shared/public/.well-known/apple-app-site-association?raw", nil},
		{"../node_modules/acme/index.ts", nil}, // never a package
		{"mobile", nil},                        // ambiently declared
		{"#nothing/here", nil},                 // no imports entry
		{"virtual:routes", nil},                // bundler-synthesised

		// A dot-directory whose BUILD file is checked in IS a package, and the
		// dep on it has to survive: dropping it is the mirror-image defect.
		{"../.github/scripts/request-author-team-reviewers.js", []string{"//.github/scripts"}},
		// Imported bare, where the dot-name is the LAST segment: path.Ext read
		// the whole of it as a file extension, which the row above never hit.
		{"./tools/.internal", []string{"//web/tools/.internal"}},
		{"@/lib", []string{"//web/shared/lib"}},        // path alias
		{"./shared/lib", []string{"//web/shared/lib"}}, // relative
		{"react", []string{"@npm//:react"}},            // npm
	} {
		r := rule.NewRule("ts_compile", "web")
		r.SetAttr("srcs", srcs)
		resolveImports(c, ix, r, []string{tt.imp}, from)

		got := r.AttrStrings("deps")
		if len(got) != len(tt.want) || (len(got) > 0 && !reflect.DeepEqual(got, tt.want)) {
			t.Errorf("%s: deps = %v, want %v", tt.imp, got, tt.want)
		}
		for _, dep := range got {
			assertLoadable(t, root, ambientModuleNames(c, r, from), tt.imp, dep)
		}
	}
}

// assertLoadable fails when dep names something Bazel cannot load: a target
// name it will not parse, or -- for a label in this repository -- a directory
// that is not there or that the generator refuses to walk, and so will never
// hold a BUILD file.
func assertLoadable(t *testing.T, root string, ambient []string, imp, dep string) {
	t.Helper()
	if strings.ContainsAny(dep, "?#*[] ") {
		t.Errorf("%s: dep %q is not a label Bazel will parse", imp, dep)
		return
	}
	pkg, ok := strings.CutPrefix(dep, "//")
	if !ok {
		// An external repository has no oracle here, bar one: a specifier the
		// target's own sources declare is installed nowhere, so no hub declares
		// a target for it. //tests/ambient_npm_types is the end-to-end claim.
		if declaredAmbiently(ambient, imp) {
			t.Errorf("%s: dep %q, but the target's own sources declare the module", imp, dep)
		}
		return
	}
	pkg, _, _ = strings.Cut(pkg, ":")
	dir := filepath.Join(root, filepath.FromSlash(pkg))
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		t.Errorf("%s: dep %q names %q, which is not a directory in the workspace", imp, dep, pkg)
		return
	}
	// A checked-in BUILD file makes it loadable whatever the generator's walk
	// would do -- reading that walk as the answer is what #90 got wrong.
	if generatorSkips(pkg) && !isBazelPackage(dir) {
		t.Errorf("%s: dep %q names a directory that will never hold a BUILD file", imp, dep)
	}
}
