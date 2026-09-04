package typescript

// testdata/workers_download_test.listing.txt is the Lovable monorepo's
// workers/download/test program, listed from its root with --explainFiles.

import (
	"flag"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bazelbuild/bazel-gazelle/config"
	"github.com/bazelbuild/bazel-gazelle/language"
)

const sampleListing = "testdata/workers_download_test.listing.txt"

func parseSampleListing(t *testing.T) *program {
	t.Helper()
	data, err := os.ReadFile(sampleListing)
	if err != nil {
		t.Fatal(err)
	}
	p, err := parseListing(string(data))
	if err != nil {
		t.Fatalf("parseListing(%s): %v", sampleListing, err)
	}
	return p
}

func TestProgram_SampleRoots(t *testing.T) {
	p := parseSampleListing(t)
	want := []string{
		"workers/download/test/env.d.ts",
		"workers/download/test/index.spec.ts",
		"workers/download/worker-configuration.d.ts",
	}
	if !slices.Equal(p.roots, want) {
		t.Errorf("roots = %q, want %q", p.roots, want)
	}
	if len(p.files) != 208 {
		t.Errorf("files listed = %d, want 208 (the file lines of the sample)", len(p.files))
	}
	if len(p.edges) != 352 {
		t.Errorf("edges = %d, want 352 (283 imports, 61 references, 8 type-library references)", len(p.edges))
	}
	if len(p.diagnostics) != 0 {
		t.Errorf("diagnostics = %q, want none", p.diagnostics)
	}
}

func TestProgram_SampleEdgeIntoAnotherProgram(t *testing.T) {
	p := parseSampleListing(t)
	want := edge{
		kind:      edgeImport,
		from:      "workers/download/test/index.spec.ts",
		to:        "workers/download/src/index.ts",
		specifier: "../src/index",
	}
	if !slices.Contains(p.edges, want) {
		t.Errorf("no edge %+v; the edges from the test file are %+v", want, edgesFrom(p, want.from))
	}
	typeRef := edge{
		kind:      edgeTypeReference,
		from:      "../../../.local/share/pnpm/store/v11/links/@/undici-types/6.20.0/91f3a19b95acf703b5bb73429c4b4f8a36342005ce1ad1bb380fcbef595f5a3b/node_modules/undici-types/formdata.d.ts",
		to:        "../../../.local/share/pnpm/store/v11/links/@types/node/22.13.13/9435199b93d373e137e4c716aec7ae4974ab7bb876d51e74a0919a6d6d780519/node_modules/@types/node/index.d.ts",
		specifier: "node",
	}
	if !slices.Contains(p.edges, typeRef) {
		t.Errorf("no /// <reference types> edge %+v", typeRef)
	}
}

func edgesFrom(p *program, from string) []edge {
	var out []edge
	for _, e := range p.edges {
		if e.from == from {
			out = append(out, e)
		}
	}
	return out
}

func TestProgram_SampleTypeEntriesVerbatim(t *testing.T) {
	p := parseSampleListing(t)
	want := []typeEntry{
		{entry: "../worker-configuration.d.ts", file: "workers/download/worker-configuration.d.ts"},
		{entry: "@cloudflare/vitest-pool-workers/types", file: "../../../.local/share/pnpm/store/v11/links/@cloudflare/vitest-pool-workers/0.18.4/8648841c094c3e6cd8f80b31b4203386c6154c0a84ccf11aa2c8b9b382106bb9/node_modules/@cloudflare/vitest-pool-workers/types/cloudflare-test.d.ts"},
		{entry: "node", file: "../../../.local/share/pnpm/store/v11/links/@types/node/22.13.13/9435199b93d373e137e4c716aec7ae4974ab7bb876d51e74a0919a6d6d780519/node_modules/@types/node/index.d.ts"},
	}
	if !slices.Equal(p.types, want) {
		t.Errorf("types = %+v, want %+v", p.types, want)
	}
	if len(p.implicit) != 0 {
		t.Errorf("implicit type libraries = %+v, want none: the tsconfig has a types key", p.implicit)
	}
}

func TestProgram_ReasonForms(t *testing.T) {
	cases := []struct{ line, want string }{
		{`Imported via "./b" from file 'pkg/src/a.ts'`, "import ./b from pkg/src/a.ts"},
		{`Imported via "react" from file 'pkg/src/a.ts' with packageId 'react/index.d.ts@19.0.0'`, "import react from pkg/src/a.ts"},
		{`Imported via "react/jsx-runtime" from file 'pkg/src/a.ts' to import 'jsx' and 'jsxs' factory functions`, "import react/jsx-runtime from pkg/src/a.ts"},
		{`Imported via "react/jsx-runtime" from file 'pkg/src/a.ts' with packageId 'react/jsx-runtime.d.ts@19.0.0' to import 'jsx' and 'jsxs' factory functions`, "import react/jsx-runtime from pkg/src/a.ts"},
		{`Imported via "tslib" from file 'pkg/src/a.ts' to import 'importHelpers' as specified in compilerOptions`, "import tslib from pkg/src/a.ts"},
		{`Imported via "tslib" from file 'pkg/src/a.ts' with packageId 'tslib/tslib.d.ts@2.8.1' to import 'importHelpers' as specified in compilerOptions`, "import tslib from pkg/src/a.ts"},
		{`Referenced via './globals.d.ts' from file 'pkg/src/a.ts'`, "reference ./globals.d.ts from pkg/src/a.ts"},
		{`Type library referenced via 'node' from file 'pkg/src/a.ts'`, "typeref node from pkg/src/a.ts"},
		{`Type library referenced via 'node' from file 'pkg/src/a.ts' with packageId '@types/node/index.d.ts@22.13.13'`, "typeref node from pkg/src/a.ts"},
		{`Entry point of type library 'node' specified in compilerOptions`, "types node"},
		{`Entry point of type library 'node' specified in compilerOptions with packageId '@types/node/index.d.ts@22.13.13'`, "types node"},
		{`Entry point for implicit type library 'node'`, "implicit node"},
		{`Entry point for implicit type library 'node' with packageId '@types/node/index.d.ts@22.13.13'`, "implicit node"},
		{`Matched by include pattern 'src/**/*.ts' in 'pkg/tsconfig.json'`, "root"},
		{`Matched by default include pattern '**/*'`, "root"},
		{`Part of 'files' list in tsconfig.json`, "root"},
		{`Root file specified for compilation`, "root"},
		{`Library referenced via 'es2015' from file 'pkg/src/a.ts'`, "nothing"},
		{`Library 'lib.es2022.d.ts' specified in compilerOptions`, "nothing"},
		{`Default library for target 'es2022'`, "nothing"},
		{`Default library`, "nothing"},
		{`File is ECMAScript module because 'pkg/package.json' has field "type" with value "module"`, "nothing"},
		{`File is CommonJS module because 'pkg/package.json' has field "type" whose value is not "module"`, "nothing"},
		{`File is CommonJS module because 'pkg/package.json' does not have field "type"`, "nothing"},
		{`File is CommonJS module because 'package.json' was not found`, "nothing"},
		{`File redirects to file 'pkg/node_modules/.pnpm/react@19.0.0/node_modules/react/index.d.ts'`, "nothing"},
	}
	if len(cases) != len(reasonForms) {
		t.Fatalf("%d forms in this table, %d in the parser", len(cases), len(reasonForms))
	}
	for _, c := range cases {
		p, err := parseListing("pkg/src/target.ts\n   " + c.line + "\n")
		if err != nil {
			t.Errorf("%s: %v", c.line, err)
			continue
		}
		if got := readingOf(p); got != c.want {
			t.Errorf("%s:\n  read as %q, want %q", c.line, got, c.want)
		}
	}
}

func TestProgram_SpecifierQuoteSpelling(t *testing.T) {
	for _, line := range []string{
		`Imported via './b' from file 'pkg/src/a.ts'`,
		`Imported via 'react' from file 'pkg/src/a.ts' with packageId 'react/index.d.ts@19.0.0'`,
		`Imported via "it's" from file 'pkg/src/a.ts'`,
	} {
		p, err := parseListing("pkg/src/target.ts\n   " + line + "\n")
		if err != nil {
			t.Errorf("%s: %v", line, err)
			continue
		}
		if len(p.edges) != 1 || p.edges[0].kind != edgeImport || p.edges[0].from != "pkg/src/a.ts" {
			t.Errorf("%s: read as %+v", line, p)
		}
	}
}

// tsgo prints a path as it is, quotes included; the grammar must not end the
// path at one, nor read a form's own suffix into it.
func TestProgram_QuotesInsidePaths(t *testing.T) {
	for _, c := range []struct{ line, want string }{
		{`Imported via "./b" from file 'pkg/o'brien/a.ts'`, "import ./b from pkg/o'brien/a.ts"},
		{`Imported via './b' from file 'pkg/o'brien/a.ts' with packageId 'b/index.d.ts@1.0.0'`, "import ./b from pkg/o'brien/a.ts"},
		{`Imported via "react/jsx-runtime" from file 'pkg/o'brien/a.tsx' with packageId 'react/jsx-runtime.d.ts@19.0.0' to import 'jsx' and 'jsxs' factory functions`, "import react/jsx-runtime from pkg/o'brien/a.tsx"},
		{`Imported via "it's" from file 'pkg/o'brien/a.ts'`, "import it's from pkg/o'brien/a.ts"},
		{`Referenced via './x.d.ts' from file 'pkg/o'brien/a.ts'`, "reference ./x.d.ts from pkg/o'brien/a.ts"},
		{`Type library referenced via 'node' from file 'pkg/o'brien/a.ts' with packageId '@types/node/index.d.ts@22.13.13'`, "typeref node from pkg/o'brien/a.ts"},
		{`Matched by include pattern 'src/**/*.ts' in 'pkg/o'brien/tsconfig.json'`, "root"},
		{`File redirects to file 'pkg/o'brien/node_modules/react/index.d.ts'`, "nothing"},
	} {
		p, err := parseListing("pkg/src/target.ts\n   " + c.line + "\n")
		if err != nil {
			t.Errorf("%s: %v", c.line, err)
			continue
		}
		if got := readingOf(p); got != c.want {
			t.Errorf("%s:\n  read as %q, want %q", c.line, got, c.want)
		}
	}
}

func readingOf(p *program) string {
	switch {
	case len(p.roots) == 1:
		return "root"
	case len(p.edges) == 1:
		e := p.edges[0]
		kind := map[edgeKind]string{edgeImport: "import", edgeReference: "reference", edgeTypeReference: "typeref"}[e.kind]
		return fmt.Sprintf("%s %s from %s", kind, e.specifier, e.from)
	case len(p.types) == 1:
		return "types " + p.types[0].entry
	case len(p.implicit) == 1:
		return "implicit " + p.implicit[0].entry
	case len(p.roots)+len(p.edges)+len(p.types)+len(p.implicit) == 0:
		return "nothing"
	}
	return fmt.Sprintf("%+v", p)
}

func TestProgram_UnknownReasonLineIsAnError(t *testing.T) {
	const line = `Output from referenced project 'pkg/lib/tsconfig.json' included because '--module' is specified as 'none'`
	_, err := parseListing("pkg/src/a.ts\n   " + line + "\n")
	if err == nil {
		t.Fatal("a reason line the grammar does not know parsed without error")
	}
	if !strings.Contains(err.Error(), line) {
		t.Errorf("the error does not quote the line:\n%v", err)
	}
	if _, err := parseListing("   Matched by default include pattern '**/*'\n"); err == nil {
		t.Error("a reason line before any file line parsed without error")
	}
}

// What tsgo prints, with exit 2, for a tsconfig.json whose include matches nothing.
const noInputsOutput = `error TS18003: No inputs were found in config file '/w/pkg/tsconfig.json'. Specified 'include' paths were '["src/**/*.ts","bin/*.ts"]' and 'exclude' paths were '["node_modules"]'.`

func TestProgram_NoInputsIsZeroRoots(t *testing.T) {
	p, err := parseListing(noInputsOutput + "\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.roots) != 0 || len(p.files) != 0 {
		t.Errorf("roots = %q, files = %q, want none", p.roots, p.files)
	}
	if len(p.diagnostics) != 1 || !strings.Contains(p.diagnostics[0], "TS18003") {
		t.Errorf("diagnostics = %q, want the TS18003 line", p.diagnostics)
	}

	// A diagnostic's continuation lines are indented two spaces per level of
	// its message chain, a reason three; the listing follows the diagnostics.
	withListing := "eslint-plugin/tsconfig.json(31,5): error TS5102: Option 'baseUrl' has been removed. Please remove it from your configuration.\n" +
		"  Use '\"paths\": {\"*\": [\"./*\"]}' instead.\n" +
		"    A second level of the chain, indented two more.\n" +
		"eslint-plugin/src/index.ts\n" +
		"   Matched by include pattern 'src/**/*.ts' in 'eslint-plugin/tsconfig.json'\n"
	p, err = parseListing(withListing)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(p.roots, []string{"eslint-plugin/src/index.ts"}) {
		t.Errorf("roots = %q, want the one include match", p.roots)
	}
	if len(p.diagnostics) != 1 || strings.Count(p.diagnostics[0], "\n") != 2 || !strings.HasSuffix(p.diagnostics[0], "indented two more.") {
		t.Errorf("diagnostics = %q, want TS5102 with its two continuation lines", p.diagnostics)
	}
}

// A stand-in tsgo: a script printing output and exiting with exit.
func fakeTsgo(t *testing.T, dir, name, output string, exit int) string {
	t.Helper()
	out := filepath.Join(dir, name+".out")
	if err := os.WriteFile(out, []byte(output), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, name)
	if err := os.WriteFile(bin, []byte(fmt.Sprintf("#!/bin/sh\ncat %q\nexit %d\n", out, exit)), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

// FORCE_COLOR in the environment colours tsgo's diagnostics and changes their
// position grammar unless --pretty false is on the command line.
func TestProgram_ArgvPinsPrettyFalse(t *testing.T) {
	root := t.TempDir()
	argv := filepath.Join(root, "argv")
	bin := filepath.Join(root, "tsgo")
	if err := os.WriteFile(bin, []byte(fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$@\" > %q\n", argv)), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := listProgram(root, "pkg", bin); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(argv)
	if err != nil {
		t.Fatal(err)
	}
	want := "-p\npkg/tsconfig.json\n--noEmit\n--listFilesOnly\n--explainFiles\n--pretty\nfalse\n"
	if string(got) != want {
		t.Errorf("tsgo argv:\n%s\nwant:\n%s", got, want)
	}
}

// The exit policy, through a stand-in binary: what tsgo listed is kept whatever
// it exited with; nothing listed and a diagnostic beyond TS18003 is a refusal.
func TestProgram_ExitCodePolicy(t *testing.T) {
	root := t.TempDir()

	p, err := listProgram(root, "pkg", fakeTsgo(t, root, "tsgo-no-inputs", noInputsOutput+"\n", 2))
	if err != nil {
		t.Fatalf("an exit 2 explained by TS18003 failed the listing: %v", err)
	}
	if len(p.roots) != 0 || p.dir != "pkg" || p.refused != "" {
		t.Errorf("program = %+v, want pkg with no roots and no refusal", p)
	}

	if _, err := listProgram(root, "pkg", fakeTsgo(t, root, "tsgo-silent", "", 1)); err == nil {
		t.Error("an exit 1 with no diagnostic did not fail the listing")
	} else if !strings.Contains(err.Error(), "pkg/tsconfig.json") {
		t.Errorf("the error does not name the tsconfig:\n%v", err)
	}

	removedOption := "pkg/tsconfig.json(2,5): error TS5102: Option 'baseUrl' has been removed. Please remove it from your configuration.\n" +
		"  Use '\"paths\": {\"*\": [\"./*\"]}' instead.\n" +
		"pkg/src/a.ts\n" +
		"   Matched by include pattern 'src/**/*.ts' in 'pkg/tsconfig.json'\n"
	p, err = listProgram(root, "pkg", fakeTsgo(t, root, "tsgo-baseurl", removedOption, 2))
	if err != nil {
		t.Fatalf("an exit 2 explained by TS5102 failed the run: %v", err)
	}
	if p.refused != "" || !slices.Equal(p.roots, []string{"pkg/src/a.ts"}) {
		t.Errorf("program = %+v, want its root kept and no refusal", p)
	}
	if len(p.diagnostics) != 1 || !strings.Contains(p.diagnostics[0], "TS5102") {
		t.Errorf("diagnostics = %q, want the TS5102", p.diagnostics)
	}

	syntaxError := "pkg/src/broken.ts(1,18): error TS1109: Expression expected.\n" +
		"pkg/src/a.ts\n" +
		"   Matched by include pattern 'src/**/*.ts' in 'pkg/tsconfig.json'\n" +
		"pkg/src/broken.ts\n" +
		"   Matched by include pattern 'src/**/*.ts' in 'pkg/tsconfig.json'\n"
	p, err = listProgram(root, "pkg", fakeTsgo(t, root, "tsgo-syntax", syntaxError, 2))
	if err != nil {
		t.Fatalf("an exit 2 explained by a syntax error failed the run: %v", err)
	}
	if p.refused != "" || !slices.Equal(p.roots, []string{"pkg/src/a.ts", "pkg/src/broken.ts"}) {
		t.Errorf("program = %+v, want both roots kept and no refusal", p)
	}

	unreadable := noInputsOutput + "\npkg/tsconfig.json(2,1): error TS1005: ']' expected.\n"
	p, err = listProgram(root, "pkg", fakeTsgo(t, root, "tsgo-unreadable", unreadable, 2))
	if err != nil {
		t.Fatalf("an exit 2 with nothing listed failed the run instead of refusing the program: %v", err)
	}
	if !strings.Contains(p.refused, "TS1005") || !strings.Contains(p.refused, "exit status 2") {
		t.Errorf("refused = %q, want tsgo's exit and its diagnostic", p.refused)
	}
	if len(p.roots) != 0 || len(p.files) != 0 {
		t.Errorf("a refused program kept roots %q and files %q", p.roots, p.files)
	}
}

// What tsgo 7.0.2 prints, before the listing, for a tsconfig.json whose types
// names a generated file absent from the checkout (the trial's workers/download).
const missingTypesBlock = "error TS2688: Cannot find type definition file for './worker-configuration.d.ts'.\n" +
	"  The file is in the program because:\n" +
	"    Entry point of type library './worker-configuration.d.ts' specified in compilerOptions"

func TestProgram_DiagnosticsAreSaidUnderVerboseOnly(t *testing.T) {
	listing := missingTypesBlock + "\npkg/src/index.ts\n   Matched by include pattern 'src/**/*.ts' in 'pkg/tsconfig.json'\n"
	for _, verbose := range []bool{false, true} {
		root := t.TempDir()
		writeWorkspace(t, root, map[string]string{
			"package.json":      `{"name":"w"}` + "\n",
			"pkg/tsconfig.json": `{"compilerOptions":{"types":["./worker-configuration.d.ts"]},"include":["src/**/*.ts"]}` + "\n",
			"pkg/src/index.ts":  "export const a = 1;\n",
		})
		c := &config.Config{RepoRoot: root, Exts: make(map[string]interface{})}
		configureTsConfig(c, "", nil)
		store := getConfig(c).programs
		store.tsgoFlag = fakeTsgo(t, t.TempDir(), "tsgo", listing, 1)
		store.verbose = verbose

		logged := generateDir(t, c, root, "pkg")

		p := store.programs["pkg"]
		if p == nil || p.refused != "" || !slices.Equal(p.roots, []string{"pkg/src/index.ts"}) {
			t.Fatalf("-ts_verbose=%v: program = %+v, want pkg listed with its root", verbose, p)
		}
		if !slices.Equal(p.diagnostics, []string{missingTypesBlock}) {
			t.Errorf("-ts_verbose=%v: diagnostics = %q, want the TS2688 block, one diagnostic", verbose, p.diagnostics)
		}
		said := strings.Contains(logged, "typescript: pkg/tsconfig.json: "+missingTypesBlock+"\n")
		switch {
		case verbose && !said:
			t.Errorf("-ts_verbose did not say the block under its tsconfig.json:\n%s", logged)
		case !verbose && logged != "":
			t.Errorf("a run without -ts_verbose said:\n%s", logged)
		}
	}
}

func TestProgram_InputsOverTheExtendsChain(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"tsconfig.json":          `{"compilerOptions":{"types":[]}}` + "\n",
		"pkg/tsconfig.json":      `{"include":["src/**/*.ts"]}` + "\n",
		"listed/tsconfig.json":   `{"files":["main.ts"]}` + "\n",
		"child/tsconfig.json":    `{"extends":"../pkg/tsconfig.json","compilerOptions":{"strict":true}}` + "\n",
		"orphan/tsconfig.json":   `{"extends":"../tsconfig.json"}` + "\n",
		"solution/tsconfig.json": `{"files":[],"references":[{"path":"./src"}]}` + "\n",
		"broken/tsconfig.json":   `{"include": [` + "\n",
	})
	for rel, want := range map[string]bool{
		"":         false,
		"pkg":      true,
		"listed":   true,
		"child":    true,
		"orphan":   false,
		"solution": true,
	} {
		inputs, ok := programNamesInputs(filepath.Join(root, rel, "tsconfig.json"))
		if !ok {
			t.Errorf("%s/tsconfig.json: not readable", rel)
		}
		if inputs != want {
			t.Errorf("%s/tsconfig.json: inputs = %v, want %v", rel, inputs, want)
		}
	}
	captureLog(t, func() {
		if _, ok := programNamesInputs(filepath.Join(root, "broken", "tsconfig.json")); ok {
			t.Error("a tsconfig.json that does not parse was read as a program")
		}
	})
}

// generateRules over root/rel with the files on disk, returning what it logged.
func generateDir(t *testing.T, c *config.Config, root, rel string) string {
	t.Helper()
	cc := c.Clone()
	configureTsConfig(cc, rel, nil)
	dir := filepath.Join(root, rel)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var files, subdirs []string
	for _, e := range entries {
		if e.IsDir() {
			subdirs = append(subdirs, e.Name())
		} else {
			files = append(files, e.Name())
		}
	}
	return captureLog(t, func() {
		generateRules(language.GenerateArgs{Config: cc, Dir: dir, Rel: rel, RegularFiles: files, Subdirs: subdirs})
	})
}

func TestProgram_GenerateRulesRecordsThePrograms(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"package.json":         `{"name":"w"}` + "\n",
		"tsconfig.json":        `{"compilerOptions":{"types":[]}}` + "\n",
		"broken/tsconfig.json": `{"extends":"./missing.json","include":["*.ts"]}` + "\n",
		"broken/b.ts":          "export const b = 1;\n",
		"pkg/tsconfig.json":    `{"compilerOptions":{"lib":["es2022"]},"include":["src/**/*.ts"]}` + "\n",
		"pkg/src/a.ts":         "import { b } from \"./b\";\nexport const a = b;\n",
		"pkg/src/b.ts":         "export const b = 1;\n",
		"empty/tsconfig.json":  `{"compilerOptions":{"lib":["es2022"]},"include":["src/**/*.ts"]}` + "\n",
		"empty/README.md":      "no sources\n",
		"alias/tsconfig.json":  `{"compilerOptions":{"lib":["es2022"],"paths":{"#x/*":["./*"]}}}` + "\n",
		"alias/x.ts":           "export const x = 1;\n",
	})
	c := &config.Config{RepoRoot: root, Exts: make(map[string]interface{})}
	configureTsConfig(c, "", nil)
	store := getConfig(c).programs
	if _, err := store.binary(); err != nil {
		t.Skipf("no tsgo binary: %v", err)
	}

	var logged string
	for _, rel := range []string{"pkg/src", "pkg", "empty", "alias", "broken", ""} {
		logged += generateDir(t, c, root, rel)
	}

	if got := store.programs[""]; got == nil || got.refused == "" {
		t.Errorf("the root tsconfig.json was not recorded as refused: %+v", got)
	}
	if _, ok := store.programs["pkg/src"]; ok {
		t.Error("pkg/src holds no tsconfig.json and was listed anyway")
	}
	pkg := store.programs["pkg"]
	if pkg == nil {
		t.Fatalf("pkg was not listed; recorded: %v", slices.Sorted(maps.Keys(store.programs)))
	}
	if want := []string{"pkg/src/a.ts", "pkg/src/b.ts"}; !slices.Equal(slices.Sorted(slices.Values(pkg.roots)), want) {
		t.Errorf("pkg roots = %q, want %q", pkg.roots, want)
	}
	if want := (edge{kind: edgeImport, from: "pkg/src/a.ts", to: "pkg/src/b.ts", specifier: "./b"}); !slices.Contains(pkg.edges, want) {
		t.Errorf("pkg edges = %+v, want %+v among them", pkg.edges, want)
	}
	if empty := store.programs["empty"]; empty == nil || len(empty.roots) != 0 || empty.refused != "" {
		t.Errorf("empty (TS18003) = %+v, want listed with no roots", empty)
	}
	// Below the root, a tsconfig.json with neither key is tsc's program over
	// its own directory tree, and that is what gets listed.
	if alias := store.programs["alias"]; alias == nil || !slices.Equal(alias.roots, []string{"alias/x.ts"}) {
		t.Errorf("alias = %+v, want listed with alias/x.ts as its root", alias)
	}
	// tsgo lists what it can of a program whose extends target is missing; the
	// listing is kept with the TS5083 on it.
	broken := store.programs["broken"]
	if broken == nil {
		t.Fatalf("broken was not listed; recorded: %v", slices.Sorted(maps.Keys(store.programs)))
	}
	if broken.refused != "" || !slices.Equal(broken.roots, []string{"broken/b.ts"}) ||
		len(broken.diagnostics) != 1 || !strings.Contains(broken.diagnostics[0], "TS5083") {
		t.Errorf("broken = %+v, want listed with broken/b.ts as its root and the TS5083", broken)
	}
	if strings.Contains(logged, "error TS") {
		t.Errorf("a listing's diagnostic was said without -ts_verbose:\n%s", logged)
	}
}

// A go test outside Bazel has no runfiles; the walk goes on without the listing.
func TestProgram_NoBinarySkipsTheListing(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"package.json":        `{"name":"w"}` + "\n",
		"pkg/tsconfig.json":   `{"include":["*.ts"]}` + "\n",
		"pkg/a.ts":            "export const a = 1;\n",
		"other/tsconfig.json": `{"include":["*.ts"]}` + "\n",
		"other/b.ts":          "export const b = 1;\n",
	})
	saved := tsgoRlocationpath
	tsgoRlocationpath = ""
	t.Cleanup(func() { tsgoRlocationpath = saved })
	t.Setenv("TSGO", "")
	c := &config.Config{RepoRoot: root, Exts: make(map[string]interface{})}
	configureTsConfig(c, "", nil)

	var logged string
	for _, rel := range []string{"pkg", "other", ""} {
		logged += generateDir(t, c, root, rel)
	}

	if n := strings.Count(logged, "typescript: programs are not listed: no tsgo binary"); n != 1 {
		t.Errorf("the missing binary was said %d times, want once:\n%s", n, logged)
	}
	if got := getConfig(c).programs.programs; len(got) != 0 {
		t.Errorf("programs recorded without a binary: %v", slices.Sorted(maps.Keys(got)))
	}
}

func TestProgram_FilesInNoProgramAreReported(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"package.json":        `{"name":"w"}` + "\n",
		"tsconfig.json":       `{"compilerOptions":{"types":[]}}` + "\n",
		"root.ts":             "export const r = 1;\n",
		"pkg/tsconfig.json":   `{"compilerOptions":{"lib":["es2022"]},"include":["src/**/*.ts"]}` + "\n",
		"pkg/src/a.ts":        "export const a = 1;\n",
		"pkg/scripts/run.mts": "export const run = 1;\n",
		"stray/x.tsx":         "export const x = 1;\n",
		"stray/y.cts":         "export const y = 1;\n",
		"stray/notes.md":      "not a source\n",
	})
	l := NewLanguage().(*tsLang)
	c := &config.Config{RepoRoot: root, Exts: make(map[string]interface{})}
	fs := flag.NewFlagSet("gazelle", flag.ContinueOnError)
	l.RegisterFlags(fs, "update", c)
	if err := fs.Parse([]string{"-ts_verbose"}); err != nil {
		t.Fatal(err)
	}
	if _, err := getConfig(c).programs.binary(); err != nil {
		t.Skipf("no tsgo binary: %v", err)
	}
	l.Configure(c, "", nil)

	var logged string
	for _, rel := range []string{"pkg/src", "pkg/scripts", "pkg", "stray", ""} {
		logged += generateDir(t, c, root, rel)
	}
	if strings.Contains(logged, "in no program") {
		t.Errorf("the report was said before the walk was done:\n%s", logged)
	}

	want := []string{
		"typescript: .: 1 file in no program",
		"typescript: pkg/scripts: 1 file in no program",
		"typescript: stray: 2 files in no program",
		"typescript: 4 .ts/.tsx/.mts/.cts files in no program across 3 directories",
	}
	done := captureLog(t, l.DoneGeneratingRules)
	if got := strings.Split(strings.TrimSpace(done), "\n"); !slices.Equal(got, want) {
		t.Errorf("DoneGeneratingRules said:\n%s\nwant:\n%s", done, strings.Join(want, "\n"))
	}
}
