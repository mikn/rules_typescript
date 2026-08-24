package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func planVitest(cfg *Config, r *Resolver, plan *Plan) (*Plan, error) {
	v := cfg.Vitest
	plan.Dir = testWorkingDir(r, v.UpdateSnapshots)

	nodeModules := ""
	if v.NodeModules != "" {
		dir, err := r.Path(v.NodeModules)
		if err != nil {
			return nil, err
		}
		if st, statErr := os.Stat(dir); statErr == nil && st.IsDir() {
			nodeModules = dir
			plan.prependPath("NODE_PATH", dir)
			// Vitest reaches its optional deps (@vitest/coverage-v8, the environment
			// packages) through ESM resolution, which ignores NODE_PATH.
			linkAs(filepath.Join(filepath.Dir(dir), "node_modules"), dir)
			if r.Dir() != "" {
				linkAs(filepath.Join(r.Dir(), "node_modules"), dir)
			}
		}
	}

	configFile, err := r.Path(v.ConfigFile)
	if err != nil {
		return nil, err
	}

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
	if v.UpdateSnapshots {
		flags = append(flags, "--update")
	}
	flags = append(flags, coverageFlags(v.Coverage)...)

	vitestBin, viaPath, err := resolveVitest(r, v, nodeModules)
	if err != nil {
		return nil, err
	}

	argv := []string{}
	switch {
	case viaPath || v.VitestIsNpmBin:
		argv = append(argv, vitestBin)
	default:
		runtime, err := runtimeCommand(cfg, r)
		if err != nil {
			return nil, err
		}
		argv = append(runtime, vitestBin)
	}
	plan.Argv = append(append(argv, flags...), files...)
	plan.UseExec = false
	plan.PostRun = writeCoverage(cfg.Workspace)
	return plan, nil
}

// testWorkingDir puts snapshot updates in the source tree so vitest writes
// .snap files back; everything else runs at the runfiles root, if there is one.
func testWorkingDir(r *Resolver, updateSnapshots bool) string {
	if updateSnapshots {
		if ws := os.Getenv("BUILD_WORKSPACE_DIRECTORY"); ws != "" {
			return ws
		}
	}
	return r.Dir()
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

func linkAs(link, target string) {
	if _, err := os.Lstat(link); err == nil {
		return
	}
	_ = os.Symlink(target, link)
}

func resolveVitest(r *Resolver, v *VitestConfig, nodeModules string) (path string, viaPath bool, err error) {
	if v.Vitest != "" {
		p, err := r.Path(v.Vitest)
		if err != nil {
			return "", false, err
		}
		if fileExists(p) {
			return p, false, nil
		}
	}
	if v.VitestInTree != "" && nodeModules != "" {
		p := filepath.Join(nodeModules, filepath.FromSlash(v.VitestInTree))
		if fileExists(p) {
			return p, false, nil
		}
	}
	if p, lookErr := exec.LookPath("vitest"); lookErr == nil {
		return p, true, nil
	}
	return "", false, fmt.Errorf(
		"ts_test: vitest not found. Set the vitest attr or add @npm//:vitest to the " +
			"deps of the node_modules() target this test uses.")
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

// coverageFlags is unconditional on COVERAGE_OUTPUT_FILE so `bazel coverage`
// works on every ts_test; the attr only adds coverage to plain `bazel test`.
func coverageFlags(coverageAttr bool) []string {
	if out := os.Getenv("COVERAGE_OUTPUT_FILE"); out != "" {
		dir := filepath.Dir(out)
		_ = os.MkdirAll(dir, 0o755)
		return []string{
			"--coverage.enabled", "true",
			"--coverage.provider", "v8",
			"--coverage.reporter", "lcov",
			"--coverage.reportsDirectory", dir,
		}
	}
	if coverageAttr && os.Getenv("COVERAGE_ENABLED") == "true" {
		return []string{"--coverage.enabled", "true", "--coverage.provider", "v8"}
	}
	return nil
}

func writeCoverage(workspace string) func(int) error {
	out := os.Getenv("COVERAGE_OUTPUT_FILE")
	if out == "" {
		return nil
	}
	return func(int) error {
		data, err := os.ReadFile(filepath.Join(filepath.Dir(out), "lcov.info"))
		if err != nil {
			return os.WriteFile(out, nil, 0o644)
		}
		return os.WriteFile(out, RewriteLcov(data, workspace), 0o644)
	}
}

// RewriteLcov strips the repository prefix vitest reports, which Bazel's
// lcov_merger expects to be absent.
func RewriteLcov(data []byte, workspace string) []byte {
	if workspace == "" {
		return data
	}
	prefix := "SF:" + workspace + "/"
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, prefix) {
			lines[i] = "SF:" + strings.TrimPrefix(line, prefix)
		}
	}
	return []byte(strings.Join(lines, "\n"))
}
