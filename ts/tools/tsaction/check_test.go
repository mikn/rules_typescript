package main

import (
	"strings"
	"testing"

	"github.com/mikn/rules_typescript/ts/tools/explainfiles"
)

const strictDepsBin = binDir + "/tests/strict_deps"

// The chain hidden <- leaf <- middle, one target per file, as tsgo lists it
// for :middle with :leaf declared: leaf.d.ts and hidden.d.ts are inputs.
const middleListing = strictDepsBin + `/hidden.d.ts
   Imported via "./hidden" from file '` + strictDepsBin + `/leaf.d.ts'
   Imported via "./hidden" from file 'tests/strict_deps/middle.ts'
` + strictDepsBin + `/leaf.d.ts
   Imported via "./leaf" from file 'tests/strict_deps/middle.ts'
tests/strict_deps/middle.ts
   Matched by include pattern '../../../../../tests/strict_deps/middle.ts'` +
	` in '` + strictDepsBin + `/middle.tsconfig.json'
`

const middleOwnership = `label	//tests/strict_deps:middle
own	tests/strict_deps/middle.ts
direct	//tests/strict_deps:leaf
file	//tests/strict_deps:leaf	` + strictDepsBin + `/leaf.d.ts
file	//tests/strict_deps:hidden	` + strictDepsBin + `/hidden.d.ts
`

// runCheck checks a listing against a manifest; ok is false when an edge
// names a label to add, and out is the message a failing action prints.
func runCheck(t *testing.T, manifest, listing string) (out string, ok bool) {
	t.Helper()
	own, err := parseOwnership(manifest)
	if err != nil {
		t.Fatalf("parseOwnership: %v", err)
	}
	l, err := explainfiles.Parse(listing)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	findings, err := own.check(l)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(findings) == 0 {
		return "", true
	}
	return own.report(findings), false
}

func TestCheck_TypeImportThroughADepNamesTheOwner(t *testing.T) {
	out, ok := runCheck(t, middleOwnership, middleListing)
	if ok {
		t.Fatal("middle.ts reaches hidden.d.ts through :leaf alone, " +
			"and the check passed")
	}
	for _, want := range []string{
		"//tests/strict_deps:middle imports files no direct dep provides:",
		`tests/strict_deps/middle.ts imports "./hidden"`,
		"resolved to " + strictDepsBin + "/hidden.d.ts",
		"add //tests/strict_deps:hidden to deps",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the message lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "add //tests/strict_deps:leaf") {
		t.Errorf("the declared dep :leaf is reported:\n%s", out)
	}
}

// The edge from leaf.d.ts to hidden.d.ts is :leaf's to declare, not :middle's.
func TestCheck_EdgesFromADepsFileAreNotThisTargets(t *testing.T) {
	listing := strictDepsBin + `/hidden.d.ts
   Imported via "./hidden" from file '` + strictDepsBin + `/leaf.d.ts'
` + strictDepsBin + `/leaf.d.ts
   Imported via "./leaf" from file 'tests/strict_deps/middle.ts'
tests/strict_deps/middle.ts
   Root file specified for compilation
`
	if out, ok := runCheck(t, middleOwnership, listing); !ok {
		t.Errorf("an edge from a dep's file failed the check:\n%s", out)
	}
}

func TestCheck_RelativeImportInsideTheTargetPasses(t *testing.T) {
	manifest := `label	//tests/strict_deps:inside
own	tests/strict_deps/inside.ts
own	tests/strict_deps/inside_part.ts
`
	listing := `tests/strict_deps/inside_part.ts
   Imported via "./inside_part" from file 'tests/strict_deps/inside.ts'
tests/strict_deps/inside.ts
   Root file specified for compilation
`
	if out, ok := runCheck(t, manifest, listing); !ok {
		t.Errorf("an import inside the target failed the check:\n%s", out)
	}
}

const builtinsOwnership = `label	//tests/strict_deps:builtins
own	tests/strict_deps/builtins.ts
npm-direct	@types/node
npm	undici-types	@npm//:undici-types
`

func TestCheck_NodeBuiltinIntoTypesNodeDeclaredPasses(t *testing.T) {
	listing := `node_modules/@types/node/fs.d.ts
   Imported via "node:fs" from file 'tests/strict_deps/builtins.ts'
node_modules/@types/node/path.d.ts
   Imported via "path" from file 'tests/strict_deps/builtins.ts'
node_modules/@types/node/globals.d.ts
   Referenced via 'globals.d.ts' from file 'node_modules/@types/node/index.d.ts'
node_modules/undici-types/index.d.ts
   Imported via "undici-types" from file ` +
		`'node_modules/@types/node/globals.d.ts'` +
		` with packageId 'undici-types/index.d.ts@6.21.0'
tests/strict_deps/builtins.ts
   Root file specified for compilation
`
	if out, ok := runCheck(t, builtinsOwnership, listing); !ok {
		t.Errorf("node: and bare builtins into the declared "+
			"@types/node failed:\n%s", out)
	}
}

func TestCheck_NpmEdgeIntoATransitivePackageNamesItsLabel(t *testing.T) {
	listing := `node_modules/undici-types/index.d.ts
   Imported via "undici-types" from file 'tests/strict_deps/builtins.ts'` +
		` with packageId 'undici-types/index.d.ts@6.21.0'
tests/strict_deps/builtins.ts
   Root file specified for compilation
`
	out, ok := runCheck(t, builtinsOwnership, listing)
	if ok {
		t.Fatal("an import into a package only @types/node reaches passed")
	}
	if !strings.Contains(out, `builtins.ts imports "undici-types"`) ||
		!strings.Contains(out, "add @npm//:undici-types to deps") {
		t.Errorf("the message names neither the edge nor the label:\n%s", out)
	}
}

// A store path names its package by the segments after the last node_modules/.
func TestCheck_NestedStorePathNamesThePackage(t *testing.T) {
	manifest := `label	//pkg:app
own	pkg/app.ts
npm-direct	zod
npm	@scope/util	@npm//:scope_util
`
	store := "node_modules/.pnpm/@scope+util@1.0.0/node_modules/@scope/util"
	listing := store + `/index.d.ts
   Imported via "@scope/util" from file 'pkg/app.ts'` +
		` with packageId '@scope/util/index.d.ts@1.0.0'
node_modules/zod/node_modules/@scope/util/index.d.ts
   Imported via "@scope/util/index" from file 'pkg/app.ts'
node_modules/zod/index.d.ts
   Imported via "zod" from file 'pkg/app.ts'` +
		` with packageId 'zod/index.d.ts@3.24.2'
pkg/app.ts
   Root file specified for compilation
`
	out, ok := runCheck(t, manifest, listing)
	if ok {
		t.Fatal("imports into a transitive @scope/util passed")
	}
	if got := strings.Count(out, "add @npm//:scope_util to deps"); got != 2 {
		t.Errorf("want the label once per edge (2), got %d:\n%s", got, out)
	}
	if strings.Contains(out, `imports "zod"`) {
		t.Errorf("the declared zod is reported:\n%s", out)
	}
}

func TestCheck_ReferenceDirectivesAreEdgesToo(t *testing.T) {
	manifest := `label	//tests/strict_deps:refs
own	tests/strict_deps/refs.ts
direct	//tests/strict_deps:leaf
file	//tests/strict_deps:hidden	` + strictDepsBin + `/hidden.d.ts
npm	@types/node	@npm//:types_node
`
	ref := "../../" + strictDepsBin + "/hidden.d.ts"
	listing := strictDepsBin + `/hidden.d.ts
   Referenced via '` + ref + `' from file 'tests/strict_deps/refs.ts'
node_modules/@types/node/index.d.ts
   Type library referenced via 'node' from file 'tests/strict_deps/refs.ts'
tests/strict_deps/refs.ts
   Root file specified for compilation
`
	out, ok := runCheck(t, manifest, listing)
	if ok {
		t.Fatal("reference directives into undeclared labels passed")
	}
	for _, want := range []string{
		"refs.ts references '" + ref + "'",
		"add //tests/strict_deps:hidden to deps",
		"refs.ts references types 'node'",
		"add @npm//:types_node to deps",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the message lacks %q:\n%s", want, out)
		}
	}
}

// A ts_codegen tree is one entry; every file under it is the codegen's.
func TestCheck_ATreeOwnsEverythingUnderIt(t *testing.T) {
	tree := binDir + "/gen/types"
	listing := tree + `/env.d.ts
   Imported via "#gen/env" from file 'pkg/app.ts'
pkg/app.ts
   Root file specified for compilation
`
	declared := "label\t//pkg:app\nown\tpkg/app.ts\ndirect\t//gen:types\n" +
		"file\t//gen:types\t" + tree + "\n"
	if out, ok := runCheck(t, declared, listing); !ok {
		t.Errorf("a file under a declared tree failed the check:\n%s", out)
	}
	transitive := "label\t//pkg:app\nown\tpkg/app.ts\ndirect\t//pkg:lib\n" +
		"file\t//gen:types\t" + tree + "\n"
	out, ok := runCheck(t, transitive, listing)
	if ok {
		t.Fatal("a file under a tree only a dep declares passed")
	}
	if !strings.Contains(out, "add //gen:types to deps") {
		t.Errorf("the message does not name the tree's target:\n%s", out)
	}
}

// Two targets may list one declaration; the declared one's claim answers.
func TestCheck_ADirectClaimWinsOverATransitiveOne(t *testing.T) {
	shared := "tests/strict_deps/shared.d.ts"
	manifest := "label\t//pkg:app\nown\tpkg/app.ts\ndirect\t//pkg:lib\n" +
		"file\t//pkg:other\t" + shared + "\nfile\t//pkg:lib\t" + shared + "\n"
	listing := shared + `
   Imported via "../tests/strict_deps/shared" from file 'pkg/app.ts'
pkg/app.ts
   Root file specified for compilation
`
	if out, ok := runCheck(t, manifest, listing); !ok {
		t.Errorf("a file the direct dep also stages failed the check:\n%s", out)
	}
}

func TestCheck_AFileNothingOwnsIsAnError(t *testing.T) {
	own, err := parseOwnership("label\t//pkg:app\nown\tpkg/app.ts\n")
	if err != nil {
		t.Fatal(err)
	}
	l, err := explainfiles.Parse(binDir + `/pkg/stray.d.ts
   Imported via "./stray" from file 'pkg/app.ts'
pkg/app.ts
   Root file specified for compilation
`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = own.check(l)
	if err == nil || !strings.Contains(err.Error(), binDir+"/pkg/stray.d.ts") {
		t.Errorf("check = %v, want an error naming the stray file", err)
	}
}

func TestReport_OneEntryPerEdgeInPathOrder(t *testing.T) {
	manifest := "label\t//pkg:app\nown\tpkg/b.ts\nown\tpkg/a.ts\n" +
		"file\t//pkg:x\t" + binDir + "/pkg/x.d.ts\n" +
		"file\t//pkg:y\t" + binDir + "/pkg/y.d.ts\n"
	listing := binDir + `/pkg/y.d.ts
   Imported via "./y" from file 'pkg/b.ts'
   Imported via "./y" from file 'pkg/b.ts'
   Imported via "./y" from file 'pkg/a.ts'
` + binDir + `/pkg/x.d.ts
   Imported via "./x" from file 'pkg/a.ts'
pkg/a.ts
   Root file specified for compilation
pkg/b.ts
   Root file specified for compilation
`
	out, ok := runCheck(t, manifest, listing)
	if ok {
		t.Fatal("undeclared edges passed")
	}
	want := `//pkg:app imports files no direct dep provides:
  pkg/a.ts imports "./x"
    resolved to ` + binDir + `/pkg/x.d.ts
    add //pkg:x to deps
  pkg/a.ts imports "./y"
    resolved to ` + binDir + `/pkg/y.d.ts
    add //pkg:y to deps
  pkg/b.ts imports "./y"
    resolved to ` + binDir + `/pkg/y.d.ts
    add //pkg:y to deps
Each reaches this target only through another dep's own deps. Run Gazelle,
which writes deps from these edges, or add the labels above by hand.
`
	if out != want {
		t.Errorf("report:\n%s\nwant:\n%s", out, want)
	}
}

func TestNpmPackageOf(t *testing.T) {
	for p, want := range map[string]string{
		"node_modules/zod/index.d.ts":                                  "zod",
		"node_modules/@types/node/fs.d.ts":                             "@types/node",
		"node_modules/.pnpm/foo@1.0.0/node_modules/foo/index.d.ts":     "foo",
		"node_modules/a/node_modules/@s/b/x.d.ts":                      "@s/b",
		"node_modules/@types/node":                                     "@types/node",
		"tests/strict_deps/middle.ts":                                  "",
		binDir + "/tests/strict_deps/hidden.d.ts":                      "",
		"../../external/+ts+tsgo_linux_amd64/lib/lib.es2022.full.d.ts": "",
	} {
		if got := npmPackageOf(p); got != want {
			t.Errorf("npmPackageOf(%q) = %q, want %q", p, got, want)
		}
	}
}

func TestParseOwnership_RefusesAnUnknownLine(t *testing.T) {
	_, err := parseOwnership("label\t//pkg:app\nowner\t//pkg:x\tpkg/x.ts\n")
	if err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Errorf("parseOwnership = %v, want an error naming line 2", err)
	}
}
