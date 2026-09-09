package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// ReadsFlag is the launcher's own argument: under `bazel run <test> -- --reads`
// the run reports the workspace files the tests read; the rest are vitest's.
const ReadsFlag = "--reads"

func splitReadsFlag(args []string) (bool, []string) {
	rest := slices.DeleteFunc(slices.Clone(args), func(a string) bool {
		return a == ReadsFlag
	})
	return len(rest) < len(args), rest
}

func planVitest(
	cfg *Config, r *Resolver, plan *Plan, args []string,
) (*Plan, error) {
	v := cfg.Vitest
	readsRequested, args := splitReadsFlag(args)
	var reads *readsRun
	if readsRequested {
		var err error
		if r, reads, err = startReads(cfg.Label, r); err != nil {
			return nil, err
		}
	}
	plan.Dir = r.Dir()

	nodeModules, err := installNodeModules(r, plan, v.NodeModules)
	if err != nil {
		return nil, err
	}

	configFile, err := r.Path(v.ConfigFile)
	if err != nil {
		return nil, err
	}

	// A user config is staged wherever its own imports resolve, which is not the
	// package directory -- so a path it needs to name (the compiled worker a
	// Workers pool boots, say) cannot be written relative to itself. The
	// generated config does sit in the package's output directory, so its
	// dirname is the anchor, and it is exported rather than derived so that
	// moving either file does not silently change what a config resolves.
	plan.setEnv("TS_TEST_PACKAGE_DIR", filepath.Dir(configFile))

	shard, err := shardFiles(r, v.TestFilesList)
	if err != nil {
		return nil, err
	}
	if len(shard) == 0 {
		plan.ExitEarly = true
		plan.Messages = append(plan.Messages, fmt.Sprintf(
			"ts_test: no test files assigned to shard %d/%d", shardIndex(), totalShards()))
		return plan, nil
	}

	files := make([]string, 0, len(shard))
	for _, f := range shard {
		files = append(files, f.path)
	}

	// Vitest globs its root -- the working directory -- for tests, and positional
	// args only substring-filter that; bazel-bin holds every sibling's copy too.
	if plan.Dir == "" {
		root, staged, err := stageTestRoot(shard)
		if err != nil {
			return nil, err
		}
		plan.Dir, files = root, staged
		plan.Cleanup = func() { _ = os.RemoveAll(root) }
		if nodeModules != "" {
			linkAs(filepath.Join(root, "node_modules"), nodeModules)
		}
	}

	flags := []string{"run", "--config", configFile}
	flags = append(flags, coverageFlags()...)
	flags = append(flags, args...)

	vitestBin, err := resolveVitest(v, nodeModules)
	if err != nil {
		return nil, err
	}
	runtime, err := runtimeCommand(cfg, r)
	if err != nil {
		return nil, err
	}
	argv := append(runtime, vitestBin)
	plan.Argv = append(append(argv, flags...), files...)
	plan.UseExec = false
	root := filepath.Join(filepath.Dir(configFile), v.RootRel)
	plan.PostRun = writeCoverage(cfg.Workspace, plan.Dir, root)
	if reads != nil {
		hook, err := r.Path(v.ReadsHook)
		if err != nil {
			return nil, err
		}
		reads.install(plan, hook, r.Dir())
		plan.PostRun = chainPostRun(plan.PostRun, reads.report(os.Stdout))
	}
	return plan, nil
}

// stageTestRoot gives a manifest-only run a root of its own: symlinks to just
// this shard's files, at the runfiles paths a runfiles tree would have used.
func stageTestRoot(files []testFile) (root string, staged []string, err error) {
	root, err = os.MkdirTemp(os.Getenv("TEST_TMPDIR"), "ts_test_root")
	if err != nil {
		return "", nil, err
	}
	for _, f := range files {
		link := filepath.Join(root, filepath.FromSlash(f.rlocation))
		if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
			return "", nil, err
		}
		_ = os.Remove(link)
		if err := os.Symlink(f.path, link); err != nil {
			return "", nil, err
		}
		staged = append(staged, link)
	}
	return root, staged, nil
}

// resolveVitest finds vitest's bin entry inside the test's node_modules tree,
// the one place it can come from: the runner requires the package in deps.
func resolveVitest(v *VitestConfig, nodeModules string) (string, error) {
	if v.VitestInTree != "" && nodeModules != "" {
		p := filepath.Join(nodeModules, filepath.FromSlash(v.VitestInTree))
		if fileExists(p) {
			return p, nil
		}
	}
	return "", fmt.Errorf(
		"ts_test: vitest is not in the node_modules tree at %q; "+
			"add @npm//:vitest to deps", nodeModules)
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func shardIndex() int { return envInt("TEST_SHARD_INDEX", 0) }
func totalShards() int {
	n := envInt("TEST_TOTAL_SHARDS", 1)
	if n < 1 {
		return 1
	}
	return n
}

func envInt(name string, fallback int) int {
	v, err := strconv.Atoi(os.Getenv(name))
	if err != nil {
		return fallback
	}
	return v
}

// testFile is one declared test file: the runfiles path the rule wrote and the
// filesystem path it resolves to.
type testFile struct {
	rlocation string
	path      string
}

// shardFiles reads the generated list of compiled test files (one runfiles path
// per line) and keeps the ones belonging to this shard.
func shardFiles(r *Resolver, listPath string) ([]testFile, error) {
	list, err := r.Path(listPath)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(list)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	index, total := shardIndex(), totalShards()
	out := []testFile{}
	scanner := bufio.NewScanner(f)
	i := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if i%total == index {
			abs, err := r.Path(line)
			if err != nil {
				return nil, err
			}
			out = append(out, testFile{rlocation: line, path: abs})
		}
		i++
	}
	return out, scanner.Err()
}

func coverageFlags() []string {
	dir := os.Getenv("COVERAGE_DIR")
	if dir == "" {
		return nil
	}
	return []string{
		"--coverage.enabled", "true",
		"--coverage.reporter", "lcov",
		"--coverage.reportsDirectory", filepath.Join(dir, "vitest"),
	}
}

// writeCoverage is the run's post-step under `bazel coverage`: vitest's lcov,
// its paths made workspace-relative, as the .dat the rule's merger reads.
func writeCoverage(workspace, runDir, root string) func(int) error {
	dir := os.Getenv("COVERAGE_DIR")
	if dir == "" {
		return nil
	}
	return func(code int) error {
		if code != 0 {
			return nil
		}
		data, err := os.ReadFile(filepath.Join(dir, "vitest", "lcov.info"))
		if err != nil {
			return fmt.Errorf("ts_test: vitest wrote no coverage report: %w", err)
		}
		lcov := RewriteLcov(data, workspace, runDir, root)
		return os.WriteFile(filepath.Join(dir, "vitest.dat"), lcov, 0o644)
	}
}

// RewriteLcov turns the paths vitest reports, relative to its root, into the
// workspace-relative ones the coverage manifest is matched against.
func RewriteLcov(data []byte, workspace, runDir, root string) []byte {
	tree := ""
	if runDir != "" && workspace != "" {
		tree = filepath.ToSlash(runDir) + "/" + workspace + "/"
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		rest, isSF := strings.CutPrefix(line, "SF:")
		if !isSF {
			continue
		}
		p := rest
		if !filepath.IsAbs(p) && root != "" {
			p = filepath.Join(root, p)
		}
		p = filepath.ToSlash(filepath.Clean(p))
		if tree != "" && strings.HasPrefix(p, tree) {
			lines[i] = "SF:" + strings.TrimPrefix(p, tree)
		} else if pkg, ok := packagePathUnderBazelOut(p); ok {
			lines[i] = "SF:" + pkg
		}
	}
	return []byte(strings.Join(lines, "\n"))
}

// packagePathUnderBazelOut recovers the package path of a build output istanbul
// named by its execroot realpath: a module a pool resolved past its symlink.
func packagePathUnderBazelOut(p string) (string, bool) {
	i := strings.LastIndex(p, "/bazel-out/")
	if i < 0 {
		return "", false
	}
	parts := strings.SplitN(p[i+len("/bazel-out/"):], "/", 3)
	if len(parts) != 3 || parts[1] != "bin" || strings.Contains(parts[2], ".runfiles/") {
		return "", false
	}
	return parts[2], true
}
