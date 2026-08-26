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
	plan.PostRun = writeCoverage(cfg.Workspace, plan.Dir)
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
// works on every ts_test; the attr only adds coverage to plain `bazel test`,
// where nothing consumes a report -- so it stays out of the runfiles tree the
// test is running in.
func coverageFlags(coverageAttr bool) []string {
	if out := os.Getenv("COVERAGE_OUTPUT_FILE"); out != "" {
		dir := filepath.Dir(out)
		_ = os.MkdirAll(dir, 0o755)
		return []string{
			"--coverage.enabled", "true",
			"--coverage.reporter", "lcov",
			"--coverage.reportsDirectory", dir,
		}
	}
	if !coverageAttr {
		return nil
	}
	flags := []string{"--coverage.enabled", "true"}
	if tmp := os.Getenv("TEST_TMPDIR"); tmp != "" {
		flags = append(flags, "--coverage.reportsDirectory", filepath.Join(tmp, "coverage"))
	}
	return flags
}

func writeCoverage(workspace, runDir string) func(int) error {
	out := os.Getenv("COVERAGE_OUTPUT_FILE")
	if out == "" {
		return nil
	}
	manifest := os.Getenv("COVERAGE_MANIFEST")
	return func(int) error {
		data, err := os.ReadFile(filepath.Join(filepath.Dir(out), "lcov.info"))
		if err != nil {
			return os.WriteFile(out, nil, 0o644)
		}
		lcov := RewriteLcov(data, workspace, runDir)
		if selected, ok := readCoverageManifest(manifest); ok {
			lcov = SelectInstrumented(lcov, selected)
		}
		return os.WriteFile(out, lcov, 0o644)
	}
}

// readCoverageManifest reads the files --instrumentation_filter selected for
// this test, which Bazel writes from the InstrumentedFilesInfo of every target
// in the test's dependency graph. A manifest that exists and selects nothing is
// a filter that excluded everything, which is not the same answer as a run with
// no manifest at all -- hence the second return value.
func readCoverageManifest(path string) (map[string]bool, bool) {
	if path == "" {
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	selected := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		if key := coverageKey(line); key != "" {
			selected[key] = true
		}
	}
	return selected, true
}

// SelectInstrumented drops every record naming a file the manifest does not
// select, which is what makes --instrumentation_filter change the report.
func SelectInstrumented(data []byte, selected map[string]bool) []byte {
	text := string(data)
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	kept := make([]string, 0, len(lines))

	// A record runs from its SF: line to end_of_record; whatever precedes the
	// SF: (lcov's TN: test-name line) belongs to the record that follows it.
	record, inRecord, keep := []string{}, false, false
	for _, line := range lines {
		if rest, isSF := strings.CutPrefix(line, "SF:"); isSF {
			inRecord, keep = true, selected[coverageKey(rest)]
		}
		record = append(record, line)
		if line == "end_of_record" {
			if keep {
				kept = append(kept, record...)
			}
			record, inRecord, keep = nil, false, false
		}
	}
	if !inRecord || keep {
		kept = append(kept, record...)
	}

	out := strings.Join(kept, "\n")
	if out != "" && strings.HasSuffix(text, "\n") {
		out += "\n"
	}
	return []byte(out)
}

// coverageKey identifies a file across the two spellings of it that have to
// meet: the manifest names the .ts a target declared, the report names the .js
// the compiler emitted for it.
func coverageKey(p string) string {
	p = filepath.ToSlash(strings.TrimSpace(p))
	if p == "" {
		return ""
	}
	if rest, ok := strings.CutPrefix(p, "bazel-out/"); ok {
		if parts := strings.SplitN(rest, "/", 3); len(parts) == 3 {
			p = parts[2]
		}
	}
	return strings.TrimSuffix(p, filepath.Ext(p))
}

// RewriteLcov turns the source paths vitest reports into the workspace-relative
// ones Bazel's lcov_merger expects.
func RewriteLcov(data []byte, workspace, runDir string) []byte {
	prefix := "SF:" + workspace + "/"
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		rest, isSF := strings.CutPrefix(line, "SF:")
		if !isSF {
			continue
		}
		if workspace != "" && strings.HasPrefix(line, prefix) {
			lines[i] = "SF:" + strings.TrimPrefix(line, prefix)
			continue
		}
		if p, ok := packagePathUnderBazelOut(rest, runDir); ok {
			lines[i] = "SF:" + p
		}
	}
	return []byte(strings.Join(lines, "\n"))
}

// packagePathUnderBazelOut recovers the package path of a build output named
// from outside the vite root, which is how istanbul reports a module a pool
// resolved through its execroot realpath rather than its runfiles symlink.
func packagePathUnderBazelOut(p, runDir string) (string, bool) {
	if !filepath.IsAbs(p) {
		if runDir == "" {
			return "", false
		}
		p = filepath.Join(runDir, p)
	}
	p = filepath.ToSlash(filepath.Clean(p))
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
