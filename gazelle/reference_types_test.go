package typescript

// tsc resolves a source's `/// <reference types="x" />` through node_modules/@types;
// the sandbox has none, so the declarations reach the program only as a dep.

import (
	"reflect"
	"strings"
	"testing"
)

const (
	mapsDirective = "/// <reference types=\"google.maps\" />\n"
	mapsUse       = "export const map: google.maps.Map | null = null;\n"
)

// The lockfile behind the cases below. What it lacks is as much the fixture as
// what it names: no @types/bun-types, no deno.
const referenceTypesLock = `lockfileVersion: '9.0'

importers:

  .:
    devDependencies:
      '@cloudflare/workers-types':
        specifier: 4.20250101.0
        version: 4.20250101.0
      '@types/google.maps':
        specifier: 3.58.1
        version: 3.58.1
      '@types/node':
        specifier: 22.20.1
        version: 22.20.1
      bun-types:
        specifier: 1.3.5
        version: 1.3.5
      vite:
        specifier: 8.2.2
        version: 8.2.2

packages:

  '@cloudflare/workers-types@4.20250101.0':
    resolution: {integrity: sha512-aaa}

  '@types/google.maps@3.58.1':
    resolution: {integrity: sha512-bbb}

  '@types/node@22.20.1':
    resolution: {integrity: sha512-ccc}

  bun-types@1.3.5:
    resolution: {integrity: sha512-ddd}

  vite@8.2.2:
    resolution: {integrity: sha512-eee}

snapshots:

  '@cloudflare/workers-types@4.20250101.0': {}

  '@types/google.maps@3.58.1': {}

  '@types/node@22.20.1': {}

  bun-types@1.3.5: {}

  vite@8.2.2: {}
`

func depsOf(t *testing.T, root, pkg, kind, name string) []string {
	t.Helper()
	for _, r := range loadRules(t, root, pkg) {
		if r.Kind() == kind && r.Name() == name {
			return attrValues(r, "deps")
		}
	}
	t.Fatalf("no %s(%s) in //%s", kind, name, pkg)
	return nil
}

func TestReferenceTypes_DirectiveBecomesADep(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"pnpm-lock.yaml":    referenceTypesLock,
		"src/vite-env.d.ts": mapsDirective,
		"src/map.ts":        mapsUse,
	})
	captureLog(t, func() { convergeGazelle(t, root) })

	if got := depsOf(t, root, "src", "ts_compile", "src"); !contains(got, "@npm//:types_google.maps") {
		t.Errorf("//src deps = %v: vite-env.d.ts references google.maps and nothing else names @types/google.maps", got)
	}
	if build := generated(t, root, "src", "BUILD.bazel"); strings.Contains(build, "types =") {
		t.Errorf("the directive became a types attribute; it is a dep, the tsconfig's list is the attribute's source:\n%s", build)
	}
}

// Nothing imports an ambient .d.ts, so each program that needs it lists it, and
// each such program needs the dep its directive names.
func TestReferenceTypes_ReachesEveryTargetListingTheSrc(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"pnpm-lock.yaml":      referenceTypesLock,
		"src/vite-env.d.ts":   mapsDirective,
		"src/map.ts":          mapsUse,
		"src/map.test.ts":     "/// <reference types=\"node\" />\nexport const cwd: string = process.cwd();\n",
		"src/map.stories.tsx": "export const story: google.maps.Map | null = null;\n",
	})
	captureLog(t, func() { convergeGazelle(t, root) })

	for _, target := range []struct{ kind, name string }{
		{"ts_compile", "src"}, {"ts_test", "src_test"}, {"ts_compile", "src_doc"},
	} {
		if got := depsOf(t, root, "src", target.kind, target.name); !contains(got, "@npm//:types_google.maps") {
			t.Errorf("%s(%s) deps = %v: it lists vite-env.d.ts and lacks the dep the file references", target.kind, target.name, got)
		}
	}
	if got := depsOf(t, root, "src", "ts_test", "src_test"); !contains(got, "@npm//:types_node") {
		t.Errorf("ts_test deps = %v: the test file references node", got)
	}
	if got := depsOf(t, root, "src", "ts_compile", "src"); contains(got, "@npm//:types_node") {
		t.Errorf("ts_compile deps = %v: only the test file references node, and the package target does not list it", got)
	}
}

// The name maps as a tsconfig `types` entry of the same spelling maps: a bare
// name is a DefinitelyTyped package, a scoped or subpath name is the package.
func TestReferenceTypes_NameMapsAsATypesEntryDoes(t *testing.T) {
	for name, want := range map[string]string{
		"node":                      "@npm//:types_node",
		"vite/client":               "@npm//:vite",
		"@cloudflare/workers-types": "@npm//:cloudflare_workers-types",
	} {
		root := t.TempDir()
		writeWorkspace(t, root, map[string]string{
			"pnpm-lock.yaml": referenceTypesLock,
			"src/env.d.ts":   "/// <reference types=\"" + name + "\" />\n",
			"src/index.ts":   "export const n = 1;\n",
		})
		captureLog(t, func() { convergeGazelle(t, root) })

		if got := depsOf(t, root, "src", "ts_compile", "src"); !reflect.DeepEqual(got, []string{want}) {
			t.Errorf("%s: deps = %v, want [%s]", name, got, want)
		}
	}
}

// tsc tries @types/<name> under typeRoots and then a package called <name>.
// bun-types ships its own declarations and no @types/bun-types exists.
func TestReferenceTypes_BareNameFallsBackToThePackageItself(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"pnpm-lock.yaml": referenceTypesLock,
		"src/env.d.ts":   "/// <reference types=\"bun-types\" />\n",
		"src/index.ts":   "export const n = 1;\n",
	})
	captureLog(t, func() { convergeGazelle(t, root) })

	if got := depsOf(t, root, "src", "ts_compile", "src"); !reflect.DeepEqual(got, []string{"@npm//:bun-types"}) {
		t.Errorf("deps = %v, want [@npm//:bun-types]: the lockfile names bun-types and no @types/bun-types", got)
	}
}

// A label no hub declares fails analysis for the whole workspace, where the
// missing global fails one target with TS2304.
func TestReferenceTypes_NameTheLockfileLacksIsNoDep(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"BUILD.bazel":    "# gazelle:ts_warn_unresolved true\n",
		"pnpm-lock.yaml": referenceTypesLock,
		"src/env.d.ts":   "/// <reference types=\"deno\" />\n",
		"src/index.ts":   "export const n = 1;\n",
	})
	logged := captureLog(t, func() { convergeGazelle(t, root) })

	if build := generated(t, root, "src", "BUILD.bazel"); strings.Contains(build, "deps") {
		t.Errorf("a dep was written for a name the lockfile does not answer:\n%s", build)
	}
	if !strings.Contains(logged, `unresolved /// <reference types="deno" /> in //src:src`) {
		t.Errorf("the run did not say which directive it wrote no dep for:\n%s", logged)
	}
}

// TypeScript reads the directive out of the comments before the first token.
func TestReferenceTypes_DirectiveAfterAStatementIsAComment(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"pnpm-lock.yaml": referenceTypesLock,
		"src/index.ts":   "export const n = 1;\n/// <reference types=\"node\" />\n",
	})
	captureLog(t, func() { convergeGazelle(t, root) })

	if build := generated(t, root, "src", "BUILD.bazel"); strings.Contains(build, "deps") {
		t.Errorf("a directive below a statement produced a dep:\n%s", build)
	}
}

// No lockfile is no information, not an empty inventory: the label is written
// as a tsconfig `types` entry's is, and Bazel says whether the hub declares it.
func TestReferenceTypes_NoLockfileTakesTheTypesLabel(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"src/env.d.ts": "/// <reference types=\"node\" />\n",
		"src/index.ts": "export const n = 1;\n",
	})
	captureLog(t, func() { convergeGazelle(t, root) })

	if got := depsOf(t, root, "src", "ts_compile", "src"); !reflect.DeepEqual(got, []string{"@npm//:types_node"}) {
		t.Errorf("deps = %v, want [@npm//:types_node]", got)
	}
}

// The tsconfig's list is written whole and stays what the tsconfig says; the
// directive adds a dep beside the list's own.
func TestReferenceTypes_TsconfigTypesListIsNotRewritten(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"pnpm-lock.yaml":    referenceTypesLock,
		"src/tsconfig.json": `{"compilerOptions": {"types": ["vite/client"]}}` + "\n",
		"src/vite-env.d.ts": "/// <reference types=\"vite/client\" />\n" + mapsDirective,
		"src/map.ts":        mapsUse,
	})
	captureLog(t, func() { convergeGazelle(t, root) })

	build := generated(t, root, "src", "BUILD.bazel")
	if !strings.Contains(build, `types = ["vite/client"]`) {
		t.Errorf("the tsconfig's types list is not written as the tsconfig states it:\n%s", build)
	}
	if strings.Contains(build, "google.maps\"]") {
		t.Errorf("the directive's name joined the types attribute:\n%s", build)
	}
	got := depsOf(t, root, "src", "ts_compile", "src")
	for _, want := range []string{"@npm//:vite", "@npm//:types_google.maps"} {
		if !contains(got, want) {
			t.Errorf("deps = %v: missing %s", got, want)
		}
	}
}
