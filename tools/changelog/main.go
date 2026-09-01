// Command changelog assembles the entries in changelog.d/ into one CHANGELOG.md
// section.
//
// CHANGELOG.md is a serialisation point: every parallel PR appends to the same
// place, so every one of them rebases on the one that merged first. Nine such
// rebases in a single day, none of them resolving anything but a union of two
// additions, is what this replaces. A fragment is a file of its own, so two PRs
// touch the same bytes only if they pick the same filename.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "changelog: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("changelog", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	write := fs.Bool("write", false, "splice the section into CHANGELOG.md and delete the fragments")
	version := fs.String("version", "", "release the section as this version instead of [Unreleased]")
	fs.Usage = func() {
		fmt.Println(`bazel run //tools/changelog [-- flags]

Reads changelog.d/*.md and prints the CHANGELOG.md section they add up to.
Every fragment opens with the "### <section>" heading it belongs under; the
rest of the file is the entry, verbatim. Malformed fragments are an error, so
a plain run is also the check.

  bazel run //tools/changelog                          print the section
  bazel run //tools/changelog -- --version 0.3.0       print it as [0.3.0]
  bazel run //tools/changelog -- --version 0.3.0 --write

--write inserts the section above the topmost "## [" heading in CHANGELOG.md
and removes the fragments it consumed. Nothing already in the file is touched.

Flags:`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() > 0 {
		fs.Usage()
		return fmt.Errorf("unexpected argument %q; the version is a flag, --version %s", fs.Arg(0), fs.Arg(0))
	}

	root, err := repoRoot()
	if err != nil {
		return err
	}
	dir := filepath.Join(root, "changelog.d")
	fragments, err := ReadFragments(dir)
	if err != nil {
		return err
	}
	if len(fragments) == 0 {
		fmt.Fprintf(stdout, "changelog.d/ holds no entries; CHANGELOG.md is up to date.\n")
		return nil
	}

	section := Render(fragments, *version)
	if !*write {
		fmt.Fprint(stdout, section)
		return nil
	}

	path := filepath.Join(root, "CHANGELOG.md")
	before, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	after, err := Splice(string(before), section)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(after), 0o644); err != nil {
		return err
	}
	for _, f := range fragments {
		if err := os.Remove(filepath.Join(dir, f.Name)); err != nil {
			return err
		}
	}
	fmt.Fprintf(stdout, "CHANGELOG.md: folded in %d fragments, and removed them.\n", len(fragments))
	return nil
}

type Fragment struct {
	Name    string
	Section string
	Body    string
}

// ReadFragments parses every *.md in dir except README.md, which documents the
// format. The result is ordered by file name, which is what makes the assembled
// section reproducible.
func ReadFragments(dir string) ([]Fragment, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") || e.Name() == "README.md" {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)

	var fragments []Fragment
	for _, name := range names {
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		f, err := ParseFragment(name, string(body))
		if err != nil {
			return nil, err
		}
		fragments = append(fragments, f)
	}
	return fragments, nil
}

func ParseFragment(name, text string) (Fragment, error) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	i := 0
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	if i == len(lines) {
		return Fragment{}, fmt.Errorf("%s is empty", name)
	}
	heading, ok := sectionHeading(lines[i])
	if !ok {
		return Fragment{}, fmt.Errorf(`%s must open with the section it belongs under, e.g. "### Fixed" or "### Breaking — `+"`ts_compile`"+`", not %q`, name, lines[i])
	}

	body := strings.Trim(strings.Join(lines[i+1:], "\n"), "\n")
	if strings.TrimSpace(body) == "" {
		return Fragment{}, fmt.Errorf("%s has a heading and no entry under it", name)
	}
	if line, ok := strayHeading(body); ok {
		return Fragment{}, fmt.Errorf("%s carries a second section, %q. One section per fragment: put that entry in a file of its own", name, line)
	}
	return Fragment{Name: name, Section: heading, Body: body}, nil
}

func sectionHeading(line string) (string, bool) {
	if !strings.HasPrefix(line, "### ") {
		return "", false
	}
	name := strings.TrimSpace(strings.TrimPrefix(line, "### "))
	return name, name != ""
}

// strayHeading reports a second "###" outside a fenced block. Entries carry
// code blocks, and a fence can hold anything.
func strayHeading(body string) (string, bool) {
	fenced := false
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced
			continue
		}
		if !fenced && strings.HasPrefix(line, "###") {
			return line, true
		}
	}
	return "", false
}

// knownSections is the Keep a Changelog order. Anything else sorts after them,
// except a "Breaking" section, which leads: this project is pre-1.0 and the
// breaks are what a reader is looking for.
var knownSections = []string{"Added", "Changed", "Deprecated", "Removed", "Fixed", "Security"}

// Render lays the fragments out as one CHANGELOG.md section. Section order is
// fixed rather than first-seen, so the output does not depend on which PR
// landed first.
func Render(fragments []Fragment, version string) string {
	grouped := map[string][]Fragment{}
	for _, f := range fragments {
		grouped[f.Section] = append(grouped[f.Section], f)
	}

	var breaking, known, other []string
	for section := range grouped {
		switch {
		case strings.HasPrefix(section, "Breaking"):
			breaking = append(breaking, section)
		case sectionRank(section) >= 0:
			known = append(known, section)
		default:
			other = append(other, section)
		}
	}
	sort.Strings(breaking)
	sort.Slice(known, func(i, j int) bool { return sectionRank(known[i]) < sectionRank(known[j]) })
	sort.Strings(other)

	title := "[Unreleased]"
	if version != "" {
		title = "[" + version + "]"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "## %s\n", title)
	for _, section := range append(append(breaking, known...), other...) {
		fmt.Fprintf(&b, "\n### %s\n\n", section)
		for _, f := range grouped[section] {
			b.WriteString(f.Body)
			b.WriteString("\n")
		}
	}
	return b.String()
}

func sectionRank(section string) int {
	for i, known := range knownSections {
		if section == known {
			return i
		}
	}
	return -1
}

// Splice inserts a rendered section above the topmost "## " heading, which is
// the newest release. The preamble above it and every released section below
// are returned byte for byte.
func Splice(changelog, section string) (string, error) {
	lines := strings.Split(changelog, "\n")
	for i, line := range lines {
		if !strings.HasPrefix(line, "## ") {
			continue
		}
		head := strings.Join(lines[:i], "\n")
		tail := strings.Join(lines[i:], "\n")
		return strings.TrimRight(head, "\n") + "\n\n" + section + "\n" + tail, nil
	}
	return "", errors.New(`CHANGELOG.md has no "## " release heading to insert above`)
}

// repoRoot is BUILD_WORKSPACE_DIRECTORY under `bazel run`, as in
// tools/ci/check_test_sources.sh, so the tool writes to the source tree rather
// than the read-only runfiles copy of it.
func repoRoot() (string, error) {
	if dir := os.Getenv("BUILD_WORKSPACE_DIRECTORY"); dir != "" {
		return dir, nil
	}
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", errors.New("no BUILD_WORKSPACE_DIRECTORY and `git rev-parse --show-toplevel` failed.\nDid you mean to run this as `bazel run //tools/changelog`?")
	}
	return strings.TrimSpace(string(out)), nil
}
