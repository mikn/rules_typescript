package typescript

// private_globals is the one ts_compile attribute a generator cannot derive: a
// .d.ts declares globals or it does not, and whether those are public is a
// decision nothing in the source states. So there is no directive that writes
// it, and the attribute is only useful if a hand-written value survives the
// next run -- which it does by being absent from ts_compile's MergeableAttrs,
// where a rule kind's managed set is declared.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrivateGlobals_SurvivesGeneration(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"package.json":     `{"name":"w"}` + "\n",
		"src/index.ts":     "export const version = 1;\n",
		"src/ambient.d.ts": "declare const STANDALONE_MODE: string;\n",
	})
	captureLog(t, func() { convergeGazelle(t, root) })

	build := filepath.Join(root, "src", "BUILD.bazel")
	before, err := os.ReadFile(build)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(before), `"ambient.d.ts"`) {
		t.Fatalf("generation did not put the ambient in srcs, so there is nothing to withhold:\n%s", before)
	}

	edited := strings.Replace(
		string(before),
		`    srcs = [`,
		"    private_globals = [\"ambient.d.ts\"],\n    srcs = [",
		1,
	)
	if edited == string(before) {
		t.Fatalf("could not find a srcs list to hand-edit in:\n%s", before)
	}
	if err := os.WriteFile(build, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	logged := captureLog(t, func() { convergeGazelle(t, root) })

	after, err := os.ReadFile(build)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after), "private_globals") {
		t.Errorf("Gazelle dropped the hand-written private_globals, which no directive can put back.\n"+
			"the run said:\n%s\nthe file is now:\n%s", logged, after)
	}
	if !strings.Contains(string(after), `"ambient.d.ts"`) {
		t.Errorf("the withheld src left srcs, where private_globals requires it:\n%s", after)
	}
}
