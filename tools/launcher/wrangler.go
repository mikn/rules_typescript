package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
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
	staged := make(map[string]bool, len(w.WorkerFiles))
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
		staged[rel] = true
	}

	configText, err := os.ReadFile(configFile)
	if err != nil {
		return nil, err
	}
	retargeted := retargetMain(string(configText), func(rel string) bool { return staged[rel] })
	if err := os.WriteFile(stagedConfig, []byte(retargeted), 0o644); err != nil {
		return nil, err
	}

	// esbuild -- the bundler wrangler runs over the worker -- resolves a bare
	// specifier by walking up from the importing file, and the importing file is
	// the staged copy. Without a node_modules beside it, a worker that imports
	// any npm package at all fails to bundle, and wrangler's own nodejs_compat
	// preset (unenv) fails with it. NODE_PATH does not cover either: it is not
	// consulted by esbuild, nor by an ESM import.
	linkedTree := filepath.Join(scratch, "node_modules")
	if err := os.Symlink(nodeModules, linkedTree); err != nil {
		return nil, err
	}
	// Run wrangler through the link, not through its own path in the runfiles:
	// a package it imports is a sibling of the link, not of the tree.
	entry = filepath.Join(linkedTree, filepath.FromSlash(w.WranglerInTree))

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
	// Node resolves a module to its realpath before looking for sibling
	// packages, so without this the link above buys nothing: the realpath of
	// node_modules/wrangler is the tree's own directory, whose name is the
	// Bazel target's and not "node_modules". wrangler's nodejs_compat preset
	// (unenv) is imported that way and would not resolve.
	plan.setEnv("NODE_OPTIONS", "--preserve-symlinks --preserve-symlinks-main")

	outDir := filepath.Join(scratch, "dist")
	plan.Dir = scratch
	argv := append(runtime, entry,
		"deploy", "--dry-run", "--outdir", outDir, "-c", stagedConfig)
	if w.EnvName != "" {
		argv = append(argv, "--env", w.EnvName)
	}
	plan.Argv = append(argv, args...)
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

// mainEntry matches the `main` key of a wrangler config and captures its value.
// The config is JSONC, so it is not parsed: a round trip through a JSON encoder
// would drop the comments a human wrote and hand them back a file they do not
// recognise in the error messages.
var mainEntry = regexp.MustCompile(`("main"\s*:\s*")([^"]*)(")`)

// retargetMain points a `main` that names a TypeScript entry at the JavaScript
// Bazel compiled from it.
//
// wrangler compiles TypeScript itself, so a worker's config names the .ts file
// -- which is what `wrangler dev` needs, and what every worker in a real repo
// therefore says. Bazel stages what it built, so the file beside the staged
// config is the .js, and wrangler stops at "The entry-point file at
// src/index.ts was not found". Requiring the config to name the .js instead
// would fix the dry run by breaking the dev command.
//
// A `main` whose compiled sibling was not staged is left exactly as written, so
// wrangler still reports the entry point the author named.
func retargetMain(config string, staged func(rel string) bool) string {
	return mainEntry.ReplaceAllStringFunc(config, func(match string) string {
		groups := mainEntry.FindStringSubmatch(match)
		ext := filepath.Ext(groups[2])
		if ext != ".ts" && ext != ".tsx" {
			return match
		}
		compiled := strings.TrimSuffix(groups[2], ext) + ".js"
		if !staged(strings.TrimPrefix(compiled, "./")) {
			return match
		}
		return groups[1] + compiled + groups[3]
	})
}
