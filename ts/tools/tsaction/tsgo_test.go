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
		"-source=pkg/a.ts",
		"-source=pkg/package.json",
		"-node_modules=" + subImporter,
		"-node_modules=" + rootImporter,
		"-check=" + manifest,
	}
	return append(flags, rest...)
}

// A fake exec root: the action's sources beside files it does not name, the
// importers' node_modules and pkg's outputs (both manifests, a declaration).
func newTsgoExecroot(t *testing.T, script string) (root, argv string) {
	t.Helper()
	root = linkedDir(t)
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

func linkedDir(t *testing.T) string {
	t.Helper()
	link := filepath.Join(t.TempDir(), "execroot")
	if err := os.Symlink(t.TempDir(), link); err != nil {
		t.Fatal(err)
	}
	return link
}

func realpath(t *testing.T, p string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

// The root holds each -source at its path, the output tree whole, the chain's
// node_modules, and no file the action did not name; then it is gone, stamped.
func TestTsgoStep_RunsFromAProgramRootOfTheSourcesNamed(t *testing.T) {
	root, argv := newTsgoExecroot(t,
		"pwd >> \"$0.argv\"\nreadlink node_modules >> \"$0.argv\"\n"+
			"readlink pkg/sub/node_modules >> \"$0.argv\"\n"+
			"ls | tr '\\n' ' ' >> \"$0.argv\"\necho >> \"$0.argv\"\n"+
			"test -d pkg -a ! -L pkg && echo pkg-is-real >> \"$0.argv\"\n"+
			"readlink pkg/a.ts >> \"$0.argv\"\n"+
			"readlink bazel-out >> \"$0.argv\"\n"+
			"test -e pkg/lib.ts || echo lib-absent >> \"$0.argv\"\n"+
			"test -e pkg/other || echo other-absent >> \"$0.argv\"\n")
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
	if want := filepath.Join(root, programRoot); got[3] != want {
		t.Errorf("tsgo ran in %s, want the program root %s", got[3], want)
	}
	if want := filepath.Join(root, rootImporter); got[4] != want {
		t.Errorf("node_modules -> %s, want the root importer's %s", got[4], want)
	}
	if want := filepath.Join(root, subImporter); got[5] != want {
		t.Errorf("pkg/sub/node_modules -> %s, want the importer's %s", got[5], want)
	}
	if want := "bazel-out node_modules pkg "; got[6] != want {
		t.Errorf("the program root lists %q, want %q: the sources' "+
			"directories, the output tree and the root importer alone", got[6], want)
	}
	if got[7] != "pkg-is-real" {
		t.Errorf("pkg is not a real directory on the way to pkg/sub: %q", got[7:])
	}
	if want := filepath.Join(root, "pkg/a.ts"); got[8] != want {
		t.Errorf("pkg/a.ts -> %s, want the source at its exec path %s", got[8], want)
	}
	if want := filepath.Join(root, "bazel-out"); got[9] != want {
		t.Errorf("bazel-out -> %s, want the output tree whole %s", got[9], want)
	}
	want := []string{"lib-absent", "other-absent"}
	if !reflect.DeepEqual(got[10:12], want) {
		t.Errorf("the root holds files the action does not name: %q, want %q",
			got[10:12], want)
	}
	if _, err := os.Stat(stamp); err != nil {
		t.Errorf("no stamp after a passing run: %v", err)
	}
	if _, err := os.Stat(programRoot); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the program root outlived the action: stat = %v", err)
	}
}

// A -source under the output tree would be written into the tree the root
// links whole; the step refuses it before tsgo runs.
func TestTsgoStep_ASourceUnderTheOutputTreeIsRefused(t *testing.T) {
	_, argv := newTsgoExecroot(t, "echo ran\n")

	err := runTsgo(tsgoArgs("-source="+binDir+"/pkg/lib.d.ts", "--",
		"external/tsgo/tsc"))
	if err == nil || !strings.Contains(err.Error(), binDir+"/pkg/lib.d.ts") {
		t.Errorf("runTsgo = %v, want the output-tree source refused", err)
	}
	if _, err := os.Stat(argv); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("tsgo ran after the refusal: stat = %v", err)
	}
}

// -overlay lays a dep's declarations over its package and -manifest its
// manifest as built; the dep's sources, importer links and the root are left.
func TestTsgoStep_LaysADepsOutputsOverItsDirectory(t *testing.T) {
	root, argv := newTsgoExecroot(t,
		"readlink pkg/package.json >> \"$0.argv\"\n"+
			"readlink pkg/lib.d.ts >> \"$0.argv\"\n"+
			"test -e pkg/lib.ts || echo lib-source-absent >> \"$0.argv\"\n"+
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
		"lib-source-absent",
		filepath.Join(root, subImporter),
		"root-skipped",
		"sub-is-real",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the program root reads\n%q\nwant\n%q", got, want)
	}
}

// The overlay lays a dep's declarations, manifest and data over its package
// and none of its JavaScript: a program reads a dep through its declarations.
func TestTsgoStep_LaysNoJavaScriptOverThePackage(t *testing.T) {
	laid := []string{"gen/m.d.ts", "gen/n.d.mts", "gen/data.json", "package.json"}
	left := []string{"gen/m.js", "gen/n.mjs", "gen/util.cjs", "gen/view.jsx"}
	files := strings.Join(laid, " ") + " " + strings.Join(left, " ")
	root, argv := newTsgoExecroot(t,
		"for f in "+files+"; do "+
			"if test -e pkg/$f; then echo \"$f\" >> \"$0.argv\"; fi; done\n")
	for _, rel := range strings.Fields(files) {
		writeFile(t, filepath.Join(root, binDir, "pkg", rel), "\n")
	}

	err := runTsgo(tsgoArgs("-overlay="+binDir+"/pkg", "--", "external/tsgo/tsc"))
	if err != nil {
		t.Fatalf("runTsgo: %v", err)
	}
	if got := recordedArgs(t, argv)[1:]; !reflect.DeepEqual(got, laid) {
		t.Errorf("the overlay lays %q over pkg, want %q: the declarations, "+
			"the manifest and the data, and no JavaScript", got, laid)
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

func TestTsgoStep_ChecksAFileUnderALinkByItsStoreTree(t *testing.T) {
	const key = "qs@6.14.0"
	storeFile := filepath.Join(rootImporter, ".pnpm", key,
		"node_modules/qs/lib/utils.d.ts")
	listing := "pkg/sub/node_modules/qs/lib/utils.d.ts\n" +
		"   Imported via \"./sub/node_modules/qs/lib/utils.js\"" +
		" from file 'pkg/a.ts'\n" +
		"pkg/a.ts\n   Root file specified for compilation\n"
	for name, sandboxed := range map[string]bool{
		"real": false, "sandboxed": true,
	} {
		t.Run(name, func(t *testing.T) {
			root, _ := newTsgoExecroot(t, "cat "+binDir+"/pkg/listing.txt\n")
			writeFile(t, filepath.Join(root, binDir, "pkg/listing.txt"), listing)
			at := filepath.Join(root, storeFile)
			if sandboxed {
				real := filepath.Join(t.TempDir(), storeFile)
				writeFile(t, real, "export {};\n")
				if err := os.MkdirAll(filepath.Dir(at), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(real, at); err != nil {
					t.Fatal(err)
				}
			} else {
				writeFile(t, at, "export {};\n")
				t.Chdir(realpath(t, root))
			}
			err := os.Symlink("../../../node_modules/.pnpm/"+key+"/node_modules/qs",
				filepath.Join(root, subImporter, "qs"))
			if err != nil {
				t.Fatal(err)
			}
			args := tsgoArgs("--", "external/tsgo/tsc")
			own := "label\t//pkg:app\nown\tpkg/a.ts\n"

			writeFile(t, filepath.Join(root, manifest),
				own+"npm-direct\tqs\t"+key+"\n")
			if err := runTsgo(args); err != nil {
				t.Errorf("runTsgo with the link's package in deps: %v", err)
			}
			writeFile(t, filepath.Join(root, manifest),
				own+"npm\tqs\t@npm//:qs\t"+key+"\n")
			err = runTsgo(args)
			if err == nil || !strings.Contains(err.Error(), "add @npm//:qs to deps") {
				t.Errorf("runTsgo with the package in the closure alone = %v, "+
					"want the label to add", err)
			}
			writeFile(t, filepath.Join(root, manifest), own)
			err = runTsgo(args)
			if err == nil ||
				!strings.Contains(err.Error(), "npm closure does not hold") {
				t.Errorf("runTsgo with the tree outside the closure = %v, "+
					"want the closure error", err)
			}
		})
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

// Without -check the tool runs from the same root with its output relayed and
// nothing parsed: the declare's run, which lists no edges and writes no stamp.
func TestTsgoStep_WithoutCheckRunsTheToolAlone(t *testing.T) {
	_, argv := newTsgoExecroot(t, "pwd >> \"$0.argv\"\necho declared\n")
	stdout := captureStdout(t)

	err := runTsgo([]string{"-root=" + programRoot, "-source=pkg/a.ts",
		"-node_modules=" + rootImporter, "--", "external/tsgo/tsc",
		"--project", binDir + "/pkg/app.tsconfig.json", "--emitDeclarationOnly"})
	if err != nil {
		t.Fatalf("runTsgo without -check: %v", err)
	}
	got := recordedArgs(t, argv)
	want := []string{
		"--project", binDir + "/pkg/app.tsconfig.json", "--emitDeclarationOnly",
	}
	if !reflect.DeepEqual(got[:3], want) {
		t.Errorf("tsgo ran with %q, want %q", got[:3], want)
	}
	if !strings.HasSuffix(got[3], programRoot) {
		t.Errorf("tsgo ran in %s, want the program root", got[3])
	}
	if out := stdout(); !strings.Contains(out, "declared") {
		t.Errorf("the tool's output is not relayed:\n%s", out)
	}
	if _, err := os.Stat(programRoot); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the program root outlived the action: stat = %v", err)
	}
}

// A src the chain's exclude names and no import brings in is outside the
// program: the step fails naming the src and the entry, and writes no stamp.
func TestTsgoStep_ASrcTheChainExcludesAndTheProgramLacksFails(t *testing.T) {
	root, _ := newTsgoExecroot(t, "cat "+binDir+"/pkg/listing.txt\n")
	writeFile(t, filepath.Join(root, "pkg/b.ts"), "export const b = 1;\n")
	writeFile(t, filepath.Join(root, "pkg/tsconfig.json"),
		`{"include": ["*.ts"], "exclude": ["b.ts"]}`+"\n")
	writeFile(t, filepath.Join(root, manifest),
		"label\t//pkg:app\nown\tpkg/a.ts\nown\tpkg/b.ts\n")
	rootA := "pkg/a.ts\n   Matched by include pattern '../../../../pkg/*.ts'" +
		" in '" + binDir + "/pkg/app.tsconfig.json'\n"
	writeFile(t, filepath.Join(root, binDir, "pkg/listing.txt"), rootA)
	stamp := binDir + "/pkg/app.tscheck"
	args := tsgoArgs("-tsconfig=pkg/tsconfig.json", "-source=pkg/b.ts",
		"-stamp="+stamp, "--", "external/tsgo/tsc")

	err := runTsgo(args)
	for _, want := range []string{
		"//pkg:app: srcs the program never read", "pkg/tsconfig.json",
		"  pkg/b.ts\texcluded by \"b.ts\"",
	} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("runTsgo = %v, want the message with %q", err, want)
		}
	}
	if _, err := os.Stat(stamp); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a stamp was written for a src the program lacks: %v", err)
	}

	writeFile(t, filepath.Join(root, binDir, "pkg/listing.txt"),
		"pkg/b.ts\n   Imported via \"./b\" from file 'pkg/a.ts'\n"+rootA)
	if err := runTsgo(args); err != nil {
		t.Errorf("runTsgo with b.ts imported into the program: %v", err)
	}
	if _, err := os.Stat(stamp); err != nil {
		t.Errorf("no stamp after a passing check: %v", err)
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

func TestProgramCopiesConfigImportsWithoutLosingWorkspacePaths(t *testing.T) {
	root, _ := newTsgoExecroot(t, "")
	for file, body := range map[string]string{
		"lint/config.mjs":          "import './plugins/rule.mjs';",
		"lint/plugins/rule.mjs":    "import './options.mjs';",
		"lint/plugins/options.mjs": "export default {};",
	} {
		writeFile(t, filepath.Join(root, file), body)
	}
	tool, _ := fakeTool(t, root, "lint-tool", `set -eu
[ "$1" = "--config" ]
[ "$2" = "lint/config.mjs" ]
[ "$3" = "pkg/a.ts" ]
[ -f "$3" ]
[ ! -L lint/config.mjs ]
[ ! -L lint/plugins/rule.mjs ]
[ ! -L lint/plugins/options.mjs ]
[ -f node_modules/zod/index.d.ts ]
[ -f pkg/sub/node_modules/ms/index.d.ts ]
[ -f bazel-out/k8-fastbuild/bin/pkg/lib.d.ts ]
[ -f bazel-out/k8-fastbuild/bin/pkg/app.tsconfig.json ]
`)
	stamp := binDir + "/pkg/app.tslint"
	args := []string{
		"-root=" + programRoot,
		"-source=pkg/a.ts",
		"-source=lint/config.mjs",
		"-copy=lint/config.mjs",
		"-copy=lint/plugins/rule.mjs",
		"-copy=lint/plugins/options.mjs",
		"-node_modules=" + subImporter,
		"-node_modules=" + rootImporter,
		"-stamp=" + stamp,
		"--", tool, "--config", "lint/config.mjs", "pkg/a.ts",
	}
	if err := runTsgo(args); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stamp); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(programRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("program root was not cleaned up: %v", err)
	}
	if got, err := os.ReadFile("lint/config.mjs"); err != nil || string(got) != "import './plugins/rule.mjs';" {
		t.Fatalf("source config changed: %q, %v", got, err)
	}
}

func TestProgramToolEnvironmentKeepsAbsoluteExecutableAndRunfiles(t *testing.T) {
	root, _ := newTsgoExecroot(t, "")
	sidecar := "tools/side car=declared"
	writeFile(t, filepath.Join(root, sidecar), "#!/bin/sh\ncat \"$0.runfiles/payload\"\n")
	if err := os.Chmod(sidecar, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, sidecar+".runfiles/payload"), "declared runfile")
	tool, _ := fakeTool(t, root, "lint-tool", `set -eu
case "$LINT_AUXILIARY" in /*) ;; *) exit 20;; esac
[ "$LINT_AUXILIARY" = "$1" ]
[ "$("$LINT_AUXILIARY")" = "declared runfile" ]
[ -f pkg/a.ts ]
`)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("LINT_AUXILIARY", "must be replaced")
	if err := runTsgo([]string{
		"-root=" + programRoot,
		"-source=pkg/a.ts",
		"-tool-env=LINT_AUXILIARY=" + sidecar,
		"--", tool, filepath.Join(cwd, sidecar),
	}); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("LINT_AUXILIARY"); got != "must be replaced" {
		t.Fatalf("tool environment escaped the child process: %q", got)
	}
}

func TestFormatterCannotRepairBadInputAndReportSuccess(t *testing.T) {
	root, _ := newTsgoExecroot(t, "printf 'fixed\\n' > pkg/a.ts\n")
	tool := filepath.Join(root, "external/tsgo/tsc")
	before, err := os.ReadFile("pkg/a.ts")
	if err != nil {
		t.Fatal(err)
	}
	stamp := filepath.Join(binDir, "format.stamp")
	err = runTsgo([]string{"-root=" + programRoot, "-source=pkg/a.ts", "-copy=pkg/a.ts", "-verify-copies", "-stamp=" + stamp, "--", tool})
	if err == nil || !strings.Contains(err.Error(), "validation changed input pkg/a.ts") {
		t.Fatalf("got %v", err)
	}
	after, err := os.ReadFile("pkg/a.ts")
	if err != nil || string(after) != string(before) {
		t.Fatalf("source changed: %q, %v", after, err)
	}
	if _, err := os.Stat(stamp); !os.IsNotExist(err) {
		t.Fatalf("failed formatter wrote stamp: %v", err)
	}
}

func TestFormatterPathsStayAbsoluteInsideProgramRoot(t *testing.T) {
	root, _ := newTsgoExecroot(t, "case \"$1\" in /*) ;; *) exit 19;; esac\nprintf 'fixed\\n' > \"$1\"\n")
	tool := filepath.Join(root, "external/tsgo/tsc")
	err := runTsgo([]string{"-root=" + programRoot, "-source=pkg/a.ts", "-copy=pkg/a.ts", "-absolute-copy-args", "-verify-copies", "--", tool, "pkg/a.ts"})
	if err == nil || !strings.Contains(err.Error(), "validation changed input pkg/a.ts") {
		t.Fatalf("formatter did not receive the absolute copied input: %v", err)
	}
}
