package typescript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bazelbuild/bazel-gazelle/config"
	"github.com/bazelbuild/bazel-gazelle/label"
	"github.com/bazelbuild/bazel-gazelle/rule"
)

// Six defects so far are one defect: Gazelle wrote a dep naming something that
// cannot exist, and Bazel answers `no such package` / `no such target` during
// ANALYSIS -- failing every target in the build, where a dropped dep fails one
// and leaves the compiler to report one TS2307.
//
// The guards live apart because the specifiers reach the label by different
// routes. The invariant does not: whatever route a dep came by, it must name
// something. This test is that invariant, run over a corpus holding every
// shape that has broken so far.
//
// The npm half of the corpus is checked for a loadable label only. The
// inventory would be the oracle for whether the hub declares a name, and it
// under-claims -- selfImportLock's own `zod` is missing from it -- so a
// membership assertion here would encode the parser's gaps as the contract.
func TestResolveImports_EveryDepNamesSomethingLoadable(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{
		"web/src", "web/shared/lib", "web/shared/public/.well-known", "web/node_modules/acme",
	} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(dir)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(root, "web/shared/public/.well-known/assetlinks.json"), "{}\n")
	writeFile(t, filepath.Join(root, "web/shared/public/.well-known/apple-app-site-association"), "{}\n")
	writeFile(t, filepath.Join(root, "web/shared/types/mobile.d.ts"), "declare module \"mobile\" {}\n")

	c := &config.Config{RepoRoot: root, Exts: make(map[string]interface{})}
	c.Exts[languageName] = makeConfig("", []rule.Directive{
		directive("ts_path_alias", "@/ web/shared/"),
	})
	ix := buildIndex(t, c,
		indexedRule{kind: "ts_compile", name: "lib", pkg: "web/shared/lib", srcs: []string{"index.ts"}},
	)
	from := label.New("", "web", "web")
	srcs := []string{"src/app.ts", "shared/types/mobile.d.ts"}

	// Each row is a specifier that has produced an unloadable label, and the
	// route it took. The controls at the end must still produce a dep, or the
	// invariant below would hold over an empty list.
	corpus := []string{
		"./shared/lib/config.json?raw",                    // bundler query suffix
		"./shared/lib/notes.rst",                          // unclassified extension
		"./shared/i18n/compiled/messages",                 // directory not on disk
		"./shared/public/.well-known/assetlinks.json?raw", // dot-directory
		"./shared/public/.well-known/apple-app-site-association?raw",
		"../node_modules/acme/index.ts", // never a package
		"mobile",                        // ambiently declared
		"#nothing/here",                 // no imports entry
		"virtual:routes",                // bundler-synthesised
		"@/lib",                         // control: path alias
		"./shared/lib",                  // control: relative
		"react",                         // control: npm
	}

	produced := 0
	for _, imp := range corpus {
		r := rule.NewRule("ts_compile", "web")
		r.SetAttr("srcs", srcs)
		resolveImports(c, ix, r, []string{imp}, from)
		for _, dep := range r.AttrStrings("deps") {
			produced++
			assertLoadable(t, root, ambientModuleNames(c, r, from), imp, dep)
		}
	}
	if produced < 3 {
		t.Fatalf("only %d deps produced; the corpus controls stopped resolving and the invariant is vacuous", produced)
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
	if generatorSkips(pkg) {
		t.Errorf("%s: dep %q names a directory the generator refuses to walk", imp, dep)
		return
	}
	info, err := os.Stat(filepath.Join(root, filepath.FromSlash(pkg)))
	if err != nil || !info.IsDir() {
		t.Errorf("%s: dep %q names %q, which is not a directory in the workspace", imp, dep, pkg)
	}
}
