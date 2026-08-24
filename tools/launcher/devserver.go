package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func planDevServer(cfg *Config, r *Resolver, plan *Plan, args []string) (*Plan, error) {
	d := cfg.DevServer
	if cfg.Runtime == "" {
		return nil, fmt.Errorf(
			"ts_dev_server: %s resolved no JS runtime toolchain.\n"+
				"Did you mean to register the toolchains in MODULE.bazel?\n"+
				"    register_toolchains(\"@rules_typescript//ts/toolchain:all\")", cfg.Label)
	}
	runtime, err := runtimeCommand(cfg, r)
	if err != nil {
		return nil, fmt.Errorf(
			"ts_dev_server: the toolchain JS runtime is missing from the runfiles of %s: %w\n"+
				"Re-run via `bazel run %s`; a host node is not used.", cfg.Label, err, cfg.Label)
	}
	configFile, err := r.Path(d.ConfigFile)
	if err != nil {
		return nil, err
	}
	if d.NodeModules == "" {
		return nil, fmt.Errorf(
			"ts_dev_server: %s has no node_modules attr, so vite is not in runfiles.\n"+
				"Add node_modules = \":node_modules\" pointing at a node_modules() target whose "+
				"deps include @npm//:vite; there is no host-PATH fallback.", cfg.Label)
	}
	nodeModules, err := r.Path(d.NodeModules)
	if err != nil {
		return nil, err
	}
	viteBin := filepath.Join(nodeModules, filepath.FromSlash(d.ViteInTree))
	if !fileExists(viteBin) {
		return nil, fmt.Errorf(
			"ts_dev_server: vite is missing from the node_modules tree of %s:\n"+
				"                 %s\n"+
				"                 Add @npm//:vite to the deps of that node_modules() target.",
			cfg.Label, viteBin)
	}
	plan.setEnv("NODE_MODULES_PATH", nodeModules)

	for name, rl := range map[string]string{
		"VITE_PLUGIN_PATH":      d.Plugin,
		"VITE_USER_CONFIG_PATH": d.UserConfig,
		"BUNDLER_BINARY":        d.BundlerBinary,
	} {
		if rl == "" {
			continue
		}
		p, err := r.Path(rl)
		if err != nil {
			return nil, err
		}
		plan.setEnv(name, p)
	}

	workspace := os.Getenv("BUILD_WORKSPACE_DIRECTORY")
	bazelBin := ""
	if workspace != "" {
		plan.Dir = workspace
		bazelBin = filepath.Join(workspace, "bazel-bin")
	} else {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		bazelBin = filepath.Join(cwd, "bazel-bin")
		workspace = cwd
	}
	plan.setEnv("BAZEL_BIN_DIR", bazelBin)

	plan.Messages = append(plan.Messages,
		fmt.Sprintf("[ts_dev_server] Starting Vite dev server on port %d...", d.Port),
		fmt.Sprintf("[ts_dev_server] Workspace: %s", workspace),
		fmt.Sprintf("[ts_dev_server] bazel-bin: %s", bazelBin),
		fmt.Sprintf("[ts_dev_server] node_modules: %s", nodeModules),
		fmt.Sprintf("[ts_dev_server] Config: %s", configFile),
		fmt.Sprintf("[ts_dev_server] Vite: %s", viteBin),
	)

	plan.Argv = append(append(runtime, viteBin, "dev", "--config", configFile), args...)
	plan.UseExec = false
	// ibazel SIGTERMs the runner after every rebuild; vite has to survive that
	// and pick the new .js up through its watcher, so only Ctrl-C stops it.
	plan.Supervise = SuperviseOptions{IgnoreTerm: true, ExitZeroOnInterrupt: true}
	return plan, nil
}
