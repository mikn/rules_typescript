package main

import (
	"errors"
	"io"
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

// tsgo runs from the program root and finds the source through its links;
// afterwards the root is gone and the stamp is there.
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

// With -check, the listing tsgo prints is read against the manifest and never
// reaches the action's stdout; an undeclared edge is the step's failure.
func TestTsgoStep_ChecksTheListingAgainstTheManifest(t *testing.T) {
	listing := "bazel-out/k8-fastbuild/bin/pkg/hidden.d.ts\n" +
		"   Imported via \"./hidden\" from file 'pkg/a.ts'\n" +
		"pkg/a.ts\n   Root file specified for compilation\n"
	root, _ := newTsgoExecroot(t, "cat "+binDir+"/pkg/listing.txt\n")
	writeFile(t, filepath.Join(root, binDir, "pkg/listing.txt"), listing)
	manifest := binDir + "/pkg/app.ownership"
	stamp := binDir + "/pkg/app.tscheck"
	run := func() error {
		return runTsgo([]string{"-root=" + programRoot, "-node_modules=" + forestDir,
			"-check=" + manifest, "-stamp=" + stamp, "--", "external/tsgo/tsc"})
	}

	writeFile(t, filepath.Join(root, manifest),
		"label\t//pkg:app\nown\tpkg/a.ts\ndirect\t//pkg:lib\n"+
			"file\t//pkg:hidden\t"+binDir+"/pkg/hidden.d.ts\n")
	err := run()
	if err == nil || !strings.Contains(err.Error(), "add //pkg:hidden to deps") {
		t.Errorf("runTsgo = %v, want the undeclared edge naming //pkg:hidden", err)
	}
	if _, err := os.Stat(stamp); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a stamp was written for a failing check: stat = %v", err)
	}

	writeFile(t, filepath.Join(root, manifest),
		"label\t//pkg:app\nown\tpkg/a.ts\ndirect\t//pkg:hidden\n"+
			"file\t//pkg:hidden\t"+binDir+"/pkg/hidden.d.ts\n")
	if err := run(); err != nil {
		t.Errorf("runTsgo with the dep declared: %v", err)
	}
	if _, err := os.Stat(stamp); err != nil {
		t.Errorf("no stamp after a passing check: %v", err)
	}
}

// tsgo's own failure is relayed with its diagnostics and without the listing.
func TestTsgoStep_AFailingTsgoRelaysItsDiagnostics(t *testing.T) {
	root, _ := newTsgoExecroot(t, "cat "+binDir+"/pkg/listing.txt\nexit 2\n")
	writeFile(t, filepath.Join(root, binDir, "pkg/listing.txt"),
		"pkg/a.ts\n   Root file specified for compilation\n"+
			"pkg/a.ts(1,8): error TS2307: Cannot find module './gone'.\n")
	manifest := binDir + "/pkg/app.ownership"
	writeFile(t, filepath.Join(root, manifest),
		"label\t//pkg:app\nown\tpkg/a.ts\n")
	stdout := captureStdout(t)

	err := runTsgo([]string{"-root=" + programRoot, "-node_modules=" + forestDir,
		"-check=" + manifest, "--", "external/tsgo/tsc"})
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 2 {
		t.Errorf("runTsgo = %v, want tsgo's exit status 2", err)
	}
	out := stdout()
	if !strings.Contains(out, "error TS2307") {
		t.Errorf("the diagnostic is not relayed:\n%s", out)
	}
	if strings.Contains(out, "Root file specified") {
		t.Errorf("the listing reached stdout:\n%s", out)
	}
}

func captureStdout(t *testing.T) func() string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stdout
	os.Stdout = w
	return func() string {
		os.Stdout = saved
		w.Close()
		data, err := io.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
}
