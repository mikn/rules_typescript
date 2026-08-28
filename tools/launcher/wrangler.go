package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// planWrangler runs `wrangler deploy --dry-run` over a worker Bazel built.
//
// A dry run is the deployable half that can be verified hermetically: it does
// everything a deploy does up to the upload -- resolves the config, bundles the
// worker, applies the compatibility date and the bindings -- and then writes the
// result to disk instead of sending it. It needs no credentials and no network.
//
// Everything is staged into a writable scratch directory first, which is not a
// convenience: wrangler puts `.wrangler/tmp` NEXT TO THE CONFIG FILE rather than
// under the cwd, and a Bazel output directory is read-only. Its own state
// directory has the same requirement, hence HOME.
func planWrangler(cfg *Config, r *Resolver, plan *Plan, args []string) (*Plan, error) {
	w := cfg.Wrangler

	runtime, err := runtimeCommand(cfg, r)
	if err != nil {
		return nil, fmt.Errorf(
			"ts_worker_dry_run: the toolchain JS runtime is missing from the runfiles of %s: %w",
			cfg.Label, err)
	}

	nodeModules, err := r.Path(w.NodeModules)
	if err != nil {
		return nil, fmt.Errorf(
			"ts_worker_dry_run: %s has no node_modules tree in runfiles: %w\n"+
				"The tree must carry @npm//:wrangler; there is no host fallback.",
			cfg.Label, err)
	}
	entry := filepath.Join(nodeModules, filepath.FromSlash(w.WranglerInTree))
	if !fileExists(entry) {
		return nil, fmt.Errorf(
			"ts_worker_dry_run: wrangler is missing from the node_modules tree of %s:\n"+
				"                 %s\n"+
				"                 Add @npm_workers//:wrangler (or your hub's) to that "+
				"node_modules() target.",
			cfg.Label, entry)
	}

	scratch, err := os.MkdirTemp(os.Getenv("TEST_TMPDIR"), "wrangler")
	if err != nil {
		return nil, err
	}

	// The config and the worker closure, side by side, because the config's
	// `main` is relative to the config.
	configFile, err := r.Path(w.ConfigFile)
	if err != nil {
		return nil, err
	}
	stagedConfig := filepath.Join(scratch, filepath.Base(configFile))
	if err := copyFile(configFile, stagedConfig); err != nil {
		return nil, err
	}
	for _, rl := range w.WorkerFiles {
		src, err := r.Path(rl)
		if err != nil {
			return nil, err
		}
		// Package-relative, so a `main` of "src/index.js" still finds it.
		rel := rl
		if w.PackagePrefix != "" && strings.HasPrefix(rl, w.PackagePrefix) {
			rel = strings.TrimPrefix(rl, w.PackagePrefix)
		}
		if err := copyFile(src, filepath.Join(scratch, filepath.FromSlash(rel))); err != nil {
			return nil, err
		}
	}

	home := filepath.Join(scratch, ".home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		return nil, err
	}
	plan.setEnv("HOME", home)
	plan.setEnv("XDG_CONFIG_HOME", home)
	plan.setEnv("TMPDIR", scratch)
	// Wrangler phones home for metrics and for a version check unless told not
	// to, which is the only reason a dry run would need the network at all.
	plan.setEnv("WRANGLER_SEND_METRICS", "false")
	plan.setEnv("CI", "true")
	plan.setEnv("NODE_PATH", nodeModules)

	outDir := filepath.Join(scratch, "dist")
	plan.Dir = scratch
	plan.Argv = append(append(runtime, entry,
		"deploy", "--dry-run", "--outdir", outDir, "-c", stagedConfig), args...)
	plan.Messages = append(plan.Messages,
		fmt.Sprintf("[ts_worker_dry_run] %s", cfg.Label),
		fmt.Sprintf("[ts_worker_dry_run] config:  %s", stagedConfig),
		fmt.Sprintf("[ts_worker_dry_run] outdir:  %s", outDir),
	)
	return plan, nil
}

func copyFile(src, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
