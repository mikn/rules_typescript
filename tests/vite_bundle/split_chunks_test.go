package vite_bundle_test

import (
	"path"
	"strings"
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

// split_chunks has to be visible in the bundle, not in the generated config: the
// config named a Vite plugin that had been removed for two majors, and no
// assertion over the config text could tell.
func TestSplitChunksSeparatesVendorFromFirstParty(t *testing.T) {
	dir := verify.New(t).Dir("tests/vite_bundle/vendor_split_bundle")

	entry := dir.File("vendor_entry.es.js")
	entry.Contains("add", "PI")

	// The chunk's extension is Vite's to pick, not ours -- lib mode derives it
	// from the nearest package.json "type" -- so every emitted module is a
	// candidate and the entry is the only one named.
	var vendor []verify.File
	for _, f := range dir.Glob("*") {
		if path.Base(f.Name()) == "vendor_entry.es.js" {
			continue
		}
		vendor = append(vendor, f)
	}
	if len(vendor) != 1 {
		names := make([]string, 0, len(vendor))
		for _, f := range vendor {
			names = append(names, f.Name())
		}
		t.Fatalf("want exactly one chunk beside the entry, got %d: %s",
			len(vendor), strings.Join(names, ", "))
	}

	// nanoid is the only npm package the entry imports, so it is the whole of
	// what "vendor" can mean here.
	vendor[0].Contains("getRandomValues")
	// First-party code in the vendor chunk means the split ran but grouped the
	// wrong modules -- the failure a config-text assertion cannot see.
	vendor[0].Excludes("function add", "3.14159")
	entry.Contains(path.Base(vendor[0].Name()))
}

// Same sources, split_chunks off: one module, vendor inlined. Without this the
// test above would pass on a bundler that always splits.
func TestWithoutSplitChunksVendorIsInlined(t *testing.T) {
	js := verify.New(t).File("tests/vite_bundle/vendor_unsplit_bundle/vendor_entry.es.js")
	js.Contains("function add", "3.14159", "getRandomValues")
	js.Excludes(verify.PlaceholderBundle)
}
