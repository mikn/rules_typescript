package typescript

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bazelbuild/bazel-gazelle/config"
)

// configAt runs Configure over a real directory tree, which is what a tsconfig
// or package.json on disk needs: the directive tests never touch the
// filesystem.
func configAt(t *testing.T, repoRoot, rel string) *tsConfig {
	t.Helper()
	c := &config.Config{RepoRoot: repoRoot, Exts: make(map[string]interface{})}
	configureTsConfig(c, "", nil)
	if rel != "" {
		configureTsConfig(c, rel, nil)
	}
	return getConfig(c)
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func hasLabel(labels []string, want string) bool {
	for _, l := range labels {
		if l == want {
			return true
		}
	}
	return false
}

// A package named in compilerOptions.types has no import anywhere in the
// sources -- that is what "ambient" means -- so nothing but the tsconfig can
// tell Gazelle the dep exists.
func TestTsConfigTypes_NamedPackageBecomesAmbientDep(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "tsconfig.json"), `{
  "compilerOptions": { "types": ["@cloudflare/workers-types", "node", "vite/client"] }
}`)

	tc := configAt(t, root, "")

	for _, want := range []string{"@npm//:cloudflare_workers-types", "@npm//:types_node", "@npm//:vite"} {
		if !hasLabel(tc.tsconfigAmbientTypes, want) {
			t.Errorf("ambientTypes %v: missing %s", tc.tsconfigAmbientTypes, want)
		}
	}
}

// TypeScript with no `types` key includes every @types/* package it can see,
// which under pnpm is exactly the ones the package declares.
func TestTsConfigTypes_AbsentFallsBackToDeclaredTypesPackages(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "tsconfig.json"), `{
  "compilerOptions": { "strict": true }
}`)
	writeFile(t, filepath.Join(root, "package.json"), `{
  "name": "widget",
  "dependencies": { "zod": "^3.0.0", "@types/react": "^19.0.0" },
  "devDependencies": { "@types/node": "^22.0.0", "vitest": "^2.0.0" }
}`)

	tc := configAt(t, root, "")

	for _, want := range []string{"@npm//:types_node", "@npm//:types_react"} {
		if !hasLabel(tc.tsconfigAmbientTypes, want) {
			t.Errorf("ambientTypes %v: missing %s", tc.tsconfigAmbientTypes, want)
		}
	}
	for _, unwanted := range []string{"@npm//:zod", "@npm//:vitest"} {
		if hasLabel(tc.tsconfigAmbientTypes, unwanted) {
			t.Errorf("ambientTypes %v: %s is imported, not ambient", tc.tsconfigAmbientTypes, unwanted)
		}
	}
}

// "types": [] is TypeScript's way of saying there are none; it is not the same
// as leaving the key out.
func TestTsConfigTypes_EmptyListSuppressesTheFallback(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "tsconfig.json"), `{
  "compilerOptions": { "types": [] }
}`)
	writeFile(t, filepath.Join(root, "package.json"), `{
  "name": "widget",
  "devDependencies": { "@types/node": "^22.0.0" }
}`)

	tc := configAt(t, root, "")

	if len(tc.tsconfigAmbientTypes) != 0 {
		t.Errorf("ambientTypes: got %v, want none", tc.tsconfigAmbientTypes)
	}
}

// A relative path in `types` names a file in the tree, not a package. The label
// it would otherwise produce -- @npm//:._worker-configuration.d.ts -- does not
// parse, so a wrong entry here fails the whole build rather than one target.
func TestTsConfigTypes_RelativePathIsNotAnNpmLabel(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "tsconfig.json"), `{
  "compilerOptions": { "types": ["./worker-configuration.d.ts"] }
}`)

	tc := configAt(t, root, "")

	for _, l := range tc.tsconfigAmbientTypes {
		if l == "@npm//:._worker-configuration.d.ts" || l == "@npm//:worker-configuration.d.ts" {
			t.Errorf("ambientTypes %v: a file path became an npm label", tc.tsconfigAmbientTypes)
		}
	}
}

// A tsconfig deeper in the tree replaces the inherited answer, exactly as
// tsc picks the nearest project; the parent's list must survive it.
func TestTsConfigTypes_NearestTsConfigWinsWithoutMutatingTheParent(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "tsconfig.json"), `{
  "compilerOptions": { "types": ["@types/node"] }
}`)
	writeFile(t, filepath.Join(root, "worker", "tsconfig.json"), `{
  "compilerOptions": { "types": ["@cloudflare/workers-types"] }
}`)

	c := &config.Config{RepoRoot: root, Exts: make(map[string]interface{})}
	configureTsConfig(c, "", nil)
	parent := getConfig(c)
	parentBefore := append([]string(nil), parent.tsconfigAmbientTypes...)

	configureTsConfig(c, "worker", nil)
	child := getConfig(c)

	if !hasLabel(child.tsconfigAmbientTypes, "@npm//:cloudflare_workers-types") {
		t.Errorf("child ambientTypes %v: missing the nearest tsconfig's entry", child.tsconfigAmbientTypes)
	}
	if len(parent.tsconfigAmbientTypes) != len(parentBefore) {
		t.Errorf("the child mutated the parent: %v vs %v", parent.tsconfigAmbientTypes, parentBefore)
	}
}

// The rule reads the same shapes out of its `types` attr, in
// types_entry_package_ref (ts/private/ts_compile.bzl), and fails analysis for
// every entry it reads as a package that no dep answers. An entry this side
// writes no dep for and that side reads as a package is a fail() nothing can
// clear, so the classification is pinned on both sides: this table, and
// types_entry_package_ref_test in //tests/compiler_options/analysis.
func TestTsConfigTypes_EntryShapesAreClassifiedLikeTheRule(t *testing.T) {
	for _, tc := range []struct {
		entry string
		want  string
	}{
		{"vite/client", "@npm//:vite"},
		{"node", "@npm//:types_node"},
		{" vite/client ", "@npm//:vite"},
		{"\tnode\n", "@npm//:types_node"},
		{"", ""},
		{"   ", ""},
		{"./typings", ""},
		{"../sibling/typings", ""},
		{"/abs/typings", ""},
		{"vendor/local.d.ts", ""},
		{"vendor/local.d.mts", ""},
		{"vendor/local.d.cts", ""},
	} {
		if got := ambientTypeLabel(tc.entry); got != tc.want {
			t.Errorf("ambientTypeLabel(%q) = %q, want %q", tc.entry, got, tc.want)
		}
	}
}

// The other half of the same vocabulary, and tsc names a .mjs module's
// declaration .d.mts: a file entry is one whatever declaration extension it ends in.
func TestTsConfigTypes_FileEntryNamesEveryDeclarationExtension(t *testing.T) {
	for _, tc := range []struct {
		entry  string
		name   string
		isFile bool
	}{
		{"./worker-configuration.d.ts", "worker-configuration.d.ts", true},
		{"./compile.d.mts", "compile.d.mts", true},
		{"./shim.d.cts", "shim.d.cts", true},
		{"./types/globals.d.mts", "", true},
		{"./typings", "", false},
	} {
		name, isFile := typeEntryFileName(tc.entry)
		if name != tc.name || isFile != tc.isFile {
			t.Errorf("typeEntryFileName(%q) = (%q, %v), want (%q, %v)", tc.entry, name, isFile, tc.name, tc.isFile)
		}
	}
}
