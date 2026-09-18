package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Plan is everything the launcher decided before it touched the process table.
// Keeping it a value is what makes the decisions unit-testable and what
// --dump-config prints.
type Plan struct {
	Label        string            `json:"label"`
	Mode         string            `json:"mode"`
	Argv         []string          `json:"argv"`
	Dir          string            `json:"dir,omitempty"`
	EnvOverrides map[string]string `json:"env,omitempty"`
	Messages     []string          `json:"messages,omitempty"`
	ExitEarly    bool              `json:"exit_early,omitempty"`

	UseExec     bool `json:"use_exec"`
	runfilesEnv []string
	Supervise   SuperviseOptions `json:"-"`
	Cleanup     func()           `json:"-"`
	PostRun     func(int) error  `json:"-"`
}

func (p *Plan) setEnv(key, value string) {
	if p.EnvOverrides == nil {
		p.EnvOverrides = map[string]string{}
	}
	p.EnvOverrides[key] = value
}

func (p *Plan) prependPath(key, value string) {
	existing := os.Getenv(key)
	if cur, ok := p.EnvOverrides[key]; ok {
		existing = cur
	}
	if existing == "" {
		p.setEnv(key, value)
		return
	}
	p.setEnv(key, value+string(os.PathListSeparator)+existing)
}

// MakePlan resolves a config into a Plan. args are the arguments the caller
// passed to the launcher, already stripped of launcher flags.
func MakePlan(cfg *Config, r *Resolver, args []string, shard Shard) (*Plan, error) {
	plan := &Plan{Label: cfg.Label, Mode: cfg.Mode, UseExec: true, runfilesEnv: r.Env()}
	for k, v := range cfg.Env {
		plan.setEnv(k, v)
	}
	switch cfg.Mode {
	case ModeNode:
		return planNode(cfg, r, plan, args)
	case ModeVitest:
		return planVitest(cfg, r, plan, args, shard)
	case ModeNodeTest:
		return planNodeTest(cfg, r, plan, args, shard)
	case ModeDevServer:
		return planDevServer(cfg, r, plan, args)
	}
	return nil, fmt.Errorf("ts_launcher: unhandled mode %q", cfg.Mode)
}

type Shard struct {
	Index int
	Total int
}

func parseShard(index, total string) (Shard, error) {
	shard := Shard{Total: 1}
	var err error
	if index != "" {
		shard.Index, err = strconv.Atoi(index)
		if err != nil {
			return Shard{}, fmt.Errorf("ts_test: invalid TEST_SHARD_INDEX: %w", err)
		}
	}
	if total != "" {
		shard.Total, err = strconv.Atoi(total)
		if err != nil {
			return Shard{}, fmt.Errorf("ts_test: invalid TEST_TOTAL_SHARDS: %w", err)
		}
	}
	if shard.Total < 1 || shard.Index < 0 || shard.Index >= shard.Total {
		return Shard{}, fmt.Errorf("ts_test: shard index %d must be in [0, %d)", shard.Index, shard.Total)
	}
	return shard, nil
}

func advertiseSharding(path string) error {
	if path == "" {
		return nil
	}
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		return fmt.Errorf("ts_test: advertising sharding: %w", err)
	}
	return nil
}

func emptyShard(plan *Plan, shard Shard) *Plan {
	plan.ExitEarly = true
	plan.Messages = append(plan.Messages, fmt.Sprintf(
		"ts_test: no test files assigned to shard %d/%d", shard.Index, shard.Total))
	return plan
}

// runtimeCommand resolves the JS runtime: the toolchain binary from runfiles,
// or a bare "node" for the rules that document a system fallback.
func runtimeCommand(cfg *Config, r *Resolver) ([]string, error) {
	if cfg.Runtime == "" {
		return append([]string{"node"}, cfg.RunArgs...), nil
	}
	path, err := r.Path(cfg.Runtime)
	if err != nil {
		return nil, err
	}
	return append([]string{path}, cfg.RunArgs...), nil
}

// installNodeModules puts the chain on NODE_PATH nearest first and links its
// root in as <workspace>/node_modules, the walk-up's last stop for ESM.
func installNodeModules(
	r *Resolver, plan *Plan, root, workspace string, rlocations []string,
) ([]string, error) {
	if len(rlocations) == 0 {
		return nil, nil
	}
	if r.Dir() == "" {
		if err := stageNodeModules(root, runfilesEnv(r.Env(), "RUNFILES_MANIFEST_FILE")); err != nil {
			return nil, err
		}
	}
	dirs := make([]string, 0, len(rlocations))
	for _, rlocation := range rlocations {
		dirs = append(dirs, filepath.Join(root, filepath.FromSlash(rlocation)))
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		if isDir(dirs[i]) {
			plan.prependPath("NODE_PATH", dirs[i])
		}
	}
	if chainRoot := dirs[len(dirs)-1]; isDir(chainRoot) {
		linkAs(filepath.Join(root, workspace, "node_modules"), chainRoot)
	}
	return dirs, nil
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// stageNodeModules links every node_modules entry of the manifest into root
// at its runfiles path, a declared link's relative target kept verbatim.
func stageNodeModules(root, manifest string) error {
	if manifest == "" {
		return errors.New("ts_launcher: no runfiles directory and no " +
			"RUNFILES_MANIFEST_FILE")
	}
	data, err := os.ReadFile(manifest)
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		rlocation, target, ok := manifestEntry(line)
		if !ok || !strings.Contains(rlocation, "/node_modules/") {
			continue
		}
		link := filepath.Join(root, filepath.FromSlash(rlocation))
		if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
			return err
		}
		if err := os.Remove(link); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := os.Symlink(filepath.FromSlash(target), link); err != nil {
			return err
		}
	}
	return nil
}

// manifestEntry reads one runfiles manifest line, "<rlocation> <target>"; a
// line starting with a space escapes both as \s, \n and \b.
func manifestEntry(line string) (rlocation, target string, ok bool) {
	escaped := strings.HasPrefix(line, " ")
	rlocation, target, ok = strings.Cut(strings.TrimPrefix(line, " "), " ")
	if !ok || rlocation == "" {
		return "", "", false
	}
	if escaped {
		unescape := strings.NewReplacer(`\s`, " ", `\n`, "\n", `\b`, `\`)
		rlocation, target = unescape.Replace(rlocation), unescape.Replace(target)
	}
	return rlocation, target, true
}

// linkAs creates link -> target unless something already sits at link.
func linkAs(link, target string) {
	if _, err := os.Lstat(link); err == nil {
		return
	}
	_ = os.Symlink(target, link)
}

func Run(plan *Plan) (int, error) {
	if plan.Cleanup != nil && !plan.UseExec {
		defer plan.Cleanup()
	}
	for _, m := range plan.Messages {
		fmt.Fprintln(os.Stderr, m)
	}
	if plan.ExitEarly {
		return 0, nil
	}
	if plan.Dir != "" {
		if err := os.Chdir(plan.Dir); err != nil {
			return 1, err
		}
	}
	env := Environ(plan.EnvOverrides, plan.runfilesEnv)
	if plan.UseExec && plan.PostRun == nil {
		return 1, Exec(plan.Argv, env)
	}
	code, err := Supervise(plan.Argv, env, plan.Supervise)
	if err != nil {
		return code, err
	}
	if plan.PostRun != nil {
		if err := plan.PostRun(code); err != nil {
			return code, err
		}
	}
	return code, nil
}

// Dump prints the plan as JSON: the escape hatch for "what would this launcher
// actually do", which a generated shell script used to answer by being readable.
func Dump(plan *Plan, cfgPath string, w *os.File) error {
	payload := struct {
		ConfigFile string `json:"config_file"`
		*Plan
	}{ConfigFile: cfgPath, Plan: plan}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}
