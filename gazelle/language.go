// Package typescript is the Gazelle language for rules_typescript: a package
// per tsconfig.json program, its targets from tsgo's listing.
package typescript

import (
	"flag"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/bazelbuild/bazel-gazelle/config"
	"github.com/bazelbuild/bazel-gazelle/label"
	"github.com/bazelbuild/bazel-gazelle/language"
	"github.com/bazelbuild/bazel-gazelle/repo"
	"github.com/bazelbuild/bazel-gazelle/resolve"
	"github.com/bazelbuild/bazel-gazelle/rule"
)

const languageName = "typescript"

type tsLang struct {
	language.BaseLifecycleManager
	programs *programStore
}

func NewLanguage() language.Language {
	return &tsLang{}
}

func (l *tsLang) Name() string { return languageName }

func (l *tsLang) RegisterFlags(fs *flag.FlagSet, _ string, c *config.Config) {
	tc := defaultTsConfig()
	fs.StringVar(&tc.programs.tsgoFlag, "ts_tsgo", "",
		"the tsgo binary that lists each tsconfig.json program; the default is the toolchain's, in the Gazelle binary's runfiles")
	fs.BoolVar(&tc.programs.verbose, "ts_verbose", false,
		"say which tsgo lists the programs, one line per tsconfig.json with "+
			"what it listed or why it is not a package, how many are, and the "+
			".ts files no program lists")
	l.programs = tc.programs
	c.Exts[languageName] = tc
}

func (l *tsLang) CheckFlags(fs *flag.FlagSet, c *config.Config) error {
	store := getConfig(c).protos
	dirs := fs.Args()
	if len(dirs) == 0 {
		dirs = []string{"."}
	}
	store.roots = nil
	for _, dir := range dirs {
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(c.WorkDir, dir)
		}
		rel, err := filepath.Rel(c.RepoRoot, filepath.Clean(dir))
		if err != nil {
			return err
		}
		if rel == "." {
			rel = ""
		}
		store.roots = append(store.roots, filepath.ToSlash(rel))
	}
	if recursive := fs.Lookup("r"); recursive != nil {
		store.recursive = recursive.Value.String() == "true"
	}

	if tsgo := getConfig(c).programs.tsgoFlag; tsgo != "" {
		if _, err := os.Stat(tsgo); err != nil {
			return fmt.Errorf("-ts_tsgo: %w", err)
		}
	}
	return nil
}

func (l *tsLang) KnownDirectives() []string {
	return []string{"ts_proto"}
}

func (l *tsLang) Configure(c *config.Config, rel string, f *rule.File) {
	configureTsConfig(c, rel, f)
	l.programs = getConfig(c).programs
}

// Kinds is every rule Gazelle writes or withdraws, with the attributes it
// recomputes; a rule matches by name, which the merger checks unasked.
func (l *tsLang) Kinds() map[string]rule.KindInfo {
	return map[string]rule.KindInfo{
		"ts_compile": {
			NonEmptyAttrs: map[string]bool{
				"srcs": true,
			},
			MergeableAttrs: map[string]bool{
				"srcs":         true,
				"deps":         true,
				"visibility":   true,
				"tsconfig":     true,
				"node_modules": true,
				"emit":         true,
			},
			ResolveAttrs: map[string]bool{
				"deps": true,
			},
		},
		"ts_test": {
			NonEmptyAttrs: map[string]bool{
				"srcs": true,
			},
			MergeableAttrs: map[string]bool{
				"srcs":         true,
				"deps":         true,
				"tsconfig":     true,
				"config":       true,
				"node_modules": true,
				"emit":         true,
			},
			// Written at Resolve, from the config's listing: its modules and
			// the pool's attributes.
			ResolveAttrs: map[string]bool{
				"deps":              true,
				"config_srcs":       true,
				"wrangler_config":   true,
				"coverage_provider": true,
			},
		},
		// ts_config makes a package's tsconfig.json a label. deps, jsx and module
		// are read out of the file, so all three are Gazelle's (# keep otherwise).
		"ts_config": {
			NonEmptyAttrs: map[string]bool{
				"src": true,
			},
			MergeableAttrs: map[string]bool{
				"src":        true,
				"deps":       true,
				"jsx":        true,
				"module":     true,
				"visibility": true,
			},
		},
		// ts_codegen is hand-written and never generated; a Kind so that its
		// out_dir is indexed (codegenTreeSpecs) and its outs are deps (D9).
		"ts_codegen":      {},
		"ts_proto_config": {},
		"ts_proto_library": {
			NonEmptyAttrs:  map[string]bool{"proto": true},
			MergeableAttrs: map[string]bool{"proto": true, "out_dir": true, "tsconfig": true, "node_modules": true, "options": true, "deps": true, "visibility": true},
			ResolveAttrs:   map[string]bool{"deps": true},
		},
		"ts_dev_server": {
			NonEmptyAttrs: map[string]bool{"entry_point": true},
			MergeableAttrs: map[string]bool{
				"entry_point":  true,
				"node_modules": true,
				"plugin":       true,
				"visibility":   true,
			},
		},
		// filegroup makes a vitest config, or the wrangler config it names, a
		// label for the packages below it.
		"filegroup": {
			NonEmptyAttrs: map[string]bool{
				"srcs": true,
			},
			MergeableAttrs: map[string]bool{
				"srcs":       true,
				"visibility": true,
			},
		},
		// An importer declaring nothing has no deps, so deps, parent and
		// hoist together decide emptiness.
		"node_modules": {
			NonEmptyAttrs: map[string]bool{
				"deps":   true,
				"parent": true,
				"hoist":  true,
			},
			MergeableAttrs: map[string]bool{
				"deps":       true,
				"parent":     true,
				"hoist":      true,
				"visibility": true,
			},
		},
		"node_modules_member": {
			NonEmptyAttrs: map[string]bool{
				"member": true,
			},
			MergeableAttrs: map[string]bool{
				"member":     true,
				"visibility": true,
			},
		},
		// The lockfile package's store call, named after its directory.
		"npm_virtual_store": {},
	}
}

// The load each kind comes from; filegroup is native. The hub is spelled
// @npm, as every npm label Gazelle writes is.
var kindLoads = map[string]string{
	"ts_codegen":          "//ts:defs.bzl",
	"ts_proto_library":    "//proto:defs.bzl",
	"ts_proto_config":     "//proto:defs.bzl",
	"ts_compile":          "//ts:defs.bzl",
	"ts_config":           "//ts:defs.bzl",
	"ts_dev_server":       "//ts:defs.bzl",
	"ts_test":             "//ts:defs.bzl",
	"node_modules":        "//npm:defs.bzl",
	"node_modules_member": "//npm:defs.bzl",
	"npm_virtual_store":   "@npm//:defs.bzl",
}

func (l *tsLang) Loads() []rule.LoadInfo {
	return l.ApparentLoads(func(string) string { return "" })
}

func (l *tsLang) ApparentLoads(
	moduleToApparentName func(string) string,
) []rule.LoadInfo {
	rulesTs := moduleToApparentName("rules_typescript")
	if rulesTs == "" {
		rulesTs = "rules_typescript"
	}
	symbols := map[string][]string{}
	for kind := range l.Kinds() {
		file, ok := kindLoads[kind]
		if !ok {
			continue
		}
		if !strings.HasPrefix(file, "@") {
			file = "@" + rulesTs + file
		}
		symbols[file] = append(symbols[file], kind)
	}
	var loads []rule.LoadInfo
	for _, file := range slices.Sorted(maps.Keys(symbols)) {
		sort.Strings(symbols[file])
		loads = append(loads, rule.LoadInfo{Name: file, Symbols: symbols[file]})
	}
	return loads
}

// Fix is empty: no ruleset code knows a retired attribute.
func (l *tsLang) Fix(_ *config.Config, _ *rule.File) {}

func (l *tsLang) GenerateRules(args language.GenerateArgs) language.GenerateResult {
	return generateRules(args)
}

func (l *tsLang) DoneGeneratingRules() {
	if l.programs != nil {
		l.programs.reportCensus()
		l.programs.reportUnlisted()
		l.programs.reportUnowned()
	}
}

func (l *tsLang) Imports(c *config.Config, r *rule.Rule, f *rule.File) []resolve.ImportSpec {
	return importsForRule(c, r, f)
}

func (l *tsLang) Embeds(_ *rule.Rule, _ label.Label) []label.Label { return nil }

func (l *tsLang) Resolve(
	c *config.Config,
	ix *resolve.RuleIndex,
	_ *repo.RemoteCache,
	r *rule.Rule,
	imports any,
	from label.Label,
) {
	if imps, ok := imports.(*protoRuleImports); ok && imps != nil {
		resolveProtoLibrary(c, ix, r, imps, from)
		return
	}
	if imps, ok := imports.(*ruleImports); ok && imps != nil {
		resolveEdges(c, ix, r, imps, from)
	}
	if g := getConfig(c).programs.emission; g != nil {
		g.rules[emissionLabel(from.Pkg, ":"+from.Name)] = r
	}
}
