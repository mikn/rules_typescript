package npm_test

import (
	"path/filepath"
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

// app-a and app-b declare different majors of ansi-regex and resolve
// ansi-styles@6.2.3 against each; every version is real, so the links tell.
func TestPeerVariantsAndPerImporterResolution(t *testing.T) {
	tree := verify.New(t)

	for _, c := range []struct{ importer, want, why string }{
		{"app_a", "5.0.1", "app-a declared ^5.0.0 and resolved ansi-styles " +
			"against it; 6.2.2 means the two peer variants were merged."},
		{"app_b", "6.2.2", "app-b declared ^6.0.0 and resolved ansi-styles " +
			"against it; 5.0.1 means the two peer variants were merged."},
	} {
		nm := tree.FoundDir("*/" + c.importer + "/node_modules")
		if got := versionOf(t, filepath.Join(nm.Abs(), "ansi-regex")); got != c.want {
			t.Errorf("%s links ansi-regex %s, want %s\n  %s", c.importer,
				got, c.want, c.why)
		}
		peer := beside(t, filepath.Join(nm.Abs(), "ansi-styles"), "ansi-regex")
		if got := versionOf(t, peer); got != c.want {
			t.Errorf("%s's ansi-styles resolves ansi-regex %s, want %s\n  %s",
				c.importer, got, c.want, c.why)
		}
	}
}
