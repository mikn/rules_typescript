package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const (
	programRoot = binDir + "/pkg/app.program"
	forestDir   = binDir + "/pkg/app/node_modules"
)

// A fake exec root: a source, a forest under the bin dir and a tool under
// external/, which is where the sandbox puts the toolchain.
func newTsgoExecroot(t *testing.T, script string) (root, argv string) {
	t.Helper()
	root = t.TempDir()
	for rel, body := range map[string]string{
		"pkg/a.ts":                        "export {};\n",
		forestDir + "/zod/index.d.ts":     "export {};\n",
		binDir + "/pkg/app.tsconfig.json": "{}\n",
	} {
		writeFile(t, filepath.Join(root, rel), body)
	}
	if err := os.MkdirAll(filepath.Join(root, "external/tsgo"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, argv = fakeTool(t, filepath.Join(root, "external/tsgo"), "tsc", script)
	t.Chdir(root)
	return root, argv
}

func realpath(t *testing.T, p string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

// tsgo runs from the program root, where the exec root's top-level entries
// and the forest are links, and finds the source through them; afterwards the
// root is gone and the stamp is there.
func TestTsgoStep_RunsFromAProgramRoot(t *testing.T) {
	root, argv := newTsgoExecroot(t,
		"pwd >> \"$0.argv\"\nreadlink node_modules >> \"$0.argv\"\nls | tr '\\n' ' ' >> \"$0.argv\"\necho >> \"$0.argv\"\n"+
			"test -f pkg/a.ts && echo source-through-link >> \"$0.argv\"\n")
	stamp := binDir + "/pkg/app.tscheck"

	err := runTsgo([]string{"-root=" + programRoot, "-node_modules=" + forestDir, "-stamp=" + stamp, "--",
		"external/tsgo/tsc", "--project", binDir + "/pkg/app.tsconfig.json", "--noEmit"})
	if err != nil {
		t.Fatalf("runTsgo: %v", err)
	}

	got := recordedArgs(t, argv)
	if want := []string{"--project", binDir + "/pkg/app.tsconfig.json", "--noEmit"}; !reflect.DeepEqual(got[:3], want) {
		t.Errorf("tsgo ran with %q, want %q", got[:3], want)
	}
	if want := filepath.Join(realpath(t, root), programRoot); got[3] != want {
		t.Errorf("tsgo ran in %s, want the program root %s", got[3], want)
	}
	if want := filepath.Join(root, forestDir); got[4] != want {
		t.Errorf("node_modules -> %s, want the forest %s", got[4], want)
	}
	for _, entry := range []string{"bazel-out", "external", "node_modules", "pkg"} {
		if !strings.Contains(" "+got[5], " "+entry+" ") {
			t.Errorf("the program root lists %q, want %s in it", got[5], entry)
		}
	}
	if got[6] != "source-through-link" {
		t.Errorf("pkg/a.ts is not reachable from the program root: %q", got[6:])
	}
	if _, err := os.Stat(stamp); err != nil {
		t.Errorf("no stamp after a passing run: %v", err)
	}
	if _, err := os.Stat(programRoot); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the program root outlived the action: stat = %v", err)
	}
}

// The tool's exit status is the step's, no stamp is written for it, and the
// program root is removed all the same.
func TestTsgoStep_ExitCodeIsTheToolsAndNoStamp(t *testing.T) {
	newTsgoExecroot(t, "exit 3\n")
	stamp := binDir + "/pkg/app.tscheck"

	err := runTsgo([]string{"-root=" + programRoot, "-node_modules=" + forestDir, "-stamp=" + stamp, "--", "external/tsgo/tsc"})
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 3 {
		t.Errorf("runTsgo = %v, want tsgo's exit status 3", err)
	}
	if _, err := os.Stat(stamp); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a stamp was written for a failing run: stat = %v", err)
	}
	if _, err := os.Stat(programRoot); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the program root outlived the action: stat = %v", err)
	}
}
