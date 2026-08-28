// Command tsaction implements the rules_typescript build actions that a plain
// ctx.actions.run cannot express, so that none of them needs a shell on the
// exec platform.
//
//	tsaction stamp -stamp=FILE -- TOOL [ARG...]
//	tsaction stage -out=DIR SRC DEST [SRC DEST...]
//	tsaction tar -out=FILE -dir=DIR [-prefix=P]
//
// Any argument of the form @FILE is a Bazel params file in "multiline" format
// and is replaced by one argument per line.
package main

import (
	"archive/tar"
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Substituted with the action's working directory. npm bin wrappers cd to
// RUNFILES_DIR before exec'ing the real tool, which invalidates every
// execroot-relative path they were handed.
const execrootToken = "{{EXECROOT}}"

const usage = `usage:
  tsaction stamp -stamp=FILE -- TOOL [ARG...]
  tsaction stage -out=DIR SRC DEST [SRC DEST...]
  tsaction tar -out=FILE -dir=DIR [-prefix=P]`

func main() {
	if len(os.Args) < 2 {
		fatal(errors.New(usage))
	}
	args, err := expandParamFiles(os.Args[2:])
	if err != nil {
		fatal(err)
	}
	switch os.Args[1] {
	case "stamp":
		err = stamp(args)
	case "stage":
		err = stage(args)
	case "tar":
		err = writeTar(args)
	default:
		err = fmt.Errorf("unknown subcommand %q\n%s", os.Args[1], usage)
	}
	if err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "tsaction:", err)
	os.Exit(1)
}

func expandParamFiles(args []string) ([]string, error) {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		if !strings.HasPrefix(arg, "@") {
			out = append(out, arg)
			continue
		}
		f, err := os.Open(strings.TrimPrefix(arg, "@"))
		if err != nil {
			return nil, err
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			out = append(out, scanner.Text())
		}
		err = errors.Join(scanner.Err(), f.Close())
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func stamp(args []string) error {
	flags := flag.NewFlagSet("stamp", flag.ExitOnError)
	out := flags.String("stamp", "", "file to create when the command exits 0")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cmdline := flags.Args()
	if *out == "" || len(cmdline) == 0 {
		return errors.New("stamp needs -stamp=FILE and a command after --")
	}

	execroot, err := os.Getwd()
	if err != nil {
		return err
	}
	for i, arg := range cmdline {
		cmdline[i] = strings.ReplaceAll(arg, execrootToken, execroot)
	}

	cmd := exec.Command(cmdline[0], cmdline[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() > 0 {
			os.Exit(exit.ExitCode())
		}
		return fmt.Errorf("%s: %w", cmdline[0], err)
	}
	return os.WriteFile(*out, nil, 0o644)
}

func stage(args []string) error {
	flags := flag.NewFlagSet("stage", flag.ExitOnError)
	out := flags.String("out", "", "directory to populate")
	if err := flags.Parse(args); err != nil {
		return err
	}
	pairs := flags.Args()
	if *out == "" {
		return errors.New("stage needs -out=DIR")
	}
	if len(pairs)%2 != 0 {
		return fmt.Errorf("stage takes SRC DEST pairs, got %d arguments", len(pairs))
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		return err
	}
	for i := 0; i < len(pairs); i += 2 {
		dest := filepath.Join(*out, pairs[i+1])
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		if err := copyFile(pairs[i], dest); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	outFile, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(outFile, in); err != nil {
		outFile.Close()
		return err
	}
	return outFile.Close()
}

func writeTar(args []string) error {
	flags := flag.NewFlagSet("tar", flag.ExitOnError)
	out := flags.String("out", "", "tar archive to write")
	dir := flags.String("dir", "", "directory whose tree is archived")
	prefix := flags.String("prefix", "", "path prepended to every archive entry")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *out == "" || *dir == "" {
		return errors.New("tar needs -out=FILE and -dir=DIR")
	}

	f, err := os.Create(*out)
	if err != nil {
		return err
	}
	w := tar.NewWriter(f)
	err = filepath.WalkDir(*dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(*dir, path)
		if err != nil {
			return err
		}
		name := *prefix
		if rel != "." {
			name = strings.TrimPrefix(*prefix+"/"+filepath.ToSlash(rel), "/")
		}
		if name == "" {
			return nil
		}
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		return writeTarEntry(w, name, path, info)
	})
	return errors.Join(err, w.Close(), f.Close())
}

// Fixed modes and a fixed timestamp: the archive has to be byte-identical
// across machines for Bazel to cache and share it.
func writeTarEntry(w *tar.Writer, name, path string, info os.FileInfo) error {
	header := &tar.Header{Name: name, ModTime: time.Unix(0, 0).UTC()}
	switch {
	case info.IsDir():
		header.Typeflag, header.Name, header.Mode = tar.TypeDir, name+"/", 0o755
	case info.Mode().IsRegular():
		header.Typeflag, header.Mode, header.Size = tar.TypeReg, 0o644, info.Size()
	default:
		return fmt.Errorf("%s: cannot archive file mode %s", path, info.Mode())
	}
	if err := w.WriteHeader(header); err != nil {
		return err
	}
	if header.Typeflag == tar.TypeDir {
		return nil
	}
	src, err := os.Open(path)
	if err != nil {
		return err
	}
	defer src.Close()
	_, err = io.Copy(w, src)
	return err
}
