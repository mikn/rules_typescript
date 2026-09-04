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
	"asset_library",
	"css_library",
	"css_module",
	"filegroup",
	"json_library",
	"node_modules",
	"ts_add_package",
	"ts_codegen",
	"ts_compile",
	"ts_config",
	"ts_dev_server",
	"ts_lint",
	"ts_pnpm",
	"ts_test",
}

// A goneKind back in Kinds() or a load would have Gazelle write a rule no .bzl
// defines.
var goneKinds = []string{"next_build", "next_dev_server", "sveltekit_build", "ts_bundle", "vite_bundler"}

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

func TestLoads_NameEveryKindAndNothingGone(t *testing.T) {
	lang := &tsLang{}
	kinds := lang.Kinds()
	for name, loads := range map[string][]rule.LoadInfo{
		"Loads":         lang.Loads(),
		"ApparentLoads": lang.ApparentLoads(func(string) string { return "" }),
	} {
		loaded := map[string]bool{}
		for _, li := range loads {
			if strings.HasSuffix(li.Name, "//vite:bundler.bzl") {
				t.Errorf("%s still loads %s", name, li.Name)
			}
			for _, symbol := range li.Symbols {
				loaded[symbol] = true
				if slices.Contains(goneKinds, symbol) {
					t.Errorf("%s still loads %q from %s", name, symbol, li.Name)
				}
				if _, ok := kinds[symbol]; !ok {
					t.Errorf("%s loads %q, which Kinds() does not know", name, symbol)
				}
			}
		}
		for kind := range kinds {
			// filegroup is native; nothing loads it.
			if kind != "filegroup" && !loaded[kind] {
				t.Errorf("%s has no load for kind %q", name, kind)
			}
		}
	}
}
