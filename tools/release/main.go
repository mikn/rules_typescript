// Command release cuts a rules_typescript release: bump the module version,
// commit, tag, and (optionally) push.
//
// Everything downstream of the tag — tarball, GitHub release, BCR PR — is
// .github/workflows/release.yml. Building a tarball here would produce a
// different archive than the published one, and so a wrong integrity hash.
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
	semver     = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9._]+)?$`)
	moduleName = regexp.MustCompile(`(?m)^\s*name = "rules_typescript",`)
	versionKV  = regexp.MustCompile(`^(\s*version = ")([^"]*)(",?)$`)
)

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

Bumps module(version) in MODULE.bazel, commits it, and creates the annotated
tag v<version>. Pushing that tag runs .github/workflows/release.yml, which
builds the tarball with git archive, publishes the GitHub release, and opens
the PR that fills in .bcr/source.json.

  bazel run //tools/release -- 0.2.0 --dry-run
  bazel run //tools/release -- 0.2.0 --push

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
	if len(positional) != 1 {
		fs.Usage()
		return errors.New("expected exactly one version argument, e.g. 0.2.0")
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
	if out, err := r.git("status", "--porcelain", "--untracked-files=no"); err != nil {
		return err
	} else if strings.TrimSpace(out) != "" {
		return fmt.Errorf("working tree has uncommitted changes:\n%s\nCommit or stash them first", out)
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

	fmt.Printf("[3/3] tag %s\n", tag)
	if err := r.gitWrite("tag", "-a", tag, "-m", "rules_typescript "+version); err != nil {
		return err
	}

	fmt.Println()
	if *push {
		if err := r.gitWrite("push", *remote, tag); err != nil {
			return err
		}
		fmt.Printf("Pushed %s. Watch the release: gh run list --workflow=release.yml\n", tag)
		return nil
	}
	fmt.Printf(`Nothing has been pushed. To publish:

  git push %s %s

That starts .github/workflows/release.yml: tarball, GitHub release, and the
.bcr/source.json PR. To undo instead: git tag -d %s && git reset --hard HEAD~1
`, *remote, tag, tag)
	return nil
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
