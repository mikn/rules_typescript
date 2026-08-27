// What a *.module.css import resolves to when it is SERVED rather than bundled.
//
// A dev server that scopes a stylesheet itself disagrees with the build about
// every class name in it, and the .d.ts `bazel build` typechecked against then
// describes neither. So the names the running server hands the browser are
// compared against the export map css_module wrote -- the same file the .d.ts
// was generated from.
//
// The server serves the WORKSPACE, and the workspace here is a throwaway tree
// with no bazel-bin export map in it. That is the interesting case rather than a
// contrived one: it is the shape of a real edit-and-reload loop, and under Vite
// the names still match, because the generator is a pure function of the
// stylesheet's bytes and the bytes are the ones css_module compiled.
//
// Under oj they do not, and this pins why. oj loads the plugin -- its host
// reports it active -- but adopts no `css.*` from a Vite config, so the
// `generateScopedName` the plugin's config() hook returns is dropped, and oj's
// own CSS-modules pass names the classes after the FILE:
// `panel-module_panel_4JHZ6q`. Which is both a different name from the .d.ts's
// and a name derived from a path, the thing css_module's content hash exists to
// avoid. Closing it needs oj to adopt css.modules, or needs the ruleset to emit
// the scoped stylesheet itself and take the naming away from the server -- a
// design the dev server's Bazel-out-of-the-inner-loop property argues against.
package dev_server_test

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

func TestServedCssModuleNames(t *testing.T) {
	tree := verify.New(t)
	target := env(t, "DEV_TARGET")
	impl := env(t, "DEV_IMPL")

	launcher := tree.File("tests/dev_server/" + target + "_launcher")
	stylesheet := tree.File("tests/dev_server/panel.module.css")
	exportMap := tree.File("tests/dev_server/panel.module.css.exports.json")
	for _, f := range []verify.File{launcher, stylesheet, exportMap} {
		if !f.Exists() {
			t.FailNow()
		}
	}

	exports := map[string]string{}
	exportMap.JSON(&exports)
	if len(exports) == 0 {
		t.Fatal("panel.module.css.exports.json is empty, so there is nothing to compare")
	}

	tmp := t.TempDir()
	ws := filepath.Join(tmp, "ws")
	mkdir(t, filepath.Join(ws, "bazel-bin"))
	// The bytes css_module compiled, not a paraphrase of them: the scoped name
	// is a hash of exactly these.
	write(t, filepath.Join(ws, "panel.module.css"), stylesheet.Text())
	write(t, filepath.Join(ws, "css_entry.js"),
		"import styles from \"./panel.module.css\";\nexport { styles };\n")

	srv := start(t, launcher.Abs(), ws, tmp)
	base := srv.awaitHTTP(t, "/css_entry.js")

	served := get(t, base, "/panel.module.css")
	if served.status != 200 {
		t.Fatalf("GET /panel.module.css answered %d\nbody:\n%s\n%s",
			served.status, served.body, srv.log(t))
	}

	if impl == "oj" {
		assertOjKeepsItsOwnNames(t, srv, served.body, exports)
		return
	}

	// Key next to value, not each of them somewhere in the body: a server that
	// exported the semantic names and mentioned the scoped ones only inside the
	// stylesheet text would pass a containment check and still be wrong.
	for key, scoped := range exports {
		if !binds(served.body, key, scoped) {
			t.Errorf("the dev server does not map %q to %q, which is the name "+
				"panel.module.css.d.ts was generated from\nserved:\n%s\n%s",
				key, scoped, served.body, srv.log(t))
		}
	}
}

// assertOjKeepsItsOwnNames pins the gap, so that oj gaining the capability fails
// this lane and says so rather than passing quietly with the wrong assertion.
func assertOjKeepsItsOwnNames(t *testing.T, srv *server, body string, exports map[string]string) {
	t.Helper()

	agrees := true
	for key, scoped := range exports {
		if !binds(body, key, scoped) {
			agrees = false
		}
	}
	if agrees {
		t.Errorf("oj now serves the names css_module generated the .d.ts from. "+
			"Delete this branch and let the Vite assertion run for oj too.\nserved:\n%s", body)
		return
	}

	// The KEYS are still right -- oj reads the same stylesheet -- so the typed
	// API describes the right property set and only the strings are another
	// server's.
	for key := range exports {
		if !regexp.MustCompile(`["']` + regexp.QuoteMeta(key) + `["']\s*:`).MatchString(body) {
			t.Errorf("oj does not export %q at all, so the .d.ts is wrong about the "+
				"property set and not merely about the strings\nserved:\n%s\n%s",
				key, body, srv.log(t))
		}
	}

	// And oj's name is derived from the FILE, which is the path dependence
	// css_module's content hash exists to avoid: the same stylesheet under
	// another name would be scoped differently.
	if !strings.Contains(body, "panel-module_panel_") {
		t.Errorf("oj's scoping changed shape; this lane's description of the gap "+
			"is now wrong\nserved:\n%s", body)
	}
}

// binds reports whether the served module gives key the value scoped. Two
// spellings, because a server is free to pick either: Vite hoists an
// identifier-shaped key to `export const panel = "..."` and leaves the rest in
// the default export's object literal.
func binds(body, key, scoped string) bool {
	k, v := regexp.QuoteMeta(key), regexp.QuoteMeta(scoped)
	for _, expr := range []string{
		`export\s+const\s+` + k + `\s*=\s*["']` + v + `["']`,
		`["']?` + k + `["']?\s*:\s*["']` + v + `["']`,
	} {
		if regexp.MustCompile(expr).MatchString(body) {
			return true
		}
	}
	return false
}
