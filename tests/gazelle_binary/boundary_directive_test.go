package gazellebinary_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bazelbuild/rules_go/go/runfiles"
)

// A ts_package_boundary value the ruleset does not know stops the run instead of
// leaving the inherited mode in force, and "index-only" -- the mode that was
// removed -- says so by name. Asserted through the built binary because the exit
// status is the half a unit test cannot see: a directive that silently does
// nothing leaves a tree compiling to something other than what its author wrote,
// and a Gazelle that exits 0 is a run whose output the author then trusts.
func TestUnknownBoundaryValueStopsTheRun(t *testing.T) {
	for _, tt := range []struct {
		name  string
		value string
		says  []string
	}{
		{
			name:  "the removed mode by name",
			value: "index-only",
			says:  []string{"index-only was removed", "every-dir", "tsconfig"},
		},
		{
			name:  "a typo",
			value: "every_dir",
			says:  []string{"unknown ts_package_boundary value", "every_dir", "every-dir", "tsconfig"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			out, err := runGazelle(t, "# gazelle:ts_package_boundary "+tt.value+"\n")
			if err == nil {
				t.Fatalf("ts_package_boundary %s was accepted and the run exited 0:\n%s", tt.value, out)
			}
			for _, want := range tt.says {
				if !strings.Contains(out, want) {
					t.Errorf("the refusal does not mention %q:\n%s", want, out)
				}
			}
		})
	}
}

// The control: both remaining modes are accepted, so the test above is about the
// value and not about the fixture.
func TestTheRemainingBoundaryModesAreAccepted(t *testing.T) {
	for _, mode := range []string{"", "every-dir", "tsconfig", "true"} {
		t.Run("mode="+mode, func(t *testing.T) {
			build := "# gazelle:ts_package_boundary\n"
			if mode != "" {
				build = "# gazelle:ts_package_boundary " + mode + "\n"
			}
			if out, err := runGazelle(t, build); err != nil {
				t.Fatalf("ts_package_boundary %s was refused: %v\n%s", mode, err, out)
			}
		})
	}
}

// runGazelle runs the exported binary over a one-package fixture whose root
// BUILD file carries build, and returns everything the run wrote.
func runGazelle(t *testing.T, build string) (string, error) {
	t.Helper()
	rf, err := runfiles.New()
	if err != nil {
		t.Fatalf("runfiles: %v", err)
	}
	binary, err := rf.Rlocation(os.Getenv("GAZELLE_TYPESCRIPT"))
	if err != nil {
		t.Fatalf("GAZELLE_TYPESCRIPT: %v", err)
	}

	dir := t.TempDir()
	for name, body := range map[string]string{
		"BUILD.bazel":   build,
		"tsconfig.json": `{"compilerOptions":{"lib":["es2022"]}}` + "\n",
		"index.ts":      "export const answer = 42;\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	cmd := exec.Command(binary, "-repo_root", dir, dir)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}
