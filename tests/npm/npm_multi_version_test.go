package npm_test

import (
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

// The fixture lockfile has @vitest/pretty-format at 3.0.9 and 3.2.4, which is
// what exercises the multi-version label generation:
//
//	@npm//:vitest_pretty-format_3_0_9  versioned target for 3.0.9
//	@npm//:vitest_pretty-format_3_2_4  versioned target for 3.2.4
//	@npm//:vitest_pretty-format        alias to the highest version
func TestVersionedLabelsStayApart(t *testing.T) {
	tree := verify.New(t)

	for _, c := range []struct{ dirSuffix, want string }{
		{"vitest_pretty-format__3_0_9", "3.0.9"},
		{"vitest_pretty-format__3_2_4", "3.2.4"},
	} {
		dir := tree.FoundDir("*" + c.dirSuffix)
		var pkg struct {
			Version string `json:"version"`
		}
		dir.File("package.json").JSON(&pkg)
		if pkg.Version != c.want {
			t.Errorf("%s/package.json is version %q, want %q", dir.Name(), pkg.Version, c.want)
		}
	}
}
