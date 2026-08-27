package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mikn/rules_typescript/tests/integration/harness"
)

const (
	sourceViteConfig    = `vite_config = "remix-vite.config.mjs"`
	generatedViteConfig = `vite_config = ":remix_vite_config"`
	generatedEntryPoint = `entry_point = "//app:entry_client"`
	rootEntryPoint      = `entry_point = ":entry_client"`

	// Whole staging_srcs lines: "index.html" alone also matches the html attr.
	stagedPackageJSON = "        \"package.json\",\n"
	stagedRoutes      = "        \"//app/routes:sources\",\n"
	stagedApp         = "        \"//app:sources\",\n"
	stagedPanel       = "        \"//app/routes/panel:sources\",\n"

	panelTitleImport     = `import { panelTitle } from "./title";`
	panelTitleMarker     = "acme-panel-route-marker"
	panelColocatedMarker = "acme-panel-colocated-marker"
)

// routeChunk finds the Rollup chunk Remix emitted for one route. The hash in
// the filename is content-derived, so the prefix is all a test can pin.
func routeChunk(it *harness.IT, assets, route string) string {
	entries, err := os.ReadDir(assets)
	if err != nil {
		it.Fail("cannot read %s: %v", assets, err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, route+"-") && strings.HasSuffix(name, ".js") {
			return filepath.Join(assets, name)
		}
	}
	it.Fail("no %s-*.js chunk in %s — Remix did not compile the route", route, assets)
	return ""
}

// stagedSource matches name inside the "sources" filegroup's srcs, so a hit
// cannot come from the ts_compile that lists the same file in the same file.
func stagedSource(name string) string {
	return `(?s)filegroup\(\s*name = "sources",\s*srcs = \[[^]]*"` +
		regexp.QuoteMeta(name) + `"`
}

// requireMarkerChunk asserts some chunk carries marker. A folder route's chunk
// is named after its module rather than its directory, so the marker is what
// identifies it.
func requireMarkerChunk(it *harness.IT, assets, marker string) {
	for _, name := range it.Glob(assets, "", ".js") {
		if it.Contains(filepath.Join(assets, name), marker) {
			return
		}
	}
	it.Fail("no chunk in %s carries %q — the staged source never reached the bundle", assets, marker)
}

func main() {
	harness.Run(harness.Config{
		Name:         "remix",
		WorkspaceRel: "tests/integration/remix/workspace",
		Lockfile:     "examples/remix-app/pnpm-lock.yaml",
		Renames:      map[string]string{"BUILD.bazel.tpl": "BUILD.bazel"},
	}, func(it *harness.IT) {
		it.MustBazel("run", "//:gazelle")
		it.Pass("bazel run //:gazelle")

		build := it.Path("BUILD.bazel")
		for _, want := range []string{
			`ts_bundle(`,
			`name = "app_remix"`,
			generatedEntryPoint,
			stagedPackageJSON,
			stagedRoutes,
			stagedApp,
			`"@npm//:remix-run_dev"`,
			`vite_bundler(`,
		} {
			it.RequireContains(build, want, "generated BUILD.bazel does not contain %s", want)
		}
		it.Pass("Gazelle generated the Remix bundle, bundler and node_modules tree")

		// The entry is in app/, the only package that can hold a single-file
		// ts_compile for it; a root-relative label names nothing Gazelle writes.
		it.RequireNotContains(build, rootEntryPoint,
			"generated entry_point is root-relative, so it names no target")
		it.BazelStdout("query", "//app:entry_client")
		it.Pass("the generated entry_point label resolves to a real target")

		// app/ ships no BUILD file at all, so both targets in it are generated:
		// the single-file entry the bundle needs, and the package target that
		// must not also compile that file.
		appBuild := it.Path("app", "BUILD.bazel")
		for _, want := range []string{
			`name = "entry_client"`,
			`srcs = ["entry.client.tsx"]`,
			`"@npm//:remix-run_react"`,
		} {
			it.RequireContains(appBuild, want, "generated app/BUILD.bazel does not contain %s", want)
		}
		it.Pass("Gazelle generated the single-file client entry target")

		// A folder route (app/routes/panel/route.tsx) is not in any framework's
		// static stage-dir list, so it reaches the bundle only through a
		// filegroup Gazelle emits and a staging_srcs label Gazelle adds.
		panelDir := it.Path("app", "routes", "panel")
		panelBuild := filepath.Join(panelDir, "BUILD.bazel")
		it.RequireContains(panelBuild, "# Remix route routes/panel → /panel (parent root)",
			"generated app/routes/panel/BUILD.bazel does not annotate the route")
		for _, src := range []string{"route.tsx", "title.ts"} {
			it.RequireMatches(panelBuild, stagedSource(src),
				"the sources filegroup in app/routes/panel/BUILD.bazel does not name %s", src)
		}
		it.RequireContains(build, stagedPanel,
			"staging_srcs does not name the folder route, so the bundle never sees it")
		it.Pass("Gazelle staged the folder route and annotated its URL")

		// The second run is where the two halves used to part company: the
		// ts_compile picked the new colocated module up and the staged filegroup
		// did not, so Remix resolved an import against a file the staging tree
		// did not hold. One run cannot show that.
		it.Write(filepath.Join(panelDir, "subtitle.ts"),
			"export const panelSubtitle = \""+panelColocatedMarker+"\";\n")
		routeFile := filepath.Join(panelDir, "route.tsx")
		it.Replace(routeFile, panelTitleImport,
			"import { panelSubtitle } from \"./subtitle\";\n"+panelTitleImport)
		it.Replace(routeFile, "<h1>{panelTitle}</h1>", "<h1>{panelTitle}{panelSubtitle}</h1>")
		it.MustBazel("run", "//:gazelle")
		it.RequireMatches(panelBuild, stagedSource("subtitle.ts"),
			"the second run left the sources filegroup naming the files the first run saw")
		it.Pass("a second Gazelle run keeps the staged sources in step with the directory")

		it.Replace(build, sourceViteConfig, generatedViteConfig)
		it.MustBazel("build", "//...")
		it.Pass("bazel build //...")

		client := it.Bin("app_remix_bundle", "client")
		it.RequireDir(client, "no client/ directory in app_remix_bundle — Remix produced no SPA bundle")
		it.RequireFile(filepath.Join(client, "index.html"), "client/index.html missing")
		it.RequireContains(filepath.Join(client, "index.html"), "assets/",
			"client/index.html has no hashed asset reference — it is not Remix's generated HTML")
		it.RequireFile(filepath.Join(client, ".vite", "manifest.json"), "client/.vite/manifest.json missing")
		it.Pass("Remix wrote its client bundle into the Bazel-declared directory")

		assets := filepath.Join(client, "assets")
		for route, marker := range map[string]string{
			"_index": "acme-index-route-marker",
			"about":  "acme-about-route-marker",
		} {
			chunk := routeChunk(it, assets, route)
			it.RequireContains(chunk, marker,
				"%s does not contain %q — the staged route source was not what Remix compiled", chunk, marker)
			it.Pass("route %s compiled from its staged source", route)
		}

		// The folder route and the module the second run added: both had to be
		// staged for Remix to resolve the import at all, so a chunk carrying the
		// colocated marker is the end-to-end proof of the staging fix.
		requireMarkerChunk(it, assets, panelTitleMarker)
		requireMarkerChunk(it, assets, panelColocatedMarker)
		it.Pass("the folder route and its colocated module both reached the bundle")

		// Remix reads the project config from the staging root, so package.json
		// has to be staged with the sources rather than left at the source root.
		it.Replace(build, stagedPackageJSON, "")
		unstaged, err := it.BazelLog("unstaged_package_json.log", "build", "//:app_remix")
		if err == nil {
			unstaged.DumpTail(40)
			it.Fail("//:app_remix built without package.json in staging_srcs")
		}
		if !unstaged.Contains("_staging/package.json") {
			unstaged.DumpTail(40)
			it.Fail("//:app_remix failed for some reason other than the missing staged package.json")
		}
		it.Pass("//:app_remix fails when package.json is not staged")

		it.Replace(build, stagedRoutes, stagedPackageJSON+stagedRoutes)
		it.MustBazel("build", "//:app_remix")
		it.Pass("//:app_remix builds again once package.json is staged")

		it.Replace(build, generatedEntryPoint, rootEntryPoint)
		dangling, err := it.BazelLog("dangling_entry_point.log", "build", "//...")
		if err == nil {
			dangling.DumpTail(40)
			it.Fail("//... built with a root-relative entry_point that names no target")
		}
		if !dangling.Contains("'//:entry_client' does not exist") {
			dangling.DumpTail(40)
			it.Fail("//... failed for some reason other than the dangling entry_point")
		}
		it.Pass("a root-relative entry_point breaks bazel build //... for the whole workspace")

		it.Replace(build, rootEntryPoint, generatedEntryPoint)
		it.MustBazel("build", "//...")
		it.Pass("bazel build //... is green again")
	})
}
