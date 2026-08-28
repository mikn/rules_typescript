package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mikn/rules_typescript/tests/integration/harness"
)

const (
	pluginCall = "remix({ manifest: true })"
	spaPlugin  = "remix({ manifest: true, ssr: false })"
	movedBuild = "remix({ manifest: true, buildDirectory: \"dist\" })"

	srcsGlob    = "    ]),\n    config = \"remix-vite.config.mjs\","
	srcsNoPanel = "    ], exclude = [\"app/routes/panel/**\"]),\n    config = \"remix-vite.config.mjs\","
)

// section returns one "=== name ===" block of the probe report: its status
// line, its content-type line and its body, with the delimiters stripped.
func section(it *harness.IT, report, name string) string {
	text := it.Read(report)
	opener := "=== " + name + " ===\n"
	closer := "\n=== end " + name + " ==="
	start := strings.Index(text, opener)
	if start < 0 {
		it.Fail("%s has no %q section — the driver did not reach that request", report, name)
	}
	rest := text[start+len(opener):]
	end := strings.Index(rest, closer)
	if end < 0 {
		it.Fail("%s never closes the %q section", report, name)
	}
	return rest[:end]
}

func requireIn(it *harness.IT, haystack, needle, what string) {
	if !strings.Contains(haystack, needle) {
		it.Fail("%s does not contain %q. Got:\n%s", what, needle, haystack)
	}
}

func requireNotIn(it *harness.IT, haystack, needle, what string) {
	if strings.Contains(haystack, needle) {
		it.Fail("%s contains %q and should not. Got:\n%s", what, needle, haystack)
	}
}

// hashedAsset finds the one file in dir whose name is prefix + "-" + hash + ext.
// The hash is content-derived, so the prefix is all a test can pin.
func hashedAsset(it *harness.IT, dir, prefix, ext string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		it.Fail("cannot read %s: %v", dir, err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, prefix+"-") && strings.HasSuffix(name, ext) {
			return filepath.Join(dir, name)
		}
	}
	it.Fail("no %s-*%s in %s — Remix emitted no such asset", prefix, ext, dir)
	return ""
}

func main() {
	harness.Run(harness.Config{
		Name:         "remix_ssr",
		WorkspaceRel: "tests/integration/remix_ssr/workspace",
		Lockfile:     "examples/remix-app/pnpm-lock.yaml",
		Renames:      map[string]string{"BUILD.bazel.tpl": "BUILD.bazel"},
	}, func(it *harness.IT) {
		it.MustBazel("build", "//:app")
		it.Pass("bazel build //:app")

		out := it.Bin("app_remix_out")
		client := filepath.Join(out, "client")
		server := filepath.Join(out, "server")
		it.RequireDir(client, "no client/ directory in app_remix_out")
		it.RequireDir(server, "no server/ directory in app_remix_out — remix_build produced only the browser half")
		it.RequireFile(filepath.Join(server, "index.js"), "server/index.js missing — there is no request handler to run")
		it.Pass("remix_build produced both halves in one declared directory")

		// The config sets keys outside {plugins, root}; ts_bundle's generated
		// config throws on those, so their effect is what says this rule loads
		// the user's config rather than wrapping it.
		for _, name := range []string{"client-manifest.json", "server-manifest.json"} {
			it.RequireFile(filepath.Join(out, ".vite", name),
				".vite/%s missing — build.manifest from the vite config did not reach the build", name)
		}
		it.Pass("the vite config's own build.manifest reached both halves")

		routes := filepath.Join(out, ".remix", "manifest.json")
		it.RequireFile(routes, ".remix/manifest.json missing — the plugin's manifest option did not reach it")
		for _, want := range []string{
			`"routes/_index"`,
			`"index": true`,
			`"routes/api.health"`,
			`"path": "api/health"`,
			`"routes/dash"`,
			`"path": "dash"`,
			`"routes/dash.settings"`,
			`"parentId": "routes/dash"`,
			`"routes/panel"`,
			`"file": "routes/panel/route.tsx"`,
		} {
			it.RequireContains(routes, want, ".remix/manifest.json does not contain %s", want)
		}
		it.Pass("the build manifest carries every route, nesting and the folder route included")

		assets := filepath.Join(client, "assets")
		browserManifest := hashedAsset(it, assets, "manifest", ".js")
		manifestText := it.Read(browserManifest)
		requireIn(it, manifestText, "window.__remixManifest=", browserManifest)
		for _, want := range []string{
			`"routes/_index":{"id":"routes/_index","parentId":"root","index":true,"hasAction":false,"hasLoader":true`,
			`"routes/dash.settings":{"id":"routes/dash.settings","parentId":"routes/dash","path":"settings","hasAction":true,"hasLoader":true`,
			`"routes/panel":{"id":"routes/panel","parentId":"root","path":"panel","hasAction":false,"hasLoader":true`,
		} {
			requireIn(it, manifestText, want, browserManifest)
		}
		it.Pass("the browser manifest — emitted by the SERVER half — reports each route's real loader and action")

		// The loader body belongs to the server half only. A client build that
		// merely compiled the route would carry it in the route chunk.
		it.RequireContains(filepath.Join(server, "index.js"), "acme-index-loader-value",
			"server/index.js does not contain the loader's own value")
		indexChunk := hashedAsset(it, assets, "_index", ".js")
		it.RequireContains(indexChunk, "acme-index-route-marker",
			"%s does not contain the route's markup — it is not the compiled route", indexChunk)
		it.RequireNotContains(indexChunk, "acme-index-loader-value",
			"%s contains the loader's value — the server-only export was not stripped from the client half", indexChunk)
		it.Pass("the loader is in the server half and stripped from the client half")

		resourceChunk := hashedAsset(it, assets, "api.health", ".js")
		if info, err := os.Stat(resourceChunk); err != nil || info.Size() != 0 {
			it.Fail("%s is not empty — a resource route has nothing to ship to the browser", resourceChunk)
		}
		it.Pass("the resource route compiles to an empty client chunk")

		// Everything above is compilation. This runs the thing.
		it.MustBazel("build", "//:server_probe")
		report := it.Bin("server_probe.report.txt")
		it.RequireFile(report, "no server_probe report — the built server did not run")
		it.Pass("the built server ran under @remix-run/node's createRequestHandler")

		index := section(it, report, "get_index")
		requireIn(it, index, "status: 200", "GET /")
		requireIn(it, index, "content-type: text/html", "GET /")
		requireIn(it, index, "<p>acme-index-loader-value</p>", "GET / HTML")
		requireIn(it, index, `"loaderData":{"routes/_index":{"greeting":"acme-index-loader-value"}`, "GET / __remixContext")
		it.Pass("GET / renders the loader's data and hands it to the client in __remixContext")

		data := section(it, report, "get_index_data")
		requireIn(it, data, "status: 200", "GET /?_data=routes/_index")
		requireIn(it, data, "content-type: application/json", "GET /?_data=routes/_index")
		requireIn(it, data, `{"greeting":"acme-index-loader-value"}`, "the loader's data request")
		it.Pass("the loader answers a data request with bare JSON")

		nested := section(it, report, "get_dash_settings")
		requireIn(it, nested, "status: 200", "GET /dash/settings")
		requireIn(it, nested, "<h1>acme-dash-layout-loader</h1>", "GET /dash/settings HTML")
		requireIn(it, nested, "<h2>acme-dash-settings-loader</h2>", "GET /dash/settings HTML")
		requireIn(it, nested, `"routes/dash":{"section":"acme-dash-layout-loader"}`, "GET /dash/settings loaderData")
		requireIn(it, nested, `"routes/dash.settings":{"title":"acme-dash-settings-loader"}`, "GET /dash/settings loaderData")
		it.Pass("a nested layout and its child each run their own loader, keyed by route id")

		posted := section(it, report, "post_dash_settings")
		requireIn(it, posted, "status: 200", "POST /dash/settings")
		requireIn(it, posted, "<p>acme-posted-value</p>", "POST /dash/settings HTML")
		requireIn(it, posted, `"actionData":{"routes/dash.settings":{"echoed":"acme-posted-value"}}`,
			"POST /dash/settings __remixContext")
		postedData := section(it, report, "post_dash_settings_data")
		requireIn(it, postedData, `{"echoed":"acme-posted-value"}`, "the action's data request")
		it.Pass("the action reads the submitted form and its result reaches both the HTML and the client")

		resource := section(it, report, "get_resource")
		requireIn(it, resource, "status: 200", "GET /api/health")
		requireIn(it, resource, "content-type: text/plain", "GET /api/health")
		requireIn(it, resource, "acme-resource-route-body", "GET /api/health")
		requireNotIn(it, resource, "<html", "GET /api/health")
		requireNotIn(it, resource, "__remixContext", "GET /api/health")
		it.Pass("the resource route answers with its own Response and no HTML shell")

		panel := section(it, report, "get_panel")
		requireIn(it, panel, "status: 200", "GET /panel")
		requireIn(it, panel, "<h1>acme-panel-folder-route</h1>", "GET /panel HTML")
		it.Pass("the folder route (app/routes/panel/route.tsx) serves, with its colocated helper compiled in")

		// Negative: SPA mode cannot serve any of the above. A loader export is
		// a hard build error there, which is why remix_build is not that path.
		config := it.Path("remix-vite.config.mjs")
		it.Replace(config, pluginCall, spaPlugin)
		spa, err := it.BazelLog("spa_mode.log", "build", "//:app")
		if err == nil {
			spa.DumpTail(40)
			it.Fail("//:app built with ssr: false while the routes export loaders")
		}
		if !spa.Contains("SPA Mode") || !spa.Contains("invalid route export") {
			spa.DumpTail(40)
			it.Fail("//:app failed for some reason other than SPA mode rejecting the loader exports")
		}
		it.Pass("ssr: false is refused while a route exports a loader")
		it.Replace(config, spaPlugin, pluginCall)

		// Negative: the rule owns the build directory, because that is what it
		// moves into the output it declared. A config that redirects it has to
		// say so rather than produce an empty artifact.
		it.Replace(config, pluginCall, movedBuild)
		moved, err := it.BazelLog("moved_build_dir.log", "build", "//:app")
		if err == nil {
			moved.DumpTail(40)
			it.Fail("//:app built with the plugin's buildDirectory pointing outside the staging root")
		}
		if !moved.Contains("wrote no build directory") {
			moved.DumpTail(40)
			it.Fail("//:app failed for some reason other than the redirected buildDirectory")
		}
		it.Pass("remix_build names the redirected buildDirectory instead of declaring an empty output")
		it.Replace(config, movedBuild, pluginCall)

		it.MustBazel("build", "//:app")
		it.Pass("bazel build //:app is green again")

		// Negative: dropping a route's sources from srcs must not go green with
		// the route silently absent, which is what the ts_bundle path did.
		build := it.Path("BUILD.bazel")
		it.Replace(build, srcsGlob, srcsNoPanel)
		dropped, err := it.BazelLog("dropped_folder_route.log", "build", "//:server_probe")
		if err != nil {
			dropped.DumpTail(40)
			it.Fail("//:server_probe stopped building for some reason other than the dropped route")
		}
		gone := section(it, report, "get_panel")
		if strings.Contains(gone, "acme-panel-folder-route") {
			it.Fail("GET /panel still serves the folder route after its sources left srcs")
		}
		if !regexp.MustCompile(`status: 40\d`).MatchString(gone) {
			it.Fail("GET /panel returned neither the route nor a 4xx after its sources left srcs. Got:\n%s", gone)
		}
		it.Pass("a route dropped from srcs is gone from the served app, not silently stale")
		it.Replace(build, srcsNoPanel, srcsGlob)
	})
}
