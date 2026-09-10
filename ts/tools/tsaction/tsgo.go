// The tsgo step runs tsgo from a program root: the exec root laid out again
// under the target's output directory, the importer chain's node_modules in.

package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mikn/rules_typescript/ts/tools/explainfiles"
)

// runTsgo lays the program root out, runs the command from it and removes it:
// no directory above a source is an output, so the node_modules walk needs one.
func runTsgo(args []string) error {
	flags := flag.NewFlagSet("tsgo", flag.ExitOnError)
	root := flags.String("root", "", "the program root to lay out, under the target's output directory")
	var importers stringList
	flags.Var(&importers, "node_modules",
		"an importer's node_modules directory, nearest first (repeatable); "+
			"the last is the lockfile's root importer")
	check := flags.String("check", "",
		"the ownership manifest the --explainFiles listing is checked against")
	stamp := flags.String("stamp", "", "file to create when tsgo exits 0")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cmdline := flags.Args()
	if *root == "" || *check == "" || len(cmdline) == 0 {
		return errors.New("tsgo needs -root=DIR, -check=FILE and a command after --")
	}
	own, err := readOwnership(*check)
	if err != nil {
		return err
	}
	if err := layOutProgramRoot(*root, importers); err != nil {
		return err
	}
	defer os.RemoveAll(*root)

	tool, err := filepath.Abs(cmdline[0])
	if err != nil {
		return err
	}
	cmdline = append([]string{tool}, cmdline[1:]...)
	if err := checkedRun(*root, cmdline, own); err != nil {
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
	findings, err := own.check(listing)
	if err != nil {
		return err
	}
	if len(findings) > 0 {
		return &undeclaredDeps{own.report(findings)}
	}
	return nil
}

// layOutProgramRoot links the exec root's entries into root, the last importer
// at root/node_modules, the others under real directories of links to them.
func layOutProgramRoot(root string, importers []string) error {
	execroot, err := os.Getwd()
	if err != nil {
		return err
	}
	if err := os.RemoveAll(root); err != nil {
		return err
	}
	if err := linkEntries(root, execroot, "node_modules"); err != nil {
		return err
	}
	for i, binDir := range importers {
		at := "node_modules"
		if i < len(importers)-1 {
			dir := importerDir(binDir)
			if err := realDirs(root, execroot, dir); err != nil {
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
	return nil
}

// importerDir is the importer's directory in the exec root, read off its
// node_modules' bin-dir path bazel-out/<cfg>/bin/<dir>/node_modules.
func importerDir(binDir string) string {
	rest := binDir
	if i := strings.Index(binDir, "/bin/"); i >= 0 {
		rest = binDir[i+len("/bin/"):]
	}
	dir := filepath.Dir(filepath.FromSlash(rest))
	if dir == "." {
		return ""
	}
	return dir
}

// realDirs makes root/<each prefix of dir> a real directory holding a link
// per entry of the exec root's directory at that level.
func realDirs(root, execroot, dir string) error {
	rel := ""
	for _, part := range strings.Split(dir, string(filepath.Separator)) {
		rel = filepath.Join(rel, part)
		at := filepath.Join(root, rel)
		st, err := os.Lstat(at)
		if err == nil && st.IsDir() {
			continue
		}
		if err == nil {
			if err := os.Remove(at); err != nil {
				return err
			}
		}
		if err := linkEntries(at, filepath.Join(execroot, rel), ""); err != nil {
			return err
		}
	}
	return nil
}

// linkEntries creates dir and links every entry of from into it but skip; a
// from that is not there is an empty directory.
func linkEntries(dir, from, skip string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(from)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() == skip {
			continue
		}
		target := filepath.Join(from, entry.Name())
		if err := os.Symlink(target, filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}
