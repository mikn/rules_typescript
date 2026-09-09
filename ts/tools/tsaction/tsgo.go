// The tsgo step runs tsgo from a program root: the exec root laid out again
// under the target's output directory, with the forest at node_modules.

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
	forest := flags.String("node_modules", "", "the node_modules tree the program root holds")
	check := flags.String("check", "",
		"the ownership manifest the --explainFiles listing is checked against")
	stamp := flags.String("stamp", "", "file to create when tsgo exits 0")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cmdline := flags.Args()
	if *root == "" || *forest == "" || *check == "" || len(cmdline) == 0 {
		return errors.New("tsgo needs -root=DIR, -node_modules=DIR, " +
			"-check=FILE and a command after --")
	}
	own, err := readOwnership(*check)
	if err != nil {
		return err
	}
	if err := layOutProgramRoot(*root, *forest); err != nil {
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

// layOutProgramRoot links the exec root's top-level entries into root, and the
// forest as root/node_modules; absolute targets, the links die with the action.
func layOutProgramRoot(root, forest string) error {
	execroot, err := os.Getwd()
	if err != nil {
		return err
	}
	if err := os.RemoveAll(root); err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(execroot)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() == "node_modules" {
			continue
		}
		if err := os.Symlink(filepath.Join(execroot, entry.Name()), filepath.Join(root, entry.Name())); err != nil {
			return err
		}
	}
	return os.Symlink(filepath.Join(execroot, forest), filepath.Join(root, "node_modules"))
}
