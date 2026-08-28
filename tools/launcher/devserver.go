package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// serverCommand builds the argv for whichever dev server implementation the
// target selected. A server inside the npm tree is a path joined onto the
// resolved tree, because a file inside a TreeArtifact has no label to resolve;
// a native binary is a runfile. ts_dev_server guarantees exactly one is set.
func serverCommand(cfg *Config, r *Resolver, configFile, nodeModules string) ([]string, string, error) {
	d := cfg.DevServer
	var argv []string
	var serverPath string

	if d.ServerInTree != "" {
		if nodeModules == "" {
			return nil, "", fmt.Errorf(
				"ts_dev_server: %s selected a server that ships inside the npm tree (%s), "+
					"but the target has no node_modules attr.\n"+
					"Add node_modules = \":node_modules\" pointing at a node_modules() target "+
					"whose deps include that package; there is no host-PATH fallback.",
				cfg.Label, d.ServerInTree)
		}
		serverPath = filepath.Join(nodeModules, filepath.FromSlash(d.ServerInTree))
		if !fileExists(serverPath) {
			return nil, "", fmt.Errorf(
				"ts_dev_server: the dev server is missing from the node_modules tree of %s:\n"+
					"                 %s\n"+
					"                 Add the package providing it to the deps of that "+
					"node_modules() target.",
				cfg.Label, serverPath)
		}
	} else {
		p, err := r.Path(d.ServerBinary)
		if err != nil {
			return nil, "", fmt.Errorf(
				"ts_dev_server: the dev server binary is missing from the runfiles of %s: %w\n"+
					"Re-run via `bazel run %s`.", cfg.Label, err, cfg.Label)
		}
		serverPath = p
	}

	if d.RunsInJsRuntime {
		runtime, err := runtimeCommand(cfg, r)
		if err != nil {
			return nil, "", fmt.Errorf(
				"ts_dev_server: the toolchain JS runtime is missing from the runfiles of %s: %w\n"+
					"Re-run via `bazel run %s`; a host node is not used.", cfg.Label, err, cfg.Label)
		}
		argv = append(runtime, serverPath)
	} else {
		argv = []string{serverPath}
	}

	// The serve root is the workspace when bazel run supplies one; the config
	// carries the same value, but a server taking it from argv does not read it
	// there. See DevServerInfo.argv.
	root := os.Getenv("BUILD_WORKSPACE_DIRECTORY")
	if root == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, "", err
		}
		root = cwd
	}
	for _, a := range d.Argv {
		a = strings.ReplaceAll(a, "{config}", configFile)
		a = strings.ReplaceAll(a, "{root}", root)
		argv = append(argv, a)
	}
	return argv, serverPath, nil
}

func planDevServer(cfg *Config, r *Resolver, plan *Plan, args []string) (*Plan, error) {
	d := cfg.DevServer
	if cfg.Runtime == "" {
		return nil, fmt.Errorf(
			"ts_dev_server: %s resolved no JS runtime toolchain.\n"+
				"Did you mean to register the toolchains in MODULE.bazel?\n"+
				"    register_toolchains(\"@rules_typescript//ts/toolchain:all\")", cfg.Label)
	}
	configFile, err := r.Path(d.ConfigFile)
	if err != nil {
		return nil, err
	}
	if d.NodeModules == "" {
		return nil, fmt.Errorf(
			"ts_dev_server: %s has no node_modules attr, so the app's own dependencies "+
				"are not in runfiles.\n"+
				"Add node_modules = \":node_modules\" pointing at a node_modules() target; "+
				"the generated config resolves every bare specifier through that tree.", cfg.Label)
	}
	nodeModules, err := r.Path(d.NodeModules)
	if err != nil {
		return nil, err
	}
	plan.setEnv("NODE_MODULES_PATH", nodeModules)

	// NODE_PATH as well, which Node's own ESM resolution ignores -- but not every
	// resolver in the graph is Node's. Tailwind v4 resolves `@import "tailwindcss"`
	// from the CSS file's own directory using enhanced-resolve, which reads
	// NODE_PATH explicitly; a source-tree .css has no node_modules above it to
	// walk up to, so without this the dev server answers 500 for that stylesheet.
	plan.prependPath("NODE_PATH", nodeModules)

	argv, serverPath, err := serverCommand(cfg, r, configFile, nodeModules)
	if err != nil {
		return nil, err
	}

	// A native server still runs a Node plugin host, so the toolchain node has
	// to be findable by name rather than only by the argv above.
	if !d.RunsInJsRuntime {
		if runtime, err := runtimeCommand(cfg, r); err == nil && len(runtime) > 0 {
			plan.setEnv("PATH", filepath.Dir(runtime[0])+string(os.PathListSeparator)+os.Getenv("PATH"))
		}
	}

	for name, rl := range map[string]string{
		"VITE_PLUGIN_PATH":            d.Plugin,
		"VITE_CSS_MODULE_PLUGIN_PATH": d.CSSModulePlugin,
		"VITE_USER_CONFIG_PATH":       d.UserConfig,
		"BUNDLER_BINARY":              d.BundlerBinary,
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
		fmt.Sprintf("[ts_dev_server] Starting dev server on port %d...", d.Port),
		fmt.Sprintf("[ts_dev_server] Workspace: %s", workspace),
		fmt.Sprintf("[ts_dev_server] bazel-bin: %s", bazelBin),
		fmt.Sprintf("[ts_dev_server] node_modules: %s", nodeModules),
		fmt.Sprintf("[ts_dev_server] Config: %s", configFile),
		fmt.Sprintf("[ts_dev_server] Server: %s", serverPath),
	)

	plan.Argv = append(argv, args...)
	plan.UseExec = false
	// ibazel SIGTERMs the runner after every rebuild; the server has to survive
	// that and pick the new .js up through its watcher, so only Ctrl-C stops it.
	plan.Supervise = SuperviseOptions{IgnoreTerm: true, ExitZeroOnInterrupt: true}
	return plan, nil
}
