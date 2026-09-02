// Package harness stages a child Bazel workspace and drives the nested Bazel
// that an integration test asserts against.
package harness

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/bazelbuild/rules_go/go/runfiles"

	"github.com/mikn/rules_typescript/tests/hmrsocket"
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

	base, err := runRoot(cfg.Name, root)
	if err != nil {
		return nil, err
	}
	fmt.Printf("INFO: run_root        = %s\n", base)
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
// Downloads and action results are both shared across the suite. Each test has
// its own output base, so without the disk cache none of them reuses an action
// a sibling already ran: sequentially from cold the same three tests took 188s,
// 88s and 21s with it.
func (it *IT) shareRepositoryCache() error {
	repo := filepath.Join(cacheRoot(), "repository_cache")
	disk := filepath.Join(cacheRoot(), "disk_cache")
	for _, dir := range []string{repo, disk, bazeliskHome()} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(filepath.Join(it.WorkspaceDir, ".bazelrc"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "\ncommon --repository_cache=%s\ncommon --disk_cache=%s\n", repo, disk)
	return err
}

// The persistent root holds the two content-addressed caches, plus the
// fallback run roots below when there is no TEST_TMPDIR to use. Sharing the
// CACHES across checkouts is safe by construction -- a key is a hash of the
// content, so a stale entry is a miss and never a wrong answer -- and they are
// what makes a cold run mean "no network" instead of "no cache".
//
// RULES_TS_IT_SCRATCH keeps its name for the sake of the CI job that sets it
// (--test_env=RULES_TS_IT_SCRATCH=/mnt/rules_ts_it, whose two cache
// subdirectories the actions cache restores; the run roots are never reached
// there, since `bazel test` always supplies a TEST_TMPDIR).
//
// The last resort is os.TempDir() rather than TEST_TMPDIR: TEST_TMPDIR is now
// the run root below, which Bazel clears on each `bazel test`, so caches placed
// there would be re-fetched every time -- which is the one thing this root is
// for. That last resort needs HOME and XDG_CACHE_HOME both unset, and it is the
// one branch that can land a nested output base on a tmpfs: os.TempDir() is
// $TMPDIR or /tmp, which is a 32G tmpfs on the machine this was written on. The
// three above it name a directory CI or the developer chose.
func cacheRoot() string {
	if dir := os.Getenv("RULES_TS_IT_SCRATCH"); dir != "" {
		return dir
	}
	if dir := os.Getenv("XDG_CACHE_HOME"); dir != "" {
		return filepath.Join(dir, "rules_typescript_it")
	}
	if home := os.Getenv("HOME"); home != "" {
		return filepath.Join(home, ".cache", "rules_typescript_it")
	}
	return filepath.Join(os.TempDir(), "rules_typescript_it")
}

// The third shared cache, and the one fetch the other two do not cover:
// `bazel_binary` is not Bazel but a bazelisk wrapper, which defaults
// BAZELISK_HOME to $PWD. command() runs it from the per-run WorkspaceDir that
// prepare() has just recreated, so unset, all 18 tests fetch Bazel from
// releases.bazel.build on every run -- ~1.2GB a suite, and a network dependency
// in each one. A runner whose DNS timed out is what surfaced it; green runs hid
// it, because Bazel echoes a test's stdout only when the test fails.
//
// An inherited value wins, so a developer's populated cache is left alone.
func bazeliskHome() string {
	if dir := os.Getenv("BAZELISK_HOME"); dir != "" {
		return dir
	}
	return filepath.Join(cacheRoot(), "bazelisk")
}

// The per-run half -- child workspace, scratch dir and nested output base --
// goes under TEST_TMPDIR, and the outer Bazel clears the whole execroot _tmp on
// each `bazel test` -- measured: a full suite left 6.8G across 19 nested output
// bases there, and the next `bazel test`, of one unrelated target, left only
// that target's 264K. One persistent root keyed by the test's name is what let
// two checkouts stage into one directory and read each other's half-written
// state as their own failures; there is no name left to collide here.
//
// Measured, against the ENOSPC warning this replaces: these tests carry
// `no-sandbox` (tags.bzl), so TEST_TMPDIR is
// <outer output base>/execroot/_main/_tmp/<hash> -- the same real disk as the
// rest of the build, not a tmpfs. That hash is per target (a suite run printed
// 19 distinct ones for the 19 tests) and the outer output base is the path in
// front of it, so two targets differ in the hash and two checkouts differ in the
// prefix. On CI it is the root filesystem, which is also where /mnt/rules_ts_it
// lives on that image (see ci.yml), so the gigabytes do not change volume.
//
// Losing the retained output base costs a LOCAL developer a flat ~13.5s per
// test -- five interleaved pairs put new_project_test at 27.6s retained against
// 41.1s fresh and npm_deps_test at 28.7s against 42.5s, which is analysis and
// repo setup, not a re-fetch, because the content-addressed caches above are
// what make a warm run warm. CI pays nothing: it provisions /mnt/rules_ts_it
// with a bare `mkdir -p` on a fresh runner and restores only the two cache
// subdirectories, so every nested output base was already being created empty
// there on every run (ci.yml).
//
// The fallback, when there is no TEST_TMPDIR, is keyed by the checkout and the
// test's name under the persistent root. Not os.MkdirTemp, for two reasons: a
// fresh random name per process turns a SIGKILL'd run's multi-GB output base
// into a leak nothing can ever find again, where this bound is the one the old
// code had -- one output base per test per checkout, overwritten in place by
// that test's next run -- and os.MkdirTemp("", ...) is os.TempDir(), which is
// the tmpfs the comment here used to warn about. Adding the checkout is what
// the old path lacked; the surviving bound is one run of a given test per
// checkout at a time, which is what invoking a runner by hand means anyway.
func runRoot(name, checkout string) (string, error) {
	if dir := os.Getenv("TEST_TMPDIR"); dir != "" {
		return dir, nil
	}
	sum := sha256.Sum256([]byte(checkout))
	dir := filepath.Join(cacheRoot(), "runs", hex.EncodeToString(sum[:6]), name)
	return dir, os.MkdirAll(dir, 0o755)
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

// The output base is KEPT, for two different reasons on the two paths. Under
// `bazel test` it sits inside TEST_TMPDIR, which the next `bazel test` in this
// output base clears anyway, so deleting it here would only bill this run for
// gigabytes of unlink. Outside it, the run root is stable, so the next run of
// this test from this checkout reuses the output base instead of re-fetching
// its toolchains -- the ~13.5s in runRoot -- and overwrites it in place, which
// is what bounds it. Either way the shutdown releases the server first.
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
// it refuses with "repo contents cache is inside main repo". Its output base is
// passed explicitly and does live under that directory when there is one; what
// must not follow it there is everything the nested server derives from
// TEST_TMPDIR on its own.
func nestedEnv() []string {
	env := []string{}
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "TEST_TMPDIR=") || strings.HasPrefix(entry, "BAZELISK_HOME=") {
			continue
		}
		env = append(env, entry)
	}
	return append(env, "BAZELISK_HOME="+bazeliskHome())
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

// Server is a `bazel run` target left running, and the base URL it answers on.
type Server struct {
	Base string
	Log  string

	cmd  *exec.Cmd
	out  *strings.Builder
	stop func()
	once sync.Once
}

// Stop shuts the server down early, for a test that needs to look at what it
// left behind. Stopping twice is safe; cleanup calls it again.
func (s *Server) Stop() {
	s.once.Do(s.stop)
}

// Output is everything the server has written so far. A dev server reports most
// of what goes wrong on stderr and still answers 200, so an assertion that only
// reads the response body can pass over a broken one.
func (s *Server) Output() string { return s.out.String() }

// Serve starts a `bazel run` target on a free port and waits for it to answer,
// returning the base URL. The target is stopped when the test ends.
//
// The launcher ignores SIGTERM (that is how a dev server survives an ibazel
// rebuild) and bazel is its parent, so the whole process group is signalled.
func (it *IT) Serve(logName, target string, args ...string) *Server {
	port := freePort(it)
	run := append([]string{"run", target, "--", "--port", strconv.Itoa(port)}, args...)
	fmt.Printf("INFO: bazel %s\n", strings.Join(run, " "))
	cmd := it.command(run)
	out := &strings.Builder{}
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		it.Fail("cannot start %s: %v", target, err)
	}

	srv := &Server{
		Base: fmt.Sprintf("http://localhost:%d", port),
		Log:  it.Scratch(logName),
		cmd:  cmd,
		out:  out,
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	srv.stop = func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGINT)
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			<-done
		}
		_ = os.WriteFile(srv.Log, []byte(out.String()), 0o644)
	}
	it.OnCleanup(srv.Stop)

	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			_ = os.WriteFile(srv.Log, []byte(out.String()), 0o644)
			it.Fail("%s exited before it answered (%v):\n%s", target, err, out.String())
		default:
		}
		if resp, err := http.Get(srv.Base + "/"); err == nil {
			resp.Body.Close()
			return srv
		}
		time.Sleep(500 * time.Millisecond)
	}
	_ = os.WriteFile(srv.Log, []byte(out.String()), 0o644)
	it.Fail("%s never answered on %s:\n%s", target, srv.Base, out.String())
	return nil
}

// Get fetches a path from the running server and returns the status and body.
func (it *IT) Get(srv *Server, path string) (int, string) {
	resp, err := http.Get(srv.Base + path)
	if err != nil {
		it.Fail("GET %s: %v\n%s", path, err, srv.Output())
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		it.Fail("reading GET %s: %v", path, err)
	}
	return resp.StatusCode, string(body)
}

// DialHMR opens an HMR client against a running server, so a test can ask
// whether an edit was published rather than only whether the next request
// happens to render the new bytes.
func (it *IT) DialHMR(srv *Server, path, protocol string) *hmrsocket.Socket {
	sock, err := hmrsocket.Dial(strings.TrimPrefix(srv.Base, "http://"), path, protocol)
	if err != nil {
		it.Fail("%v\n%s", err, srv.Output())
	}
	it.OnCleanup(sock.Close)
	return sock
}

// freePort asks the kernel for one. The configured port would collide between
// two concurrent runs, and Vite silently increments past a collision -- so one
// run would answer the other's requests.
func freePort(it *IT) int {
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		it.Fail("cannot reserve a port: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
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
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		it.Fail("cannot create the directory for %s: %v", path, err)
	}
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
