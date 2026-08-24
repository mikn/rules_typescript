package vite_bundle_test

import (
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

func TestEsmBundle(t *testing.T) {
	tree := verify.New(t)

	js := tree.File("tests/vite_bundle/entry_vite_bundle/entry.es.js")
	// A real Vite bundle carries the tree-shaken, inlined source.
	js.Contains("add", "PI", "result", "sourceMappingURL")
	js.Excludes(verify.PlaceholderBundle)
	tree.File("tests/vite_bundle/entry_vite_bundle/entry.es.js.map").Exists()
}

func TestCjsBundleHasNoSourcemap(t *testing.T) {
	tree := verify.New(t)

	js := tree.File("tests/vite_bundle/entry_vite_cjs_bundle/entry.cjs.js")
	js.Contains("add")
	js.Excludes(verify.PlaceholderBundle)
	tree.Absent("tests/vite_bundle/entry_vite_cjs_bundle/entry.cjs.js.map")
}

func TestDefineAndExternalOpts(t *testing.T) {
	js := verify.New(t).File("tests/vite_bundle/entry_vite_opts_bundle/entry.es.js")
	js.Contains("add")
	js.Excludes(verify.PlaceholderBundle)
}

func TestMinifiedBundleIsSmaller(t *testing.T) {
	tree := verify.New(t)

	minified := tree.File("tests/vite_bundle/entry_vite_minified_bundle/entry.es.js")
	minified.Excludes(verify.PlaceholderBundle)

	plain := tree.File("tests/vite_bundle/entry_vite_bundle/entry.es.js").Size()
	small := minified.Size()
	if small < 0 || plain < 0 {
		return
	}
	if small >= plain {
		t.Errorf("minified bundle is %d bytes, not smaller than the unminified %d", small, plain)
	}
}

func TestSplitChunksProducesADirectoryOfChunks(t *testing.T) {
	verify.New(t).Dir("tests/vite_bundle/entry_vite_chunks_bundle").Glob("*.js")
}

// vite_bundler with node_modules = ":vite_deps" -- a tree not named
// "node_modules" -- has to be symlinked into place transparently.
func TestBundlerWithRenamedNodeModules(t *testing.T) {
	js := verify.New(t).File("tests/vite_bundle/entry_vite_alt_nm_bundle/entry.es.js")
	js.Contains("add")
	js.Excludes(verify.PlaceholderBundle)
}
