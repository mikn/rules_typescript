// The tsgo step runs tsgo from a program root under the target's output
// directory: the action's sources at their paths, the chain's node_modules in.

package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/mikn/rules_typescript/ts/tools/explainfiles"
)

// runTsgo lays the program root out, runs the command from it (checked against
// -check when given) and removes it: the node_modules walk needs a root.
func runTsgo(args []string) error {
	flags := flag.NewFlagSet("tsgo", flag.ExitOnError)
	root := flags.String("root", "", "the program root to lay out, under the target's output directory")
	var sources, importers, overlays, manifests stringList
	flags.Var(&sources, "source",
		"an input of the action in the source tree, linked at its path under "+
			"the root (repeatable); the output tree is linked whole")
	flags.Var(&importers, "node_modules",
		"an importer's node_modules directory, nearest first (repeatable); "+
			"the last is the lockfile's root importer")
	flags.Var(&overlays, "overlay",
		"the output directory of a dep whose package is at or above this "+
			"one's, laid over that package's sources (repeatable)")
	flags.Var(&manifests, "manifest",
		"such a dep's package.json as built, laid at its package's "+
			"package.json over the src (repeatable)")
	check := flags.String("check", "",
		"the ownership manifest the --explainFiles listing is checked against; "+
			"without it the run's output is relayed and nothing is parsed")
	stamp := flags.String("stamp", "", "file to create when tsgo exits 0")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cmdline := flags.Args()
	if *root == "" || len(cmdline) == 0 {
		return errors.New("tsgo needs -root=DIR and a command after --")
	}
	var own *ownership
	if *check != "" {
		var err error
		if own, err = readOwnership(*check); err != nil {
			return err
		}
	}
	err := layOutProgramRoot(*root, sources, importers, overlays, manifests)
	if err != nil {
		return err
	}
	defer os.RemoveAll(*root)

	tool, err := filepath.Abs(cmdline[0])
	if err != nil {
		return err
	}
	cmdline = append([]string{tool}, cmdline[1:]...)
	if own == nil {
		err = runToolIn(*root, os.Stdout, cmdline)
	} else {
		err = checkedRun(*root, cmdline, own)
	}
	if err != nil {
		return err
	}
	if *stamp == "" {
		return nil
	}
	return os.WriteFile(*stamp, nil, 0o644)
}

// checkedRun keeps the listing off stdout: a failing tsgo relays its
// diagnostics, or all it printed when none parse; a passing one is checked.
func checkedRun(dir string, cmdline []string, own *ownership) error {
	var out bytes.Buffer
	runErr := runToolIn(dir, &out, cmdline)
	listing, parseErr := explainfiles.Parse(out.String())
	if runErr != nil {
		if parseErr != nil || len(listing.Diagnostics) == 0 {
			os.Stdout.Write(out.Bytes())
		} else {
			fmt.Println(strings.Join(listing.Diagnostics, "\n"))
		}
		return runErr
	}
	if parseErr != nil {
		return parseErr
	}
	execroot, err := os.Getwd()
	if err != nil {
		return err
	}
	rootAbs := filepath.Join(execroot, dir)
	for i, e := range listing.Edges {
		listing.Edges[i].To = throughRoot(rootAbs, execroot, e.To)
	}
	findings, err := own.check(listing)
	if err != nil {
		return err
	}
	if len(findings) > 0 {
		return &undeclaredDeps{own.report(findings)}
	}
	return nil
}

// throughRoot is the exec-root path of a listed file the root links, an
// overlaid output; tsgo realpaths a file under node_modules and no other.
func throughRoot(rootAbs, execroot, listed string) string {
	at := filepath.Join(rootAbs, filepath.FromSlash(listed))
	target, err := os.Readlink(at)
	if err != nil {
		return listed
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(at), target)
	}
	rel, err := filepath.Rel(execroot, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
		return listed
	}
	return filepath.ToSlash(rel)
}

// Bazel's output tree, the top-level entry every action output is under.
const outputTree = "bazel-out"

// layOutProgramRoot links each source at its path under real directories, the
// output tree whole, each importer's node_modules, overlays and manifests.
func layOutProgramRoot(
	root string, sources, importers, overlays, manifests []string,
) error {
	execroot, err := os.Getwd()
	if err != nil {
		return err
	}
	if err := os.RemoveAll(root); err != nil {
		return err
	}
	if err := linkAt(root, execroot, outputTree); err != nil {
		return err
	}
	for _, file := range sources {
		if file == outputTree || strings.HasPrefix(file, outputTree+"/") {
			return fmt.Errorf("-source=%s is under %s, which the root links "+
				"whole", file, outputTree)
		}
		if err := linkAt(root, execroot, filepath.FromSlash(file)); err != nil {
			return err
		}
	}
	for i, binDir := range importers {
		at := "node_modules"
		if i < len(importers)-1 {
			dir := importerDir(binDir)
			if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
				return err
			}
			at = filepath.Join(dir, "node_modules")
		}
		link := filepath.Join(root, at)
		if err := os.RemoveAll(link); err != nil {
			return err
		}
		if err := os.Symlink(filepath.Join(execroot, binDir), link); err != nil {
			return err
		}
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	for _, binDir := range overlays {
		from := filepath.Join(execroot, filepath.FromSlash(binDir))
		rel := filepath.FromSlash(binRelative(binDir))
		if err := overlayDir(root, rootAbs, execroot, from, rel); err != nil {
			return err
		}
	}
	for _, file := range manifests {
		dir := filepath.FromSlash(binRelative(path.Dir(file)))
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			return err
		}
		at := filepath.Join(root, dir, "package.json")
		if err := os.RemoveAll(at); err != nil {
			return err
		}
		from := filepath.Join(execroot, filepath.FromSlash(file))
		if err := os.Symlink(from, at); err != nil {
			return err
		}
	}
	return nil
}

// binRelative is the path under bazel-out/<cfg>/bin/, "" for the bin directory.
func binRelative(binPath string) string {
	if strings.HasSuffix(binPath, "/bin") {
		return ""
	}
	if i := strings.Index(binPath, "/bin/"); i >= 0 {
		return binPath[i+len("/bin/"):]
	}
	return binPath
}

// importerDir is the importer's directory in the exec root, read off its
// node_modules' bin-dir path bazel-out/<cfg>/bin/<dir>/node_modules.
func importerDir(binDir string) string {
	dir := filepath.Dir(filepath.FromSlash(binRelative(binDir)))
	if dir == "." {
		return ""
	}
	return dir
}

// overlayDir links every file under from into root/rel over a source of the
// same name; node_modules and the root itself skipped.
func overlayDir(root, rootAbs, execroot, from, rel string) error {
	entries, err := os.ReadDir(from)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(root, rel), 0o755); err != nil {
		return err
	}
	for _, entry := range entries {
		source := filepath.Join(from, entry.Name())
		if entry.Name() == "node_modules" || rootAbs == source ||
			strings.HasPrefix(rootAbs, source+string(filepath.Separator)) {
			continue
		}
		st, err := os.Stat(source)
		if err != nil {
			return err
		}
		at := filepath.Join(root, rel, entry.Name())
		if st.IsDir() {
			if err := overlayDir(root, rootAbs, execroot, source,
				filepath.Join(rel, entry.Name())); err != nil {
				return err
			}
			continue
		}
		if err := os.RemoveAll(at); err != nil {
			return err
		}
		if err := os.Symlink(source, at); err != nil {
			return err
		}
	}
	return nil
}

// linkAt links the exec root's rel into root at rel, its directories real.
func linkAt(root, execroot, rel string) error {
	at := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(at), 0o755); err != nil {
		return err
	}
	return os.Symlink(filepath.Join(execroot, rel), at)
}
