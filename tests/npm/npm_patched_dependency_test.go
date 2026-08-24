package npm_test

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

// The registry tarball for nanoid@3.3.11 is byte-identical whether or not the
// patch is applied -- `packages:` records the integrity of the unpatched publish
// -- so nothing but the file content can tell the two apart. The fixture patch
// rewrites "sideEffects": false into an array -- a realistic patch, since a
// wrong sideEffects lets a bundler tree-shake away a module with side effects.
func TestPatchReachesTheFilesBazelHandsOut(t *testing.T) {
	tree := verify.New(t)

	var pkg struct {
		SideEffects json.RawMessage `json:"sideEffects"`
	}
	f := tree.FoundFile("*nanoid__3_3_11/package.json")
	f.JSON(&pkg)

	want := []string{"./index.js", "./index.cjs"}
	var got []string
	if err := json.Unmarshal(pkg.SideEffects, &got); err != nil || !slices.Equal(got, want) {
		t.Errorf("%s was not patched: sideEffects is %s, want %q\n"+
			"  The published nanoid@3.3.11 has \"sideEffects\": false; the patch in\n"+
			"  tests/npm/patches/nanoid@3.3.11.patch replaces it with the array.",
			f.Name(), pkg.SideEffects, want)
	}
}
