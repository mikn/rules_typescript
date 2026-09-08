// The tsconfig step writes the action's tsconfig from the baseline, the user's
// file and what `tsgo --showConfig` says the two mean; the oxc step reads that.

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"

	"github.com/mikn/rules_typescript/ts/tools/tsconfig"
)

// effectiveOptions is the part of the printed compilerOptions the action config
// rewrites or hands to oxc. tsgo 7 prints every enum by its lowercase name.
type effectiveOptions struct {
	Target          string    `json:"target"`
	Jsx             string    `json:"jsx"`
	JsxImportSource string    `json:"jsxImportSource"`
	Types           *[]string `json:"types"`
}

type oxcOptions struct {
	Target          string `json:"target,omitempty"`
	Jsx             string `json:"jsx,omitempty"`
	JsxImportSource string `json:"jsxImportSource,omitempty"`
}

func (o oxcOptions) flags() []string {
	var out []string
	if o.Target != "" {
		out = append(out, "--target", o.Target)
	}
	if o.Jsx != "" {
		out = append(out, "--jsx", o.Jsx)
	}
	if o.JsxImportSource != "" {
		out = append(out, "--jsx-import-source", o.JsxImportSource)
	}
	return out
}

// showConfig is the chain's merged options and its root files in tsc's order,
// relative to project's directory.
func showConfig(tsgo, project string) (*effectiveOptions, []string, error) {
	cmd := exec.Command(tsgo, "--showConfig", "-p", project)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, nil, fmt.Errorf("%s --showConfig -p %s: %w\n%s%s",
			tsgo, project, err, out, stderr.Bytes())
	}
	options, roots, err := decodeShowConfig(out)
	if err != nil {
		return nil, nil, fmt.Errorf("%s --showConfig -p %s %w", tsgo, project, err)
	}
	return options, roots, nil
}

func decodeShowConfig(out []byte) (*effectiveOptions, []string, error) {
	var config struct {
		CompilerOptions effectiveOptions `json:"compilerOptions"`
		Files           []string         `json:"files"`
	}
	if err := json.Unmarshal(out, &config); err != nil {
		return nil, nil, fmt.Errorf("printed no config (%v):\n%s", err, out)
	}
	return &config.CompilerOptions, config.Files, nil
}

// actionConfig is what the rule knows about one tsgo action and the user's
// tsconfig cannot: where the sandbox puts things and what this build emits.
type actionConfig struct {
	tsgo, project, baseline, out, options string
	binDir                                string
	typesDeps                             stringList
	srcs                                  []string
	emit                                  bool
	outDir, rootDir                       string
	declarationMap                        bool
	isolatedDeclarations                  bool
	libCheck                              bool
}

type stringList []string

func (l *stringList) String() string     { return strings.Join(*l, ",") }
func (l *stringList) Set(v string) error { *l = append(*l, v); return nil }

type tsconfigFile struct {
	Extends         []string       `json:"extends"`
	CompilerOptions map[string]any `json:"compilerOptions"`
	Include         []string       `json:"include"`
	Files           []string       `json:"files"`
	Exclude         []string       `json:"exclude"`
	References      []string       `json:"references"`
}

func writeTsconfig(args []string) error {
	var a actionConfig
	flags := flag.NewFlagSet("tsconfig", flag.ExitOnError)
	flags.StringVar(&a.tsgo, "tsgo", "", "the tsgo binary")
	flags.StringVar(&a.project, "tsconfig", "", "the user's tsconfig.json, when the target names one")
	flags.StringVar(&a.baseline, "baseline", "", "the ruleset's baseline options, extended before the user's file")
	flags.StringVar(&a.out, "out", "", "the tsconfig to write")
	flags.StringVar(&a.options, "options", "", "the oxc options file to write")
	flags.StringVar(&a.binDir, "bin_dir", "", "the output tree's root")
	flags.Var(&a.typesDeps, "types_dep", "a direct @types dep's name, written to types when the user's chain sets none (repeatable)")
	flags.BoolVar(&a.emit, "emit", false, "tsgo emits this target's declarations")
	flags.StringVar(&a.outDir, "out_dir", "", "where the declarations land, with -emit")
	flags.StringVar(&a.rootDir, "root_dir", "", "the source root the declarations mirror, with -emit")
	flags.BoolVar(&a.declarationMap, "declaration_map", false, "emit a .d.ts.map beside every declaration")
	flags.BoolVar(&a.isolatedDeclarations, "isolated_declarations", false, "oxc emits the declarations, so every export must be annotated")
	flags.BoolVar(&a.libCheck, "lib_check", false, "check the program's .d.ts closure too")
	if err := flags.Parse(args); err != nil {
		return err
	}
	a.srcs = flags.Args()
	for _, required := range []struct{ name, value string }{
		{"-tsgo", a.tsgo}, {"-baseline", a.baseline}, {"-out", a.out}, {"-options", a.options}, {"-bin_dir", a.binDir},
	} {
		if required.value == "" {
			return fmt.Errorf("tsconfig needs %s", required.name)
		}
	}

	// showConfig reads a file, and the options this program runs under are the
	// merged chain's: the config is written with its extends alone first.
	dir := path.Dir(a.out)
	if err := writeJSON(a.out, struct {
		Extends []string `json:"extends"`
		Files   []string `json:"files"`
	}{a.extends(dir), []string{}}); err != nil {
		return err
	}
	config, options, err := a.resolve(dir)
	if err != nil {
		return errors.Join(err, os.Remove(a.out))
	}
	if err := writeJSON(a.out, config); err != nil {
		return err
	}
	return writeJSON(a.options, options)
}

func (a *actionConfig) resolve(dir string) (*tsconfigFile, oxcOptions, error) {
	effective, roots, err := showConfig(a.tsgo, a.out)
	if err != nil {
		return nil, oxcOptions{}, err
	}
	var chain *tsconfig.Resolved
	if a.project != "" {
		if chain, err = tsconfig.Resolve(a.project); err != nil {
			return nil, oxcOptions{}, err
		}
	}
	config, err := a.build(effective, roots, chain, dir)
	if err != nil {
		return nil, oxcOptions{}, err
	}
	return config, oxcOptions{
		Target:          effective.Target,
		Jsx:             effective.Jsx,
		JsxImportSource: effective.JsxImportSource,
	}, nil
}

func (a *actionConfig) extends(dir string) []string {
	out := []string{fileRelative(dir, a.baseline)}
	if a.project != "" {
		out = append(out, fileRelative(dir, a.project))
	}
	return out
}

func (a *actionConfig) build(effective *effectiveOptions, roots []string,
	chain *tsconfig.Resolved, dir string) (*tsconfigFile, error) {
	types, err := a.types(effective, dir)
	if err != nil {
		return nil, err
	}
	// typeRoots stays unset: a custom one stops tsgo's node_modules walk, and
	// that walk is where a `types` entry naming a package outside @types resolves.
	opts := map[string]any{
		"types":               types,
		"rootDirs":            []string{relativePath(dir, ""), relativePath(dir, a.binDir)},
		"preserveSymlinks":    true,
		"declaration":         true,
		"emitDeclarationOnly": true,
		"declarationMap":      a.declarationMap,
		"composite":           false,
		"incremental":         false,
	}
	if a.hasJavaScriptSrc() {
		opts["allowJs"] = true
	}
	if chain != nil {
		if paths := a.paths(chain, dir); paths != nil {
			opts["paths"] = paths
		}
	}
	if a.emit {
		opts["noEmit"] = false
		opts["noEmitOnError"] = true
		opts["outDir"] = relativePath(dir, a.outDir)
		opts["declarationDir"] = opts["outDir"]
		opts["rootDir"] = relativePath(dir, a.rootDir)
	} else {
		opts["rootDir"] = relativePath(dir, "")
	}
	if a.isolatedDeclarations {
		opts["isolatedDeclarations"] = true
	}
	if a.libCheck {
		opts["skipLibCheck"] = false
	}

	files, include := a.roots(roots, dir)
	return &tsconfigFile{
		Extends:         a.extends(dir),
		CompilerOptions: opts,
		Include:         include,
		Files:           files,
		Exclude:         []string{},
		References:      []string{},
	}, nil
}

// roots splits the srcs into the root files, in the order showConfig printed
// them, and the rest: the first declaration of an ambient pattern wins.
func (a *actionConfig) roots(printed []string, dir string,
) (files, include []string) {
	rel := make([]string, len(a.srcs))
	index := make(map[string]int, len(a.srcs))
	for i, src := range a.srcs {
		rel[i] = fileRelative(dir, src)
		index[path.Clean(rel[i])] = i
	}
	files, include = []string{}, []string{}
	isRoot := make([]bool, len(a.srcs))
	for _, p := range printed {
		if i, ok := index[path.Clean(p)]; ok && !isRoot[i] {
			isRoot[i] = true
			files = append(files, rel[i])
		}
	}
	for i, r := range rel {
		if !isRoot[i] {
			include = append(include, r)
		}
	}
	return files, include
}

// A JavaScript src is in `include`; without allowJs tsgo reports TS6504 on it.
func (a *actionConfig) hasJavaScriptSrc() bool {
	for _, src := range a.srcs {
		switch path.Ext(src) {
		case ".js", ".mjs", ".cjs", ".jsx":
			return true
		}
	}
	return false
}

// paths is the user's map read from the directory of the chain file that set
// it -- which showConfig does not print -- with a bin-dir twin per value.
func (a *actionConfig) paths(chain *tsconfig.Resolved, dir string) map[string][]string {
	if chain.Paths == nil {
		return nil
	}
	out := make(map[string][]string, len(chain.Paths))
	for key, values := range chain.Paths {
		rewritten := make([]string, 0, 2*len(values))
		for _, value := range values {
			if path.IsAbs(value) {
				rewritten = append(rewritten, value)
				continue
			}
			target := path.Join(chain.PathsDir, value)
			rewritten = append(rewritten,
				explicitlyRelative(relativePath(dir, target)),
				explicitlyRelative(relativePath(dir, path.Join(a.binDir, target))))
		}
		out[key] = rewritten
	}
	return out
}

// types is the user's list, each path-shaped entry rebased to where the sandbox
// stages it, or the direct @types deps when the chain sets none. Always set.
func (a *actionConfig) types(effective *effectiveOptions, dir string) ([]string, error) {
	if effective.Types == nil {
		return append([]string{}, a.typesDeps...), nil
	}
	projectDir := "."
	if a.project != "" {
		projectDir = path.Dir(a.project)
	}
	out := make([]string, 0, len(*effective.Types))
	for _, entry := range *effective.Types {
		if !isRelative(entry) {
			out = append(out, entry)
			continue
		}
		target := path.Join(projectDir, entry)
		generated := path.Join(a.binDir, target)
		switch {
		case typesEntryExists(target):
			out = append(out, explicitlyRelative(relativePath(dir, target)))
		case typesEntryExists(generated):
			out = append(out, explicitlyRelative(relativePath(dir, generated)))
		default:
			return nil, fmt.Errorf("compilerOptions.types entry %q in %s names %s, which no input of this action sits at: "+
				"not in the source tree, not under %s.\nA declaration this program names is a src of the target "+
				"or an output of one of its deps.", entry, a.project, target, a.binDir)
		}
	}
	return out, nil
}

var typesEntryExtensions = []string{".ts", ".tsx", ".d.ts", ".mts", ".d.mts", ".cts", ".d.cts"}

// typesEntryExists is tsc's lookup for a path-shaped entry: the path as a file
// or directory, or the path with a TypeScript or declaration extension added.
func typesEntryExists(p string) bool {
	if _, err := os.Stat(p); err == nil {
		return true
	}
	for _, ext := range typesEntryExtensions {
		if _, err := os.Stat(p + ext); err == nil {
			return true
		}
	}
	return false
}

func isRelative(p string) bool {
	return p == "." || p == ".." || strings.HasPrefix(p, "./") || strings.HasPrefix(p, "../")
}

func fileRelative(dir, file string) string {
	return explicitlyRelative(relativePath(dir, path.Dir(file)) + "/" + path.Base(file))
}

// relativePath is the ruleset's spelling for a /-separated path from one
// directory to another: "." for the same one, and "" or "." name the exec root.
func relativePath(from, to string) string {
	fromParts, toParts := segments(from), segments(to)
	common := 0
	for common < len(fromParts) && common < len(toParts) && fromParts[common] == toParts[common] {
		common++
	}
	parts := make([]string, 0, len(fromParts)-common+len(toParts)-common)
	for range fromParts[common:] {
		parts = append(parts, "..")
	}
	parts = append(parts, toParts[common:]...)
	if len(parts) == 0 {
		return "."
	}
	return strings.Join(parts, "/")
}

func segments(p string) []string {
	var out []string
	for _, part := range strings.Split(p, "/") {
		if part != "" && part != "." {
			out = append(out, part)
		}
	}
	return out
}

// explicitlyRelative spells a path so TypeScript reads it as one: a bare
// segment is a package name to `paths` (TS5090), and `.bazel/x` is a directory.
func explicitlyRelative(p string) string {
	if p == "." || p == ".." || strings.HasPrefix(p, "./") || strings.HasPrefix(p, "../") || strings.HasPrefix(p, "/") {
		return p
	}
	return "./" + p
}

func writeJSON(name string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(name, append(data, '\n'), 0o644)
}

func runOxc(args []string) error {
	flags := flag.NewFlagSet("oxc", flag.ExitOnError)
	options := flags.String("options", "", "the options file the tsconfig step wrote")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cmdline := flags.Args()
	if *options == "" || len(cmdline) == 0 {
		return errors.New("oxc needs -options=FILE and a command after --")
	}
	data, err := os.ReadFile(*options)
	if err != nil {
		return err
	}
	var o oxcOptions
	if err := json.Unmarshal(data, &o); err != nil {
		return fmt.Errorf("%s: %w", *options, err)
	}
	return runTool(append(cmdline, o.flags()...))
}
