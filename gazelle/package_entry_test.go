package typescript

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/bazelbuild/bazel-gazelle/config"
	"github.com/bazelbuild/bazel-gazelle/label"
	"github.com/bazelbuild/bazel-gazelle/rule"
)

// repoWith writes each file under a fresh repo root and returns a config
// pointed at it. Fresh per test because readPackageManifest caches by path.
func repoWith(t *testing.T, files map[string]string) *config.Config {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c := &config.Config{RepoRoot: root, Exts: make(map[string]interface{})}
	c.Exts[languageName] = makeConfig("", nil)
	return c
}

func TestPackageEntryModules_RootEntry(t *testing.T) {
	c := repoWith(t, map[string]string{
		"packages/exports/package.json":   `{"exports": {".": {"import": {"types": "./src/entry.d.ts", "default": "./src/entry.js"}}}}`,
		"packages/main/package.json":      `{"main": "./lib/main.js", "types": "./lib/main.d.ts"}`,
		"packages/shorthand/package.json": `{"exports": {"types": "./index.d.ts"}}`,
	})

	for dir, want := range map[string][]string{
		"packages/exports":   {"packages/exports/src/entry"},
		"packages/main":      {"packages/main/lib/main"},
		"packages/shorthand": {"packages/shorthand/index"},
		"packages/absent":    nil,
	} {
		if got := packageEntryModules(c.RepoRoot, dir, ""); !reflect.DeepEqual(got, want) {
			t.Errorf("packageEntryModules(%q, \"\") = %v, want %v", dir, got, want)
		}
	}
}

func TestPackageEntryModules_SubpathAndPattern(t *testing.T) {
	c := repoWith(t, map[string]string{
		"packages/lib/package.json": `{"exports": {
			"./wire": "./src/wire/index.ts",
			"./icons/*": "./icons/components/*.tsx",
			"./*": "./src/*.ts"
		}}`,
	})

	for subpath, want := range map[string][]string{
		"/wire":            {"packages/lib/src/wire/index"},
		"/icons/CheckIcon": {"packages/lib/icons/components/CheckIcon"},
		"/helpers":         {"packages/lib/src/helpers"},
	} {
		if got := packageEntryModules(c.RepoRoot, "packages/lib", subpath); !reflect.DeepEqual(got, want) {
			t.Errorf("packageEntryModules(%q) = %v, want %v", subpath, got, want)
		}
	}
}

// The defect: a directory whose package.json names an entry outside it was
// resolved as if index.ts were the only way in, so the specifier reached a
// label no rule declares.
func TestResolveRelative_DirectoryUsesPackageJSONEntry(t *testing.T) {
	c := repoWith(t, map[string]string{
		"packages/lib/package.json": `{"name": "@acme/lib", "exports": {".": "./src/entry.ts"}}`,
	})
	ix := buildIndex(t, c,
		indexedRule{kind: "ts_compile", name: "src", pkg: "packages/lib/src", srcs: []string{"entry.ts"}},
	)

	r := rule.NewRule("ts_compile", "app")
	resolveImports(c, ix, r, []string{"../packages/lib"}, label.New("", "app", "app"))

	want := []string{"//packages/lib/src"}
	if got := r.AttrStrings("deps"); !reflect.DeepEqual(got, want) {
		t.Errorf("deps = %v, want %v", got, want)
	}
}

func TestResolvePathAlias_DirectoryUsesPackageJSONEntry(t *testing.T) {
	c := repoWith(t, map[string]string{
		"packages/lib/package.json": `{"main": "./src/entry.ts"}`,
	})
	c.Exts[languageName] = makeConfig("", []rule.Directive{
		directive("ts_path_alias", "~pkg/ packages/"),
	})
	ix := buildIndex(t, c,
		indexedRule{kind: "ts_compile", name: "src", pkg: "packages/lib/src", srcs: []string{"entry.ts"}},
	)

	r := rule.NewRule("ts_compile", "app")
	resolveImports(c, ix, r, []string{"~pkg/lib"}, label.New("", "app", "app"))

	want := []string{"//packages/lib/src"}
	if got := r.AttrStrings("deps"); !reflect.DeepEqual(got, want) {
		t.Errorf("deps = %v, want %v", got, want)
	}
}

// index.ts stays the answer for a directory whose manifest declares no entry,
// and for one with no manifest at all.
func TestResolveRelative_IndexStillWinsWithoutAnEntry(t *testing.T) {
	c := repoWith(t, map[string]string{
		"packages/lib/package.json": `{"name": "@acme/lib"}`,
	})
	ix := buildIndex(t, c,
		indexedRule{kind: "ts_compile", name: "lib", pkg: "packages/lib", srcs: []string{"index.ts"}},
	)

	r := rule.NewRule("ts_compile", "app")
	resolveImports(c, ix, r, []string{"../packages/lib"}, label.New("", "app", "app"))

	want := []string{"//packages/lib"}
	if got := r.AttrStrings("deps"); !reflect.DeepEqual(got, want) {
		t.Errorf("deps = %v, want %v", got, want)
	}
}
