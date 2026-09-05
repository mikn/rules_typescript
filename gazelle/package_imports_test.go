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
	if !reflect.DeepEqual(got.aliases, want) {
		t.Errorf("loadPackageImports aliases: got %v, want %v", got.aliases, want)
	}
	if len(got.npm) != 0 {
		t.Errorf("loadPackageImports npm: got %v, want none", got.npm)
	}
}

// An "imports" target may name another package instead of a path -- swapping a
// polyfill by condition is what the field is for -- and a "#" specifier reaches
// nothing but this map, so dropping the entry drops the dep entirely.
func TestLoadPackageImports_NpmTargets(t *testing.T) {
	repo := t.TempDir()
	writePackageJSON(t, filepath.Join(repo, "web/package.json"), `{
  "imports": {
    "#dep": "lodash",
    "#scoped": "@acme/ui",
    "#subpath": "lodash/fp",
    "#poly/*": "some-polyfill/*",
    "#byCondition": {"types": "./src/types.d.ts", "node": "node-only-pkg"},
    "#outside": "../elsewhere/x.ts",
    "#indirect": "#dep",
    "#bareScope": "@acme"
  }
}`)

	got := loadPackageImports(filepath.Join(repo, "web"), "web")
	wantNpm := map[string]string{
		"#dep":     "lodash",
		"#scoped":  "@acme/ui",
		"#subpath": "lodash/fp",
		"#poly/":   "some-polyfill/",
	}
	if !reflect.DeepEqual(got.npm, wantNpm) {
		t.Errorf("loadPackageImports npm: got %v, want %v", got.npm, wantNpm)
	}
	wantAliases := map[string]string{"#byCondition": "web/src/types.d.ts"}
	if !reflect.DeepEqual(got.aliases, wantAliases) {
		t.Errorf("loadPackageImports aliases: got %v, want %v", got.aliases, wantAliases)
	}
}

// Node allows "*" anywhere in the pattern. An alias key matches by prefix, so a
// key holding a literal "*" matches nothing -- drop the entry rather than
// record one that cannot fire.
func TestLoadPackageImports_NonTrailingWildcardIsDropped(t *testing.T) {
	repo := t.TempDir()
	writePackageJSON(t, filepath.Join(repo, "web/package.json"), `{
  "imports": {"#internal/*/utils": "./src/*/utils.ts", "#ok/*": "./src/*"}
}`)

	got := loadPackageImports(filepath.Join(repo, "web"), "web")
	want := map[string]string{"#ok/": "web/src/"}
	if !reflect.DeepEqual(got.aliases, want) {
		t.Errorf("loadPackageImports aliases: got %v, want %v", got.aliases, want)
	}
}

// An "imports" entry naming a package resolves to that package's label, subpath
// and wildcard included.
func TestResolveImport_PackageImportsNpmTarget(t *testing.T) {
	repo := t.TempDir()
	writePackageJSON(t, filepath.Join(repo, "web/package.json"), `{
  "imports": {"#dep": "lodash", "#poly/*": "some-polyfill/*", "#sub": "lodash/fp"}
}`)
	c := &config.Config{RepoRoot: repo, Exts: make(map[string]interface{})}
	configureTsConfig(c, "", nil)
	configureTsConfig(c, "web", nil)
	tc := getConfig(c)
	tc.npmPackages = map[string]string{"lodash": "", "some-polyfill": ""}

	ix := buildIndex(t, c)
	from := label.New("", "web/src", "src")
	for imp, want := range map[string]string{
		"#dep":         "@npm//:lodash",
		"#sub":         "@npm//:lodash",
		"#poly/stable": "@npm//:some-polyfill",
		"#unlisted":    "",
	} {
		if got := resolveImport(c, ix, tc, "ts_compile", nil, imp, from); got != want {
			t.Errorf("resolveImport(%q) = %q, want %q", imp, got, want)
		}
	}
}

// Node answers a "#" against the nearest enclosing package.json, not the
// outermost: an inner map replaces the outer one's answer for the same key.
func TestConfigureTsConfig_NearestPackageImportsWins(t *testing.T) {
	repo := t.TempDir()
	writePackageJSON(t, filepath.Join(repo, "package.json"), `{
  "imports": {"#shared/*": "./shared/*", "#root-only": "./root.ts", "#dep": "lodash"}
}`)
	writePackageJSON(t, filepath.Join(repo, "apps/web/package.json"), `{
  "imports": {"#shared/*": "./src/shared/*"}
}`)

	c := &config.Config{RepoRoot: repo, Exts: make(map[string]interface{})}
	configureTsConfig(c, "", nil)
	configureTsConfig(c, "apps", nil)
	configureTsConfig(c, "apps/web", nil)

	tc := getConfig(c)
	want := map[string]string{"#shared/": "apps/web/src/shared/"}
	if !reflect.DeepEqual(tc.pathAliases, want) {
		t.Errorf("pathAliases = %v, want %v", tc.pathAliases, want)
	}
	if len(tc.importsNpm) != 0 {
		t.Errorf("importsNpm = %v, want none: the nearer map replaces the outer one", tc.importsNpm)
	}
}

// A package.json with no "imports" map of its own is not a nearer answer: the
// enclosing package's map still applies. Node would stop at this package.json;
// stopping here would drop a key every bundler in the workspace still honours.
func TestConfigureTsConfig_PackageWithoutImportsKeepsTheOuterMap(t *testing.T) {
	repo := t.TempDir()
	writePackageJSON(t, filepath.Join(repo, "package.json"), `{
  "imports": {"#shared/*": "./shared/*"}
}`)
	writePackageJSON(t, filepath.Join(repo, "apps/web/package.json"), `{"name": "web"}`)

	c := &config.Config{RepoRoot: repo, Exts: make(map[string]interface{})}
	configureTsConfig(c, "", nil)
	configureTsConfig(c, "apps", nil)
	configureTsConfig(c, "apps/web", nil)

	if got := getConfig(c).pathAliases["#shared/"]; got != "shared/" {
		t.Errorf("#shared/ = %q, want %q", got, "shared/")
	}
}

// A tsconfig "paths" key still outranks the package.json map, at any depth.
func TestConfigureTsConfig_PathsOutrankNestedImports(t *testing.T) {
	repo := t.TempDir()
	writePackageJSON(t, filepath.Join(repo, "package.json"), `{
  "imports": {"#shared/*": "./shared/*"}
}`)
	writeTsConfig(t, filepath.Join(repo, "tsconfig.json"), `{
  "compilerOptions": {"paths": {"#shared/*": ["./canonical/*"]}}
}`)
	writePackageJSON(t, filepath.Join(repo, "apps/web/package.json"), `{
  "imports": {"#shared/*": "./src/shared/*"}
}`)

	c := &config.Config{RepoRoot: repo, Exts: make(map[string]interface{})}
	configureTsConfig(c, "", nil)
	configureTsConfig(c, "apps", nil)
	configureTsConfig(c, "apps/web", nil)

	if got := getConfig(c).pathAliases["#shared/"]; got != "canonical/" {
		t.Errorf("#shared/ = %q, want %q", got, "canonical/")
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

	if got, want := resolveImport(c, ix, tc, "ts_compile", nil, "#shared/flags", from), "//web/shared/flags"; got != want {
		t.Errorf("resolveImport(%q) = %q, want %q", "#shared/flags", got, want)
	}

	// No entry covers it, and a hub has no package of that name: a label here
	// is one whose target nothing declares, which fails analysis for the whole
	// build rather than leaving TypeScript to report TS2307.
	if got := resolveImport(c, ix, tc, "ts_compile", nil, "#nothing/here", from); got != "" {
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
