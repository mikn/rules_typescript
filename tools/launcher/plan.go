package main

import (
	"encoding/json"
	"fmt"
	"os"
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
func MakePlan(cfg *Config, r *Resolver, args []string) (*Plan, error) {
	plan := &Plan{Label: cfg.Label, Mode: cfg.Mode, UseExec: true, runfilesEnv: r.Env()}
	for k, v := range cfg.Env {
		plan.setEnv(k, v)
	}
	switch cfg.Mode {
	case ModeNode:
		return planNode(cfg, r, plan, args)
	case ModeVitest:
		return planVitest(cfg, r, plan)
	case ModeDevServer:
		return planDevServer(cfg, r, plan, args)
	}
	return nil, fmt.Errorf("ts_launcher: unhandled mode %q", cfg.Mode)
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
