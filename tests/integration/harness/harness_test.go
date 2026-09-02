package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setEnv clears every variable the two root functions read, then applies want.
// Leaving one set from the ambient `bazel test` environment would let a case
// pass for the wrong reason -- HOME and TEST_TMPDIR are both set when these
// tests themselves run.
func setEnv(t *testing.T, env map[string]string) {
	t.Helper()
	for _, key := range []string{"RULES_TS_IT_SCRATCH", "XDG_CACHE_HOME", "HOME", "TMPDIR", "TEST_TMPDIR", "BAZELISK_HOME"} {
		t.Setenv(key, env[key])
	}
}

func TestCacheRoot(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  map[string]string
		want string
	}{{
		name: "RULES_TS_IT_SCRATCH wins over everything",
		env: map[string]string{
			"RULES_TS_IT_SCRATCH": "/ci",
			"XDG_CACHE_HOME":      "/xdg",
			"HOME":                "/home/dev",
			"TMPDIR":              "/tmpfs",
		},
		want: "/ci",
	}, {
		name: "XDG_CACHE_HOME beats HOME",
		env: map[string]string{
			"XDG_CACHE_HOME": "/xdg",
			"HOME":           "/home/dev",
			"TMPDIR":         "/tmpfs",
		},
		want: "/xdg/rules_typescript_it",
	}, {
		name: "HOME beats the temp dir",
		env: map[string]string{
			"HOME":   "/home/dev",
			"TMPDIR": "/tmpfs",
		},
		want: "/home/dev/.cache/rules_typescript_it",
	}, {
		name: "the last resort is os.TempDir()",
		env: map[string]string{
			"TMPDIR": "/tmpfs",
		},
		want: "/tmpfs/rules_typescript_it",
	}} {
		t.Run(tc.name, func(t *testing.T) {
			setEnv(t, tc.env)
			if got := cacheRoot(); got != tc.want {
				t.Errorf("cacheRoot() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TEST_TMPDIR is what separates two checkouts running one test at once: Bazel
// keys it by target AND by output base, so the harness must hand it back
// untouched rather than deriving a name of its own.
func TestRunRootPrefersTestTmpdir(t *testing.T) {
	tmp := t.TempDir()
	// A writable persistent root, so a run root taken from there would be a
	// plausible path and not an error: the assertion has to be which one was
	// chosen, not whether the other one happened to be creatable.
	setEnv(t, map[string]string{
		"TEST_TMPDIR":         tmp,
		"RULES_TS_IT_SCRATCH": t.TempDir(),
		"HOME":                t.TempDir(),
	})
	got, err := runRoot("new_project", "/checkout/a")
	if err != nil {
		t.Fatalf("runRoot: %v", err)
	}
	if got != tmp {
		t.Errorf("runRoot() = %q, want the TEST_TMPDIR %q", got, tmp)
	}
}

// The fallback carries the leak bound: a stable name per (checkout, test) is
// what lets the next run of that test overwrite a SIGKILL'd run's multi-GB
// output base in place. A random per-process name would leak one per kill.
func TestRunRootFallbackIsStablePerCheckoutAndName(t *testing.T) {
	home := t.TempDir()
	setEnv(t, map[string]string{"HOME": home, "TMPDIR": t.TempDir()})

	first, err := runRoot("new_project", "/checkout/a")
	if err != nil {
		t.Fatalf("runRoot: %v", err)
	}
	again, err := runRoot("new_project", "/checkout/a")
	if err != nil {
		t.Fatalf("runRoot: %v", err)
	}
	if first != again {
		t.Errorf("two runs of one test from one checkout got %q and %q; a stale output base at the first is unreachable from the second", first, again)
	}

	otherCheckout, err := runRoot("new_project", "/checkout/b")
	if err != nil {
		t.Fatalf("runRoot: %v", err)
	}
	if otherCheckout == first {
		t.Errorf("two checkouts share the run root %q, which is the collision this keying exists to remove", first)
	}

	otherName, err := runRoot("npm_deps", "/checkout/a")
	if err != nil {
		t.Fatalf("runRoot: %v", err)
	}
	if otherName == first {
		t.Errorf("two tests share the run root %q", first)
	}
}

// A nested output base runs to gigabytes and os.TempDir() is a tmpfs on plenty
// of machines, so the fallback must land under the persistent root, on the same
// real disk the caches use.
func TestRunRootFallbackIsUnderTheCacheRootAndExists(t *testing.T) {
	home := t.TempDir()
	tmpfs := t.TempDir()
	setEnv(t, map[string]string{"HOME": home, "TMPDIR": tmpfs})

	dir, err := runRoot("new_project", "/checkout/a")
	if err != nil {
		t.Fatalf("runRoot: %v", err)
	}
	if dir == "" {
		t.Fatal("runRoot() returned an empty fallback root")
	}
	root := filepath.Join(home, ".cache", "rules_typescript_it")
	if !strings.HasPrefix(dir, root+string(filepath.Separator)) {
		t.Errorf("fallback run root %q is not under the persistent root %q", dir, root)
	}
	if strings.HasPrefix(dir, tmpfs+string(filepath.Separator)) {
		t.Errorf("fallback run root %q is under os.TempDir(), where a multi-GB output base can hit ENOSPC on a tmpfs", dir)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Errorf("os.Stat(%q) = %v, %v; runRoot must create the directory it names", dir, info, err)
	}
}

// The retained output base is the ~13.5s in runRoot's comment, and on the
// fallback path it is also what bounds the leak -- the next run overwrites it
// rather than adding one. Both stop being true if cleanup() deletes it.
func TestCleanupKeepsTheOutputBase(t *testing.T) {
	base := t.TempDir()
	it := &IT{
		WorkspaceDir: filepath.Join(base, "workspace"),
		OutputBase:   filepath.Join(base, "output_base"),
		scratchDir:   filepath.Join(base, "scratch"),
	}
	for _, dir := range []string{it.WorkspaceDir, it.OutputBase, it.scratchDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "marker"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	it.cleanup()

	if _, err := os.Stat(filepath.Join(it.OutputBase, "marker")); err != nil {
		t.Errorf("cleanup() removed the output base: %v", err)
	}
	for _, dir := range []string{it.WorkspaceDir, it.scratchDir} {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("cleanup() left %s behind (err = %v)", dir, err)
		}
	}
}

// envValue returns the effective value of key in a nestedEnv()-shaped slice,
// and how many times it appears. exec gives a later duplicate precedence, so a
// count above one is not wrong on its own -- it is just a thing a reader of the
// nested process's environment should not have to reason about.
func envValue(env []string, key string) (string, int) {
	value, count := "", 0
	for _, entry := range env {
		if rest, ok := strings.CutPrefix(entry, key+"="); ok {
			value, count = rest, count+1
		}
	}
	return value, count
}

// `bazel_binary` is not Bazel. It is a bazelisk wrapper that defaults
// BAZELISK_HOME to $PWD, and command() runs it from the per-run WorkspaceDir
// that prepare() has just recreated -- so with the variable unset, every test
// in the suite downloads Bazel from releases.bazel.build on every run. That is
// the one fetch shareRepositoryCache's two caches do not cover, and it is what
// turned one runner's DNS timeout into a red leg while every green run hid it
// (Bazel echoes a test's stdout only on failure).
func TestNestedEnvSharesTheBazeliskCache(t *testing.T) {
	scratch := t.TempDir()
	setEnv(t, map[string]string{"RULES_TS_IT_SCRATCH": scratch})

	got, count := envValue(nestedEnv(), "BAZELISK_HOME")
	if want := filepath.Join(scratch, "bazelisk"); got != want {
		t.Errorf("BAZELISK_HOME = %q, want %q", got, want)
	}
	if count != 1 {
		t.Errorf("BAZELISK_HOME appears %d times, want exactly 1", count)
	}
}

// The escape hatch: a developer who already has a populated bazelisk cache, or
// a CI job that caches one elsewhere, keeps it. Only the default is ours.
func TestNestedEnvKeepsAnExplicitBazeliskHome(t *testing.T) {
	setEnv(t, map[string]string{
		"RULES_TS_IT_SCRATCH": t.TempDir(),
		"BAZELISK_HOME":       "/dev/bazelisk",
	})

	got, count := envValue(nestedEnv(), "BAZELISK_HOME")
	if got != "/dev/bazelisk" {
		t.Errorf("BAZELISK_HOME = %q, want the inherited /dev/bazelisk", got)
	}
	if count != 1 {
		t.Errorf("BAZELISK_HOME appears %d times, want exactly 1", count)
	}
}

// The nested server refuses an output base under the outer execroot with "repo
// contents cache is inside main repo", which is why TEST_TMPDIR is dropped
// rather than adjusted. Pinned here because BAZELISK_HOME now travels the same
// path and the filtering is easy to break for both at once.
func TestNestedEnvDropsTestTmpdir(t *testing.T) {
	setEnv(t, map[string]string{
		"TEST_TMPDIR":         t.TempDir(),
		"RULES_TS_IT_SCRATCH": t.TempDir(),
	})

	if got, count := envValue(nestedEnv(), "TEST_TMPDIR"); count != 0 {
		t.Errorf("TEST_TMPDIR = %q (%d entries), want it dropped", got, count)
	}
}
