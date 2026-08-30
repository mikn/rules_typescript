package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// serverCommand builds the argv for whichever dev server implementation the
// target selected. A server inside the npm tree is a path joined onto the
// resolved tree, because a file inside a TreeArtifact has no label to resolve;
// a native binary is a runfile. ts_dev_server guarantees exactly one is set.
func serverCommand(cfg *Config, r *Resolver, configFile, nodeModules string, port int) ([]string, string, error) {
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
	named := false
	for _, a := range d.Argv {
		if strings.Contains(a, "{port}") {
			named = true
		}
		a = strings.ReplaceAll(a, "{config}", configFile)
		a = strings.ReplaceAll(a, "{root}", root)
		a = strings.ReplaceAll(a, "{port}", strconv.Itoa(port))
		argv = append(argv, a)
	}
	// A server whose argv does not name the port reads it from the config, and
	// the flag is how an override reaches it -- Vite's CLI --port beats what the
	// config says. One whose argv does name it has already been given the same
	// number, and passing it twice is an error rather than a later-wins.
	if !named {
		argv = append(argv, "--port", strconv.Itoa(port))
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

	// A server whose argv names the port takes the override there; one that reads
	// it from the config still gets it appended, which is where it looked before.
	port, args := portOverride(d.Port, args)
	argv, serverPath, err := serverCommand(cfg, r, configFile, nodeModules, port)
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

	anchor, err := anchorNodeModules(workspace, nodeModules, plan)
	if err != nil {
		return nil, err
	}

	plan.Messages = append(plan.Messages,
		fmt.Sprintf("[ts_dev_server] Starting dev server on port %d...", port),
		fmt.Sprintf("[ts_dev_server] Workspace: %s", workspace),
		fmt.Sprintf("[ts_dev_server] bazel-bin: %s", bazelBin),
		fmt.Sprintf("[ts_dev_server] node_modules: %s", nodeModules),
		fmt.Sprintf("[ts_dev_server] Config: %s", configFile),
		fmt.Sprintf("[ts_dev_server] Server: %s", serverPath),
	)
	if anchor != "" {
		plan.Messages = append(plan.Messages, fmt.Sprintf(
			"[ts_dev_server] Linked %s -> the npm tree; removed on Ctrl-C. "+
				"Add `node_modules` (no trailing slash) to .gitignore.", anchor))
	}

	plan.Argv = append(argv, args...)
	plan.UseExec = false
	// ibazel SIGTERMs the runner after every rebuild; the server has to survive
	// that and pick the new .js up through its watcher, so only Ctrl-C stops it.
	plan.Supervise = SuperviseOptions{IgnoreTerm: true, ExitZeroOnInterrupt: true}
	return plan, nil
}

// anchorNodeModules links the npm tree in as <workspace>/node_modules, and
// returns the link it created, or "" when one was already there.
//
// This is what makes a bare `import "react"` resolve at all in the parts of a
// bundler that no plugin can reach. Vite's SSR externalisation and its
// optimizeDeps.include resolution both call the resolver directly rather than
// through the plugin container, and both walk the directory chain up from the
// importer -- or, for a package in resolve.dedupe, up from `root` regardless of
// the importer. Above a checked-in source file there is nothing to find: the
// npm tree is a Bazel output somewhere else entirely. Naming it here is the
// only place the walk can see it, and it is what a bundler outside Bazel would
// have found anyway.
//
// An existing path is never replaced. A real directory is somebody's install
// and a link to another tree belongs to another dev server; either way the two
// answers cannot both be right, so the launcher says which two and stops.
func anchorNodeModules(workspace, nodeModules string, plan *Plan) (string, error) {
	link := filepath.Join(workspace, "node_modules")
	switch target, err := os.Readlink(link); {
	case err == nil && target == nodeModules:
		return "", nil
	case err == nil:
		return "", fmt.Errorf(
			"ts_dev_server: %s is already a symlink to a different npm tree:\n"+
				"                 have %s\n"+
				"                 want %s\n"+
				"A dev server resolves bare specifiers by walking up from the "+
				"workspace root, so the two trees cannot both be there. Stop the "+
				"other dev server, or point both targets at one node_modules().",
			link, target, nodeModules)
	}
	if _, err := os.Lstat(link); err == nil {
		return "", fmt.Errorf(
			"ts_dev_server: %s already exists and is not a symlink.\n"+
				"That is usually a `pnpm install` tree. The dev server resolves "+
				"through the Bazel npm tree at\n"+
				"                 %s\n"+
				"and will not delete an install to get there -- remove it yourself "+
				"if Bazel should own the dependencies.", link, nodeModules)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.Symlink(nodeModules, link); err != nil {
		return "", fmt.Errorf(
			"ts_dev_server: could not link %s -> %s: %w\n"+
				"On Windows a symlink needs Developer Mode or an elevated shell.",
			link, nodeModules, err)
	}
	// ibazel SIGTERMs the launcher on every rebuild and Supervise.IgnoreTerm
	// swallows it, so this runs on Ctrl-C rather than once per rebuild. Remove
	// the link only -- never its target, which is the Bazel tree.
	plan.Cleanup = func() { os.Remove(link) }
	return link, nil
}
