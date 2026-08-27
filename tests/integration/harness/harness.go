// Package harness stages a child Bazel workspace and drives the nested Bazel
// that an integration test asserts against.
package harness

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strings"

	"github.com/bazelbuild/rules_go/go/runfiles"
)

type Config struct {
	Name         string
	WorkspaceRel string
	Lockfile     string
	Renames      map[string]string
}

type IT struct {
	Name         string
	RulesTSRoot  string
	WorkspaceDir string
	OutputBase   string

	bazel      string
	scratchDir string
	bazelBin   string
	stops      []func()
}

type Log struct {
	Path string
	Text string
}

type failure struct{ msg string }

func Run(cfg Config, body func(*IT)) {
	it, err := start(cfg)
	if err != nil {
		if it != nil {
			it.cleanup()
		}
		fmt.Fprintf(os.Stderr, "FAIL: %v\n", err)
		os.Exit(1)
	}
	code := invoke(it, body)
	it.cleanup()
	if code == 0 {
		fmt.Println("ALL PASSED")
	}
	os.Exit(code)
}

func invoke(it *IT, body func(*IT)) (code int) {
	defer func() {
		switch r := recover().(type) {
		case nil:
		case failure:
			fmt.Fprintf(os.Stderr, "FAIL: %s\n", r.msg)
			code = 1
		default:
			fmt.Fprintf(os.Stderr, "FAIL: panic: %v\n%s\n", r, debug.Stack())
			code = 1
		}
	}()
	body(it)
	return 0
}

func start(cfg Config) (*IT, error) {
	bazel := os.Getenv("BIT_BAZEL_BINARY")
	if bazel == "" {
		return nil, errors.New("BIT_BAZEL_BINARY not set")
	}
	workspaceSrc := os.Getenv("BIT_WORKSPACE_DIR")
	if workspaceSrc == "" {
		return nil, errors.New("BIT_WORKSPACE_DIR not set")
	}
	fmt.Printf("INFO: bazel           = %s\n", bazel)
	fmt.Printf("INFO: workspace_dir   = %s\n", workspaceSrc)

	root, via, err := rulesTSRoot(workspaceSrc, cfg.WorkspaceRel)
	if err != nil {
		return nil, err
	}
	fmt.Printf("INFO: rules_ts_root   = %s (via %s)\n", root, via)

	base := filepath.Join(scratchRoot(), cfg.Name)
	it := &IT{
		Name:         cfg.Name,
		RulesTSRoot:  root,
		WorkspaceDir: filepath.Join(base, "workspace"),
		OutputBase:   filepath.Join(base, "output_base"),
		bazel:        bazel,
		scratchDir:   filepath.Join(base, "scratch"),
	}
	return it, it.prepare(cfg, workspaceSrc)
}

func (it *IT) prepare(cfg Config, workspaceSrc string) error {
	for _, dir := range []string{it.WorkspaceDir, it.scratchDir} {
		makeWritable(dir)
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(it.OutputBase, 0o755); err != nil {
		return err
	}
	if err := stage(workspaceSrc, it.WorkspaceDir); err != nil {
		return err
	}
	module := filepath.Join(it.WorkspaceDir, "MODULE.bazel")
	text, err := os.ReadFile(module)
	if err != nil {
		return err
	}
	if err := os.WriteFile(module, []byte(strings.ReplaceAll(string(text), "{RULES_TS_ROOT}", it.RulesTSRoot)), 0o644); err != nil {
		return err
	}
	for from, to := range cfg.Renames {
		if err := os.Rename(filepath.Join(it.WorkspaceDir, from), filepath.Join(it.WorkspaceDir, to)); err != nil {
			return err
		}
	}
	if cfg.Lockfile != "" {
		src := filepath.Join(it.RulesTSRoot, cfg.Lockfile)
		if _, err := os.Stat(src); err != nil {
			return fmt.Errorf("pnpm-lock.yaml not found at %s", src)
		}
		if err := copyFile(src, filepath.Join(it.WorkspaceDir, "pnpm-lock.yaml")); err != nil {
			return err
		}
	}
	return it.shareRepositoryCache()
}

// Each test gets its own output base, so without a shared repository cache all
// of them re-fetch the whole BCR registry at once and the concurrent DNS
// lookups start failing ("Unknown host: bcr.bazel.build") on a different
// subset of tests each run.
func (it *IT) shareRepositoryCache() error {
	cache := filepath.Join(scratchRoot(), "repository_cache")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(it.WorkspaceDir, ".bazelrc"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "\ncommon --repository_cache=%s\n", cache)
	return err
}

// TEST_TMPDIR is the LAST choice: a nested output base runs to gigabytes, and
// TEST_TMPDIR is often a small tmpfs where the run dies of ENOSPC mid-assertion.
func scratchRoot() string {
	if dir := os.Getenv("RULES_TS_IT_SCRATCH"); dir != "" {
		return dir
	}
	if dir := os.Getenv("XDG_CACHE_HOME"); dir != "" {
		return filepath.Join(dir, "rules_typescript_it")
	}
	if home := os.Getenv("HOME"); home != "" {
		return filepath.Join(home, ".cache", "rules_typescript_it")
	}
	return filepath.Join(os.Getenv("TEST_TMPDIR"), "rules_typescript_it")
}

func rulesTSRoot(workspaceSrc, workspaceRel string) (root, via string, err error) {
	tried := []string{}
	if rf, err := runfiles.New(); err == nil {
		if path, err := rf.Rlocation("_main/MODULE.bazel"); err == nil {
			tried = append(tried, path)
			if root, err := checkoutRoot(path); err == nil {
				return root, "runfiles", nil
			}
		}
	}
	trimmed := strings.TrimSuffix(workspaceSrc, "/"+workspaceRel)
	if trimmed == workspaceSrc {
		return "", "", fmt.Errorf("BIT_WORKSPACE_DIR %q does not end with %q", workspaceSrc, workspaceRel)
	}
	path := filepath.Join(trimmed, "MODULE.bazel")
	tried = append(tried, path)
	root, err = checkoutRoot(path)
	if err != nil {
		return "", "", fmt.Errorf("cannot locate the rules_typescript checkout (tried %s): %w", strings.Join(tried, ", "), err)
	}
	return root, "BIT_WORKSPACE_DIR", nil
}

func checkoutRoot(moduleInRunfiles string) (string, error) {
	resolved, err := filepath.EvalSymlinks(moduleInRunfiles)
	if err != nil {
		return "", err
	}
	root := filepath.Dir(resolved)
	if _, err := os.Stat(filepath.Join(root, "oxc_cli")); err != nil {
		return "", fmt.Errorf("%s has no oxc_cli directory", root)
	}
	module, err := os.ReadFile(filepath.Join(root, "MODULE.bazel"))
	if err != nil {
		return "", err
	}
	if !strings.Contains(string(module), `"rules_typescript"`) {
		return "", fmt.Errorf("%s/MODULE.bazel does not declare rules_typescript", root)
	}
	return root, nil
}

func stage(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		from := filepath.Join(src, entry.Name())
		to := filepath.Join(dst, entry.Name())
		info, err := os.Stat(from)
		if err != nil {
			return err
		}
		if info.IsDir() {
			if err := os.MkdirAll(to, 0o755); err != nil {
				return err
			}
			if err := stage(from, to); err != nil {
				return err
			}
			continue
		}
		if err := copyFile(from, to); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(from, to string) error {
	info, err := os.Stat(from)
	if err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if info.Mode()&0o111 != 0 {
		mode = 0o755
	}
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func makeWritable(dir string) {
	filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		extra := os.FileMode(0o200)
		if entry.IsDir() {
			extra = 0o300
		}
		os.Chmod(path, info.Mode().Perm()|extra)
		return nil
	})
}

// The output base is KEPT: each nested run otherwise re-fetches its toolchains
// and npm closure, and these tests are `exclusive`, so nothing races it.
func (it *IT) cleanup() {
	for i := len(it.stops) - 1; i >= 0; i-- {
		it.stops[i]()
	}
	shutdown := exec.Command(it.bazel, "--output_base="+it.OutputBase, "shutdown")
	shutdown.Env = nestedEnv()
	shutdown.Run()
	for _, dir := range []string{it.WorkspaceDir, it.scratchDir} {
		makeWritable(dir)
		os.RemoveAll(dir)
	}
}

func (it *IT) OnCleanup(stop func()) {
	it.stops = append(it.stops, stop)
}

func (it *IT) Pass(format string, a ...any) {
	fmt.Printf("PASS: "+format+"\n", a...)
}

func (it *IT) Fail(format string, a ...any) {
	panic(failure{msg: fmt.Sprintf(format, a...)})
}

func (it *IT) Path(rel ...string) string {
	return filepath.Join(append([]string{it.WorkspaceDir}, rel...)...)
}

func (it *IT) Bin(rel ...string) string {
	return filepath.Join(append([]string{it.BazelBin()}, rel...)...)
}

func (it *IT) Scratch(rel ...string) string {
	path := filepath.Join(append([]string{it.scratchDir}, rel...)...)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		it.Fail("cannot create %s: %v", filepath.Dir(path), err)
	}
	return path
}

// Unsetting TEST_TMPDIR keeps the nested Bazel out of the outer execroot, which
// it refuses with "repo contents cache is inside main repo".
func nestedEnv() []string {
	env := []string{}
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "TEST_TMPDIR=") {
			continue
		}
		env = append(env, entry)
	}
	return env
}

func (it *IT) command(args []string) *exec.Cmd {
	cmd := exec.Command(it.bazel, append([]string{"--output_base=" + it.OutputBase}, args...)...)
	cmd.Dir = it.WorkspaceDir
	cmd.Env = nestedEnv()
	return cmd
}

func (it *IT) Bazel(args ...string) error {
	fmt.Printf("INFO: bazel %s\n", strings.Join(args, " "))
	cmd := it.command(args)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (it *IT) MustBazel(args ...string) {
	if err := it.Bazel(args...); err != nil {
		it.Fail("bazel %s exited non-zero: %v", strings.Join(args, " "), err)
	}
}

func (it *IT) BazelStdout(args ...string) string {
	fmt.Printf("INFO: bazel %s\n", strings.Join(args, " "))
	cmd := it.command(args)
	out := &strings.Builder{}
	cmd.Stdout = out
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		it.Fail("bazel %s exited non-zero: %v", strings.Join(args, " "), err)
	}
	return out.String()
}

func (it *IT) BazelLog(logName string, args ...string) (*Log, error) {
	fmt.Printf("INFO: bazel %s\n", strings.Join(args, " "))
	cmd := it.command(args)
	out := &strings.Builder{}
	cmd.Stdout = out
	cmd.Stderr = out
	err := cmd.Run()
	log := &Log{Path: it.Scratch(logName), Text: out.String()}
	if writeErr := os.WriteFile(log.Path, []byte(log.Text), 0o644); writeErr != nil {
		it.Fail("cannot write %s: %v", log.Path, writeErr)
	}
	return log, err
}

func (it *IT) BazelBin() string {
	if it.bazelBin == "" {
		cmd := it.command([]string{"info", "bazel-bin"})
		out := &strings.Builder{}
		cmd.Stdout = out
		if err := cmd.Run(); err != nil {
			it.Fail("bazel info bazel-bin failed: %v", err)
		}
		it.bazelBin = strings.TrimSpace(out.String())
		if it.bazelBin == "" {
			it.Fail("bazel info bazel-bin printed nothing")
		}
	}
	return it.bazelBin
}

func (l *Log) Contains(text string) bool {
	return strings.Contains(l.Text, text)
}

func (l *Log) Matches(pattern string) bool {
	return regexp.MustCompile(pattern).MatchString(l.Text)
}

func (l *Log) Lines() []string {
	return strings.Split(l.Text, "\n")
}

func (l *Log) Dump() {
	fmt.Fprintln(os.Stderr, "--- build output ---")
	fmt.Fprintln(os.Stderr, l.Text)
}

func (l *Log) DumpTail(lines int) {
	all := l.Lines()
	if len(all) > lines {
		all = all[len(all)-lines:]
	}
	fmt.Fprintln(os.Stderr, "--- build output ---")
	fmt.Fprintln(os.Stderr, strings.Join(all, "\n"))
}

// Runfile resolves a path in the TEST's own runfiles -- the outer repository's,
// which is where a tool the assertions need (a Node binary, say) comes from. The
// child workspace has its own outputs and knows nothing about these.
func (it *IT) Runfile(rel string) string {
	rf, err := runfiles.New()
	if err != nil {
		it.Fail("cannot open runfiles: %v", err)
	}
	path, err := rf.Rlocation(rel)
	if err != nil {
		it.Fail("cannot resolve runfile %s: %v", rel, err)
	}
	return path
}

// Exec runs a command outside Bazel and keeps its output alongside the nested
// build logs, so a failing assertion has the same paper trail as a failing build.
func (it *IT) Exec(logName, name string, args ...string) (*Log, error) {
	fmt.Printf("INFO: %s %s\n", name, strings.Join(args, " "))
	cmd := exec.Command(name, args...)
	cmd.Dir = it.WorkspaceDir
	out := &strings.Builder{}
	cmd.Stdout = out
	cmd.Stderr = out
	err := cmd.Run()
	log := &Log{Path: it.Scratch(logName), Text: out.String()}
	if writeErr := os.WriteFile(log.Path, []byte(log.Text), 0o644); writeErr != nil {
		it.Fail("cannot write %s: %v", log.Path, writeErr)
	}
	return log, err
}

// Glob returns the names of the entries in dir whose name starts with prefix and
// ends with suffix. Rollup names every chunk with a content hash, so the two ends
// are all a test can pin.
func (it *IT) Glob(dir, prefix, suffix string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		it.Fail("cannot read %s: %v", dir, err)
	}
	var found []string
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, prefix) && strings.HasSuffix(name, suffix) {
			found = append(found, name)
		}
	}
	return found
}

func (it *IT) Read(path string) string {
	text, err := os.ReadFile(path)
	if err != nil {
		it.Fail("cannot read %s: %v", path, err)
	}
	return string(text)
}

func (it *IT) Write(path, content string) {
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		it.Fail("cannot write %s: %v", path, err)
	}
}

func (it *IT) Replace(path, old, new string) {
	text := it.Read(path)
	if !strings.Contains(text, old) {
		it.Fail("%s does not contain %q, so the edit did not apply", path, old)
	}
	it.Write(path, strings.ReplaceAll(text, old, new))
}

func (it *IT) RequireFile(path, format string, a ...any) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		it.Fail(format, a...)
	}
}

func (it *IT) RequireNoFile(path, format string, a ...any) {
	if _, err := os.Stat(path); err == nil {
		it.Fail(format, a...)
	}
}

func (it *IT) RequireExecutable(path, format string, a ...any) {
	info, err := os.Stat(path)
	if err != nil || info.Mode()&0o111 == 0 {
		it.Fail(format, a...)
	}
}

func (it *IT) RequireDir(path, format string, a ...any) {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		it.Fail(format, a...)
	}
}

func (it *IT) RequireNoDir(path, format string, a ...any) {
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		it.Fail(format, a...)
	}
}

func (it *IT) RequireContains(path, text, format string, a ...any) {
	if !strings.Contains(it.Read(path), text) {
		it.Fail(format, a...)
	}
}

func (it *IT) RequireNotContains(path, text, format string, a ...any) {
	if strings.Contains(it.Read(path), text) {
		it.Fail(format, a...)
	}
}

func (it *IT) RequireMatches(path, pattern, format string, a ...any) {
	if !regexp.MustCompile(pattern).MatchString(it.Read(path)) {
		it.Fail(format, a...)
	}
}

func (it *IT) RequireNotMatches(path, pattern, format string, a ...any) {
	if regexp.MustCompile(pattern).MatchString(it.Read(path)) {
		it.Fail(format, a...)
	}
}

func (it *IT) Dump(path string) {
	fmt.Printf("--- %s ---\n%s------------------------\n", path, it.Read(path))
}

func (it *IT) Contains(path, text string) bool {
	return strings.Contains(it.Read(path), text)
}

func (it *IT) Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
