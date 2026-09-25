package typescript

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

	"github.com/mikn/rules_typescript/ts/tools/explainfiles"
)

// What tsgo prints, with exit 2, for a tsconfig.json whose include matches nothing.
const noInputsOutput = `error TS18003: No inputs were found in config file '/w/pkg/tsconfig.json'. Specified 'include' paths were '["src/**/*.ts","bin/*.ts"]' and 'exclude' paths were '["node_modules"]'.`

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
	if _, err := listProgram(root, "pkg/tsconfig.json", bin); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(argv)
	if err != nil {
		t.Fatal(err)
	}
	want := "-p\npkg/tsconfig.json\n--noEmit\n--listFilesOnly\n--explainFiles\n--pretty\nfalse\n--traceResolution\n--locale\nen\n"
	if string(got) != want {
		t.Errorf("tsgo argv:\n%s\nwant:\n%s", got, want)
	}
}

// The exit policy, through a stand-in binary: what tsgo listed is kept whatever
// it exited with; nothing listed and a diagnostic beyond TS18003 is a refusal.
func TestProgram_ExitCodePolicy(t *testing.T) {
	root := t.TempDir()

	p, err := listProgram(root, "pkg/tsconfig.json",
		fakeTsgo(t, root, "tsgo-no-inputs", noInputsOutput+"\n", 2))
	if err != nil {
		t.Fatalf("an exit 2 explained by TS18003 failed the listing: %v", err)
	}
	if len(p.Roots) != 0 || p.dir != "pkg" || p.refused != "" {
		t.Errorf("program = %+v, want pkg with no roots and no refusal", p)
	}

	if _, err := listProgram(root, "pkg/tsconfig.json",
		fakeTsgo(t, root, "tsgo-silent", "", 1)); err == nil {
		t.Error("an exit 1 with no diagnostic did not fail the listing")
	} else if !strings.Contains(err.Error(), "pkg/tsconfig.json") {
		t.Errorf("the error does not name the tsconfig:\n%v", err)
	}

	removedOption := "pkg/tsconfig.json(2,5): error TS5102: Option 'baseUrl' has been removed. Please remove it from your configuration.\n" +
		"  Use '\"paths\": {\"*\": [\"./*\"]}' instead.\n" +
		"pkg/src/a.ts\n" +
		"   Matched by include pattern 'src/**/*.ts' in 'pkg/tsconfig.json'\n"
	p, err = listProgram(root, "pkg/tsconfig.json",
		fakeTsgo(t, root, "tsgo-baseurl", removedOption, 2))
	if err != nil {
		t.Fatalf("an exit 2 explained by TS5102 failed the run: %v", err)
	}
	if p.refused != "" || !slices.Equal(p.Roots, []string{"pkg/src/a.ts"}) {
		t.Errorf("program = %+v, want its root kept and no refusal", p)
	}
	if len(p.Diagnostics) != 1 || !strings.Contains(p.Diagnostics[0], "TS5102") {
		t.Errorf("diagnostics = %q, want the TS5102", p.Diagnostics)
	}

	syntaxError := "pkg/src/broken.ts(1,18): error TS1109: Expression expected.\n" +
		"pkg/src/a.ts\n" +
		"   Matched by include pattern 'src/**/*.ts' in 'pkg/tsconfig.json'\n" +
		"pkg/src/broken.ts\n" +
		"   Matched by include pattern 'src/**/*.ts' in 'pkg/tsconfig.json'\n"
	p, err = listProgram(root, "pkg/tsconfig.json",
		fakeTsgo(t, root, "tsgo-syntax", syntaxError, 2))
	if err != nil {
		t.Fatalf("an exit 2 explained by a syntax error failed the run: %v", err)
	}
	if p.refused != "" || !slices.Equal(p.Roots,
		[]string{"pkg/src/a.ts", "pkg/src/broken.ts"}) {
		t.Errorf("program = %+v, want both roots kept and no refusal", p)
	}

	unreadable := noInputsOutput + "\npkg/tsconfig.json(2,1): error TS1005: ']' expected.\n"
	p, err = listProgram(root, "pkg/tsconfig.json",
		fakeTsgo(t, root, "tsgo-unreadable", unreadable, 2))
	if err != nil {
		t.Fatalf("an exit 2 with nothing listed failed the run instead of refusing the program: %v", err)
	}
	if !strings.Contains(p.refused, "TS1005") || !strings.Contains(p.refused, "exit status 2") {
		t.Errorf("refused = %q, want tsgo's exit and its diagnostic", p.refused)
	}
	if len(p.Roots) != 0 || len(p.Files) != 0 {
		t.Errorf("a refused program kept roots %q and files %q", p.Roots, p.Files)
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
		if p == nil || p.refused != "" ||
			!slices.Equal(p.Roots, []string{"pkg/src/index.ts"}) {
			t.Fatalf("-ts_verbose=%v: program = %+v, want pkg listed with its root", verbose, p)
		}
		if !slices.Equal(p.Diagnostics, []string{missingTypesBlock}) {
			t.Errorf("-ts_verbose=%v: diagnostics = %q, want the TS2688 block, "+
				"one diagnostic", verbose, p.Diagnostics)
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
		configureTsConfig(cc, rel, nil)
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
	roots := slices.Sorted(slices.Values(pkg.Roots))
	want := []string{"pkg/src/a.ts", "pkg/src/b.ts"}
	if !slices.Equal(roots, want) {
		t.Errorf("pkg roots = %q, want %q", pkg.Roots, want)
	}
	edge := explainfiles.Edge{Kind: explainfiles.Import, From: "pkg/src/a.ts",
		To: "pkg/src/b.ts", Specifier: "./b"}
	if !slices.Contains(pkg.Edges, edge) {
		t.Errorf("pkg edges = %+v, want %+v among them", pkg.Edges, edge)
	}
	empty := store.programs["empty"]
	if empty == nil || len(empty.Roots) != 0 || empty.refused != "" {
		t.Errorf("empty (TS18003) = %+v, want listed with no roots", empty)
	}
	// Below the root, a tsconfig.json with neither key is tsc's program over
	// its own directory tree, and that is what gets listed.
	alias := store.programs["alias"]
	if alias == nil || !slices.Equal(alias.Roots, []string{"alias/x.ts"}) {
		t.Errorf("alias = %+v, want listed with alias/x.ts as its root", alias)
	}
	// tsgo lists what it can of a program whose extends target is missing; the
	// listing is kept with the TS5083 on it.
	broken := store.programs["broken"]
	if broken == nil {
		t.Fatalf("broken was not listed; recorded: %v", slices.Sorted(maps.Keys(store.programs)))
	}
	if broken.refused != "" ||
		!slices.Equal(broken.Roots, []string{"broken/b.ts"}) ||
		len(broken.Diagnostics) != 1 ||
		!strings.Contains(broken.Diagnostics[0], "TS5083") {
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
		"typescript: 2 tsconfig.json: 1 package, 1 refused, " +
			"0 listing no first-party file",
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

// ---- the combined vitest run, the foreign project, the install ------------

// A stand-in tsgo writing its argv, one per line, to a file beside it.
func argvTsgo(t *testing.T, root string) (bin, argv string) {
	t.Helper()
	argv = filepath.Join(root, "argv")
	bin = filepath.Join(root, "tsgo")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$@\" > %q\n", argv)
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, argv
}

// A foreign tsconfig.json is refused and never handed to tsgo, the manifest
// named once; a package under the root's manifest, an importer, is listed.
func TestProgram_AForeignProjectIsNotListed(t *testing.T) {
	root := t.TempDir()
	bin, argv := argvTsgo(t, root)
	writeWorkspace(t, root, map[string]string{
		"package.json":          `{"name":"w"}` + "\n",
		pnpmLockfileName:        "lockfileVersion: '9.0'\n\nimporters:\n\n  .: {}\n",
		"foreign/package.json":  `{"name":"foreign"}` + "\n",
		"foreign/tsconfig.json": `{"include":["*.ts"]}` + "\n",
		"foreign/a.ts":          "export const a = 1;\n",
		"pkg/tsconfig.json":     `{"include":["*.ts"]}` + "\n",
		"pkg/a.ts":              "export const p = 1;\n",
	})
	c := &config.Config{RepoRoot: root, Exts: make(map[string]interface{})}
	configureTsConfig(c, "", nil)
	store := getConfig(c).programs
	store.tsgoFlag = bin
	store.verbose = true

	logged := generateDir(t, c, root, "foreign")
	if _, err := os.Stat(argv); err == nil {
		t.Error("tsgo listed the foreign project's tsconfig.json")
	}
	refused := "foreign/package.json is no importer in pnpm-lock.yaml"
	if p := store.programs["foreign"]; p == nil || p.refused != refused {
		t.Errorf("foreign = %+v, want recorded as refused: %s", p, refused)
	}
	for _, want := range []string{
		"typescript: foreign/tsconfig.json: not listed: " + refused + "\n",
		"typescript: " + refused + ", so pnpm installs nothing for it; " +
			"nothing is written under foreign\n",
	} {
		if !strings.Contains(logged, want) {
			t.Errorf("log lacks %q:\n%s", want, logged)
		}
	}

	logged = generateDir(t, c, root, "pkg")
	got, err := os.ReadFile(argv)
	if err != nil {
		t.Fatalf("tsgo did not list pkg/tsconfig.json: %v", err)
	}
	if !strings.Contains(string(got), "pkg/tsconfig.json\n") ||
		!strings.Contains(string(got), "--traceResolution") {
		t.Errorf("tsgo argv for pkg:\n%s", got)
	}
	if strings.Contains(logged, "no importer") {
		t.Errorf("pkg, under the root's manifest, was said to be foreign:\n%s",
			logged)
	}
}

// The combined run's flags: --ignoreConfig, or tsgo refuses with TS5112 when
// the root holds a tsconfig.json; --allowJs, or a .mjs config is TS6504.
func TestProgram_CombinedVitestRunArgv(t *testing.T) {
	root := t.TempDir()
	bin, argv := argvTsgo(t, root)
	configs := []string{"a/vitest.config.mts", "b/vitest.workers.config.mjs"}
	if _, err := listVitestConfigs(root, bin, configs); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(argv)
	if err != nil {
		t.Fatal(err)
	}
	want := "--noEmit\n--listFilesOnly\n--explainFiles\n--ignoreConfig\n" +
		"--allowJs\n--module\nesnext\n--moduleResolution\nbundler\n" +
		"--skipLibCheck\n--pretty\nfalse\na/vitest.config.mts\n" +
		"b/vitest.workers.config.mjs\n--traceResolution\n--locale\nen\n"
	if string(got) != want {
		t.Errorf("tsgo argv:\n%s\nwant:\n%s", got, want)
	}
}

// One run over every registered config at the first ask, edges by from-file,
// over the ruleset's own two: a plugin object with no import, a .mjs with one.
func TestProgram_CombinedVitestRunEdgesPerConfig(t *testing.T) {
	root := t.TempDir()
	const (
		configured = "tests/integration/gazelle_roundtrip/configured/" +
			"vitest.config.mts"
		workers = "tests/workers/vitest.workers.config.mjs"
		pool    = "node_modules/@cloudflare/vitest-pool-workers/index.d.ts"
	)
	files := map[string]string{
		"package.json": `{"name":"w","type":"module"}` + "\n",
		"tsconfig.json": `{"compilerOptions":{"types":[]},` +
			`"include":["nothing/**/*"]}` + "\n",
		"node_modules/@cloudflare/vitest-pool-workers/package.json": `{"name":` +
			`"@cloudflare/vitest-pool-workers","version":"0.18.4",` +
			`"types":"index.d.ts"}` + "\n",
		pool: "export declare function cloudflareTest(o: unknown): unknown;\n",
	}
	for _, cfg := range []string{configured, workers} {
		data, err := os.ReadFile("../" + cfg)
		if err != nil {
			t.Fatal(err)
		}
		files[cfg] = string(data)
	}
	writeWorkspace(t, root, files)
	s := newProgramStore()
	if _, err := s.binary(); err != nil {
		t.Skipf("no tsgo binary: %v", err)
	}
	s.vitestConfig(configured)
	s.vitestConfig(workers)

	want := []explainfiles.Edge{{Kind: explainfiles.Import, From: workers,
		To: pool, Specifier: "@cloudflare/vitest-pool-workers"}}
	if got := s.configEdges(root, workers); !slices.Equal(got, want) {
		t.Errorf("edges of %s = %+v, want %+v", workers, got, want)
	}
	if got := s.configEdges(root, configured); len(got) != 0 {
		t.Errorf("edges of %s = %+v, want none: it imports nothing", configured, got)
	}
	if got := s.configEdges(root, "tests/other/vitest.config.ts"); got != nil {
		t.Errorf("an unregistered config has edges %+v", got)
	}
}

// A config's edges are its closure's: the config imports its plugin module,
// the module a bare package and a JSON sibling; the first-party ones are srcs.
func TestProgram_ConfigEdgesAndSrcsAreTheClosure(t *testing.T) {
	root := t.TempDir()
	const (
		cfg    = "pkg/vitest.config.mts"
		plugin = "pkg/plugins/define.ts"
		meta   = "pkg/plugins/meta.json"
		pool   = "node_modules/@cloudflare/vitest-pool-workers/index.d.ts"
	)
	writeWorkspace(t, root, map[string]string{
		"package.json": `{"name":"w","type":"module"}` + "\n",
		"tsconfig.json": `{"compilerOptions":{"types":[]},` +
			`"include":["nothing/**/*"]}` + "\n",
		"node_modules/@cloudflare/vitest-pool-workers/package.json": `{"name":` +
			`"@cloudflare/vitest-pool-workers","version":"0.18.4",` +
			`"types":"index.d.ts"}` + "\n",
		pool: "export declare function cloudflareTest(o: unknown): unknown;\n",
		cfg: "import { define } from \"./plugins/define\";\n" +
			"export default { plugins: [define()] };\n",
		plugin: "import { cloudflareTest } from " +
			"\"@cloudflare/vitest-pool-workers\";\n" +
			"import meta from \"./meta.json\";\n" +
			"export const define = () => ({ name: meta.name, cloudflareTest });\n",
		meta: `{"name":"define"}` + "\n",
	})
	s := newProgramStore()
	if _, err := s.binary(); err != nil {
		t.Skipf("no tsgo binary: %v", err)
	}
	s.vitestConfig(cfg)

	want := []explainfiles.Edge{
		{Kind: explainfiles.Import, From: cfg, To: plugin,
			Specifier: "./plugins/define"},
		{Kind: explainfiles.Import, From: plugin, To: meta,
			Specifier: "./meta.json"},
		{Kind: explainfiles.Import, From: plugin, To: pool,
			Specifier: "@cloudflare/vitest-pool-workers"},
	}
	byTarget := func(a, b explainfiles.Edge) int {
		return strings.Compare(a.To, b.To)
	}
	got := s.configEdges(root, cfg)
	slices.SortFunc(got, byTarget)
	slices.SortFunc(want, byTarget)
	if !slices.Equal(got, want) {
		t.Errorf("edges of %s = %+v, want %+v", cfg, got, want)
	}
	srcs := []string{plugin, meta}
	if got := s.configSrcs(root, cfg); !slices.Equal(got, srcs) {
		t.Errorf("srcs of %s = %v, want %v", cfg, got, srcs)
	}
	if got := s.configSrcs(root, "tests/other/vitest.config.ts"); got != nil {
		t.Errorf("an unregistered config has srcs %v", got)
	}
}

// The listing prints no line for a specifier a missing install left unresolved,
// so a lockfile without its node_modules refuses; no lockfile has no install.
func TestProgram_InstallMissing(t *testing.T) {
	root := t.TempDir()
	if got := installMissing(root); got != "" {
		t.Errorf("no lockfile: %q, want nothing to say", got)
	}
	writeFile(t, filepath.Join(root, pnpmLockfileName), "lockfileVersion: '9.0'\n")
	got := installMissing(root)
	for _, want := range []string{"node_modules/.modules.yaml", "pnpm install"} {
		if !strings.Contains(got, want) {
			t.Errorf("installMissing = %q, want %q in it", got, want)
		}
	}
	writeFile(t, filepath.Join(root, "node_modules/.modules.yaml"),
		"layoutVersion: 5\n")
	if got := installMissing(root); got != "" {
		t.Errorf("installed: %q, want nothing to say", got)
	}
}
