package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bazelbuild/rules_go/go/runfiles"
)

// Resolver turns the runfiles paths in a Config into filesystem paths, and is
// the only place that knows how runfiles are laid out.
type Resolver struct {
	rf  *runfiles.Runfiles
	dir string
}

func NewResolver() (*Resolver, error) {
	return newResolver()
}

func newResolver(opts ...runfiles.Option) (*Resolver, error) {
	rf, err := runfiles.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("ts_launcher: %w", err)
	}
	return &Resolver{rf: rf, dir: runfilesDir(rf.Env())}, nil
}

// directoryResolver resolves through the runfiles tree at dir, whatever the
// environment names.
func directoryResolver(dir string) (*Resolver, error) {
	rf, err := runfiles.New(runfiles.Directory(dir))
	if err != nil {
		return nil, fmt.Errorf("ts_launcher: %w", err)
	}
	return &Resolver{rf: rf, dir: dir}, nil
}

// runfilesDir returns the absolute runfiles directory, or "" when the layout is
// manifest-only. Callers must treat "" as "no tree to write into".
func runfilesDir(env []string) string {
	if runfilesEnv(env, "RUNFILES_MANIFEST_FILE") != "" {
		return ""
	}
	for _, v := range []string{runfilesEnv(env, "RUNFILES_DIR"), runfilesEnv(env, "TEST_SRCDIR")} {
		if v == "" {
			continue
		}
		abs, err := filepath.Abs(v)
		if err != nil {
			continue
		}
		if st, err := os.Stat(abs); err == nil && st.IsDir() {
			return abs
		}
	}
	return ""
}

func runfilesEnv(env []string, name string) string {
	for _, entry := range env {
		if value, ok := strings.CutPrefix(entry, name+"="); ok {
			return value
		}
	}
	return ""
}

// Dir is the absolute runfiles directory, or "" in manifest-only mode.
func (r *Resolver) Dir() string { return r.dir }

// Env returns the runfiles variables to hand to child processes.
func (r *Resolver) Env() []string { return r.rf.Env() }

// Path resolves one runfiles path to an absolute filesystem path.
func (r *Resolver) Path(rlocation string) (string, error) {
	if rlocation == "" {
		return "", fmt.Errorf("ts_launcher: empty runfiles path")
	}
	p, err := r.rf.Rlocation(rlocation)
	if err != nil {
		return "", fmt.Errorf("ts_launcher: runfiles lookup failed for %q: %w", rlocation, err)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return abs, nil
}
