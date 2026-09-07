// The tsgo step runs tsgo from a program root: the exec root laid out again
// under the target's output directory, with the forest at node_modules.

package main

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
)

// runTsgo lays the program root out, runs the command from it and removes it:
// no directory above a source is an output, so the node_modules walk needs one.
func runTsgo(args []string) error {
	flags := flag.NewFlagSet("tsgo", flag.ExitOnError)
	root := flags.String("root", "", "the program root to lay out, under the target's output directory")
	forest := flags.String("node_modules", "", "the node_modules tree the program root holds")
	stamp := flags.String("stamp", "", "file to create when tsgo exits 0")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cmdline := flags.Args()
	if *root == "" || *forest == "" || len(cmdline) == 0 {
		return errors.New("tsgo needs -root=DIR, -node_modules=DIR and a command after --")
	}
	if err := layOutProgramRoot(*root, *forest); err != nil {
		return err
	}
	defer os.RemoveAll(*root)

	tool, err := filepath.Abs(cmdline[0])
	if err != nil {
		return err
	}
	if err := runToolIn(*root, append([]string{tool}, cmdline[1:]...)); err != nil {
		return err
	}
	if *stamp == "" {
		return nil
	}
	return os.WriteFile(*stamp, nil, 0o644)
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
