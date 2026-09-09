// The emit step writes the program's JavaScript: oxc's transform when the
// chain's module is ESM-shaped, tsgo's emit from the program root otherwise.

package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

type emitConfig struct {
	options, tsconfig, forest, scratch, outDir string
	oxc, tsgo                                  string
	roots                                      stringList
	sourceMap, declarations                    bool
	srcs                                       []string
}

func runEmit(args []string) error {
	var e emitConfig
	flags := flag.NewFlagSet("emit", flag.ExitOnError)
	flags.StringVar(&e.options, "options", "",
		"the options file the tsconfig step wrote")
	flags.StringVar(&e.tsconfig, "tsconfig", "",
		"the tsconfig the tsconfig step wrote")
	flags.StringVar(&e.forest, "node_modules", "",
		"the node_modules tree the program root holds")
	flags.StringVar(&e.scratch, "scratch", "",
		"where the program root and tsgo's outDir go, removed after")
	flags.StringVar(&e.outDir, "out_dir", "",
		"the target's output directory, where every src's emit lands")
	flags.StringVar(&e.oxc, "oxc", "", "the oxc-bazel binary")
	flags.StringVar(&e.tsgo, "tsgo", "", "the tsgo binary")
	flags.Var(&e.roots, "root",
		"a directory the srcs hang off, exec-root relative (repeatable)")
	flags.BoolVar(&e.sourceMap, "source_map", false,
		"emit a .js.map beside every .js")
	flags.BoolVar(&e.declarations, "declarations", false,
		"emit a .d.ts beside every .js, with isolated declarations")
	if err := flags.Parse(args); err != nil {
		return err
	}
	e.srcs = flags.Args()
	for _, required := range []struct{ name, value string }{
		{"-options", e.options}, {"-tsconfig", e.tsconfig},
		{"-node_modules", e.forest}, {"-scratch", e.scratch},
		{"-out_dir", e.outDir}, {"-oxc", e.oxc}, {"-tsgo", e.tsgo},
	} {
		if required.value == "" {
			return fmt.Errorf("emit needs %s", required.name)
		}
	}
	if len(e.roots) == 0 || len(e.srcs) == 0 {
		return errors.New("emit needs a -root and a src")
	}

	o, err := readOptions(e.options)
	if err != nil {
		return err
	}
	groups, err := e.groupByRoot()
	if err != nil {
		return err
	}
	if emitsWithOxc(o.Module) {
		return e.oxcEmit(groups, o)
	}
	return e.tsgoEmit(groups, o)
}

func readOptions(name string) (oxcOptions, error) {
	data, err := os.ReadFile(name)
	if err != nil {
		return oxcOptions{}, err
	}
	var o oxcOptions
	if err := json.Unmarshal(data, &o); err != nil {
		return oxcOptions{}, fmt.Errorf("%s: %w", name, err)
	}
	return o, nil
}

// oxc keeps the module syntax it reads: every ES kind and preserve. tsgo emits
// commonjs and the node kinds, whose format is the nearest package.json's.
func emitsWithOxc(module string) bool {
	m := strings.ToLower(module)
	return m == "preserve" || strings.HasPrefix(m, "es")
}

// groupByRoot files each src under the longest root that contains it.
func (e *emitConfig) groupByRoot() (map[string][]string, error) {
	groups := map[string][]string{}
	for _, src := range e.srcs {
		best, found := "", false
		for _, root := range e.roots {
			if (root == "" || strings.HasPrefix(src, root+"/")) &&
				(!found || len(root) > len(best)) {
				best, found = root, true
			}
		}
		if !found {
			return nil, fmt.Errorf("%s hangs off none of the roots %q", src, e.roots)
		}
		groups[best] = append(groups[best], src)
	}
	return groups, nil
}

func sortedRoots(groups map[string][]string) []string {
	roots := make([]string, 0, len(groups))
	for root := range groups {
		roots = append(roots, root)
	}
	sort.Strings(roots)
	return roots
}

func (e *emitConfig) oxcEmit(groups map[string][]string, o oxcOptions) error {
	for _, root := range sortedRoots(groups) {
		cmdline := append([]string{e.oxc, "--files"}, groups[root]...)
		cmdline = append(cmdline, "--out-dir", e.outDir)
		if root != "" {
			cmdline = append(cmdline, "--strip-dir-prefix", root)
		}
		if e.sourceMap {
			cmdline = append(cmdline, "--source-map")
		}
		if e.declarations {
			cmdline = append(cmdline, "--declaration", "--isolated-declarations")
		}
		if err := runTool(append(cmdline, o.flags()...)); err != nil {
			return err
		}
	}
	return nil
}

// tsgoEmit runs tsgo's emit alone from the program root into a scratch outDir
// and moves each src's outputs over; a JavaScript src's emit stays behind.
func (e *emitConfig) tsgoEmit(groups map[string][]string, o oxcOptions) error {
	roots := sortedRoots(groups)
	if len(roots) != 1 {
		return fmt.Errorf("the program's module is %q, so tsgo emits its "+
			"JavaScript, and one emit has one rootDir; the srcs hang off %d "+
			"roots:\n  %s\nPut the generated sources in their own target and "+
			"depend on it.", o.Module, len(roots), strings.Join(roots, "\n  "))
	}
	root := roots[0]
	programRoot := filepath.Join(e.scratch, "root")
	scratchOut := filepath.Join(e.scratch, "out")
	if err := layOutProgramRoot(programRoot, e.forest); err != nil {
		return err
	}
	defer os.RemoveAll(e.scratch)

	tsgo, err := filepath.Abs(e.tsgo)
	if err != nil {
		return err
	}
	cmdline := []string{
		tsgo, "--project", e.tsconfig, "--noCheck",
		"--noEmit", "false", "--emitDeclarationOnly", "false",
		"--declaration", fmt.Sprint(e.declarations),
		"--declarationMap", "false",
		"--sourceMap", fmt.Sprint(e.sourceMap),
		"--inlineSources", fmt.Sprint(e.sourceMap),
		"--outDir", scratchOut, "--rootDir", rootDirFlag(root),
	}
	if err := runToolIn(programRoot, os.Stdout, cmdline); err != nil {
		return err
	}
	for _, src := range groups[root] {
		for _, out := range e.outputsOf(src, root, o) {
			from, to := filepath.Join(scratchOut, out), filepath.Join(e.outDir, out)
			move := moveFile
			if strings.HasSuffix(out, ".map") {
				move = moveMap
			}
			if err := move(from, to); err != nil {
				return err
			}
		}
	}
	return nil
}

func rootDirFlag(root string) string {
	if root == "" {
		return "."
	}
	return root
}

// outputsOf names a src's emit relative to the root, as tsc names it: .jsx
// for a .tsx under jsx preserve, .js otherwise, then the map and declaration.
func (e *emitConfig) outputsOf(src, root string, o oxcOptions) []string {
	rel := src
	if root != "" {
		rel = strings.TrimPrefix(src, root+"/")
	}
	ext := path.Ext(rel)
	stem := strings.TrimSuffix(rel, ext)
	js := ".js"
	if ext == ".tsx" && strings.EqualFold(o.Jsx, "preserve") {
		js = ".jsx"
	}
	out := []string{stem + js}
	if e.sourceMap {
		out = append(out, stem+js+".map")
	}
	if e.declarations {
		out = append(out, stem+".d.ts")
	}
	return out
}

func moveFile(from, to string) error {
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	if err := os.Rename(from, to); err != nil {
		return fmt.Errorf("tsgo emitted no %s: %w", filepath.Base(to), err)
	}
	return nil
}

type sourceMap struct {
	Version        int      `json:"version"`
	File           string   `json:"file"`
	SourceRoot     string   `json:"sourceRoot"`
	Sources        []string `json:"sources"`
	Names          []string `json:"names"`
	Mappings       string   `json:"mappings"`
	SourcesContent []string `json:"sourcesContent"`
}

// moveMap resolves the sources tsc wrote relative to the map's scratch
// directory into exec-root paths, the form oxc's maps have.
func moveMap(from, to string) error {
	data, err := os.ReadFile(from)
	if err != nil {
		return fmt.Errorf("tsgo emitted no %s: %w", filepath.Base(to), err)
	}
	var m sourceMap
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("%s: %w", from, err)
	}
	for i, src := range m.Sources {
		m.Sources[i] = filepath.ToSlash(filepath.Join(filepath.Dir(from), src))
	}
	out, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	return os.WriteFile(to, out, 0o644)
}
