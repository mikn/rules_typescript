package explainfiles

// testdata/workers_download_test.listing.txt is the Lovable monorepo's
// workers/download/test program, listed from its root with --explainFiles.

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
)

const (
	sample    = "testdata/workers_download_test.listing.txt"
	store     = "../../../.local/share/pnpm/store/v11/links/"
	storeNode = store + "@types/node/22.13.13/" +
		"9435199b93d373e137e4c716aec7ae4974ab7bb876d51e74a0919a6d6d780519" +
		"/node_modules/@types/node/index.d.ts"
	storeUndici = store + "@/undici-types/6.20.0/" +
		"91f3a19b95acf703b5bb73429c4b4f8a36342005ce1ad1bb380fcbef595f5a3b" +
		"/node_modules/undici-types/formdata.d.ts"
	storePoolTypes = store + "@cloudflare/vitest-pool-workers/0.18.4/" +
		"8648841c094c3e6cd8f80b31b4203386c6154c0a84ccf11aa2c8b9b382106bb9" +
		"/node_modules/@cloudflare/vitest-pool-workers/types/cloudflare-test.d.ts"
)

func parseSample(t *testing.T) *Listing {
	t.Helper()
	data, err := os.ReadFile(sample)
	if err != nil {
		t.Fatal(err)
	}
	l, err := Parse(string(data))
	if err != nil {
		t.Fatalf("Parse(%s): %v", sample, err)
	}
	return l
}

func TestParse_SampleRoots(t *testing.T) {
	l := parseSample(t)
	want := []string{
		"workers/download/test/env.d.ts",
		"workers/download/test/index.spec.ts",
		"workers/download/worker-configuration.d.ts",
	}
	if !slices.Equal(l.Roots, want) {
		t.Errorf("roots = %q, want %q", l.Roots, want)
	}
	if len(l.Files) != 208 {
		t.Errorf("files listed = %d, want 208 (the file lines of the sample)",
			len(l.Files))
	}
	if len(l.Edges) != 352 {
		t.Errorf("edges = %d, want 352 (283 imports, 61 references, "+
			"8 type-library references)", len(l.Edges))
	}
	if len(l.Diagnostics) != 0 {
		t.Errorf("diagnostics = %q, want none", l.Diagnostics)
	}
}

func TestParse_SampleEdgeIntoAnotherProgram(t *testing.T) {
	l := parseSample(t)
	want := Edge{
		Kind:      Import,
		From:      "workers/download/test/index.spec.ts",
		To:        "workers/download/src/index.ts",
		Specifier: "../src/index",
	}
	if !slices.Contains(l.Edges, want) {
		t.Errorf("no edge %+v; the edges from the test file are %+v", want,
			edgesFrom(l, want.From))
	}
	typeRef := Edge{
		Kind:      TypeReference,
		From:      storeUndici,
		To:        storeNode,
		Specifier: "node",
	}
	if !slices.Contains(l.Edges, typeRef) {
		t.Errorf("no /// <reference types> edge %+v", typeRef)
	}
}

func edgesFrom(l *Listing, from string) []Edge {
	var out []Edge
	for _, e := range l.Edges {
		if e.From == from {
			out = append(out, e)
		}
	}
	return out
}

func TestParse_SampleTypeEntriesVerbatim(t *testing.T) {
	l := parseSample(t)
	want := []TypeEntry{
		{Entry: "../worker-configuration.d.ts",
			File: "workers/download/worker-configuration.d.ts"},
		{Entry: "@cloudflare/vitest-pool-workers/types", File: storePoolTypes},
		{Entry: "node", File: storeNode},
	}
	if !slices.Equal(l.Types, want) {
		t.Errorf("types = %+v, want %+v", l.Types, want)
	}
	if len(l.Implicit) != 0 {
		t.Errorf("implicit type libraries = %+v, want none: the tsconfig "+
			"has a types key", l.Implicit)
	}
}

func TestParse_ReasonForms(t *testing.T) {
	cases := []struct{ line, want string }{
		{`Imported via "./b" from file 'pkg/src/a.ts'`,
			"import ./b from pkg/src/a.ts"},
		{`Imported via "react" from file 'pkg/src/a.ts'` +
			` with packageId 'react/index.d.ts@19.0.0'`,
			"import react from pkg/src/a.ts"},
		{`Imported via "react/jsx-runtime" from file 'pkg/src/a.ts'` +
			` to import 'jsx' and 'jsxs' factory functions`,
			"import react/jsx-runtime from pkg/src/a.ts"},
		{`Imported via "react/jsx-runtime" from file 'pkg/src/a.ts'` +
			` with packageId 'react/jsx-runtime.d.ts@19.0.0'` +
			` to import 'jsx' and 'jsxs' factory functions`,
			"import react/jsx-runtime from pkg/src/a.ts"},
		{`Imported via "tslib" from file 'pkg/src/a.ts'` +
			` to import 'importHelpers' as specified in compilerOptions`,
			"import tslib from pkg/src/a.ts"},
		{`Imported via "tslib" from file 'pkg/src/a.ts'` +
			` with packageId 'tslib/tslib.d.ts@2.8.1'` +
			` to import 'importHelpers' as specified in compilerOptions`,
			"import tslib from pkg/src/a.ts"},
		{`Referenced via './globals.d.ts' from file 'pkg/src/a.ts'`,
			"reference ./globals.d.ts from pkg/src/a.ts"},
		{`Type library referenced via 'node' from file 'pkg/src/a.ts'`,
			"typeref node from pkg/src/a.ts"},
		{`Type library referenced via 'node' from file 'pkg/src/a.ts'` +
			` with packageId '@types/node/index.d.ts@22.13.13'`,
			"typeref node from pkg/src/a.ts"},
		{`Entry point of type library 'node' specified in compilerOptions`,
			"types node"},
		{`Entry point of type library 'node' specified in compilerOptions` +
			` with packageId '@types/node/index.d.ts@22.13.13'`,
			"types node"},
		{`Entry point for implicit type library 'node'`, "implicit node"},
		{`Entry point for implicit type library 'node'` +
			` with packageId '@types/node/index.d.ts@22.13.13'`,
			"implicit node"},
		{`Matched by include pattern 'src/**/*.ts' in 'pkg/tsconfig.json'`,
			"root"},
		{`Matched by default include pattern '**/*'`, "root"},
		{`Part of 'files' list in tsconfig.json`, "root"},
		{`Root file specified for compilation`, "root"},
		{`Library referenced via 'es2015' from file 'pkg/src/a.ts'`,
			"nothing"},
		{`Library 'lib.es2022.d.ts' specified in compilerOptions`, "nothing"},
		{`Default library for target 'es2022'`, "nothing"},
		{`Default library`, "nothing"},
		{`File is ECMAScript module because 'pkg/package.json'` +
			` has field "type" with value "module"`, "nothing"},
		{`File is CommonJS module because 'pkg/package.json'` +
			` has field "type" whose value is not "module"`, "nothing"},
		{`File is CommonJS module because 'pkg/package.json'` +
			` does not have field "type"`, "nothing"},
		{`File is CommonJS module because 'package.json' was not found`,
			"nothing"},
		{`File redirects to file 'pkg/node_modules/.pnpm/react@19.0.0` +
			`/node_modules/react/index.d.ts'`, "nothing"},
	}
	if len(cases) != len(reasonForms) {
		t.Fatalf("%d forms in this table, %d in the parser", len(cases),
			len(reasonForms))
	}
	for _, c := range cases {
		l, err := Parse("pkg/src/target.ts\n   " + c.line + "\n")
		if err != nil {
			t.Errorf("%s: %v", c.line, err)
			continue
		}
		if got := readingOf(l); got != c.want {
			t.Errorf("%s:\n  read as %q, want %q", c.line, got, c.want)
		}
	}
}

func TestParse_SpecifierQuoteSpelling(t *testing.T) {
	for _, line := range []string{
		`Imported via './b' from file 'pkg/src/a.ts'`,
		`Imported via 'react' from file 'pkg/src/a.ts'` +
			` with packageId 'react/index.d.ts@19.0.0'`,
		`Imported via "it's" from file 'pkg/src/a.ts'`,
	} {
		l, err := Parse("pkg/src/target.ts\n   " + line + "\n")
		if err != nil {
			t.Errorf("%s: %v", line, err)
			continue
		}
		if len(l.Edges) != 1 || l.Edges[0].Kind != Import ||
			l.Edges[0].From != "pkg/src/a.ts" {
			t.Errorf("%s: read as %+v", line, l)
		}
	}
}

// tsgo prints a path as it is, quotes included; the grammar must not end the
// path at one, nor read a form's own suffix into it.
func TestParse_QuotesInsidePaths(t *testing.T) {
	for _, c := range []struct{ line, want string }{
		{`Imported via "./b" from file 'pkg/o'brien/a.ts'`,
			"import ./b from pkg/o'brien/a.ts"},
		{`Imported via './b' from file 'pkg/o'brien/a.ts'` +
			` with packageId 'b/index.d.ts@1.0.0'`,
			"import ./b from pkg/o'brien/a.ts"},
		{`Imported via "react/jsx-runtime" from file 'pkg/o'brien/a.tsx'` +
			` with packageId 'react/jsx-runtime.d.ts@19.0.0'` +
			` to import 'jsx' and 'jsxs' factory functions`,
			"import react/jsx-runtime from pkg/o'brien/a.tsx"},
		{`Imported via "it's" from file 'pkg/o'brien/a.ts'`,
			"import it's from pkg/o'brien/a.ts"},
		{`Referenced via './x.d.ts' from file 'pkg/o'brien/a.ts'`,
			"reference ./x.d.ts from pkg/o'brien/a.ts"},
		{`Type library referenced via 'node' from file 'pkg/o'brien/a.ts'` +
			` with packageId '@types/node/index.d.ts@22.13.13'`,
			"typeref node from pkg/o'brien/a.ts"},
		{`Matched by include pattern 'src/**/*.ts' in 'pkg/o'brien/tsconfig.json'`,
			"root"},
		{`File redirects to file 'pkg/o'brien/node_modules/react/index.d.ts'`,
			"nothing"},
	} {
		l, err := Parse("pkg/src/target.ts\n   " + c.line + "\n")
		if err != nil {
			t.Errorf("%s: %v", c.line, err)
			continue
		}
		if got := readingOf(l); got != c.want {
			t.Errorf("%s:\n  read as %q, want %q", c.line, got, c.want)
		}
	}
}

func readingOf(l *Listing) string {
	switch {
	case len(l.Roots) == 1:
		return "root"
	case len(l.Edges) == 1:
		e := l.Edges[0]
		kind := map[EdgeKind]string{Import: "import", Reference: "reference",
			TypeReference: "typeref"}[e.Kind]
		return fmt.Sprintf("%s %s from %s", kind, e.Specifier, e.From)
	case len(l.Types) == 1:
		return "types " + l.Types[0].Entry
	case len(l.Implicit) == 1:
		return "implicit " + l.Implicit[0].Entry
	case len(l.Roots)+len(l.Edges)+len(l.Types)+len(l.Implicit) == 0:
		return "nothing"
	}
	return fmt.Sprintf("%+v", l)
}

func TestParse_UnknownReasonLineIsAnError(t *testing.T) {
	const line = `Output from referenced project 'pkg/lib/tsconfig.json'` +
		` included because '--module' is specified as 'none'`
	_, err := Parse("pkg/src/a.ts\n   " + line + "\n")
	if err == nil {
		t.Fatal("a reason line the grammar does not know parsed without error")
	}
	if !strings.Contains(err.Error(), line) {
		t.Errorf("the error does not quote the line:\n%v", err)
	}
	_, err = Parse("   Matched by default include pattern '**/*'\n")
	if err == nil {
		t.Error("a reason line before any file line parsed without error")
	}
}

// What tsgo prints, with exit 2, for a tsconfig.json whose include matches
// nothing.
const noInputsOutput = `error TS18003: No inputs were found in config file ` +
	`'/w/pkg/tsconfig.json'. Specified 'include' paths were ` +
	`'["src/**/*.ts","bin/*.ts"]' and 'exclude' paths were '["node_modules"]'.`

func TestParse_NoInputsIsZeroRoots(t *testing.T) {
	l, err := Parse(noInputsOutput + "\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Roots) != 0 || len(l.Files) != 0 {
		t.Errorf("roots = %q, files = %q, want none", l.Roots, l.Files)
	}
	if len(l.Diagnostics) != 1 || !strings.Contains(l.Diagnostics[0], "TS18003") {
		t.Errorf("diagnostics = %q, want the TS18003 line", l.Diagnostics)
	}

	// A diagnostic's continuation lines are indented two spaces per level of
	// its message chain, a reason three; the listing follows the diagnostics.
	withListing := "eslint-plugin/tsconfig.json(31,5): error TS5102: Option " +
		"'baseUrl' has been removed. Please remove it from your configuration.\n" +
		"  Use '\"paths\": {\"*\": [\"./*\"]}' instead.\n" +
		"    A second level of the chain, indented two more.\n" +
		"eslint-plugin/src/index.ts\n" +
		"   Matched by include pattern 'src/**/*.ts' in " +
		"'eslint-plugin/tsconfig.json'\n"
	l, err = Parse(withListing)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(l.Roots, []string{"eslint-plugin/src/index.ts"}) {
		t.Errorf("roots = %q, want the one include match", l.Roots)
	}
	if len(l.Diagnostics) != 1 || strings.Count(l.Diagnostics[0], "\n") != 2 ||
		!strings.HasSuffix(l.Diagnostics[0], "indented two more.") {
		t.Errorf("diagnostics = %q, want TS5102 with its two continuation lines",
			l.Diagnostics)
	}
}

func TestParse_TraceBlocksAreNotFiles(t *testing.T) {
	text := strings.Join([]string{
		"======== Resolving module 'nope' from '/w/island/a.ts'. ========",
		"Explicitly specified module resolution kind: 'Bundler'.",
		"File '/w/node_modules/nope.ts' does not exist.",
		"======== Module name 'nope' was not resolved. ========",
		"======== Resolving module 'foo' from '/w/island/a.ts'. ========",
		"Found 'package.json' at '/w/node_modules/foo/package.json'.",
		"======== Module name 'foo' was successfully resolved to " +
			"'/w/node_modules/foo/index.d.ts' with Package ID " +
			"'foo/index.d.ts@1.0.0'. ========",
		"======== Resolving type reference directive 'gone', containing file " +
			"'/w/island/__inferred type names__.ts', root directory " +
			"'/w/island/node_modules/@types'. ========",
		"Resolving with primary search path '/w/island/node_modules/@types'.",
		"======== Type reference directive 'gone' was not resolved. ========",
		"node_modules/foo/index.d.ts",
		"   Imported via \"foo\" from file 'island/a.ts' with packageId " +
			"'foo/index.d.ts@1.0.0'",
		"island/a.ts",
		"   Matched by include pattern '*.ts' in 'island/tsconfig.json'",
	}, "\n") + "\n"
	l, err := Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"node_modules/foo/index.d.ts", "island/a.ts"}
	if !slices.Equal(l.Files, want) {
		t.Errorf("files = %q, want %q", l.Files, want)
	}
	if want := []string{"nope", "gone"}; !slices.Equal(l.Unresolved, want) {
		t.Errorf("unresolved = %q, want %q", l.Unresolved, want)
	}
	if len(l.Edges) != 1 || len(l.Roots) != 1 {
		t.Errorf("edges %+v roots %q, want one of each", l.Edges, l.Roots)
	}
}
