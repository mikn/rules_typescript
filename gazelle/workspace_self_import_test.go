package typescript

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/bazelbuild/bazel-gazelle/config"
	"github.com/bazelbuild/bazel-gazelle/label"
	"github.com/bazelbuild/bazel-gazelle/resolve"
	"github.com/bazelbuild/bazel-gazelle/rule"
)

// packages/lib is a workspace member; packages/lib/example is a package of its
// own nested inside it, which is not.
const selfImportLock = `lockfileVersion: '9.0'

importers:

  .:
    dependencies:
      '@acme/lib':
        specifier: workspace:*
        version: link:packages/lib

  packages/lib:
    dependencies:
      zod:
        specifier: ^3.0.0
        version: 3.24.2

packages:

  zod@3.24.2: {}

snapshots:

  zod@3.24.2: {}
`

func selfImportRepo(t *testing.T) (*config.Config, *tsConfig) {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, pnpmLockfileName), selfImportLock)
	writeFile(t, filepath.Join(root, "packages/lib/package.json"), `{
		"name": "@acme/lib",
		"exports": {
			".": "./src/index.ts",
			"./wire": "./src/wire/index.ts",
			"./icons/*": "./src/icons/*.tsx"
		}
	}`)
	writeFile(t, filepath.Join(root, "packages/lib/example/package.json"), `{"name": "@acme/lib-example"}`)

	c := &config.Config{RepoRoot: root, Exts: make(map[string]interface{})}
	configureTsConfig(c, "", nil)
	return c, getConfig(c)
}

func selfImportIndex(t *testing.T, c *config.Config) *resolve.RuleIndex {
	t.Helper()
	return buildIndex(t, c,
		indexedRule{kind: "ts_compile", name: "src", pkg: "packages/lib/src", srcs: []string{"index.ts"}},
		indexedRule{kind: "ts_compile", name: "wire", pkg: "packages/lib/src/wire", srcs: []string{"index.ts"}},
		indexedRule{kind: "ts_compile", name: "icons", pkg: "packages/lib/src/icons", srcs: []string{"Check.tsx"}},
	)
}

// The defect: the npm hub declares a workspace member's own name, because pnpm
// resolved it to a link, and its target is the member's compiling target. A dep
// on it from inside the member points back at the importer.
func TestResolveImports_SelfReferenceResolvesLocally(t *testing.T) {
	c, _ := selfImportRepo(t)
	ix := selfImportIndex(t, c)

	for imp, want := range map[string][]string{
		"@acme/lib/wire":        {"//packages/lib/src/wire"},
		"@acme/lib/icons/Check": {"//packages/lib/src/icons"},
		"@acme/lib":             {"//packages/lib/src"},
	} {
		r := rule.NewRule("ts_compile", "consumer")
		resolveImports(c, ix, r, []string{imp}, label.New("", "packages/lib/consumer", "consumer"))
		if got := r.AttrStrings("deps"); !reflect.DeepEqual(got, want) {
			t.Errorf("%q: deps = %v, want %v", imp, got, want)
		}
	}
}

// A package nested inside a member is its own package and consumes the member
// the way anyone outside does. The hub declares that name because the importer
// listed the link under it, which is what makes @npm//:acme_lib a target.
func TestResolveImports_NestedPackageKeepsTheHubLabel(t *testing.T) {
	c, _ := selfImportRepo(t)
	ix := selfImportIndex(t, c)

	r := rule.NewRule("ts_compile", "example")
	resolveImports(c, ix, r, []string{"@acme/lib/wire"}, label.New("", "packages/lib/example/src", "src"))

	want := []string{"@npm//:acme_lib"}
	if got := r.AttrStrings("deps"); !reflect.DeepEqual(got, want) {
		t.Errorf("deps = %v, want %v", got, want)
	}
}

// A member importing a package it merely installed still goes to the hub.
func TestResolveImports_SelfReferenceLeavesInstalledPackagesAlone(t *testing.T) {
	c, _ := selfImportRepo(t)
	ix := selfImportIndex(t, c)

	r := rule.NewRule("ts_compile", "src")
	resolveImports(c, ix, r, []string{"zod"}, label.New("", "packages/lib/src", "src"))

	want := []string{"@npm//:zod"}
	if got := r.AttrStrings("deps"); !reflect.DeepEqual(got, want) {
		t.Errorf("deps = %v, want %v", got, want)
	}
}

func TestParsePnpmImporterDirs(t *testing.T) {
	got := parsePnpmImporterDirs(strings.Split(selfImportLock, "\n"))
	want := map[string]bool{"": true, "packages/lib": true}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parsePnpmImporterDirs = %v, want %v", got, want)
	}
}
