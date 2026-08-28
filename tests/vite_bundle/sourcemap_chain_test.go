package vite_bundle_test

import (
	"strings"
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

type sourceMap struct {
	Sources        []string  `json:"sources"`
	SourcesContent []*string `json:"sourcesContent"`
}

// The chain is .ts → oxc .js.map → Vite bundle .js.map → browser.
func TestSourcemapChain(t *testing.T) {
	tree := verify.New(t)

	tree.File("tests/vite_bundle/entry_vite_bundle/entry.es.js").Contains("sourceMappingURL")

	// The Vite map points at the oxc-compiled .js, not at the raw .ts.
	bundleMap := tree.File("tests/vite_bundle/entry_vite_bundle/entry.es.js.map")
	var bundle sourceMap
	bundleMap.JSON(&bundle)
	requireSourceSuffix(t, bundleMap.Name(), bundle.Sources, ".js")
	requireSourcesContent(t, bundleMap.Name(), bundle.SourcesContent)

	// Each of those .js files has a sibling map that points back at the .ts.
	for _, rel := range []string{"tests/vite_bundle/lib.js.map", "tests/vite_bundle/entry.js.map"} {
		f := tree.File(rel)
		var oxc sourceMap
		f.JSON(&oxc)
		requireSourceSuffix(t, rel, oxc.Sources, ".ts")
		requireSourcesContent(t, rel, oxc.SourcesContent)
	}
}

func requireSourceSuffix(t *testing.T, name string, sources []string, suffix string) {
	t.Helper()
	for _, s := range sources {
		if strings.HasSuffix(strings.SplitN(s, "?", 2)[0], suffix) {
			return
		}
	}
	t.Errorf("%s: no source ends in %s: %q", name, suffix, sources)
}

// A source-less debugging session needs the content, and a map full of nulls
// looks identical to a populated one from the outside.
func requireSourcesContent(t *testing.T, name string, content []*string) {
	t.Helper()
	for _, c := range content {
		if c != nil && *c != "" {
			return
		}
	}
	t.Errorf("%s: sourcesContent holds %d entries, none populated", name, len(content))
}
