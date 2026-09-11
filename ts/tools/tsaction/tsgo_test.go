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
	programRoot  = binDir + "/pkg/app.program"
	rootImporter = binDir + "/node_modules"
	subImporter  = binDir + "/pkg/sub/node_modules"
	manifest     = binDir + "/pkg/app.ownership"
)

func tsgoArgs(rest ...string) []string {
	flags := []string{
		"-root=" + programRoot,
		"-node_modules=" + subImporter,
		"-node_modules=" + rootImporter,
		"-check=" + manifest,
	}
	return append(flags, rest...)
}

// A fake exec root: sources, the importers' node_modules and pkg's outputs
// under the bin dir: the manifest as written and as built, a declaration.
func newTsgoExecroot(t *testing.T, script string) (root, argv string) {
	t.Helper()
	root = t.TempDir()
	for rel, body := range map[string]string{
		"pkg/a.ts":                        "export {};\n",
		"pkg/lib.ts":                      "export {};\n",
		"pkg/package.json":                `{"exports": "./lib.ts"}` + "\n",
		"pkg/sub/b.ts":                    "export {};\n",
		"pkg/other/c.ts":                  "export {};\n",
		rootImporter + "/zod/index.d.ts":  "export {};\n",
		subImporter + "/ms/index.d.ts":    "export {};\n",
		binDir + "/pkg/app.tsconfig.json": "{}\n",
		binDir + "/pkg/package.json":      `{"exports": "./lib.ts"}` + "\n",
		binDir + "/pkg/app.package.json":  `{"exports": "./lib.js"}` + "\n",
		binDir + "/pkg/lib.package.json":  `{"exports": "./lib.js"}` + "\n",
		binDir + "/pkg/lib.d.ts":          "export {};\n",
	} {
		writeFile(t, filepath.Join(root, rel), body)
	}
	writeFile(t, filepath.Join(root, manifest),
		"label\t//pkg:app\nown\tpkg/a.ts\n")
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

// The root importer at node_modules, pkg/sub's at its directory made real with
// the sibling kept as a link; afterwards the root is gone, the stamp is there.
func TestTsgoStep_RunsFromAProgramRoot(t *testing.T) {
	root, argv := newTsgoExecroot(t,
		"pwd >> \"$0.argv\"\nreadlink node_modules >> \"$0.argv\"\n"+
			"readlink pkg/sub/node_modules >> \"$0.argv\"\n"+
			"ls | tr '\\n' ' ' >> \"$0.argv\"\necho >> \"$0.argv\"\n"+
			"test -d pkg -a ! -L pkg && echo pkg-is-real >> \"$0.argv\"\n"+
			"readlink pkg/other >> \"$0.argv\"\n"+
			"test -f pkg/a.ts && echo source-through-link >> \"$0.argv\"\n")
	stamp := binDir + "/pkg/app.tscheck"

	err := runTsgo(tsgoArgs("-stamp="+stamp, "--", "external/tsgo/tsc",
		"--project", binDir+"/pkg/app.tsconfig.json", "--noEmit"))
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
	if want := filepath.Join(root, rootImporter); got[4] != want {
		t.Errorf("node_modules -> %s, want the root importer's %s", got[4], want)
	}
	if want := filepath.Join(root, subImporter); got[5] != want {
		t.Errorf("pkg/sub/node_modules -> %s, want the importer's %s", got[5], want)
	}
	for _, entry := range []string{"bazel-out", "external", "node_modules", "pkg"} {
		if !strings.Contains(" "+got[6], " "+entry+" ") {
			t.Errorf("the program root lists %q, want %s in it", got[6], entry)
		}
	}
	if got[7] != "pkg-is-real" {
		t.Errorf("pkg is not a real directory on the way to pkg/sub: %q", got[7:])
	}
	if want := filepath.Join(root, "pkg/other"); got[8] != want {
		t.Errorf("pkg/other -> %s, want the exec root's sibling %s", got[8], want)
	}
	if got[9] != "source-through-link" {
		t.Errorf("pkg/a.ts is not reachable from the program root: %q", got[9:])
	}
	if _, err := os.Stat(stamp); err != nil {
		t.Errorf("no stamp after a passing run: %v", err)
	}
	if _, err := os.Stat(programRoot); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the program root outlived the action: stat = %v", err)
	}
}

// -overlay lays a dep's outputs over its package, -manifest its package.json
// as built over the src; the importer's node_modules and the root are left.
func TestTsgoStep_LaysADepsOutputsOverItsDirectory(t *testing.T) {
	root, argv := newTsgoExecroot(t,
		"readlink pkg/package.json >> \"$0.argv\"\n"+
			"readlink pkg/lib.d.ts >> \"$0.argv\"\n"+
			"readlink pkg/lib.ts >> \"$0.argv\"\n"+
			"readlink pkg/sub/node_modules >> \"$0.argv\"\n"+
			"test -e pkg/app.program && echo root-linked >> \"$0.argv\" || "+
			"echo root-skipped >> \"$0.argv\"\n"+
			"test -d pkg/sub -a ! -L pkg/sub && echo sub-is-real >> \"$0.argv\"\n")

	err := runTsgo(tsgoArgs("-overlay="+binDir+"/pkg",
		"-manifest="+binDir+"/pkg/app.package.json",
		"--", "external/tsgo/tsc"))
	if err != nil {
		t.Fatalf("runTsgo: %v", err)
	}
	got := recordedArgs(t, argv)[1:]
	want := []string{
		filepath.Join(root, binDir, "pkg/app.package.json"),
		filepath.Join(root, binDir, "pkg/lib.d.ts"),
		filepath.Join(root, "pkg/lib.ts"),
		filepath.Join(root, subImporter),
		"root-skipped",
		"sub-is-real",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the program root reads\n%q\nwant\n%q", got, want)
	}
}

// An overlaid declaration is listed by its path under the root; the check reads
// it through the link. Without the overlay the listed file is nobody's.
func TestTsgoStep_ChecksAnOverlaidFileByItsOutput(t *testing.T) {
	listing := "pkg/lib.d.ts\n" +
		"   Imported via \"./lib\" from file 'pkg/a.ts'\n" +
		"pkg/a.ts\n   Root file specified for compilation\n"
	root, _ := newTsgoExecroot(t, "cat "+binDir+"/pkg/listing.txt\n")
	writeFile(t, filepath.Join(root, binDir, "pkg/listing.txt"), listing)
	writeFile(t, filepath.Join(root, manifest),
		"label\t//pkg:app\nown\tpkg/a.ts\ndirect\t//pkg:lib\n"+
			"file\t//pkg:lib\t"+binDir+"/pkg/lib.d.ts\n")

	err := runTsgo(tsgoArgs("-overlay="+binDir+"/pkg", "--", "external/tsgo/tsc"))
	if err != nil {
		t.Errorf("runTsgo with the dep overlaid: %v", err)
	}
	err = runTsgo(tsgoArgs("--", "external/tsgo/tsc"))
	if err == nil || !strings.Contains(err.Error(), "no src, dep or npm package") {
		t.Errorf("runTsgo without the overlay = %v, want the file unowned", err)
	}
}

// The manifest -manifest lays at pkg/package.json is listed by that path; the
// check reads it through the link, so the dep's <name>.package.json owns it.
func TestTsgoStep_ChecksALaidOverManifestByItsOutput(t *testing.T) {
	listing := "pkg/package.json\n" +
		"   Imported via \"./package.json\" from file 'pkg/a.ts'\n" +
		"pkg/a.ts\n   Root file specified for compilation\n"
	root, _ := newTsgoExecroot(t, "cat "+binDir+"/pkg/listing.txt\n")
	writeFile(t, filepath.Join(root, binDir, "pkg/listing.txt"), listing)
	asWritten := "label\t//pkg:app\nown\tpkg/a.ts\ndirect\t//pkg:lib\n" +
		"file\t//pkg:lib\t" + binDir + "/pkg/package.json\n"
	args := tsgoArgs("-manifest="+binDir+"/pkg/lib.package.json",
		"--", "external/tsgo/tsc")

	writeFile(t, filepath.Join(root, manifest),
		asWritten+"file\t//pkg:lib\t"+binDir+"/pkg/lib.package.json\n")
	if err := runTsgo(args); err != nil {
		t.Errorf("runTsgo with the manifest as built owned: %v", err)
	}
	writeFile(t, filepath.Join(root, manifest), asWritten)
	err := runTsgo(args)
	if err == nil || !strings.Contains(err.Error(), "no src, dep or npm package") {
		t.Errorf("runTsgo with the src alone owned = %v, want the file unowned", err)
	}
}

func TestBinRelative(t *testing.T) {
	for _, c := range []struct{ binPath, want, importer string }{
		{binDir + "/pkg/sub/node_modules", "pkg/sub/node_modules", "pkg/sub"},
		{binDir + "/node_modules", "node_modules", ""},
		{binDir + "/pkg", "pkg", ""},
		{binDir, "", ""},
	} {
		if got := binRelative(c.binPath); got != c.want {
			t.Errorf("binRelative(%q) = %q, want %q", c.binPath, got, c.want)
		}
		if strings.HasSuffix(c.binPath, "node_modules") {
			if got := importerDir(c.binPath); got != c.importer {
				t.Errorf("importerDir(%q) = %q, want %q", c.binPath, got, c.importer)
			}
		}
	}
}

// The tool's exit status is the step's, no stamp is written for it, and the
// program root is removed all the same.
func TestTsgoStep_ExitCodeIsTheToolsAndNoStamp(t *testing.T) {
	newTsgoExecroot(t, "exit 3\n")
	stamp := binDir + "/pkg/app.tscheck"

	err := runTsgo(tsgoArgs("-stamp="+stamp, "--", "external/tsgo/tsc"))
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
	stamp := binDir + "/pkg/app.tscheck"
	args := tsgoArgs("-stamp="+stamp, "--", "external/tsgo/tsc")

	writeFile(t, filepath.Join(root, manifest),
		"label\t//pkg:app\nown\tpkg/a.ts\ndirect\t//pkg:lib\n"+
			"file\t//pkg:hidden\t"+binDir+"/pkg/hidden.d.ts\n")
	err := runTsgo(args)
	if err == nil || !strings.Contains(err.Error(), "add //pkg:hidden to deps") {
		t.Errorf("runTsgo = %v, want the undeclared edge naming //pkg:hidden", err)
	}
	if _, err := os.Stat(stamp); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a stamp was written for a failing check: stat = %v", err)
	}

	writeFile(t, filepath.Join(root, manifest),
		"label\t//pkg:app\nown\tpkg/a.ts\ndirect\t//pkg:hidden\n"+
			"file\t//pkg:hidden\t"+binDir+"/pkg/hidden.d.ts\n")
	if err := runTsgo(args); err != nil {
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
	stdout := captureStdout(t)

	err := runTsgo(tsgoArgs("--", "external/tsgo/tsc"))
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

// A failing tsgo that printed no diagnostic -- a usage message, a crash line
// -- has its whole output relayed; only a parsed listing stays off stdout.
func TestTsgoStep_AFailingTsgoWithNoDiagnosticRelaysItsOutput(t *testing.T) {
	newTsgoExecroot(t, "echo 'tsc: unknown option --explainFile'\nexit 1\n")
	stdout := captureStdout(t)

	err := runTsgo(tsgoArgs("--", "external/tsgo/tsc"))
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Errorf("runTsgo = %v, want tsgo's exit status 1", err)
	}
	if out := stdout(); !strings.Contains(out, "unknown option") {
		t.Errorf("tsgo's output is not relayed:\n%s", out)
	}
}

func TestTsgoStep_NeedsTheManifest(t *testing.T) {
	_, argv := newTsgoExecroot(t, "echo ran\n")

	err := runTsgo([]string{"-root=" + programRoot,
		"-node_modules=" + rootImporter, "--", "external/tsgo/tsc"})
	if err == nil || !strings.Contains(err.Error(), "-check=FILE") {
		t.Errorf("runTsgo without -check = %v, want an error", err)
	}
	if _, err := os.Stat(argv); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("tsgo ran without a manifest: stat = %v", err)
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
