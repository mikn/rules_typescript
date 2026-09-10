// Package typescript is the Gazelle language for rules_typescript: a package
// per tsconfig.json program, its targets from tsgo's listing.
package typescript

import (
	"flag"
	"fmt"
	"os"
	"sort"

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

func (l *tsLang) CheckFlags(_ *flag.FlagSet, c *config.Config) error {
	if tsgo := getConfig(c).programs.tsgoFlag; tsgo != "" {
		if _, err := os.Stat(tsgo); err != nil {
			return fmt.Errorf("-ts_tsgo: %w", err)
		}
	}
	return nil
}

// KnownDirectives is empty: the tsconfig.json, the lockfile and the manifest
// say everything; # gazelle:exclude, # gazelle:resolve and # keep are core's.
func (l *tsLang) KnownDirectives() []string {
	return nil
}

func (l *tsLang) Configure(c *config.Config, rel string, f *rule.File) {
	configureTsConfig(c, rel, f)
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
				"srcs":       true,
				"deps":       true,
				"visibility": true,
				"tsconfig":   true,
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
				"srcs":     true,
				"deps":     true,
				"tsconfig": true,
				"config":   true,
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
		"ts_codegen": {},
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
	}
}

// Every Kind but filegroup, which is native, is a symbol of //ts:defs.bzl.
func (l *tsLang) defsSymbols() []string {
	var symbols []string
	for kind := range l.Kinds() {
		if kind != "filegroup" {
			symbols = append(symbols, kind)
		}
	}
	sort.Strings(symbols)
	return symbols
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
	return []rule.LoadInfo{{
		Name:    "@" + rulesTs + "//ts:defs.bzl",
		Symbols: l.defsSymbols(),
	}}
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
	if imps, ok := imports.(*ruleImports); ok && imps != nil {
		resolveEdges(c, ix, r, imps, from)
	}
}
