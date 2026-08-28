package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mikn/rules_typescript/tests/integration/harness"
)

const (
	pinnedVersion   = `version: { name: "integration-test" },`
	unpinnedVersion = `/* unpinned */`
	svelteConfigJS  = `svelte_config = "svelte.config.js"`
	svelteConfigMJS = `svelte_config = "svelte.config.mjs"`
	svelteConfigTS  = `svelte_config = "svelte.config.ts"`
)

var chunkName = regexp.MustCompile(`[A-Za-z0-9_-]+\.[A-Za-z0-9_-]{8}\.js$`)

// clientChunks is the set of hashed filenames the client half emitted. The
// names are content-derived, so comparing the set across two builds is what
// sees a build whose output nothing downstream can ever cache.
func clientChunks(it *harness.IT, out string) []string {
	var names []string
	root := filepath.Join(out, "client", "_app", "immutable")
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && chunkName.MatchString(d.Name()) {
			names = append(names, d.Name())
		}
		return nil
	})
	if err != nil {
		it.Fail("cannot walk %s: %v", root, err)
	}
	if len(names) == 0 {
		it.Fail("%s holds no hashed chunks", root)
	}
	return names
}

// holdsMarker reports whether any file under dir contains text. A build that
// compiled none of the app still exits 0 with a normal-looking summary, so the
// markers are the assertion and the exit code is not.
func holdsMarker(it *harness.IT, dir, text string) bool {
	found := false
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(body), text) {
			found = true
		}
		return nil
	})
	if err != nil {
		it.Fail("cannot walk %s: %v", dir, err)
	}
	return found
}

// move renames a file out of the way and returns a restore func, so a negative
// case can take a source the build reads off disk away from it.
func move(it *harness.IT, from, to string) func() {
	if err := os.Rename(from, to); err != nil {
		it.Fail("cannot move %s: %v", from, err)
	}
	return func() {
		if err := os.Rename(to, from); err != nil {
			it.Fail("cannot restore %s: %v", from, err)
		}
	}
}

func main() {
	harness.Run(harness.Config{
		Name:         "sveltekit",
		WorkspaceRel: "tests/integration/sveltekit/workspace",
		Renames:      map[string]string{"BUILD.bazel.tpl": "BUILD.bazel"},
	}, func(it *harness.IT) {
		it.MustBazel("run", "//:gazelle")
		it.Pass("bazel run //:gazelle")

		build := it.Path("BUILD.bazel")
		for _, want := range []string{
			`sveltekit_build(`,
			`name = "app"`,
			svelteConfigJS,
			`config = "vite.config.mjs"`,
			`glob(["src/**"])`,
			`"@npm//:sveltejs_kit"`,
			`"@npm//:svelte"`,
		} {
			it.RequireContains(build, want, "generated BUILD.bazel does not contain %s", want)
		}
		it.RequireNotContains(build, "ts_bundle(",
			"Gazelle generated a ts_bundle for SvelteKit, which cannot host it")
		it.Pass("Gazelle generated the sveltekit_build and its node_modules tree")

		// A BUILD file under src/ would make a subpackage, and the srcs glob
		// does not descend into one -- so the staged tree would silently lose
		// the modules the app imports.
		for _, dir := range []string{"routes", "lib"} {
			it.RequireNoFile(it.Path("src", dir, "BUILD.bazel"),
				"Gazelle wrote a BUILD file in src/%s, which the srcs glob cannot see into", dir)
		}
		it.Pass("nothing under src/ became a subpackage")

		it.MustBazel("build", "//:app")
		it.Pass("bazel build //:app")

		out := it.Bin("app_sveltekit_out")
		for _, half := range []string{"client", "server"} {
			it.RequireDir(filepath.Join(out, half),
				"%s missing — one vite build is two passes and both must land", half)
		}
		it.RequireNoDir(filepath.Join(out, "_staging"),
			"the staging root survived into the declared output")
		it.Pass("the output holds SvelteKit's own client/ and server/ halves")

		// The server half names its entries after the route files, so these
		// paths existing is what says SvelteKit's own conventions were applied
		// rather than a plain Vite bundle produced. `+` is not legal in an
		// output filename, so SvelteKit writes `_`.
		for _, entry := range []string{
			"server/entries/pages/_page.svelte.js",
			"server/entries/pages/_page.server.ts.js",
			"server/entries/pages/blog/_slug_/_page.svelte.js",
			"server/entries/endpoints/api/_server.ts.js",
			"server/entries/fallbacks/layout.svelte.js",
			"server/entries/fallbacks/error.svelte.js",
			"server/index.js",
			"client/_app/version.json",
		} {
			it.RequireFile(filepath.Join(out, entry),
				"%s missing — the build did not apply SvelteKit's route conventions", entry)
		}
		it.Pass("+page.svelte and +page.server.ts became route entries, with the default fallbacks")

		// The route ids are SvelteKit's own reading of the route tree, written
		// by SvelteKit and by nothing else. A plain Vite bundle of the same
		// sources has no manifest.js, no route ids and no [slug] pattern.
		serverManifest := filepath.Join(out, "server", "manifest.js")
		for _, id := range []string{`id: "/"`, `id: "/api"`, `id: "/blog/[slug]"`} {
			it.RequireContains(serverManifest, id,
				"server/manifest.js does not carry %s — SvelteKit did not read the route tree", id)
		}
		it.Pass("server/manifest.js carries a route id per route directory, [slug] pattern included")

		it.RequireContains(filepath.Join(out, "server", "entries", "pages", "_page.server.ts.js"),
			"acme-server-marker", "the +page.server.ts entry does not come from the staged source")
		it.RequireContains(filepath.Join(out, "client", "_app", "version.json"),
			"integration-test", "kit.version.name from svelte.config.js did not reach the build")
		it.Pass("the .ts server module compiled with no ts_compile in front of it")

		if !holdsMarker(it, filepath.Join(out, "client"), "acme-page-marker") {
			it.Fail("no client chunk contains the marker from +page.svelte")
		}
		if !holdsMarker(it, filepath.Join(out, "server"), "acme-lib-marker") {
			it.Fail("no server chunk contains the marker from $lib/greeting.ts")
		}
		it.Pass("the .svelte route and its $lib import both compiled into the bundle")

		first := clientChunks(it, out)
		it.MustBazel("clean")
		it.MustBazel("build", "//:app")
		second := clientChunks(it, it.Bin("app_sveltekit_out"))
		if strings.Join(first, ",") != strings.Join(second, ",") {
			it.Fail("chunk names differ across two clean builds:\n  %v\n  %v", first, second)
		}
		it.Pass("two clean builds emit the same %d hashed chunk names", len(first))

		// An unpinned kit.version.name is hashed into every chunk name, so the
		// build stays green and nothing downstream of it ever cache-hits.
		// Starlark cannot see it, so the rule says so after the fact.
		config := it.Path("svelte.config.js")
		originalConfig := it.Read(config)
		it.Replace(config, pinnedVersion, unpinnedVersion)
		unpinned, err := it.BazelLog("unpinned_version.log", "build", "//:app")
		if err != nil {
			unpinned.DumpTail(30)
			it.Fail("//:app failed to build with an unpinned kit.version.name")
		}
		if !unpinned.Contains("kit.version.name reads as epoch-milliseconds") {
			unpinned.DumpTail(40)
			it.Fail("an unpinned kit.version.name built silently, poisoning the cache")
		}
		it.Pass("an unpinned kit.version.name is called out in the build log")
		it.Write(config, originalConfig)

		// src/app.html reaches the build through srcs and nothing else: no
		// import names it, and SvelteKit reads it off disk from its cwd.
		restore := move(it, it.Path("src", "app.html"), it.Scratch("app.html"))
		noHTML, err := it.BazelLog("no_app_html.log", "build", "//:app")
		if err == nil {
			noHTML.DumpTail(30)
			it.Fail("//:app built without src/app.html")
		}
		if !noHTML.Contains("src/app.html does not exist") {
			noHTML.DumpTail(30)
			it.Fail("the missing-app.html failure does not name the file")
		}
		it.Pass("a missing src/app.html fails naming the file")
		restore()

		// The rule stages the svelte config as svelte.config.js whatever it is
		// called, so a .mjs would build here and nowhere else: SvelteKit's own
		// load_config() globs svelte.config.{js,ts} and would not find it. A
		// .ts is in that glob but the toolchain Node cannot load one at all.
		for _, tt := range []struct {
			ext, attr, want string
		}{
			{"mjs", svelteConfigMJS, "is not a .js file"},
			{"ts", svelteConfigTS, "is a .ts file"},
		} {
			it.Write(it.Path("svelte.config."+tt.ext), originalConfig)
			it.Replace(build, svelteConfigJS, tt.attr)
			rejected, err := it.BazelLog("config_"+tt.ext+".log", "build", "//:app")
			if err == nil {
				rejected.DumpTail(30)
				it.Fail("//:app accepted a svelte.config.%s", tt.ext)
			}
			if !rejected.Contains(tt.want) {
				rejected.DumpTail(30)
				it.Fail("the svelte.config.%s rejection does not say %q", tt.ext, tt.want)
			}
			it.Replace(build, tt.attr, svelteConfigJS)
		}
		it.Pass("a svelte.config.mjs and a svelte.config.ts are both rejected at analysis time")

		// The route tree reaches the build off disk too, and a build with no
		// routes exits 0 with a normal-looking summary and only the fallback
		// entries. The rule turns that into a failure.
		restore = move(it, it.Path("src", "routes"), it.Scratch("routes"))
		noRoutes, err := it.BazelLog("no_routes.log", "build", "//:app")
		if err == nil {
			noRoutes.DumpTail(30)
			it.Fail("//:app built green with no route staged at all")
		}
		if !noRoutes.Contains("no SvelteKit route file") {
			noRoutes.DumpTail(30)
			it.Fail("a routeless build failed for some other reason")
		}
		it.Pass("a build that staged no routes fails rather than looking green")
		restore()

		// Losing *part* of the route tree is the dangerous half: the routes
		// that survived still build, and the deployed app is short one route.
		// A BUILD file inside a route directory is how it happens -- the srcs
		// glob stops at a package boundary.
		nested := it.Path("src", "routes", "blog", "[slug]", "BUILD.bazel")
		it.Write(nested, "")
		hole, err := it.BazelLog("subpackage.log", "build", "//:app")
		if err == nil {
			hole.DumpTail(30)
			it.Fail("//:app built green while src/routes/blog/[slug] was a subpackage")
		}
		for _, want := range []string{"src/routes/blog/[slug]", "allow_subpackages"} {
			if !hole.Contains(want) {
				hole.DumpTail(30)
				it.Fail("the subpackage failure does not mention %q", want)
			}
		}
		if err := os.Remove(nested); err != nil {
			it.Fail("cannot remove %s: %v", nested, err)
		}
		it.MustBazel("build", "//:app")
		it.RequireContains(filepath.Join(it.Bin("app_sveltekit_out"), "server", "manifest.js"),
			`id: "/blog/[slug]"`, "the route did not come back once the BUILD file was gone")
		it.Pass("a BUILD file inside a route directory fails the build naming that directory")

		// The other half of the same defect: a route file that was staged but
		// that SvelteKit never compiled. Only SvelteKit knows which is which,
		// so the rule reads it back out of the Vite manifest.
		stray := it.Path("src", "lib", "+page.svelte")
		it.Write(stray, "<h1>stray</h1>\n")
		uncompiled, err := it.BazelLog("uncompiled_route.log", "build", "//:app")
		if err == nil {
			uncompiled.DumpTail(30)
			it.Fail("//:app built green with a staged route SvelteKit never compiled")
		}
		if !uncompiled.Contains("src/lib/+page.svelte") {
			uncompiled.DumpTail(30)
			it.Fail("the uncompiled-route failure does not name the file")
		}
		if err := os.Remove(stray); err != nil {
			it.Fail("cannot remove %s: %v", stray, err)
		}
		it.Pass("a staged route SvelteKit did not compile fails the build naming the file")

		it.MustBazel("build", "//...")
		it.Pass("bazel build //... is green again")
	})
}
