// The TanStack Start integration test: Gazelle a fresh workspace, build the
// framework bundle it generates, and ask the framework's own artefacts whether
// both halves are really there.
//
// A green build proves very little here, because the plugin happily produces a
// client bundle with an empty route manifest when nothing exports a Route. So
// the assertions are over what @tanstack/react-start emitted:
//
//   - one Rollup chunk per route file, each carrying that route's marker;
//   - the route manifest chunk, with every route id and a ROOT-RELATIVE file
//     path in it -- an absolute path is the sandbox's, so it would move the
//     chunk's content hash on every build;
//   - the server bundle, and the sha256 the plugin keys the server function by,
//     both in a client chunk and in the server bundle's resolver;
//   - the built server bundle ANSWERING: SSR HTML carrying the loader's
//     server-function data, a Zod validateSearch that parses and that rejects,
//     and a /_serverFn call returning the handler's payload.
//
// Two negatives keep the positives honest: dropping the path-scrub plugin from
// the vite config puts an absolute path back in the manifest, and dropping the
// vite config entirely leaves no server half at all.
//
// Then the thing a user does next: add a route. The checked-in route tree is
// what types every createFileRoute(), so the build breaks until the tree is
// regenerated -- so the test walks the whole remedy, from the red staleness
// test to a green build with the new route in both bundles.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mikn/rules_typescript/tests/integration/harness"
)

// The module the served document tells the browser to import to hydrate with.
// The Start plugin names it through a virtual id, so the path is the framework's
// to choose and only its shape is stable.
var clientEntryRE = regexp.MustCompile(`import\("(/@id/[^"]+client-entry[^"]*)"\)`)

const (
	// The plugin keys a server function by sha256 over the vite-root-relative
	// path of the module it was declared in and the variable it was bound to.
	// Root-relative is what makes it reproducible under Bazel: the vite root is
	// the staging directory, whose absolute path is the action's sandbox.
	serverFnPreimage = "src/routes/index.tsx--readGreeting_createServerFn_handler"

	// A param route: the route path has to reach FileRoutesByPath before
	// createFileRoute accepts it, and params.userId before the loader types.
	paramRoute = `import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/about/$slug")({
  loader: ({ params }) => params.slug,
  component: () => <p>acme-param-route-marker {Route.useLoaderData()}</p>,
});
`

	scrubPlugin    = "    stableRoutePaths(root),\n"
	viteConfigAttr = `    vite_config = "tanstack-vite.config.mjs",` + "\n"
	manifestPrefix = "_tanstack-start-manifest_v-"
)

// routeMarkers maps the Rollup chunk-name prefix of each route to the string
// only that route's source contains. index.tsx takes its chunk name from the
// directory, the way Rollup dedupes an "index" entry.
var routeMarkers = map[string]string{
	"routes": "acme-index-route-marker",
	"about":  "acme-about-route-marker",
}

func serverFnID() string {
	sum := sha256.Sum256([]byte(serverFnPreimage))
	return hex.EncodeToString(sum[:])
}

// one returns the single directory entry matching prefix/suffix, failing when
// there is none or more than one.
func one(it *harness.IT, dir, prefix, suffix, what string) string {
	found := it.Glob(dir, prefix, suffix)
	if len(found) != 1 {
		it.Fail("want exactly one %s in %s (%s*%s), got %v", what, dir, prefix, suffix, found)
	}
	return filepath.Join(dir, found[0])
}

func probeScript(it *harness.IT, bundle, id string) string {
	path := it.Scratch("probe.mjs")
	it.Write(path, fmt.Sprintf(`
const entry = await import(%q);
const handler = entry.default;
const probes = [
  ["page", new Request("http://localhost/")],
  ["search-ok", new Request("http://localhost/about?page=3")],
  ["search-rejected", new Request("http://localhost/about?page=notanumber")],
  ["server-fn", new Request("http://localhost/_serverFn/%s", {
    headers: { "x-tsr-serverFn": "true" },
  })],
];
for (const [name, request] of probes) {
  const response = await handler.fetch(request);
  const body = await response.text();
  console.log("PROBE " + name + " " + response.status + " " + JSON.stringify(body));
}
`, bundle, id))
	return path
}

func main() {
	harness.Run(harness.Config{
		Name:         "tanstack",
		WorkspaceRel: "tests/integration/tanstack/workspace",
		Lockfile:     "examples/tanstack-app/pnpm-lock.yaml",
		Renames: map[string]string{
			"BUILD.bazel.tpl":            "BUILD.bazel",
			"src/routes/BUILD.bazel.tpl": "src/routes/BUILD.bazel",
		},
	}, func(it *harness.IT) {
		it.MustBazel("run", "//:gazelle")
		it.Pass("bazel run //:gazelle")

		build := it.Path("BUILD.bazel")
		for _, want := range []string{
			`ts_bundle(`,
			`name = "app"`,
			`entry_point = "//src/app:main"`,
			viteConfigAttr,
			"        \"//src/routes:sources\",\n",
			"        \"//src/app:sources\",\n",
			"        \"//src/lib:sources\",\n",
			"        \"//src/components:sources\",\n",
			`"@npm//:tanstack_react-start"`,
			`vite_bundler(`,
		} {
			it.RequireContains(build, want, "generated BUILD.bazel does not contain %s", want)
		}
		it.Pass("Gazelle generated the TanStack Start bundle, bundler and node_modules tree")

		// src/app/ ships no BUILD file, so the entry_point above names a target
		// Gazelle has to write: ts_bundle takes exactly one .js from it, while
		// the package's own ts_compile carries every other source in it.
		it.BazelStdout("query", "//src/app:main")
		it.Pass("the generated entry_point label resolves to a real target")

		it.MustBazel("build", "//...")
		it.Pass("bazel build //...")

		bundle := it.Bin("app_bundle")
		client := filepath.Join(bundle, "client")
		server := filepath.Join(bundle, "server")
		assets := filepath.Join(client, "assets")
		serverAssets := filepath.Join(server, "assets")
		it.RequireDir(client, "no client/ in app_bundle — the plugin built no client environment")
		it.RequireDir(server, "no server/ in app_bundle — the plugin built no server environment")
		it.RequireFile(filepath.Join(server, "server.mjs"),
			"server/server.mjs missing — there is no request handler to serve")

		// ts_bundle demands an html attr in app mode, and TanStack Start ignores
		// it: it overrides the client input with its own virtual entry and emits
		// no HTML at all. Pinning that keeps the wart documented rather than
		// rediscovered.
		it.RequireNoFile(filepath.Join(client, "index.html"),
			"client/index.html exists — Start emits no HTML, so the html attr should stay inert")
		it.Pass("both framework environments landed in the one Bazel-declared directory")

		for prefix, marker := range routeMarkers {
			chunk := one(it, assets, prefix+"-", ".js", "client chunk for route "+prefix)
			it.RequireContains(chunk, marker,
				"%s does not contain %q — the staged route source was not what the plugin compiled",
				chunk, marker)
			it.Pass("route %s compiled into its own client chunk", prefix)
		}

		manifest := one(it, serverAssets, manifestPrefix, ".mjs", "route manifest chunk")
		for _, want := range []string{
			`__root__`,
			`"/about"`,
			`src/routes/__root.tsx`,
			`src/routes/index.tsx`,
			`src/routes/about.tsx`,
		} {
			it.RequireContains(manifest, want, "the route manifest does not carry %s", want)
		}
		it.RequireNotContains(manifest, "/execroot/",
			"the route manifest embeds an absolute sandbox path, so its content hash moves every build")
		it.Pass("the route manifest names every route by a root-relative path")

		clientEntry := one(it, assets, "main-", ".js", "client entry chunk")
		it.RequireContains(manifest, "/assets/"+filepath.Base(clientEntry),
			"the route manifest's clientEntry does not name the emitted entry chunk")
		it.Pass("the route manifest points at the client entry the build emitted")

		// The id is derived here, not read out of the build: a plugin that
		// changed how it keys server functions -- or that lost the
		// root-relative path and baked in the sandbox -- fails at this line
		// rather than silently.
		id := serverFnID()
		inClient := false
		for _, name := range it.Glob(assets, "", ".js") {
			if it.Contains(filepath.Join(assets, name), id) {
				inClient = true
			}
		}
		if !inClient {
			it.Fail("no client chunk carries the server-function id %s (sha256 of %q)", id, serverFnPreimage)
		}
		it.RequireContains(filepath.Join(server, "server.mjs"), id,
			"the server bundle's resolver has no entry for server-function id %s", id)
		it.Pass("the server function is keyed by sha256(%q) on both sides", serverFnPreimage)

		// Everything above could still hold for a bundle that cannot serve a
		// request. The server bundle imports react and @tanstack/* as bare
		// specifiers, so it only runs with the npm tree above it in bazel-bin.
		node := it.Runfile("_main/ts/toolchain/node_resolved/node")
		probes, err := it.Exec("probes.log", node,
			probeScript(it, filepath.Join(server, "server.mjs"), id))
		if err != nil {
			probes.Dump()
			it.Fail("the built server bundle could not be run: %v", err)
		}
		for _, want := range []string{
			`PROBE page 200`,
			`acme-index-route-marker`,
			`acme-server-fn-marker`,
			`PROBE search-ok 200`,
			`acme-about-route-marker`,
			`page <!-- -->3`,
			`PROBE search-rejected 500`,
			`PROBE server-fn 200`,
		} {
			if !probes.Contains(want) {
				probes.Dump()
				it.Fail("the built server bundle's answers do not contain %q", want)
			}
		}
		it.Pass("the built server bundle renders SSR, runs the loader's server function and validates search params")

		// ── negative: the path scrub is what keeps the manifest hermetic ──
		config := it.Path("tanstack-vite.config.mjs")
		configText := it.Read(config)
		it.Replace(config, scrubPlugin, "")
		it.MustBazel("build", "//:app")
		it.RequireContains(one(it, serverAssets, manifestPrefix, ".mjs", "route manifest chunk"),
			"/execroot/",
			"without the path-scrub plugin the manifest should carry the absolute sandbox path")
		it.Pass("the path-scrub plugin is what keeps the sandbox path out of the manifest")

		it.Write(config, configText)
		it.MustBazel("build", "//:app")
		it.RequireNotContains(one(it, serverAssets, manifestPrefix, ".mjs", "route manifest chunk"),
			"/execroot/", "the manifest is still absolute after restoring the plugin")
		it.Pass("//:app is hermetic again once the plugin is back")

		// ── negative: without the vite_config there is no framework at all ──
		buildText := it.Read(build)
		it.Replace(build, viteConfigAttr, "")
		it.MustBazel("build", "//:app")
		it.RequireNoDir(server,
			"app_bundle still has a server/ without the vite_config — the plugin is not what produced it")
		it.RequireNoDir(client,
			"app_bundle still has a client/ without the vite_config — the plugin is not what produced it")
		it.Pass("no vite_config means no server half and no per-route chunks: the plugin produces both")

		it.Write(build, buildText)
		it.MustBazel("build", "//...")
		it.Pass("bazel build //... is green again")

		// ── the dev server ──
		// SSR is the half that a Bazel npm tree breaks: Vite decides whether a
		// package is external without consulting any plugin, and a package it
		// cannot resolve is inlined -- so react/jsx-runtime's CJS gets evaluated
		// as ESM and every route answers 500 with an error-overlay page that is
		// still text/html. So the assertions are over what the routes RENDER.
		it.RequireContains(build, `ts_dev_server(`,
			"Gazelle generated no dev server for a TanStack Start app")
		it.RequireContains(build, `name = "dev"`, "the generated dev server is not named dev")
		it.Pass("Gazelle generated the dev server beside the bundle")

		treeBefore := it.Read(it.Path("src", "routes", "routeTree.gen.ts"))

		srv := it.Serve("dev_server.log", "//:dev")
		for _, probe := range []struct{ path, want string }{
			{"/", "acme-index-route-marker"},
			{"/about", "acme-about-route-marker"},
		} {
			status, body := it.Get(srv, probe.path)
			if status != 200 {
				it.Fail("GET %s returned %d, want 200\n%s\n%s", probe.path, status, body, srv.Output())
			}
			if !strings.Contains(body, probe.want) {
				it.Fail("GET %s did not server-render %s -- 200 with no route in it is what the "+
					"dev overlay answers\n%s\n%s", probe.path, probe.want, body, srv.Output())
			}
		}
		it.Pass("the dev server renders every route server-side")

		// The client half is a separate failure: SSR can be perfect while the
		// entry the document asks for 404s, and the page is then inert.
		_, index := it.Get(srv, "/")
		entry := clientEntryRE.FindStringSubmatch(index)
		if entry == nil {
			it.Fail("the served document names no client entry to hydrate with\n%s", index)
		}
		if status, body := it.Get(srv, entry[1]); status != 200 {
			it.Fail("the client entry %s answers %d, want 200\n%s", entry[1], status, body)
		}
		it.Pass("the document's client entry is served")

		if out := srv.Output(); strings.Contains(out, "module is not defined") ||
			strings.Contains(out, "Failed to resolve dependency") {
			it.Fail("the dev server resolved a package into the wrong module system:\n%s", out)
		}
		it.Pass("no package was inlined that should have been external")

		// The framework's own route generator runs while it serves. If it writes
		// a tree the Bazel generator would not, `bazel run //:dev` silently makes
		// :route_tree_test red.
		srv.Stop()
		if it.Read(it.Path("src", "routes", "routeTree.gen.ts")) != treeBefore {
			it.Fail("serving rewrote src/routes/routeTree.gen.ts, so the dev server and " +
				"//src/routes:route_tree disagree about the route tree")
		}
		it.MustBazel("test", "//src/routes:route_tree_test")
		it.Pass("serving leaves the checked-in route tree alone")

		it.RequireNoFile(it.Path("node_modules"),
			"the npm tree link was left at the workspace root after the server stopped")
		it.Pass("the workspace npm link is removed when the server stops")

		// ── adding a route: the break, the named remedy, and the fix ──
		it.Write(it.Path("src", "routes", "about.$slug.tsx"), paramRoute)
		it.MustBazel("run", "//:gazelle")
		it.RequireContains(it.Path("src", "routes", "BUILD.bazel"), `"about.$slug.tsx"`,
			"Gazelle did not pick the new route file up into the routes target")

		if err := it.Bazel("build", "//src/routes:routes"); err == nil {
			it.Fail("//src/routes:routes still builds with a stale route tree, so nothing types the new route path")
		}
		it.Pass("a new route does not type-check against the old route tree")

		stale, err := it.BazelLog("route_tree_stale.log", "test", "//src/routes:route_tree_test")
		if err == nil {
			it.Fail("//src/routes:route_tree_test passed with a route the checked-in tree does not have")
		}
		if !stale.Contains("bazel run //src/routes:update_route_tree") {
			stale.Dump()
			it.Fail("the staleness test does not name the command that fixes it")
		}
		it.Pass("the staleness test is what reports it, and it names the remedy")

		it.MustBazel("run", "//src/routes:update_route_tree")
		it.RequireContains(it.Path("src", "routes", "routeTree.gen.ts"), "'/about/$slug'",
			"the regenerated tree does not carry the new route path")
		it.MustBazel("build", "//...")
		it.MustBazel("test", "//src/routes:route_tree_test")
		it.Pass("the remedy regenerates the tree in the source tree and the build goes green")

		// The bundle half has to agree: the plugin scans the staged sources, so
		// the new route is only really in the app if it emitted a chunk of its own.
		chunk := one(it, assets, "about.", ".js", "client chunk for the new param route")
		it.RequireContains(chunk, "acme-param-route-marker",
			"%s is not the new route's chunk", chunk)
		it.RequireContains(one(it, serverAssets, manifestPrefix, ".mjs", "route manifest chunk"),
			`src/routes/about.$slug.tsx`,
			"the route manifest does not name the new route file")
		it.Pass("the new route reaches the client chunks and the route manifest")
	})
}
