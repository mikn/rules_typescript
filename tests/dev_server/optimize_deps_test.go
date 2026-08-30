// Vite resolves optimizeDeps.include with no importer and no plugin container:
// createIdResolver builds a container of the alias and native resolve plugins
// only, and calls it with `undefined` for the importer, so the walk starts at
// `root`. The bazel:npm-resolve plugin cannot see that path -- it is a
// resolveId hook, and no hook runs there.
//
// So this is the one behaviour that separates the two mechanisms. Before the
// launcher linked the npm tree in at the workspace root, a config naming a
// package here logged "Failed to resolve dependency" and the package was left
// un-prebundled; a CJS one then reached the browser raw.
package dev_server_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

func TestOptimizeDepsIncludeResolvesThroughTheWorkspaceLink(t *testing.T) {
	tree := verify.New(t)
	node := tree.File("ts/toolchain/node_resolved/node")
	launcher := tree.File("tests/dev_server/dev_with_optimize_deps_launcher")
	for _, f := range []verify.File{node, launcher} {
		if !f.Exists() {
			t.FailNow()
		}
	}

	tmp := t.TempDir()
	ws := filepath.Join(tmp, "ws")
	mkdir(t, filepath.Join(ws, "bazel-bin"))
	write(t, filepath.Join(ws, "npm_entry.js"), "import { z } from \"zod\";\nexport { z };\n")

	srv := start(t, launcher.Abs(), ws, tmp)
	base := srv.awaitHTTP(t, "/npm_entry.js")

	if log := srv.log(t); strings.Contains(log, "Failed to resolve dependency") {
		t.Errorf("a package named in optimizeDeps.include did not resolve:\n%s", log)
	}

	r := get(t, base, "/npm_entry.js")
	if r.status != 200 {
		t.Fatalf("GET /npm_entry.js returned %d, want 200\n%s\n%s", r.status, r.body, srv.log(t))
	}
	dep := depURL(r.body)
	if dep == "" {
		t.Fatalf("nothing in the response points at a resolved dependency:\n%s", r.body)
	}
	// Pre-bundled rather than served from the tree: that is the observable
	// difference between the optimiser having resolved it and having skipped it.
	if !strings.Contains(dep, "/deps/") {
		t.Errorf("`zod` is in optimizeDeps.include but was served from %q, "+
			"not the dependency cache", dep)
	}
	if m := get(t, base, dep); m.status != 200 {
		t.Errorf("the pre-bundled dependency %s answers %d, want 200\n%s", dep, m.status, m.body)
	}
}
