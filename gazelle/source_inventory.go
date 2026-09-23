package typescript

import (
	"context"
	"log"
	"path/filepath"

	"github.com/bazelbuild/bazel-gazelle/config"
	"github.com/bazelbuild/bazel-gazelle/label"
	"github.com/bazelbuild/bazel-gazelle/language"
	"github.com/bazelbuild/bazel-gazelle/repo"
	"github.com/bazelbuild/bazel-gazelle/resolve"
	"github.com/bazelbuild/bazel-gazelle/rule"
)

// NewSourceInventoryLanguage refreshes existing canonical ts_compile srcs only.
// Run it separately from NewLanguage with Gazelle's generation_mode update_only.
func NewSourceInventoryLanguage() language.Language {
	return &sourceInventory{tsLang: &tsLang{}}
}

type sourceInventory struct{ *tsLang }

func existingCompile(f *rule.File, rel string) *rule.Rule {
	if f != nil {
		for _, r := range f.Rules {
			if r.Kind() == "ts_compile" && r.Name() == packageName(rel) && !r.ShouldKeep() && !attrKept(r, "srcs") {
				return r
			}
		}
	}
	return nil
}

func (l *sourceInventory) Configure(c *config.Config, rel string, f *rule.File) {
	existing := existingCompile(f, rel)
	if existing != nil {
		if existing.AttrString("tsconfig") != ":tsconfig" || handWrittenTsConfigIn(filepath.Join(c.RepoRoot, rel), c.RepoRoot) == "" {
			log.Fatalf("typescript source inventory: //%s:%s must use :tsconfig backed by this directory's hand-written tsconfig.json; use ordinary Gazelle or refresh this custom target explicitly", rel, existing.Name())
		}
		found := false
		for _, r := range f.Rules {
			if r.Kind() == "ts_config" && r.Name() == "tsconfig" && r.AttrString("src") == "tsconfig.json" {
				found = true
			}
		}
		if !found {
			log.Fatalf("typescript source inventory: //%s:%s has no ts_config(name = \"tsconfig\", src = \"tsconfig.json\"); refresh this custom target explicitly", rel, existing.Name())
		}
	}
	configureProgram(c, rel, f, existing != nil)
	l.programs = getConfig(c).programs
	if f != nil && l.programs.packages[rel] == nil {
		l.programs.packages[rel] = map[string]bool{}
	}
}

func (l *sourceInventory) Kinds() map[string]rule.KindInfo {
	return map[string]rule.KindInfo{"ts_compile": {MergeableAttrs: map[string]bool{"srcs": true}}}
}

func (l *sourceInventory) GenerateRules(args language.GenerateArgs) language.GenerateResult {
	tc := getConfig(args.Config)
	tc.programs.visit(args.Rel, args.RegularFiles)
	existing := existingCompile(args.File, args.Rel)
	if existing == nil {
		return language.GenerateResult{}
	}
	set := tc.programs.srcs(args.Rel, tc)
	r := rule.NewRule("ts_compile", existing.Name())
	r.SetAttr("srcs", packageSrcs(args, set.library, set.declaration, tc.programs.dataFiles(args.Rel, tc)))
	return language.GenerateResult{Gen: []*rule.Rule{r}, Imports: []any{nil}}
}

func (l *sourceInventory) Imports(*config.Config, *rule.Rule, *rule.File) []resolve.ImportSpec {
	return nil
}
func (l *sourceInventory) Resolve(*config.Config, *resolve.RuleIndex, *repo.RemoteCache, *rule.Rule, any, label.Label) {
}
func (l *sourceInventory) AfterResolvingDeps(context.Context) {}
