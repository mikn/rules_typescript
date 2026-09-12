// Command release tags a module release (v<version>, after bumping
// MODULE.bazel) or a tools release (tools-v<N>); release.yml does the rest.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	semver       = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9._]+)?$`)
	moduleName   = regexp.MustCompile(`(?m)^\s*name = "rules_typescript",`)
	versionKV    = regexp.MustCompile(`^(\s*version = ")([^"]*)(",?)$`)
	toolsVersion = regexp.MustCompile(`(?m)^TOOLS_VERSION = "([0-9]+)"$`)
)

const toolsLock = "ts/private/tools_lock.bzl"

type repo struct {
	dir    string
	dryRun bool
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "release: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("release", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	dryRun := fs.Bool("dry-run", false, "print every step and mutate nothing")
	push := fs.Bool("push", false, "push the tag to origin, which starts the Release workflow")
	remote := fs.String("remote", "origin", "remote to push the tag to")
	fs.Usage = func() {
		fmt.Println(`bazel run //tools/release -- <version> [flags]
bazel run //tools/release -- tools <N> [flags]

Bumps module(version) in MODULE.bazel, commits it, and creates the annotated
tag v<version>. Pushing that tag runs .github/workflows/release.yml, which
builds the tarball with git archive, publishes the GitHub release, and opens
the PR that fills in .bcr/source.json.

"tools <N>" creates the annotated tag tools-v<N> on HEAD, the N that
ts/private/tools_lock.bzl names; pushing it runs the workflow's tools job,
which builds the four assets, asserts their SRIs against the table and
attaches them to the tools-v<N> release.

  bazel run //tools/release -- 0.2.0 --dry-run
  bazel run //tools/release -- 0.2.0 --push
  bazel run //tools/release -- tools 1 --push

Flags:`)
		fs.PrintDefaults()
	}
	// Go's flag package stops at the first non-flag word, so `0.2.0 --dry-run`
	// would leave --dry-run unparsed. Re-parse what follows each positional.
	var positional []string
	for rest := args; ; {
		if err := fs.Parse(rest); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil
			}
			return err
		}
		if fs.NArg() == 0 {
			break
		}
		positional = append(positional, fs.Arg(0))
		rest = fs.Args()[1:]
	}
	if len(positional) == 2 && positional[0] == "tools" {
		return runTools(positional[1], *dryRun, *push, *remote)
	}
	if len(positional) != 1 {
		fs.Usage()
		return errors.New("expected exactly one version argument, e.g. 0.2.0, " +
			"or `tools <N>`")
	}

	version := positional[0]
	if !semver.MatchString(version) {
		return fmt.Errorf("invalid version %q.\nDid you mean X.Y.Z or X.Y.Z-prerelease, e.g. 0.2.0 or 0.2.0-rc.1?", version)
	}
	tag := "v" + version

	root, err := repoRoot()
	if err != nil {
		return err
	}
	r := &repo{dir: root, dryRun: *dryRun}
	fmt.Printf("Repository: %s\nRelease:    %s\n", root, tag)
	if *dryRun {
		fmt.Println("Mode:       dry run (nothing is written)")
	}
	fmt.Println()

	if out, err := r.git("tag", "--list", tag); err != nil {
		return err
	} else if strings.TrimSpace(out) != "" {
		return fmt.Errorf("tag %s already exists.\nDid you mean the next patch version? Check `git tag --list`", tag)
	}
	if err := r.requireClean(); err != nil {
		return err
	}

	modulePath := filepath.Join(root, "MODULE.bazel")
	before, err := os.ReadFile(modulePath)
	if err != nil {
		return err
	}
	after, old, err := setModuleVersion(string(before), version)
	if err != nil {
		return err
	}
	// A release PR may already carry the bump, in which case there is nothing to
	// write and `git commit` would fail with an empty index rather than tag.
	if old == version {
		fmt.Printf("[1/3] MODULE.bazel: already %s\n", version)
		fmt.Println("[2/3] commit MODULE.bazel: nothing to commit, version already in HEAD")
	} else {
		fmt.Printf("[1/3] MODULE.bazel: module version %s -> %s\n", old, version)
		if !*dryRun {
			if err := os.WriteFile(modulePath, []byte(after), 0o644); err != nil {
				return err
			}
		}

		fmt.Println("[2/3] commit MODULE.bazel")
		if err := r.gitWrite("add", "MODULE.bazel"); err != nil {
			return err
		}
		if err := r.gitWrite("commit", "-m", "chore: release "+tag); err != nil {
			return err
		}
	}

	fmt.Printf("[3/3] tag %s\n", tag)
	if err := r.gitWrite("tag", "-a", tag, "-m", "rules_typescript "+version); err != nil {
		return err
	}

	fmt.Println()
	if *push {
		return r.pushTag(*remote, tag)
	}
	fmt.Printf(`Nothing has been pushed. To publish:

  git push %s %s

That starts .github/workflows/release.yml: tarball, GitHub release, and the
.bcr/source.json PR. To undo instead: git tag -d %s && git reset --hard HEAD~1
`, *remote, tag, tag)
	return nil
}

func runTools(n string, dryRun, push bool, remote string) error {
	if !toolsVersion.MatchString(`TOOLS_VERSION = "` + n + `"`) {
		return fmt.Errorf("invalid tools version %q: the N of tools-v<N> is "+
			"digits, e.g. 1", n)
	}
	tag := "tools-v" + n
	root, err := repoRoot()
	if err != nil {
		return err
	}
	r := &repo{dir: root, dryRun: dryRun}
	lock, err := os.ReadFile(filepath.Join(root, toolsLock))
	if err != nil {
		return err
	}
	locked, err := lockedToolsVersion(string(lock))
	if err != nil {
		return err
	}
	if locked != n {
		return fmt.Errorf("%s names TOOLS_VERSION %s, not %s: the tag names "+
			"the table's release", toolsLock, locked, n)
	}
	fmt.Printf("Repository: %s\nRelease:    %s\n", root, tag)
	if dryRun {
		fmt.Println("Mode:       dry run (nothing is written)")
	}
	fmt.Println()
	if out, err := r.git("tag", "--list", tag); err != nil {
		return err
	} else if strings.TrimSpace(out) != "" {
		return fmt.Errorf("tag %s already exists; a tool change bumps "+
			"TOOLS_VERSION in %s", tag, toolsLock)
	}
	if err := r.requireClean(); err != nil {
		return err
	}
	fmt.Printf("[1/1] tag %s\n", tag)
	message := "rules_typescript tools " + n
	if err := r.gitWrite("tag", "-a", tag, "-m", message); err != nil {
		return err
	}
	fmt.Println()
	if push {
		return r.pushTag(remote, tag)
	}
	fmt.Printf(`Nothing has been pushed. To publish:

  git push %s %s

That starts the tools job of .github/workflows/release.yml: the four assets,
their SRIs against %s, the tools-v%s release. To undo instead: git tag -d %s
`, remote, tag, toolsLock, n, tag)
	return nil
}

// lockedToolsVersion reads TOOLS_VERSION out of ts/private/tools_lock.bzl.
func lockedToolsVersion(src string) (string, error) {
	m := toolsVersion.FindStringSubmatch(src)
	if m == nil {
		return "", errors.New(toolsLock + " has no TOOLS_VERSION = \"<N>\" line")
	}
	return m[1], nil
}

// setModuleVersion rewrites the version inside the module() call only. A
// file-wide substitution would also hit every bazel_dep version.
func setModuleVersion(src, version string) (string, string, error) {
	lines := strings.Split(src, "\n")
	inModule := false
	sawName := false
	for i, line := range lines {
		switch {
		case strings.HasPrefix(line, "module("):
			inModule = true
		case inModule && strings.HasPrefix(line, ")"):
			inModule = false
		}
		if !inModule {
			continue
		}
		if moduleName.MatchString(line) {
			sawName = true
		}
		m := versionKV.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if !sawName {
			return "", "", errors.New("MODULE.bazel's module() names a different module; is this the rules_typescript checkout?")
		}
		lines[i] = m[1] + version + m[3]
		return strings.Join(lines, "\n"), m[2], nil
	}
	return "", "", errors.New("no version field found inside module() in MODULE.bazel")
}

// repoRoot walks up from the directory bazel run was invoked in, so the tool
// acts on the user's checkout rather than the read-only runfiles tree.
func repoRoot() (string, error) {
	dir := os.Getenv("BUILD_WORKING_DIRECTORY")
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		dir = wd
	}
	for {
		body, err := os.ReadFile(filepath.Join(dir, "MODULE.bazel"))
		if err == nil && moduleName.Match(body) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("no rules_typescript MODULE.bazel found in this directory or any parent.\nDid you mean to run this from inside the rules_typescript checkout?")
		}
		dir = parent
	}
}

func (r *repo) requireClean() error {
	out, err := r.git("status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return err
	}
	if strings.TrimSpace(out) != "" {
		return fmt.Errorf("working tree has uncommitted changes:\n%s\n"+
			"Commit or stash them first", out)
	}
	return nil
}

func (r *repo) pushTag(remote, tag string) error {
	if err := r.gitWrite("push", remote, tag); err != nil {
		return err
	}
	fmt.Printf("Pushed %s. Watch the release: "+
		"gh run list --workflow=release.yml\n", tag)
	return nil
}

func (r *repo) git(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = r.dir
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

func (r *repo) gitWrite(args ...string) error {
	if r.dryRun {
		fmt.Printf("      would run: git %s\n", strings.Join(args, " "))
		return nil
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = r.dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return nil
}
