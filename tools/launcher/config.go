// Command ts_launcher runs the Node.js process described by a JSON config that
// a rules_typescript rule wrote beside it at analysis time. One checked-in
// binary replaces the shell script each rule used to generate.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ConfigEnvVar overrides where the config is read from.
const ConfigEnvVar = "TS_LAUNCHER_CONFIG"

// DumpEnvVar makes the launcher print the resolved config and exit.
const DumpEnvVar = "TS_LAUNCHER_DUMP_CONFIG"

// DumpFlag is the first-argument form of DumpEnvVar.
const DumpFlag = "--dump-config"

const (
	ModeNode      = "node"
	ModeVitest    = "vitest"
	ModeDevServer = "devserver"
)

// Config is the whole contract between the Starlark rules and this binary.
// Every path field is a runfiles path; nothing here is ever shell-quoted.
type Config struct {
	Label     string            `json:"label"`
	Mode      string            `json:"mode"`
	Workspace string            `json:"workspace"`
	Runtime   string            `json:"runtime,omitempty"`
	RunArgs   []string          `json:"runtime_args,omitempty"`
	Env       map[string]string `json:"env,omitempty"`

	Node      *NodeConfig      `json:"node,omitempty"`
	Vitest    *VitestConfig    `json:"vitest,omitempty"`
	DevServer *DevServerConfig `json:"dev_server,omitempty"`
}

// NodeConfig runs one .js entry point.
type NodeConfig struct {
	Entry         string        `json:"entry"`
	NodeModules   string        `json:"node_modules,omitempty"`
	ChdirRunfiles bool          `json:"chdir_runfiles,omitempty"`
	OptionalDeps  []PackageLink `json:"optional_deps,omitempty"`
}

// PackageLink is one npm package to expose under a private node_modules dir.
type PackageLink struct {
	Name        string `json:"name"`
	PackageJSON string `json:"package_json"`
}

// VitestConfig runs the vitest CLI over a sharded set of compiled test files.
type VitestConfig struct {
	Vitest          string `json:"vitest,omitempty"`
	VitestInTree    string `json:"vitest_in_tree,omitempty"`
	VitestIsNpmBin  bool   `json:"vitest_is_npm_bin,omitempty"`
	ConfigFile      string `json:"config_file"`
	TestFilesList   string `json:"test_files_list"`
	NodeModules     string `json:"node_modules,omitempty"`
	UpdateSnapshots bool   `json:"update_snapshots,omitempty"`
	Coverage        bool   `json:"coverage,omitempty"`
}

// DevServerConfig runs the vite dev server out of the node_modules tree.
type DevServerConfig struct {
	ConfigFile    string `json:"config_file"`
	NodeModules   string `json:"node_modules,omitempty"`
	ViteInTree    string `json:"vite_in_tree"`
	Plugin        string `json:"plugin,omitempty"`
	UserConfig    string `json:"user_config,omitempty"`
	BundlerBinary string `json:"bundler_binary,omitempty"`
	Port          int    `json:"port"`
}

// LoadConfig reads the config for this launcher.
func LoadConfig(argv0 string, r *Resolver) (*Config, string, error) {
	path, err := configPath(argv0, r)
	if err != nil {
		return nil, "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, path, fmt.Errorf("ts_launcher: reading config %s: %w", path, err)
	}
	cfg, err := ParseConfig(data)
	if err != nil {
		return nil, path, fmt.Errorf("ts_launcher: parsing config %s: %w", path, err)
	}
	return cfg, path, nil
}

// configPath finds the per-target config, staged at the runfiles root under
// "<launcher basename>.json" -- reachable from `bazel run`, `bazel test`, and
// from another rule's action, where argv[0] has nothing beside it.
func configPath(argv0 string, r *Resolver) (string, error) {
	if p := os.Getenv(ConfigEnvVar); p != "" {
		return p, nil
	}
	if argv0 == "" {
		return "", errors.New("ts_launcher: argv[0] is empty and " + ConfigEnvVar + " is unset")
	}
	name := filepath.Base(argv0) + ".json"
	tried := []string{}
	if r != nil {
		tried = append(tried, "runfiles:"+name)
		if p, err := r.Path(name); err == nil && isRegular(p) {
			return p, nil
		}
	}
	for _, base := range candidateArgv0(argv0) {
		candidate := base + ".json"
		tried = append(tried, candidate)
		if isRegular(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf(
		"ts_launcher: no config found for argv[0] %q (tried %s).\n"+
			"The rule stages <launcher>.json in the launcher's runfiles; set %s to point at it.",
		argv0, strings.Join(tried, ", "), ConfigEnvVar)
}

func isRegular(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Mode().IsRegular()
}

func candidateArgv0(argv0 string) []string {
	out := []string{argv0}
	if !filepath.IsAbs(argv0) {
		if cwd, err := os.Getwd(); err == nil {
			out = append(out, filepath.Join(cwd, argv0))
		}
	}
	return out
}

// ParseConfig decodes and validates a config document.
func ParseConfig(data []byte) (*Config, error) {
	cfg := &Config{}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(cfg); err != nil {
		return nil, err
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) validate() error {
	switch c.Mode {
	case ModeNode:
		if c.Node == nil {
			return errors.New(`mode "node" requires a "node" section`)
		}
		if c.Node.Entry == "" {
			return errors.New(`mode "node" requires node.entry`)
		}
	case ModeVitest:
		if c.Vitest == nil {
			return errors.New(`mode "vitest" requires a "vitest" section`)
		}
		if c.Vitest.ConfigFile == "" || c.Vitest.TestFilesList == "" {
			return errors.New(`mode "vitest" requires vitest.config_file and vitest.test_files_list`)
		}
	case ModeDevServer:
		if c.DevServer == nil {
			return errors.New(`mode "devserver" requires a "dev_server" section`)
		}
		if c.DevServer.ConfigFile == "" {
			return errors.New(`mode "devserver" requires dev_server.config_file`)
		}
	case "":
		return errors.New(`missing "mode"`)
	default:
		return fmt.Errorf("unknown mode %q (want %q, %q or %q)", c.Mode, ModeNode, ModeVitest, ModeDevServer)
	}
	return nil
}
