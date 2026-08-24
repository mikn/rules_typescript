package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func planNode(cfg *Config, r *Resolver, plan *Plan, args []string) (*Plan, error) {
	n := cfg.Node
	argv, err := runtimeCommand(cfg, r)
	if err != nil {
		return nil, err
	}
	entry, err := r.Path(n.Entry)
	if err != nil {
		return nil, err
	}

	if n.ChdirRunfiles && r.Dir() != "" {
		plan.Dir = r.Dir()
		plan.setEnv("RUNFILES_DIR", r.Dir())
	}

	if n.NodeModules != "" {
		if dir, err := r.Path(n.NodeModules); err == nil {
			if st, statErr := os.Stat(dir); statErr == nil && st.IsDir() {
				plan.prependPath("NODE_PATH", dir)
			}
		}
	}

	if len(n.OptionalDeps) > 0 {
		// A temp directory, not the runfiles tree: the runfiles tree is read-only
		// for tools inside action sandboxes and immutable after `bazel run`.
		tmp, err := os.MkdirTemp("", "ts_launcher_nm")
		if err != nil {
			return nil, err
		}
		plan.Cleanup = func() { _ = os.RemoveAll(tmp) }
		plan.UseExec = false
		for _, dep := range n.OptionalDeps {
			if err := linkPackage(r, tmp, dep); err != nil {
				return nil, err
			}
		}
		plan.prependPath("NODE_PATH", tmp)
	}

	plan.Argv = append(append(argv, entry), args...)
	return plan, nil
}

// linkPackage exposes one npm package as $root/<name>, because Node resolves
// require.resolve('<name>/binary') only inside a directory called node_modules
// and one-repository-per-package leaves no such directory near the script.
func linkPackage(r *Resolver, root string, dep PackageLink) error {
	if dep.Name == "" || dep.PackageJSON == "" {
		return fmt.Errorf("ts_launcher: optional dep needs both name and package_json, got %+v", dep)
	}
	pkgJSON, err := r.Path(dep.PackageJSON)
	if err != nil {
		return err
	}
	link := filepath.Join(root, filepath.FromSlash(dep.Name))
	if strings.HasPrefix(dep.Name, "@") && !strings.Contains(dep.Name, "/") {
		return fmt.Errorf("ts_launcher: scoped package name %q has no '/'", dep.Name)
	}
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		return err
	}
	_ = os.Remove(link)
	return os.Symlink(filepath.Dir(pkgJSON), link)
}
