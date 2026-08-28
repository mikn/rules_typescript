package typescript

import (
	"bytes"
	"log"
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

// runGenerateRootWithBuild runs the generator over the workspace root with an
// existing BUILD file, the way Gazelle does on a second run.
func runGenerateRootWithBuild(t *testing.T, files map[string]string, build string) language.GenerateResult {
	t.Helper()

	repoRoot := t.TempDir()
	var names []string
	for name, content := range files {
		path := filepath.Join(repoRoot, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(name, "/") {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	var f *rule.File
	if build != "" {
		parsed, err := rule.LoadData(filepath.Join(repoRoot, "BUILD.bazel"), "", []byte(build))
		if err != nil {
			t.Fatal(err)
		}
		f = parsed
	}

	c := &config.Config{RepoRoot: repoRoot, Exts: make(map[string]interface{})}
	configureTsConfig(c, "", nil)

	return generateRules(language.GenerateArgs{
		Config:       c,
		Dir:          repoRoot,
		Rel:          "",
		File:         f,
		RegularFiles: names,
	})
}

// withFrameworkEntry adds the client entry both frameworks' entry_point names,
// since a bundle whose entry_point would resolve to nothing is not generated.
func withFrameworkEntry(packageJSON string) map[string]string {
	return map[string]string{
		"package.json":         packageJSON,
		"app/entry.client.tsx": "",
		"src/app/main.tsx":     "",
	}
}

func emptyNames(res language.GenerateResult) map[string]string {
	byName := make(map[string]string, len(res.Empty))
	for _, r := range res.Empty {
		byName[r.Name()] = r.Kind()
	}
	return byName
}

const (
	tanstackPackageJSON   = `{"dependencies":{"@tanstack/react-router":"1.0.0","react":"19.0.0"}}`
	nextPackageJSON       = `{"dependencies":{"next":"15.0.0","react":"19.0.0"}}`
	remixPackageJSON      = `{"dependencies":{"@remix-run/react":"2.0.0"}}`
	svelteKitPackageJSON  = `{"devDependencies":{"@sveltejs/kit":"2.0.0"}}`
	solidStartPackageJSON = `{"dependencies":{"@solidjs/start":"1.0.0"}}`
)

// TestFrameworkBundle_TargetNames pins the emitted names. The node_modules
// target must be named "node_modules" for every framework: Node resolves a
// module's realpath before its bare imports, so a tree named anything else
// leaves vite's own dependencies (rolldown, …) unresolvable from inside it.
func TestFrameworkBundle_TargetNames(t *testing.T) {
	for _, tt := range []struct {
		name        string
		packageJSON string
		bundleKind  string
		bundleName  string
	}{
		{"tanstack", tanstackPackageJSON, "ts_bundle", "app"},
		{"remix", remixPackageJSON, "ts_bundle", "app_remix"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			res := runGenerate(t, "", withFrameworkEntry(tt.packageJSON))
			byName := generatedNames(t, res)

			assertRule(t, byName, "node_modules", "node_modules")
			assertRule(t, byName, tt.bundleName, tt.bundleKind)
			for name := range byName {
				if name == "app_node_modules" || name == "app_vite" {
					t.Errorf("emitted legacy target %q; generated %v", name, byName)
				}
			}
			if tt.bundleKind == "ts_bundle" {
				assertRule(t, byName, "vite", "vite_bundler")
			}
		})
	}
}

// TestFrameworkBundle_WiresBundlerToNodeModules checks the attrs that carry the
// names, not just the target names themselves.
func TestFrameworkBundle_WiresBundlerToNodeModules(t *testing.T) {
	res := runGenerate(t, "", withFrameworkEntry(tanstackPackageJSON))

	for _, r := range res.Gen {
		switch r.Kind() {
		case "vite_bundler":
			if got := r.AttrString("node_modules"); got != ":node_modules" {
				t.Errorf("vite_bundler node_modules = %q, want \":node_modules\"", got)
			}
		case "ts_bundle":
			if got := r.AttrString("bundler"); got != ":vite" {
				t.Errorf("ts_bundle bundler = %q, want \":vite\"", got)
			}
		}
	}
}

// TestFrameworkBundle_SecondRunStillEmitsEveryTarget covers the second Gazelle
// run over a BUILD file that already carries the correctly-named targets. The
// candidate is emitted again on purpose: the merger only reconciles an attribute
// a candidate carries, so skipping the existing rule is what froze staging_srcs
// and the npm tree at whatever the first run wrote.
func TestFrameworkBundle_SecondRunStillEmitsEveryTarget(t *testing.T) {
	build := `node_modules(
    name = "node_modules",
    deps = ["@npm//:vite"],
)

vite_bundler(
    name = "vite",
    node_modules = ":node_modules",
    vite = "@npm//:vite",
)

ts_bundle(
    name = "app",
    bundler = ":vite",
    entry_point = "//src/app:main",
)
`
	res := runGenerateRootWithBuild(t, withFrameworkEntry(tanstackPackageJSON), build)

	byName := generatedNames(t, res)
	assertRule(t, byName, "node_modules", "node_modules")
	assertRule(t, byName, "vite", "vite_bundler")
	assertRule(t, byName, "app", "ts_bundle")
	if kind, ok := emptyNames(res)["node_modules"]; ok {
		t.Errorf("emitted deletion stub %s(name = \"node_modules\")", kind)
	}
}

// TestFrameworkBundle_MergeableAttrsAreOnesTheGeneratorWrites is the pairing
// that makes emitting on every run safe. MergeRules deletes a mergeable attr the
// candidate does not carry, so a rule kind listing one Gazelle never writes
// silently strips it -- a hand-set minify, tags, port or env.
func TestFrameworkBundle_MergeableAttrsAreOnesTheGeneratorWrites(t *testing.T) {
	kinds := (&tsLang{}).Kinds()
	for _, tt := range []struct {
		kind  string
		files map[string]string
	}{
		{"ts_bundle", withFrameworkEntry(tanstackPackageJSON)},
		{"vite_bundler", withFrameworkEntry(tanstackPackageJSON)},
		{"next_build", map[string]string{
			"package.json": nextPackageJSON, "tsconfig.json": "{}",
			"next.config.mjs": "", "app/page.tsx": "", "lib/x.ts": "",
		}},
		{"next_dev_server", map[string]string{
			"package.json": nextPackageJSON, "app/page.tsx": "",
		}},
		{"sveltekit_build", map[string]string{
			"package.json": svelteKitPackageJSON, "svelte.config.js": "",
			"vite.config.mjs": "", "src/app.html": "", "lib/x.ts": "",
		}},
	} {
		t.Run(tt.kind, func(t *testing.T) {
			res := runGenerate(t, "", tt.files)
			r := ruleOfKind(res, tt.kind)
			if r == nil {
				t.Fatalf("no %s generated for the fixture", tt.kind)
			}
			written := make(map[string]bool, len(r.AttrKeys()))
			for _, key := range r.AttrKeys() {
				written[key] = true
			}
			for attr := range kinds[tt.kind].MergeableAttrs {
				if !written[attr] {
					t.Errorf("%s declares %q mergeable but never writes it, so the merger "+
						"deletes it from whatever the user set; attrs written = %v",
						tt.kind, attr, r.AttrKeys())
				}
			}
		})
	}
}

func ruleOfKind(res language.GenerateResult, kind string) *rule.Rule {
	for _, r := range res.Gen {
		if r.Kind() == kind {
			return r
		}
	}
	return nil
}

// A "# keep" on srcs is the only thing between the recomputed glob and a srcs
// the user maintains, because the glob is written straight onto the rule rather
// than through the merger, which is where keep is normally honoured.
func TestFrameworkBundle_KeepOnTheGlobIsHonoured(t *testing.T) {
	build := `next_build(
    name = "app",
    # keep
    srcs = glob(["app/**"]),
    node_modules = ":node_modules",
)
`
	_, f := runGenerateNextWithBuild(t, map[string]string{
		"package.json": nextPackageJSON,
		"app/":         "",
		"pages/":       "",
	}, build)

	if got := globPatterns(t, mustFileRuleNamed(t, f, "app")); !slices.Equal(got, []string{"app/**"}) {
		t.Errorf("srcs patterns = %v, want [app/**]; the # keep was ignored", got)
	}
}

// TestFrameworkBundle_DeletesOrphanedLegacyTargets covers migration: the
// pre-rename pair is left behind by an older Gazelle and nothing refers to it.
func TestFrameworkBundle_DeletesOrphanedLegacyTargets(t *testing.T) {
	build := `node_modules(
    name = "node_modules",
    deps = ["@npm//:vite"],
)

vite_bundler(
    name = "vite",
    node_modules = ":node_modules",
    vite = "@npm//:vite",
)

ts_bundle(
    name = "app",
    bundler = ":vite",
    entry_point = "//src/app:main",
)

node_modules(
    name = "app_node_modules",
    deps = ["@npm//:vite"],
)

vite_bundler(
    name = "app_vite",
    node_modules = ":app_node_modules",
    vite = "@npm//:vite",
)
`
	res := runGenerateRootWithBuild(t, map[string]string{"package.json": tanstackPackageJSON}, build)

	byName := emptyNames(res)
	if byName["app_node_modules"] != "node_modules" {
		t.Errorf("app_node_modules not deleted; deletions: %v", byName)
	}
	if byName["app_vite"] != "vite_bundler" {
		t.Errorf("app_vite not deleted; deletions: %v", byName)
	}
}

// TestFrameworkBundle_KeepsReferencedLegacyTargets: a legacy target another
// rule still points at is load-bearing, so deleting it would break the build.
func TestFrameworkBundle_KeepsReferencedLegacyTargets(t *testing.T) {
	build := `node_modules(
    name = "app_node_modules",
    deps = ["@npm//:vite"],
)

vite_bundler(
    name = "app_vite",
    node_modules = ":app_node_modules",
    vite = "@npm//:vite",
)

ts_bundle(
    name = "app",
    bundler = ":app_vite",
    entry_point = "//src/app:main",
)
`
	res := runGenerateRootWithBuild(t, map[string]string{"package.json": tanstackPackageJSON}, build)

	for name := range emptyNames(res) {
		if name == "app_vite" || name == "app_node_modules" {
			t.Errorf("deleted %q while ts_bundle still references :app_vite", name)
		}
	}
}

// TestFrameworkBundle_RootTestsKeepNodeModules: the stale-node_modules cleanup
// for ts_test must not delete the framework tree at the workspace root.
func TestFrameworkBundle_RootTestsKeepNodeModules(t *testing.T) {
	files := map[string]string{
		"package.json": tanstackPackageJSON,
		"root.ts":      "export const a = 1;\n",
		"root.test.ts": "export const t = 1;\n",
	}
	build := `node_modules(
    name = "node_modules",
    deps = ["@npm//:vite"],
)
`
	res := runGenerateRootWithBuild(t, files, build)

	if kind, ok := emptyNames(res)["node_modules"]; ok {
		t.Errorf("root ts_test deleted the framework tree: %s(name = \"node_modules\")", kind)
	}
}

// captureLog collects what the generator writes to the standard logger, which
// is where Gazelle's own diagnostics go.
func captureLog(t *testing.T, body func()) string {
	t.Helper()
	var buf bytes.Buffer
	flags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(os.Stderr)
		log.SetFlags(flags)
	})
	body()
	return buf.String()
}

// TestFrameworkBundle_StagingRefusalIsSaidOnce: Remix stages app/routes and
// app, so every directory under the route tree is reached from both bases. A
// refusal printed per base is the same sentence twice for one directory.
func TestFrameworkBundle_StagingRefusalIsSaidOnce(t *testing.T) {
	repoRoot := t.TempDir()
	writeWorkspace(t, repoRoot, map[string]string{
		"package.json":                 remixPackageJSON,
		"app/root.tsx":                 "export default function R() { return null; }\n",
		"app/routes/panel/route.tsx":   "export default function P() { return null; }\n",
		"app/routes/panel/BUILD.bazel": "# gazelle:ts_ignore\n",
	})

	tc := &tsConfig{detectedFramework: FrameworkRemix, packageBoundaryMode: boundaryEveryDir}
	logged := captureLog(t, func() {
		stagedDirsUnder(repoRoot, frameworkConfigs[FrameworkRemix].StageDirs, tc)
	})

	if got := strings.Count(logged, "app/routes/panel holds staged sources"); got != 1 {
		t.Errorf("the ts_ignore refusal for app/routes/panel was printed %d times, want 1:\n%s",
			got, logged)
	}
	// The remedy it prints is one Gazelle undoes on the next run without the
	// "# keep" half, since staging_srcs is an attribute Gazelle owns.
	if !strings.Contains(logged, "# keep") {
		t.Errorf("the refusal tells the user to stage by hand without naming \"# keep\", so the "+
			"remedy is deleted by the next run:\n%s", logged)
	}
}

// TestFrameworkBundle_RoutePluginsAreWiredIntoGeneration: each framework plugin
// is reachable only through generateRules, and each plugin's own tests call it
// directly -- so unplugging the call left the whole suite green. A route
// annotation is the plugin's visible output, and it has to arrive on a rule
// generateRules returns.
func TestFrameworkBundle_RoutePluginsAreWiredIntoGeneration(t *testing.T) {
	for _, tt := range []struct {
		name    string
		rel     string
		pkgJSON string
		src     string
		want    string
	}{
		{
			name: "remix", rel: "app/routes", pkgJSON: remixPackageJSON,
			src: "_index.tsx", want: "# Remix route ",
		},
		{
			name: "tanstack", rel: "src/routes", pkgJSON: tanstackPackageJSON,
			src: "index.tsx", want: "# TanStack Start route: ",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			res := runGeneratePackage(t, tt.rel,
				map[string]string{"package.json": tt.pkgJSON},
				map[string]string{tt.src: "export default function R() { return null; }\n"}, "")

			for _, r := range res.Gen {
				if r.Kind() != "ts_compile" {
					continue
				}
				for _, comment := range r.Comments() {
					if strings.HasPrefix(comment, tt.want) {
						return
					}
				}
			}
			t.Errorf("no rule generateRules returned carries a %q annotation, so the %s plugin "+
				"is not reached: every route diagnostic and refusal it prints is unreachable too",
				tt.want, tt.name)
		})
	}
}

// TestFrameworkBundle_UnbuildableFrameworkGetsNoTargets: a framework whose
// bundle cannot build must yield no bundle targets at all. An emitted
// ts_bundle whose entry_point or vite_config nothing can satisfy fails
// `bazel build //...` for the whole workspace, not just for that target.
func TestFrameworkBundle_UnbuildableFrameworkGetsNoTargets(t *testing.T) {
	for _, tt := range []struct {
		name        string
		packageJSON string
		named       string
	}{
		{"solidstart", solidStartPackageJSON, "SolidStart"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var res language.GenerateResult
			logged := captureLog(t, func() {
				res = runGenerate(t, "", map[string]string{"package.json": tt.packageJSON})
			})

			for name, kind := range generatedNames(t, res) {
				switch kind {
				case "ts_bundle", "vite_bundler", "node_modules":
					t.Errorf("emitted %s(name = %q) for a framework that cannot be bundled", kind, name)
				}
			}
			if !strings.Contains(logged, tt.named) {
				t.Errorf("nothing logged names %s; log was %q", tt.named, logged)
			}
			if !strings.Contains(logged, "unsupported") {
				t.Errorf("log does not say bundling is unsupported; log was %q", logged)
			}
		})
	}
}

// TestFrameworkBundle_UnbuildableFrameworkStagesNothing: staging_srcs
// filegroups exist only to feed a ts_bundle, so a framework with no bundle
// must not have its route directories declared as stage dirs either.
func TestFrameworkBundle_UnbuildableFrameworkStagesNothing(t *testing.T) {
	for _, tt := range []struct {
		name      string
		framework Framework
	}{
		{"solidstart", FrameworkSolidStart},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tc := &tsConfig{detectedFramework: tt.framework}
			for _, dir := range []string{"src/routes", "src/lib", "src", "app", "app/routes"} {
				if isStagedDir(dir, tc) {
					t.Errorf("%q is a stage dir for a framework with no bundle target", dir)
				}
			}
		})
	}
}

// TestFrameworkBundle_EntryPointNamesASubpackageTarget: ts_bundle needs
// exactly one .js from entry_point, and Gazelle merges a directory's sources
// into one target -- so the entry is always a target the user declares in the
// package holding the framework's client entry file, never in the root, where
// a framework app has no TypeScript for Gazelle to make a target from.
func TestFrameworkBundle_EntryPointNamesASubpackageTarget(t *testing.T) {
	for framework, cfg := range frameworkConfigs {
		name := frameworkName(framework)
		if !strings.HasPrefix(cfg.EntryPoint, "//") {
			t.Errorf("%s entry_point = %q is relative to the root package, which holds no generated ts_compile target", name, cfg.EntryPoint)
			continue
		}
		if !strings.Contains(strings.TrimPrefix(cfg.EntryPoint, "//"), ":") {
			t.Errorf("%s entry_point = %q does not name a target", name, cfg.EntryPoint)
		}
	}
}

// TestFrameworkBundle_RemixStagesPackageJSON: the Remix Vite plugin resolves
// the project config relative to the staging root, and fails the build with
// ENOENT on <staging>/package.json when it is not staged.
func TestFrameworkBundle_RemixStagesPackageJSON(t *testing.T) {
	res := runGenerate(t, "", map[string]string{
		"package.json":         remixPackageJSON,
		"app/entry.client.tsx": "",
		"app/routes/_index.ts": "",
		"app/root.tsx":         "",
	})

	for _, r := range res.Gen {
		if r.Kind() != "ts_bundle" {
			continue
		}
		if got := r.AttrString("entry_point"); got != "//app:entry_client" {
			t.Errorf("remix entry_point = %q, want \"//app:entry_client\"", got)
		}
		staging := r.AttrStrings("staging_srcs")
		for _, want := range []string{"index.html", "package.json", "//app/routes:sources", "//app:sources"} {
			if !slices.Contains(staging, want) {
				t.Errorf("remix staging_srcs = %v, missing %q", staging, want)
			}
		}
		return
	}
	t.Fatal("no ts_bundle generated for Remix")
}

// TestFrameworkBundle_StagingNamesOnlyDirectoriesWithAFilegroup: the filegroup
// is emitted per directory holding a staged source, so a label for any other
// directory -- absent, or present but empty of TypeScript -- names a target no
// rule satisfies, and the whole workspace then fails to analyse.
func TestFrameworkBundle_StagingNamesOnlyDirectoriesWithAFilegroup(t *testing.T) {
	res := runGenerate(t, "", map[string]string{
		"package.json":              tanstackPackageJSON,
		"src/routes/index.tsx":      "",
		"src/routes/posts/list.tsx": "",
		"src/app/main.tsx":          "",
		"src/lib/":                  "",
	})

	for _, r := range res.Gen {
		if r.Kind() != "ts_bundle" {
			continue
		}
		staging := r.AttrStrings("staging_srcs")
		for _, want := range []string{"//src/routes:sources", "//src/routes/posts:sources", "//src/app:sources"} {
			if !slices.Contains(staging, want) {
				t.Errorf("staging_srcs = %v, missing %q", staging, want)
			}
		}
		for _, absent := range []string{"//src/lib:sources", "//src/components:sources"} {
			if slices.Contains(staging, absent) {
				t.Errorf("staging_srcs = %v, names %q, which no filegroup satisfies", staging, absent)
			}
		}
		return
	}
	t.Fatal("no ts_bundle generated for TanStack Start")
}

// TestFrameworkBundle_StagedDirGazelleWillNotWriteIsNamedNotLabelled: a
// ts_ignore directive stops the filegroup being written, so naming the directory
// anyway is a label nothing satisfies -- and a dangling label fails analysis for
// every target in the workspace, not just the bundle.
func TestFrameworkBundle_StagedDirGazelleWillNotWriteIsNamedNotLabelled(t *testing.T) {
	var res language.GenerateResult
	logged := captureLog(t, func() {
		res = runGenerate(t, "", map[string]string{
			"package.json":                    tanstackPackageJSON,
			"src/app/main.tsx":                "",
			"src/routes/index.tsx":            "",
			"src/routes/vendor/BUILD.bazel":   "# gazelle:ts_ignore\n",
			"src/routes/vendor/generated.tsx": "",
		})
	})

	for _, r := range res.Gen {
		if r.Kind() != "ts_bundle" {
			continue
		}
		if staging := r.AttrStrings("staging_srcs"); slices.Contains(staging, "//src/routes/vendor:sources") {
			t.Errorf("staging_srcs = %v names an ignored directory, which no filegroup satisfies", staging)
		}
	}
	if !strings.Contains(logged, "src/routes/vendor") {
		t.Errorf("the skipped directory was not named:\n%s", logged)
	}
}

// TestFrameworkBundle_HandPointedEntryPointSurvivesAMissingConventionalOne: the
// bundle is withdrawn when its entry_point names nothing, but only the label
// Gazelle wrote. A user who pointed entry_point at a target this generator does
// not read still owns their rule.
func TestFrameworkBundle_HandPointedEntryPointSurvivesAMissingConventionalOne(t *testing.T) {
	build := `ts_bundle(
    name = "app",
    bundler = ":vite",
    entry_point = "//elsewhere:boot",
    mode = "app",
)
`
	res := runGenerateRootWithBuild(t, map[string]string{
		"package.json":         tanstackPackageJSON,
		"src/routes/index.tsx": "",
	}, build)

	if kind, ok := emptyNames(res)["app"]; ok {
		t.Errorf("emitted a deletion stub %s(name = \"app\") over a hand-pointed entry_point", kind)
	}
}

// TestFrameworkBundle_WithdrawnBundleIsNamed: withdrawing the bundle is right --
// an entry_point naming nothing fails analysis for every target that reaches it
// -- but a rule the user maintains under the label the docs told them to use
// disappears with it, and a "# keep" above the rule is the only thing that holds
// one. Neither is guessable from a BUILD file that came back one rule shorter.
func TestFrameworkBundle_WithdrawnBundleIsNamed(t *testing.T) {
	build := `ts_bundle(
    name = "app",
    bundler = ":vite",
    entry_point = "//src/app:main",
    mode = "app",
)
`
	logged := captureLog(t, func() {
		runGenerateRootWithBuild(t, map[string]string{
			"package.json":         tanstackPackageJSON,
			"src/routes/index.tsx": "",
		}, build)
	})
	for _, want := range []string{"withdrawn", "//src/app:main", "# keep"} {
		if !strings.Contains(logged, want) {
			t.Errorf("the log does not mention %q, so a ts_bundle the user maintains is deleted "+
				"with nothing said about holding it:\n%s", want, logged)
		}
	}
}

// TestFrameworkBundle_NoDevServerForTanStack: main.tsx is the signal that makes
// Gazelle write a ts_dev_server, and a Start app has one -- but ts_dev_server
// cannot serve it, so the target would only look like it worked.
func TestFrameworkBundle_NoDevServerForTanStack(t *testing.T) {
	for _, tt := range []struct {
		name        string
		packageJSON string
		wantDev     bool
	}{
		{"tanstack", tanstackPackageJSON, false},
		{"plain react", `{"dependencies":{"react":"19.0.0"}}`, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repoRoot := t.TempDir()
			dir := filepath.Join(repoRoot, "src", "app")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(repoRoot, "package.json"), []byte(tt.packageJSON), 0o644); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"main.tsx", "index.ts"} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte("export {};"), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			c := &config.Config{RepoRoot: repoRoot, Exts: make(map[string]interface{})}
			configureTsConfig(c, "", nil)
			configureTsConfig(c, "src/app", nil)
			res := generateRules(language.GenerateArgs{
				Config:       c,
				Dir:          dir,
				Rel:          "src/app",
				RegularFiles: []string{"index.ts", "main.tsx"},
			})

			gotDev := false
			for _, r := range res.Gen {
				if r.Kind() == "ts_dev_server" {
					gotDev = true
				}
			}
			if gotDev != tt.wantDev {
				t.Errorf("ts_dev_server generated = %v, want %v", gotDev, tt.wantDev)
			}
		})
	}
}

// TestFrameworkBundle_SourcesFilegroupFollowsTheDirectory: the bundle reads a
// route from the staged tree, so a "sources" filegroup that stops tracking its
// directory is a route that type-checks and then is absent from the app.
func TestFrameworkBundle_SourcesFilegroupFollowsTheDirectory(t *testing.T) {
	repoRoot := t.TempDir()
	dir := filepath.Join(repoRoot, "src", "routes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "package.json"), []byte(tanstackPackageJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	names := []string{"__root.tsx", "about.tsx", "blog.$slug.tsx"}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("export {};"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// The filegroup an earlier run wrote, before blog.$slug.tsx existed.
	build := `filegroup(
    name = "sources",
    srcs = [
        "__root.tsx",
        "about.tsx",
    ],
    visibility = ["//visibility:public"],
)
`
	f, err := rule.LoadData(filepath.Join(dir, "BUILD.bazel"), "src/routes", []byte(build))
	if err != nil {
		t.Fatal(err)
	}

	c := &config.Config{RepoRoot: repoRoot, Exts: make(map[string]interface{})}
	configureTsConfig(c, "", nil)
	configureTsConfig(c, "src/routes", f)
	res := generateRules(language.GenerateArgs{
		Config:       c,
		Dir:          dir,
		Rel:          "src/routes",
		File:         f,
		RegularFiles: names,
	})

	for _, r := range res.Gen {
		if r.Kind() != "filegroup" || r.Name() != "sources" {
			continue
		}
		if got := r.AttrStrings("srcs"); !slices.Contains(got, "blog.$slug.tsx") {
			t.Errorf("sources srcs = %v, missing the route added after the filegroup was written", got)
		}
		return
	}
	t.Error("no \"sources\" filegroup re-emitted, so the staged tree keeps whatever the first run saw")
}
