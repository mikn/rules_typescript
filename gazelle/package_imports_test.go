package typescript

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/bazelbuild/bazel-gazelle/config"
	"github.com/bazelbuild/bazel-gazelle/label"
)

func writePackageJSON(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadPackageImports(t *testing.T) {
	repo := t.TempDir()
	writePackageJSON(t, filepath.Join(repo, "web/package.json"), `{
  "name": "web",
  "imports": {
    "#shared/*": "./shared/*",
    "#entry": "./src/entry.ts",
    "#conditional": {"types": "./src/types.d.ts", "default": "./src/runtime.js"},
    "#alternatives": ["./src/first.ts", "./src/second.ts"],
    "#outside": "../elsewhere/x.ts",
    "no-hash": "./src/nope.ts"
  }
}`)

	got := loadPackageImports(filepath.Join(repo, "web"), "web")
	want := map[string]string{
		"#shared/":      "web/shared/",
		"#entry":        "web/src/entry.ts",
		"#conditional":  "web/src/types.d.ts",
		"#alternatives": "web/src/first.ts",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("loadPackageImports: got %v, want %v", got, want)
	}
}

// The monorepo's shape: "#shared/lib/flags/gen/x.gen" names a module inside the
// importing package, and nothing but this map says where.
func TestResolveImport_PackageImportsSpecifier(t *testing.T) {
	c := emptyConfig()
	tc := &tsConfig{pathAliases: map[string]string{"#shared/": "web/shared/"}}
	ix := buildIndex(t, c,
		indexedRule{kind: "ts_compile", name: "flags", pkg: "web/shared/flags", srcs: []string{"index.ts"}},
	)
	from := label.New("", "web/modules/admin", "admin")

	if got, want := resolveImport(c, ix, tc, "#shared/flags", from), "//web/shared/flags"; got != want {
		t.Errorf("resolveImport(%q) = %q, want %q", "#shared/flags", got, want)
	}

	// No entry covers it, and a hub has no package of that name: a label here
	// is one whose target nothing declares, which fails analysis for the whole
	// build rather than leaving TypeScript to report TS2307.
	if got := resolveImport(c, ix, tc, "#nothing/here", from); got != "" {
		t.Errorf("resolveImport(%q) = %q, want no dep", "#nothing/here", got)
	}
}

// A package.json "imports" entry answers a key no tsconfig declared, and never
// overrides one that did.
func TestConfigureTsConfig_PackageImportsFillTheGap(t *testing.T) {
	repo := t.TempDir()
	writePackageJSON(t, filepath.Join(repo, "web/package.json"), `{
  "imports": {"#shared/*": "./shared/*", "#modules/*": "./modules/*"}
}`)
	writeTsConfig(t, filepath.Join(repo, "web/tsconfig.json"), `{
  "compilerOptions": {"paths": {"#modules/*": ["./legacy-modules/*"]}}
}`)

	c := &config.Config{RepoRoot: repo, Exts: make(map[string]interface{})}
	configureTsConfig(c, "", nil)
	configureTsConfig(c, "web", nil)

	got := getConfig(c).pathAliases
	want := map[string]string{
		"#shared/":  "web/shared/",
		"#modules/": "web/legacy-modules/",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pathAliases = %v, want %v", got, want)
	}
}
