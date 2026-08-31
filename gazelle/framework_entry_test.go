package typescript

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/bazelbuild/bazel-gazelle/config"
	"github.com/bazelbuild/bazel-gazelle/language"
	"github.com/bazelbuild/bazel-gazelle/rule"
)

// runGeneratePackage runs the generator over one sub-package of a workspace
// whose root holds rootFiles (the package.json framework detection reads), with
// build as that package's existing BUILD file.
func runGeneratePackage(
	t *testing.T,
	rel string,
	rootFiles, pkgFiles map[string]string,
	build string,
) language.GenerateResult {
	t.Helper()

	repoRoot := t.TempDir()
	for name, content := range rootFiles {
		if err := os.WriteFile(filepath.Join(repoRoot, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	dir := filepath.Join(repoRoot, rel)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var names []string
	for name, content := range pkgFiles {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	sort.Strings(names)

	var f *rule.File
	if build != "" {
		parsed, err := rule.LoadData(filepath.Join(dir, "BUILD.bazel"), rel, []byte(build))
		if err != nil {
			t.Fatal(err)
		}
		f = parsed
	}

	c := &config.Config{RepoRoot: repoRoot, Exts: make(map[string]interface{})}
	configureTsConfig(c, "", nil)
	configureTsConfig(c, rel, f)

	return generateRules(language.GenerateArgs{
		Config:       c,
		Dir:          dir,
		Rel:          rel,
		File:         f,
		RegularFiles: names,
	})
}

// runGenerateRoot runs the generator at the workspace root -- where the bundle
// targets and the entry_point diagnostic are decided -- over a workspace whose
// entry package holds pkgFiles and, when build is non-empty, that BUILD file.
func runGenerateRoot(
	t *testing.T,
	rootFiles map[string]string,
	pkgRel string,
	pkgFiles map[string]string,
	build string,
) language.GenerateResult {
	t.Helper()

	repoRoot := t.TempDir()
	var names []string
	for name, content := range rootFiles {
		if err := os.WriteFile(filepath.Join(repoRoot, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	sort.Strings(names)

	pkgDir := filepath.Join(repoRoot, pkgRel)
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range pkgFiles {
		if err := os.WriteFile(filepath.Join(pkgDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if build != "" {
		if err := os.WriteFile(filepath.Join(pkgDir, "BUILD.bazel"), []byte(build), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	c := &config.Config{
		RepoRoot:            repoRoot,
		Exts:                make(map[string]interface{}),
		ValidBuildFileNames: []string{"BUILD.bazel", "BUILD"},
	}
	configureTsConfig(c, "", nil)

	return generateRules(language.GenerateArgs{
		Config:       c,
		Dir:          repoRoot,
		Rel:          "",
		RegularFiles: names,
	})
}

const (
	remixEntryClient = "import { RemixBrowser } from \"@remix-run/react\";\n" +
		"import { hydrateRoot } from \"react-dom/client\";\n" +
		"hydrateRoot(document, <RemixBrowser />);\n"
	remixRoot = "import { Outlet } from \"@remix-run/react\";\n" +
		"export default function App() { return <Outlet />; }\n"
)

func ruleNamed(res language.GenerateResult, name string) *rule.Rule {
	for _, r := range res.Gen {
		if r.Name() == name {
			return r
		}
	}
	return nil
}

func mustRuleNamed(t *testing.T, res language.GenerateResult, name string) *rule.Rule {
	t.Helper()
	r := ruleNamed(res, name)
	if r == nil {
		t.Fatalf("no target named %q generated; generated %v", name, generatedNames(t, res))
	}
	return r
}

// TestFrameworkEntry_GeneratedAlongsideThePackageTarget: the entry_point the
// root ts_bundle names is generated, and the file it compiles is gone from the
// directory-wide target -- two targets over one source declare the same .js.
func TestFrameworkEntry_GeneratedAlongsideThePackageTarget(t *testing.T) {
	res := runGeneratePackage(t, "app",
		map[string]string{"package.json": remixPackageJSON},
		map[string]string{
			"entry.client.tsx": remixEntryClient,
			"root.tsx":         remixRoot,
		}, "")

	byName := generatedNames(t, res)
	assertRule(t, byName, "entry_client", "ts_compile")
	assertRule(t, byName, "app", "ts_compile")

	entry := mustRuleNamed(t, res, "entry_client")
	if got := entry.AttrStrings("srcs"); len(got) != 1 || got[0] != "entry.client.tsx" {
		t.Errorf("entry_client srcs = %v, want [entry.client.tsx]", got)
	}
	if got := entry.AttrStrings("visibility"); len(got) != 1 || got[0] != "//visibility:public" {
		t.Errorf("entry_client visibility = %v, want [//visibility:public]", got)
	}
	if got := mustRuleNamed(t, res, "app").AttrStrings("srcs"); len(got) != 1 || got[0] != "root.tsx" {
		t.Errorf("app srcs = %v, want [root.tsx] — the entry is compiled twice", got)
	}
	// Only a second compile action is the problem. The framework reads its client
	// entry from the staged tree by name, so the filegroup keeps it -- which is
	// what examples/remix-app's hand-written `sources` filegroup does.
	if got := mustRuleNamed(t, res, "sources").AttrStrings("srcs"); !slices.Equal(
		got, []string{"entry.client.tsx", "root.tsx"}) {
		t.Errorf("sources srcs = %v, want [entry.client.tsx root.tsx]", got)
	}
}

// An ambient .d.ts reaches a program only through srcs, and the entry is the
// one target in the package that used to be given none: the globals declared
// beside it were unknown inside it, which is a TS2304 no dep can repair.
func TestFrameworkEntry_CarriesThePackagesAmbientDeclarations(t *testing.T) {
	res := runGeneratePackage(t, "app",
		map[string]string{"package.json": remixPackageJSON},
		map[string]string{
			"entry.client.tsx": remixEntryClient,
			"root.tsx":         remixRoot,
			"globals.d.ts":     "declare const __BUILD_ID__: string;\n",
		}, "")

	want := []string{"entry.client.tsx", "globals.d.ts"}
	if got := mustRuleNamed(t, res, "entry_client").AttrStrings("srcs"); !slices.Equal(got, want) {
		t.Errorf("entry_client srcs = %v, want %v", got, want)
	}
	if got := mustRuleNamed(t, res, "app").AttrStrings("srcs"); !slices.Equal(
		got, []string{"globals.d.ts", "root.tsx"}) {
		t.Errorf("app srcs = %v, want [globals.d.ts root.tsx]", got)
	}
}

// TestFrameworkEntry_MatchesTheGeneratedBundleLabel: the entry target and the
// entry_point attr are two spellings of one thing, so a run that writes both
// must have them agree.
func TestFrameworkEntry_MatchesTheGeneratedBundleLabel(t *testing.T) {
	root := runGenerate(t, "", map[string]string{
		"package.json":         remixPackageJSON,
		"app/entry.client.tsx": remixEntryClient,
	})
	var want string
	for _, r := range root.Gen {
		if r.Kind() == "ts_bundle" {
			want = r.AttrString("entry_point")
		}
	}
	if want == "" {
		t.Fatal("no ts_bundle generated for Remix")
	}

	res := runGeneratePackage(t, "app",
		map[string]string{"package.json": remixPackageJSON},
		map[string]string{"entry.client.tsx": remixEntryClient}, "")

	pkg, name, ok := splitPackageLabel(want)
	if !ok {
		t.Fatalf("entry_point %q is not a //pkg:name label", want)
	}
	if pkg != "app" {
		t.Errorf("entry_point package = %q, want \"app\"", pkg)
	}
	if ruleNamed(res, name) == nil {
		t.Errorf("no target named %q generated in app/; entry_point %q dangles", name, want)
	}
}

// TestFrameworkEntry_ReEmittedOverAnExistingTarget: the entry target is emitted
// again on every run, whatever is already in the BUILD file, so its srcs and its
// deps follow the file. Skipping it left a single-file target frozen at the first
// run: an import added to the entry never reached deps, and renaming the entry
// left the rule naming a source that was gone.
//
// The two cases are the two shapes an existing rule can have: srcs Gazelle can
// read, and srcs it cannot (a glob), which the merger leaves alone.
func TestFrameworkEntry_ReEmittedOverAnExistingTarget(t *testing.T) {
	for _, tt := range []struct{ name, srcs string }{
		{"srcs Gazelle reads", `["entry.client.tsx"]`},
		{"srcs Gazelle cannot read", `glob(["entry.client.tsx"])`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			build := `ts_compile(
    name = "entry_client",
    srcs = ` + tt.srcs + `,
    visibility = ["//visibility:public"],
    deps = ["@npm//:react-dom"],
)
`
			res := runGeneratePackage(t, "app",
				map[string]string{"package.json": remixPackageJSON},
				map[string]string{
					"entry.client.tsx": remixEntryClient,
					"root.tsx":         remixRoot,
				}, build)

			byName := generatedNames(t, res)
			assertRule(t, byName, "entry_client", "ts_compile")
			if got := mustRuleNamed(t, res, "entry_client").AttrStrings("srcs"); !slices.Equal(
				got, []string{"entry.client.tsx"}) {
				t.Errorf("entry_client srcs = %v, want [entry.client.tsx]", got)
			}
			if got := mustRuleNamed(t, res, "app").AttrStrings("srcs"); len(got) != 1 || got[0] != "root.tsx" {
				t.Errorf("app srcs = %v, want [root.tsx]", got)
			}
			if got := mustRuleNamed(t, res, "sources").AttrStrings("srcs"); !slices.Equal(
				got, []string{"entry.client.tsx", "root.tsx"}) {
				t.Errorf("sources srcs = %v, want [entry.client.tsx root.tsx] — "+
					"the staged tree loses the entry on every run after the first", got)
			}
		})
	}
}

// TestFrameworkEntry_StaleTargetIsDeletedWhenTheEntryIsRenamed: the rule the
// previous run wrote names a source that is gone, which fails
// `bazel build //...` outright, and no later run cleared it.
func TestFrameworkEntry_StaleTargetIsDeletedWhenTheEntryIsRenamed(t *testing.T) {
	build := `ts_compile(
    name = "entry_client",
    srcs = ["entry.client.tsx"],
    visibility = ["//visibility:public"],
)
`
	res := runGeneratePackage(t, "app",
		map[string]string{"package.json": remixPackageJSON},
		map[string]string{
			"bootstrap.tsx": remixEntryClient,
			"root.tsx":      remixRoot,
		}, build)

	if kind, ok := emptyNames(res)["entry_client"]; !ok || kind != "ts_compile" {
		t.Errorf("no deletion stub for the stale entry target; empty = %v", emptyNames(res))
	}
}

// The dev-server decision reads the package as it is on disk too. The entry is
// the file that makes a directory an app, and it has left the directory target
// for one of its own -- so a decision taken over the remaining sources sees no
// app here and skips the framework's own reason for having no dev server.
func TestFrameworkEntry_TheClaimedEntryStillDecidesTheDevServer(t *testing.T) {
	var res language.GenerateResult
	logged := captureLog(t, func() {
		res = runGeneratePackage(t, "src/app",
			map[string]string{"package.json": tanstackPackageJSON},
			map[string]string{
				"main.tsx": "export {};\n",
				"boot.ts":  "export const boot = 1;\n",
			}, "")
	})

	if r := ruleNamed(res, "dev"); r != nil {
		t.Errorf("generated %s(dev) for TanStack Start, whose dev server cannot serve the app",
			r.Kind())
	}
	if !strings.Contains(logged, "no ts_dev_server generated") {
		t.Errorf("nothing said why src/app has no dev server, so the app package with the "+
			"framework entry in it looks like a package that is not an app:\n%s", logged)
	}
}

// TestFrameworkEntry_PackageAsOnDiskReachesTheLinterAndTheTest: splitting the
// entry out of the directory target changes what compiles here, not what exists
// here. The linter and the sibling ts_test read the package as it is on disk, so
// an entry Gazelle's own target no longer carries still gets linted and its npm
// imports still reach the test tree.
//
// The two cases are the two ways the entry leaves the directory target: Gazelle
// gave it a target of its own, or another target in the file already claims it.
func TestFrameworkEntry_PackageAsOnDiskReachesTheLinterAndTheTest(t *testing.T) {
	// A name of the user's own, not "entry_client": a rule under the name
	// Gazelle reserves for the entry is read as Gazelle's own, not as a claim.
	claimedByHand := `ts_compile(
    name = "boot",
    srcs = ["entry.client.tsx"],
    visibility = ["//visibility:public"],
)
`
	for _, tt := range []struct {
		name  string
		build string
	}{
		{"gazelle_owns_the_entry", ""},
		{"another_target_claims_it", claimedByHand},
	} {
		t.Run(tt.name, func(t *testing.T) {
			res := runGeneratePackage(t, "app",
				map[string]string{
					"package.json":   remixPackageJSON,
					".eslintrc.json": `{"root":true}` + "\n",
				},
				map[string]string{
					"entry.client.tsx": "import \"zod\";\nexport {};\n",
					"root.tsx":         remixRoot,
					"root.test.ts":     "export const t = 1;\n",
				}, tt.build)

			lint := mustRuleNamed(t, res, "app_lint")
			if !slices.Contains(lint.AttrStrings("srcs"), "entry.client.tsx") {
				t.Errorf("ts_lint srcs %v does not carry the entry, so the file the bundle "+
					"boots from is the one file in the package nothing lints",
					lint.AttrStrings("srcs"))
			}

			test := mustRuleNamed(t, res, "app_test")
			imports, ok := importsOf(res, test)
			if !ok {
				t.Fatal("no import list for the generated ts_test")
			}
			if !slices.Contains(imports, "zod") {
				t.Errorf("the ts_test import list %v misses the entry's npm imports, so the "+
					"node_modules tree ts_test builds for the package under test is missing "+
					"what the entry needs at runtime", imports)
			}
		})
	}
}

// importsOf returns the import list generateRules paired with r.
func importsOf(res language.GenerateResult, r *rule.Rule) ([]string, bool) {
	for i, gen := range res.Gen {
		if gen != r || i >= len(res.Imports) {
			continue
		}
		imports, ok := res.Imports[i].([]string)
		return imports, ok
	}
	return nil, false
}

// TestFrameworkEntry_ExcludedEntryLosesItsMaintenanceOutLoud: a ts_exclude on
// the entry file drops it before the generator sees it, so the entry target is
// whatever the user wrote and Gazelle maintains none of it -- an import added to
// the entry never reaches its deps, and ts_compile's strict-deps check fails on
// it. That is the pre-0.2 recipe, and the run has to say it costs this.
func TestFrameworkEntry_ExcludedEntryLosesItsMaintenanceOutLoud(t *testing.T) {
	build := `# gazelle:ts_exclude entry.client.tsx
ts_compile(
    name = "entry_client",
    srcs = ["entry.client.tsx"],
    visibility = ["//visibility:public"],
)
`
	logged := captureLog(t, func() {
		runGeneratePackage(t, "app",
			map[string]string{"package.json": remixPackageJSON},
			map[string]string{
				"entry.client.tsx": "import \"zod\";\nexport {};\n",
				"root.tsx":         remixRoot,
			}, build)
	})
	for _, want := range []string{"ts_exclude", "app/entry.client.tsx", "strict-deps"} {
		if !strings.Contains(logged, want) {
			t.Errorf("the log does not mention %q, so the entry stops being maintained in "+
				"silence:\n%s", want, logged)
		}
	}

	quiet := captureLog(t, func() {
		runGeneratePackage(t, "app",
			map[string]string{"package.json": remixPackageJSON},
			map[string]string{
				"entry.client.tsx": remixEntryClient,
				"root.tsx":         remixRoot,
			}, "")
	})
	if strings.Contains(quiet, "ts_exclude") {
		t.Errorf("reported an exclusion for a package that has none; log was %q", quiet)
	}
}

// TestFrameworkEntry_ExcludedEntryKeepsItsHandWrittenTarget: ts_exclude drops
// the file from every generated target, which is the recipe the examples follow.
// The file is still on disk, so the target over it is a decision rather than the
// leftover the case above deletes.
func TestFrameworkEntry_ExcludedEntryKeepsItsHandWrittenTarget(t *testing.T) {
	build := `# gazelle:ts_exclude entry.client.tsx
ts_compile(
    name = "entry_client",
    srcs = ["entry.client.tsx"],
    visibility = ["//visibility:public"],
)
`
	res := runGeneratePackage(t, "app",
		map[string]string{"package.json": remixPackageJSON},
		map[string]string{
			"entry.client.tsx": remixEntryClient,
			"root.tsx":         remixRoot,
		}, build)

	if kind, ok := emptyNames(res)["entry_client"]; ok {
		t.Errorf("emitted a deletion stub %s(name = \"entry_client\") for an excluded entry that is still on disk", kind)
	}
}

// TestFrameworkEntry_BuiltinExcludeAndTsIgnoreLeaveNoDanglingLabel: Gazelle's
// own exclude drops the file before the generator sees it, and ts_ignore stops
// generation in the package altogether. Either way nothing carries the entry, so
// the bundle -- and the entry_point and //app:sources labels it would name --
// must not be written, and the reason has to reach the log.
func TestFrameworkEntry_BuiltinExcludeAndTsIgnoreLeaveNoDanglingLabel(t *testing.T) {
	for _, tt := range []struct{ name, directive string }{
		{"gazelle exclude", "# gazelle:exclude entry.client.tsx\n"},
		{"ts_ignore", "# gazelle:ts_ignore\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repoRoot := t.TempDir()
			for name, body := range map[string]string{
				"package.json":         remixPackageJSON,
				"app/entry.client.tsx": remixEntryClient,
				"app/root.tsx":         remixRoot,
				"app/BUILD.bazel":      tt.directive,
			} {
				path := filepath.Join(repoRoot, name)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			c := &config.Config{RepoRoot: repoRoot, Exts: make(map[string]interface{})}
			configureTsConfig(c, "", nil)

			var res language.GenerateResult
			logged := captureLog(t, func() {
				res = generateRules(language.GenerateArgs{
					Config: c, Dir: repoRoot, Rel: "",
					RegularFiles: []string{"package.json"},
				})
			})

			for _, r := range res.Gen {
				if r.Kind() == "ts_bundle" {
					t.Errorf("generated ts_bundle with entry_point %q, which nothing declares",
						r.AttrString("entry_point"))
				}
			}
			if !strings.Contains(logged, "client entry target") {
				t.Errorf("nothing said why the bundle was skipped:\n%s", logged)
			}
		})
	}
}

// TestFrameworkEntry_NameCollisionIsRefused: with the directory target renamed
// onto the entry's name, generating both wrote two rules of one name into one
// package, which Bazel rejects outright and which no later run repaired. The
// entry stays in the package target instead, so entry_point names a target with
// more than one .js -- which ts_bundle refuses by name.
func TestFrameworkEntry_NameCollisionIsRefused(t *testing.T) {
	var res language.GenerateResult
	logged := captureLog(t, func() {
		res = runGeneratePackage(t, "src/app",
			map[string]string{"package.json": tanstackPackageJSON},
			map[string]string{"main.tsx": "export {};\n", "util.ts": "export const u = 1;\n"},
			"# gazelle:ts_target_name main\n")
	})

	named := 0
	for _, r := range res.Gen {
		if r.Kind() == "ts_compile" && r.Name() == "main" {
			named++
		}
	}
	if named != 1 {
		t.Errorf("generated %d ts_compile rules named \"main\" in one package; Bazel rejects the file", named)
	}
	if !strings.Contains(logged, "two rules of one name") {
		t.Errorf("the collision was not named:\n%s", logged)
	}
}

// TestFrameworkEntry_OnlyInTheEntryPackage: the entry_point label names one
// package, so the same filename anywhere else is an ordinary source. Remix's
// own conventions put an entry.client.tsx nowhere but app/, and a route file
// that happened to be named one would otherwise lose its compilation.
func TestFrameworkEntry_OnlyInTheEntryPackage(t *testing.T) {
	res := runGeneratePackage(t, "app/routes",
		map[string]string{"package.json": remixPackageJSON},
		map[string]string{"entry.client.tsx": remixEntryClient}, "")

	byName := generatedNames(t, res)
	assertRule(t, byName, "routes", "ts_compile")
	if _, ok := byName["entry_client"]; ok {
		t.Errorf("generated the entry target outside the entry_point label's package: %v", byName)
	}
	if got := mustRuleNamed(t, res, "routes").AttrStrings("srcs"); !slices.Equal(
		got, []string{"entry.client.tsx"}) {
		t.Errorf("routes srcs = %v, want [entry.client.tsx] — the file is compiled by nothing", got)
	}
}

// TestFrameworkEntry_EntryOnlyPackageStillStages: staging_srcs names the entry
// package's `sources` filegroup whether or not the package holds anything
// besides the entry, so the filegroup has to exist in both cases -- a dangling
// label there fails `bazel build //...` for the whole workspace.
func TestFrameworkEntry_EntryOnlyPackageStillStages(t *testing.T) {
	res := runGeneratePackage(t, "app",
		map[string]string{"package.json": remixPackageJSON},
		map[string]string{"entry.client.tsx": remixEntryClient}, "")

	assertRule(t, generatedNames(t, res), "entry_client", "ts_compile")
	if got := mustRuleNamed(t, res, "sources").AttrStrings("srcs"); !slices.Equal(
		got, []string{"entry.client.tsx"}) {
		t.Errorf("sources srcs = %v, want [entry.client.tsx]", got)
	}
}

// TestFrameworkEntry_ExcludedEntryIsReported: `# gazelle:ts_exclude` on the
// entry file -- the line the old hand-written recipe asked for -- drops it from
// srcs, so no entry target is generated over it. The generated entry_point then
// names nothing, and the run has to say so.
func TestFrameworkEntry_ExcludedEntryIsReported(t *testing.T) {
	for _, tt := range []struct {
		name     string
		build    string
		wantWarn bool
	}{
		{"exclusion with no target of its own", "# gazelle:ts_exclude entry.client.tsx\n", true},
		{"exclusion with the hand-written target", `# gazelle:ts_exclude entry.client.tsx
ts_compile(
    name = "entry_client",
    srcs = ["entry.client.tsx"],
    visibility = ["//visibility:public"],
)
`, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			logged := captureLog(t, func() {
				runGenerateRoot(t,
					map[string]string{"package.json": remixPackageJSON},
					"app", map[string]string{
						"entry.client.tsx": remixEntryClient,
						"root.tsx":         remixRoot,
					}, tt.build)
			})
			warned := strings.Contains(logged, "//app:entry_client")
			if warned != tt.wantWarn {
				t.Errorf("warned about the entry_point = %v, want %v; log was %q",
					warned, tt.wantWarn, logged)
			}
			// entry_point is an attribute Gazelle owns, so "declare it by hand"
			// without the "# keep" half is advice the next run undoes.
			if warned && !strings.Contains(logged, "# keep") {
				t.Errorf("the refusal tells the user to declare the bundle by hand without "+
					"naming \"# keep\"; log was %q", logged)
			}
		})
	}
}

// TestFrameworkEntry_ImportCycleIsReported: the split turns one directory into
// two targets, so a sibling importing the entry while the entry imports the
// package back is a dependency cycle Bazel rejects -- with nothing in either
// generated rule to say where it came from.
func TestFrameworkEntry_ImportCycleIsReported(t *testing.T) {
	pkg := map[string]string{
		"entry.client.tsx": "import App from \"./root\";\nexport default App;\n",
		"root.tsx":         remixRoot,
		"helper.tsx":       "import \"./entry.client\";\nexport const x = 1;\n",
	}
	logged := captureLog(t, func() {
		runGeneratePackage(t, "app",
			map[string]string{"package.json": remixPackageJSON}, pkg, "")
	})
	for _, want := range []string{"dependency cycle", "entry.client.tsx", "./entry.client", "./root"} {
		if !strings.Contains(logged, want) {
			t.Errorf("log does not name %q; log was %q", want, logged)
		}
	}

	quiet := captureLog(t, func() {
		runGeneratePackage(t, "app",
			map[string]string{"package.json": remixPackageJSON},
			map[string]string{"entry.client.tsx": remixEntryClient, "root.tsx": remixRoot}, "")
	})
	if strings.Contains(quiet, "dependency cycle") {
		t.Errorf("reported a cycle for a package that has none; log was %q", quiet)
	}
}

// TestFrameworkEntry_NoEntryFileIsNamed: the generated entry_point label is
// only as good as the file behind it, and a dangling label fails
// `bazel build //...` for the whole workspace rather than for the bundle.
func TestFrameworkEntry_NoEntryFileIsNamed(t *testing.T) {
	logged := captureLog(t, func() {
		runGenerate(t, "", map[string]string{"package.json": remixPackageJSON})
	})

	for _, want := range []string{"app/", "entry_client", "//app:entry_client"} {
		if !strings.Contains(logged, want) {
			t.Errorf("log does not name %q; log was %q", want, logged)
		}
	}
}

func TestEntryTargetName(t *testing.T) {
	for file, want := range map[string]string{
		"entry.client.tsx": "entry_client",
		"main.tsx":         "main",
		"main.ts":          "main",
		"entry.server.ts":  "entry_server",
	} {
		if got := entryTargetName(file); got != want {
			t.Errorf("entryTargetName(%q) = %q, want %q", file, got, want)
		}
	}
}
