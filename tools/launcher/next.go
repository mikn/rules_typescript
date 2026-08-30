package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// nextBinInTree is where the Next.js CLI sits inside an npm tree.
var nextBinInTree = []string{"next", "dist", "bin", "next"}

// planNext runs the Next.js CLI: `next dev` against the source tree, or
// `next start` over a build Bazel produced.
//
// Neither command is handed a generated config -- Next.js reads next.config.*
// from its project directory, and that file is the user's. What the launcher
// supplies is the npm tree, through NODE_PATH: Next.js seeds its webpack
// resolve.modules from it, so the app's bare imports reach the Bazel tree
// without a node_modules symlink appearing in anyone's source directory.
func planNext(cfg *Config, r *Resolver, plan *Plan, args []string) (*Plan, error) {
	n := cfg.Next

	runtime, err := runtimeCommand(cfg, r)
	if err != nil {
		return nil, fmt.Errorf(
			"%s: the toolchain JS runtime is missing from the runfiles of %s: %w\n"+
				"Re-run via `bazel run %s`; a host node is not used.",
			n.rule(), cfg.Label, err, cfg.Label)
	}
	nodeModules, err := r.Path(n.NodeModules)
	if err != nil {
		return nil, fmt.Errorf("%s: %s has no node_modules tree in runfiles: %w",
			n.rule(), cfg.Label, err)
	}
	nextBin := filepath.Join(append([]string{nodeModules}, nextBinInTree...)...)
	if !fileExists(nextBin) {
		return nil, fmt.Errorf(
			"%s: the Next.js CLI is missing from the node_modules tree of %s:\n"+
				"                 %s\n"+
				"                 Add @npm//:next to the deps of that node_modules() target.",
			n.rule(), cfg.Label, nextBin)
	}

	port, args := portOverride(n.Port, args)
	plan.prependPath("NODE_PATH", nodeModules)
	plan.setEnv("NEXT_TELEMETRY_DISABLED", "1")

	var dir string
	switch n.Command {
	case nextCommandDev:
		dir, err = nextProjectDir(n)
	case nextCommandStart:
		dir, err = stageNextServeRoot(n, r, plan)
	default:
		err = fmt.Errorf("unknown next command %q", n.Command)
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", n.rule(), err)
	}

	plan.Dir = dir
	plan.Argv = append(append(runtime, nextBin, n.Command, "--port", strconv.Itoa(port)), args...)
	plan.UseExec = false
	// ibazel SIGTERMs the runner after every rebuild; the server has to survive
	// that, so only Ctrl-C stops it.
	plan.Supervise = SuperviseOptions{IgnoreTerm: true, ExitZeroOnInterrupt: true}
	plan.Messages = append(plan.Messages,
		fmt.Sprintf("[%s] %s", n.rule(), cfg.Label),
		fmt.Sprintf("[%s] next %s on port %d", n.rule(), n.Command, port),
		fmt.Sprintf("[%s] project:      %s", n.rule(), dir),
		fmt.Sprintf("[%s] node_modules: %s", n.rule(), nodeModules),
	)
	if n.Command == nextCommandDev {
		plan.Messages = append(plan.Messages, fmt.Sprintf(
			"[%s] `next dev` writes .next/ and next-env.d.ts into the project "+
				"directory and rewrites its tsconfig.json `include`.", n.rule()))
	}
	return plan, nil
}

// portOverride lets an argument override the port the rule was given, so a test
// can take a kernel-assigned one. The override is consumed rather than appended:
// a server that takes the last of a repeated --port would leave the launcher's
// own message naming a port nothing is listening on, and one that rejects a
// repeated --port would not start at all.
func portOverride(port int, args []string) (int, []string) {
	kept := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		flag, value := args[i], ""
		if eq := strings.IndexByte(flag, '='); eq >= 0 {
			flag, value = flag[:eq], flag[eq+1:]
		} else if flag == "-p" || flag == "--port" {
			if i+1 < len(args) {
				i++
				value = args[i]
			}
		}
		if flag != "-p" && flag != "--port" {
			kept = append(kept, args[i])
			continue
		}
		if parsed, err := strconv.Atoi(value); err == nil {
			port = parsed
		}
	}
	return port, kept
}

// nextProjectDir is the source directory `next dev` serves: the package the
// target is declared in, under the workspace `bazel run` reports.
func nextProjectDir(n *NextConfig) (string, error) {
	root := os.Getenv("BUILD_WORKSPACE_DIRECTORY")
	if root == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		root = cwd
	}
	dir := filepath.Join(root, filepath.FromSlash(n.ProjectDir))
	if !dirExists(dir) {
		return "", fmt.Errorf("no such project directory: %s\n"+
			"`next dev` serves the source tree, so it has to be run from the "+
			"workspace the target lives in (`bazel run`, not a bare execution "+
			"of the launcher)", dir)
	}
	return dir, nil
}

// stageNextServeRoot assembles a project directory for `next start` around the
// build Bazel produced.
//
// The build output is COPIED rather than symlinked: the image optimizer writes
// its cache into .next/cache at request time, and a Bazel output tree is
// read-only. Everything else served from the project directory rather than from
// .next -- public/, and the config -- is staged beside it.
func stageNextServeRoot(n *NextConfig, r *Resolver, plan *Plan) (string, error) {
	build, err := r.Path(n.BuildDir)
	if err != nil {
		return "", fmt.Errorf("no build output in runfiles: %w", err)
	}
	scratch, err := os.MkdirTemp(os.Getenv("TEST_TMPDIR"), "next_start")
	if err != nil {
		return "", err
	}
	plan.Cleanup = func() { os.RemoveAll(scratch) }

	if err := copyTree(build, filepath.Join(scratch, ".next")); err != nil {
		return "", err
	}
	if n.ConfigFile != "" {
		path, err := r.Path(n.ConfigFile)
		if err != nil {
			return "", err
		}
		if err := copyFile(path, filepath.Join(scratch, filepath.Base(path))); err != nil {
			return "", err
		}
	}
	for _, rl := range n.ProjectFiles {
		path, err := r.Path(rl)
		if err != nil {
			return "", err
		}
		rel := strings.TrimPrefix(rl, n.PackagePrefix)
		if err := copyFile(path, filepath.Join(scratch, filepath.FromSlash(rel))); err != nil {
			return "", err
		}
	}
	manifest := filepath.Join(scratch, "package.json")
	if !fileExists(manifest) {
		body := `{"name":"next-serve","version":"0.0.0","private":true}` + "\n"
		if err := os.WriteFile(manifest, []byte(body), 0o644); err != nil {
			return "", err
		}
	}
	return scratch, nil
}

func copyTree(src, dest string) error {
	return filepath.WalkDir(src, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}
