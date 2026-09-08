package typescript

import (
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/bazelbuild/bazel-gazelle/rule"
)

// wantKinds is every kind this extension writes or withdraws.
var wantKinds = []string{
	"filegroup",
	"ts_codegen",
	"ts_compile",
	"ts_config",
	"ts_test",
}

// wantSymbols is the load line: every kind but filegroup, which is native.
var wantSymbols = []string{
	"ts_codegen", "ts_compile", "ts_config", "ts_test",
}

// A goneKind back in Kinds() or a load would have Gazelle write a rule it must
// not: six that no .bzl defines, and the four that are written by hand.
var goneKinds = []string{
	"next_build", "next_dev_server", "sveltekit_build", "ts_bundle",
	"vite_bundler", "ts_lint",
	"node_modules", "ts_add_package", "ts_dev_server", "ts_pnpm",
}

func TestKinds_ExactSurface(t *testing.T) {
	kinds := (&tsLang{}).Kinds()
	got := make([]string, 0, len(kinds))
	for kind := range kinds {
		got = append(got, kind)
	}
	sort.Strings(got)
	if !slices.Equal(got, wantKinds) {
		t.Errorf("Kinds() = %v\nwant %v", got, wantKinds)
	}
	for _, kind := range goneKinds {
		if _, ok := kinds[kind]; ok {
			t.Errorf("Kinds() still knows %q", kind)
		}
	}
}

// The one load, its symbols exactly wantSymbols in that order: a symbol
// Kinds() lacks or a gone kind fails here, and so does a map-ordered list.
func TestLoads_OneDefsLoadNamingEveryKind(t *testing.T) {
	lang := &tsLang{}
	for name, loads := range map[string][]rule.LoadInfo{
		"Loads":         lang.Loads(),
		"ApparentLoads": lang.ApparentLoads(func(string) string { return "" }),
	} {
		if len(loads) != 1 {
			t.Errorf("%s = %d loads, want the one over //ts:defs.bzl", name, len(loads))
			continue
		}
		li := loads[0]
		if li.Name != "@rules_typescript//ts:defs.bzl" {
			t.Errorf("%s loads %s", name, li.Name)
		}
		if !slices.Equal(li.Symbols, wantSymbols) {
			t.Errorf("%s symbols = %v\nwant %v", name, li.Symbols, wantSymbols)
		}
	}
	apparent := lang.ApparentLoads(func(module string) string {
		return strings.ReplaceAll(module, "rules_typescript", "rules_ts~")
	})
	if got := apparent[0].Name; got != "@rules_ts~//ts:defs.bzl" {
		t.Errorf("ApparentLoads under an apparent name loads %s", got)
	}
}
