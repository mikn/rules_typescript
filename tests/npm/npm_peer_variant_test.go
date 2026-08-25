package npm_test

import (
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

// The fixture lockfile resolves ansi-styles@6.2.3 twice -- (ansi-regex@5.0.1) for
// the member app-a, (ansi-regex@6.2.2) for app-b -- and has the two members
// declare different majors of ansi-regex directly as well.
//
// Both failures this guards against are silent. Merging the two snapshots onto
// `ansi-styles@6.2.3` leaves one dependency dict, so both members build against
// whichever version the reader saw last; a flat highest-version-wins hub hands
// both members 6.2.2. Every version involved is real, so the only evidence is
// the version actually present in the tree.
func TestPeerVariantsAndPerImporterResolution(t *testing.T) {
	tree := verify.New(t)

	for _, c := range []struct{ nodeModules, pkg, want, why string }{
		{"peer_variant_a_node_modules", "ansi-regex", "5.0.1",
			"app-a resolved ansi-styles@6.2.3(ansi-regex@5.0.1); 6.2.2 means the two peer variants were merged."},
		{"peer_variant_b_node_modules", "ansi-regex", "6.2.2",
			"app-b resolved ansi-styles@6.2.3(ansi-regex@6.2.2); 5.0.1 means the two peer variants were merged."},
		{"importer_a_node_modules", "ansi-regex", "5.0.1",
			"app-a declared ^5.0.0; 6.2.2 means the label is a flat highest-version-wins hub."},
		{"importer_b_node_modules", "ansi-regex", "6.2.2",
			"app-b declared ^6.0.0."},
	} {
		nm := tree.FoundDir("*/" + c.nodeModules)
		var pkg struct {
			Version string `json:"version"`
		}
		nm.File(c.pkg + "/package.json").JSON(&pkg)
		if pkg.Version != c.want {
			t.Errorf("%s/%s is %s, want %s\n  %s", nm.Name(), c.pkg, pkg.Version, c.want, c.why)
		}
	}

	// A peer suffix on its own costs nothing. app-a's tree holds exactly one
	// resolution of ansi-styles, and it is a suffixed one, so it keeps the flat
	// top-level directory and the tree has no store at all. Only a SECOND
	// resolution of one name needs one -- which is what keeps the decoration-only
	// suffixes pnpm writes, `(patch_hash=...)` among them, off the layout.
	a := tree.FoundDir("*/peer_variant_a_node_modules")
	a.File("ansi-styles/package.json").Contains(`"version": "6.2.3"`)
	if store := tree.Find("*/peer_variant_a_node_modules/.pnpm"); len(store) != 0 {
		t.Errorf("peer_variant_a_node_modules has a .pnpm store (%v); one resolution per name needs none", store)
	}
}
