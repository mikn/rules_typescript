package remix

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/bazelbuild/bazel-gazelle/config"
	"github.com/bazelbuild/bazel-gazelle/language"
	"github.com/bazelbuild/bazel-gazelle/rule"
)

// writeApp materialises an app/ directory: each key is a path relative to app/,
// each value the file's contents.
func writeApp(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	appDir := filepath.Join(root, "app")
	for rel, body := range files {
		full := filepath.Join(appDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func manifestByID(t *testing.T, files map[string]string) (map[string]Route, []string) {
	t.Helper()
	root := writeApp(t, files)
	routes, diags := Manifest(filepath.Join(root, "app"))
	byID := make(map[string]Route, len(routes))
	for _, r := range routes {
		byID[r.ID] = r
	}
	return byID, diags
}

// TestManifest_EveryConventionForm walks the forms flat-routes.js supports in
// one route directory, because the segment machine's behaviour depends on what
// the neighbouring routes are (parenting) as much as on the filename.
func TestManifest_EveryConventionForm(t *testing.T) {
	byID, diags := manifestByID(t, map[string]string{
		"root.tsx":                        "",
		"routes/_index.tsx":               "",
		"routes/about.tsx":                "",
		"routes/dash.tsx":                 "",
		"routes/dash.settings.tsx":        "",
		"routes/dashboard.tsx":            "",
		"routes/users.$userId.tsx":        "",
		"routes/files.$.tsx":              "",
		"routes/(marketing).pricing.tsx":  "",
		"routes/ping[.]txt.tsx":           "",
		"routes/_auth.tsx":                "",
		"routes/_auth.login.tsx":          "",
		"routes/parent_.child.tsx":        "",
		"routes/panel/route.tsx":          "",
		"routes/panel/helper.ts":          "",
		"routes/prefs/index.tsx":          "",
		"routes/deep/nested/route.tsx":    "",
		"routes/.hidden.tsx":              "",
		"routes/notes.README.md":          "",
		"routes/panel/nested/helper.ts":   "",
		"routes/panel/nested/thing.ts":    "",
		"components/Button.tsx":           "",
		"routes/dash.settings.styles.css": "",
	})
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}

	for _, tt := range []struct {
		id     string
		url    string
		path   string
		parent string
		index  bool
	}{
		{id: "routes/_index", url: "/", path: "", parent: "root", index: true},
		{id: "routes/about", url: "/about", path: "about", parent: "root"},
		{id: "routes/dash", url: "/dash", path: "dash", parent: "root"},
		{id: "routes/dash.settings", url: "/dash/settings", path: "settings", parent: "routes/dash"},
		// "dash" is a prefix of "dashboard" but not on a separator boundary.
		{id: "routes/dashboard", url: "/dashboard", path: "dashboard", parent: "root"},
		{id: "routes/users.$userId", url: "/users/:userId", path: "users/:userId", parent: "root"},
		{id: "routes/files.$", url: "/files/*", path: "files/*", parent: "root"},
		{id: "routes/(marketing).pricing", url: "/marketing?/pricing", path: "marketing?/pricing", parent: "root"},
		{id: "routes/ping[.]txt", url: "/ping.txt", path: "ping.txt", parent: "root"},
		{id: "routes/_auth", url: "/", path: "", parent: "root"},
		{id: "routes/_auth.login", url: "/login", path: "login", parent: "routes/_auth"},
		{id: "routes/parent_.child", url: "/parent/child", path: "parent/child", parent: "root"},
		{id: "routes/panel", url: "/panel", path: "panel", parent: "root"},
		// A folder route whose module is index.tsx is not an index route: the
		// route id is the directory, and it does not end in "_index".
		{id: "routes/prefs", url: "/prefs", path: "prefs", parent: "root"},
		{id: "routes/notes.README", url: "/notes/README", path: "notes/README", parent: "root"},
	} {
		route, ok := byID[tt.id]
		if !ok {
			t.Errorf("%s is missing from the manifest", tt.id)
			continue
		}
		if route.URL() != tt.url {
			t.Errorf("%s URL = %q, want %q", tt.id, route.URL(), tt.url)
		}
		if route.Path != tt.path {
			t.Errorf("%s Path = %q, want %q", tt.id, route.Path, tt.path)
		}
		if route.ParentID != tt.parent {
			t.Errorf("%s ParentID = %q, want %q", tt.id, route.ParentID, tt.parent)
		}
		if route.Index != tt.index {
			t.Errorf("%s Index = %v, want %v", tt.id, route.Index, tt.index)
		}
	}

	// readdirSync is one level deep: a directory two levels down is never a
	// route, and a dotfile is always ignored.
	for _, absent := range []string{
		"routes/deep",
		"routes/deep/nested",
		"routes/.hidden",
		"routes/panel/helper",
		"routes/panel/nested",
	} {
		if _, ok := byID[absent]; ok {
			t.Errorf("%s became a route; only one level of app/routes is read", absent)
		}
	}
	if len(byID) != 15 {
		ids := make([]string, 0, len(byID))
		for id := range byID {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		t.Errorf("manifest has %d routes, want 15: %v", len(byID), ids)
	}
}

func TestManifest_FolderRouteFileAndDir(t *testing.T) {
	byID, _ := manifestByID(t, map[string]string{
		"root.tsx":               "",
		"routes/panel/route.tsx": "",
		"routes/panel/helper.ts": "",
	})
	route := byID["routes/panel"]
	if route.File != "routes/panel/route.tsx" {
		t.Errorf("File = %q, want routes/panel/route.tsx", route.File)
	}
	if route.Dir != "routes/panel" {
		t.Errorf("Dir = %q, want routes/panel", route.Dir)
	}
}

// TestManifest_FolderRoutePrefersRouteOverIndex: Remix uses route.* and reports
// the other as a conflict, so the plugin must say the same thing rather than
// pick the file that happens to sort first.
func TestManifest_FolderRoutePrefersRouteOverIndex(t *testing.T) {
	byID, diags := manifestByID(t, map[string]string{
		"root.tsx":               "",
		"routes/panel/route.tsx": "",
		"routes/panel/index.tsx": "",
	})
	if got := byID["routes/panel"].File; got != "routes/panel/route.tsx" {
		t.Errorf("File = %q, want routes/panel/route.tsx", got)
	}
	if len(diags) == 0 || !strings.Contains(diags[0], "route.tsx") {
		t.Errorf("no diagnostic naming the route.tsx/index.tsx conflict: %v", diags)
	}
}

func TestManifest_RouteIDConflict(t *testing.T) {
	_, diags := manifestByID(t, map[string]string{
		"root.tsx":         "",
		"routes/about.tsx": "",
		"routes/about.jsx": "",
	})
	if len(diags) != 1 || !strings.Contains(diags[0], "routes/about") {
		t.Errorf("diagnostics = %v, want one naming routes/about", diags)
	}
}

func TestManifest_URLConflict(t *testing.T) {
	_, diags := manifestByID(t, map[string]string{
		"root.tsx":                "",
		"routes/pricing.tsx":      "",
		"routes/(a).pricing_.tsx": "",
	})
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics for distinct URLs: %v", diags)
	}

	_, diags = manifestByID(t, map[string]string{
		"root.tsx":                 "",
		"routes/pricing.tsx":       "",
		"routes/pricing/route.tsx": "",
	})
	if len(diags) != 1 || !strings.Contains(diags[0], "/pricing") {
		t.Errorf("diagnostics = %v, want one naming /pricing", diags)
	}
}

// TestManifest_PathlessLayoutsMayShareAPath: several pathless layouts on the
// same path is the point of them, so the URL-conflict check must exempt them.
func TestManifest_PathlessLayoutsMayShareAPath(t *testing.T) {
	_, diags := manifestByID(t, map[string]string{
		"root.tsx":                    "",
		"routes/account.tsx":          "",
		"routes/account._public.tsx":  "",
		"routes/account._private.tsx": "",
	})
	if len(diags) != 0 {
		t.Errorf("pathless layouts on the same path were reported as a conflict: %v", diags)
	}
}

func TestManifest_RejectsSegmentsReactRouterCannotExpress(t *testing.T) {
	for _, name := range []string{"a*b.tsx", "a:b.tsx"} {
		_, diags := manifestByID(t, map[string]string{
			"root.tsx":       "",
			"routes/" + name: "",
		})
		if len(diags) != 1 || !strings.Contains(diags[0], "cannot contain") {
			t.Errorf("routes/%s produced diagnostics %v, want one refusal", name, diags)
		}
	}
}

func TestManifest_NoRoutesDirectory(t *testing.T) {
	root := writeApp(t, map[string]string{"root.tsx": ""})
	routes, diags := Manifest(filepath.Join(root, "app"))
	if len(routes) != 0 || len(diags) != 0 {
		t.Errorf("Manifest = (%v, %v), want empty for an app with no routes/", routes, diags)
	}
}

func TestRouteComment(t *testing.T) {
	for _, tt := range []struct {
		route Route
		want  string
	}{
		{
			Route{ID: "routes/_index", Index: true, ParentID: "root"},
			"# Remix route routes/_index → / (index, parent root)",
		},
		{
			Route{ID: "routes/dash.settings", FullPath: "dash/settings", ParentID: "routes/dash"},
			"# Remix route routes/dash.settings → /dash/settings (parent routes/dash)",
		},
		{
			Route{ID: "routes/_auth", ParentID: "root"},
			"# Remix route routes/_auth → pathless layout (parent root)",
		},
	} {
		if got := routeComment(tt.route); got != tt.want {
			t.Errorf("routeComment(%s) = %q, want %q", tt.route.ID, got, tt.want)
		}
	}
}

// The annotation is the one thing in this plugin the merger cannot carry, so it
// is rewritten in place. A route added after the first run has to appear, a
// route that went away has to go, and a comment the user wrote has to stay.
func TestAnnotateRouteTargets_RewritesTheBlockOnAnExistingRule(t *testing.T) {
	root := writeApp(t, map[string]string{
		"root.tsx":          "",
		"routes/_index.tsx": "",
		"routes/about.tsx":  "",
	})
	existing := []byte(`load("@rules_typescript//ts:defs.bzl", "ts_compile")

# Hand-written note about this package.
# Remix route routes/_index → / (index, parent root)
# Remix route routes/gone → /gone (parent root)
ts_compile(
    name = "routes",
    srcs = [
        "_index.tsx",
        "about.tsx",
    ],
)
`)
	path := filepath.Join(root, "app", "routes", "BUILD.bazel")
	if err := os.WriteFile(path, existing, 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := rule.LoadFile(path, "app/routes")
	if err != nil {
		t.Fatal(err)
	}

	gen := rule.NewRule("ts_compile", "routes")
	gen.SetAttr("srcs", []string{"_index.tsx", "about.tsx"})
	AdjustGenerateResult(language.GenerateArgs{
		Config: &config.Config{RepoRoot: root},
		Dir:    filepath.Join(root, "app", "routes"),
		Rel:    "app/routes",
		File:   f,
	}, language.GenerateResult{Gen: []*rule.Rule{gen}, Imports: []any{nil}})

	got := string(f.Format())
	for _, want := range []string{
		"# Hand-written note about this package.",
		"# Remix route routes/_index → / (index, parent root)",
		"# Remix route routes/about → /about (parent root)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("annotation block is missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "routes/gone") {
		t.Errorf("the annotation for a route that no longer exists survived:\n%s", got)
	}
}

// captureLog collects what the plugin reports during body.
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

func TestReportUnreadOptions_NamesTheOptionsThatMoveTheRouteSet(t *testing.T) {
	root := t.TempDir()
	config := `import { vitePlugin as remix } from "@remix-run/dev";
export default {
  plugins: [remix({ appDirectory: "src", ignoredRouteFiles: ["**/*.css"] })],
};
`
	if err := os.WriteFile(filepath.Join(root, "remix-vite.config.mjs"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	got := captureLog(t, func() { reportUnreadOptions(root) })
	for _, want := range []string{"remix-vite.config.mjs", "appDirectory", "ignoredRouteFiles"} {
		if !strings.Contains(got, want) {
			t.Errorf("refusal %q does not name %s", got, want)
		}
	}
	if strings.Contains(got, "routes,") || strings.Contains(got, ", routes") {
		t.Errorf("refusal %q names routes, which this config does not set", got)
	}
}

// TestReportUnreadOptions_QuietOnTheConventionalConfig: the refusal has to stay
// rare enough to be read. A config that only relocates the build output honours
// the conventions, so it earns no line.
func TestReportUnreadOptions_QuietOnTheConventionalConfig(t *testing.T) {
	root := t.TempDir()
	config := `import { vitePlugin as remix } from "@remix-run/dev";
// Remix scans appDirectory (default "app") for routes.
export default {
  root: process.env["VITE_STAGING_ROOT"],
  plugins: [remix({ ssr: false, buildDirectory: process.env["VITE_OUT_DIR"] })],
};
`
	files := map[string]string{
		"remix-vite.config.mjs": config,
		"vitest.config.ts":      "export default { test: { routes: [] } };\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := captureLog(t, func() { reportUnreadOptions(root) }); got != "" {
		t.Errorf("reportUnreadOptions logged %q for a config that honours the conventions", got)
	}
}

// TestReportUnreadOptions_RoutesCallbackVsAliasKey: "routes" is a plain word.
// The refusal has to fire on the v2 routes callback, which really does move the
// route set, and stay quiet on an unrelated key of the same name in a config
// that happens to be Remix's.
func TestReportUnreadOptions_RoutesCallbackVsAliasKey(t *testing.T) {
	for _, tc := range []struct {
		name   string
		body   string
		report bool
	}{
		{"arrow callback", `routes: (defineRoutes) => defineRoutes(() => {}),`, true},
		{"async callback", `routes: async defineRoutes => defineRoutes(() => {}),`, true},
		{"method shorthand", `routes(defineRoutes) { return defineRoutes(() => {}); },`, true},
		{"resolve alias key", `resolve: { alias: { routes: "/app/routes" } },`, false},
		{"string option value", `routesDirectory: "app/routes",`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			config := "import { vitePlugin as remix } from \"@remix-run/dev\";\nexport default {\n  " +
				tc.body + "\n  plugins: [remix({ ssr: false })],\n};\n"
			if err := os.WriteFile(filepath.Join(root, "vite.config.ts"), []byte(config), 0o644); err != nil {
				t.Fatal(err)
			}
			got := captureLog(t, func() { reportUnreadOptions(root) })
			if reported := strings.Contains(got, "routes"); reported != tc.report {
				t.Errorf("reported routes = %v, want %v; log = %q", reported, tc.report, got)
			}
		})
	}
}
