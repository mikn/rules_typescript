package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/mikn/rules_typescript/ts/tools/tsconfig"
)

// tsc's exclude as tsgo 7.0.2 applies it to an include match: each entry
// written in /r/cfg, each file under /r/src, matched case-sensitively.
func TestExcludePattern(t *testing.T) {
	files := []string{"a.ts", "b.ts", "x.test.ts", "g.gen.ts", "q1.ts",
		"q12.ts", "test.ts", "test/t.ts", "test/deep/u.ts", "sub/deep/s.ts",
		"Dir/d.ts", "tests/x.ts"}
	for entry, want := range map[string][]string{
		"../src/b.ts":         {"b.ts"},
		"../src/test":         {"test/t.ts", "test/deep/u.ts"},
		"../src/test/":        {"test/t.ts", "test/deep/u.ts"},
		"**/*.test.ts":        {},
		"../src/**/*.test.ts": {"x.test.ts"},
		"../src/sub/**":       {"sub/deep/s.ts"},
		"../src/*.gen.ts":     {"g.gen.ts"},
		"../src/q?.ts":        {"q1.ts"},
		"../src/dir":          {},
		"../src/**/deep":      {"test/deep/u.ts", "sub/deep/s.ts"},
		"../src/b":            {},
		"/r/src/a.ts":         {"a.ts"},
		"../src/te*": {"test.ts", "test/t.ts", "test/deep/u.ts",
			"tests/x.ts"},
	} {
		p := excludePattern("/r/cfg", entry)
		got := []string{}
		for _, f := range files {
			if p.MatchString("/r/src/" + f) {
				got = append(got, f)
			}
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("exclude %q drops %q, want %q (%s)", entry, got, want, p)
		}
	}
}

// A src the program lacks is reported once, by the first entry naming it; one
// an import brought in, one no entry names, and a .json src are not.
func TestExcludedSrcs(t *testing.T) {
	exclude := []string{"b.ts", "*.json", "gen", "b*"}
	chain := &tsconfig.Resolved{Exclude: &exclude, ExcludeDir: "pkg"}
	own := map[string]bool{
		"pkg/a.ts": true, "pkg/b.ts": true, "pkg/c.ts": true,
		"pkg/data.json": true, "pkg/gen/g.ts": true, "pkg/gen/h.ts": true,
	}
	program := []string{"pkg/a.ts", "pkg/gen/g.ts"}

	got := excludedSrcs(own, program, chain, "/r")
	want := []excludedSrc{{"pkg/b.ts", "b.ts"}, {"pkg/gen/h.ts", "gen"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("excludedSrcs = %v, want %v", got, want)
	}
	if got := excludedSrcs(own, program, nil, "/r"); got != nil {
		t.Errorf("excludedSrcs without a chain = %v, want none", got)
	}
	none := &tsconfig.Resolved{ExcludeDir: "pkg"}
	if got := excludedSrcs(own, program, none, "/r"); got != nil {
		t.Errorf("excludedSrcs under no exclude = %v, want none", got)
	}
}

func TestReportExcluded(t *testing.T) {
	o := &ownership{label: "//pkg:app"}
	out := o.reportExcluded("pkg/tsconfig.json",
		[]excludedSrc{{"pkg/b.ts", "b.ts"}})
	for _, want := range []string{
		"//pkg:app: srcs the program never read",
		"pkg/tsconfig.json's chain",
		"  pkg/b.ts\texcluded by \"b.ts\"",
		"drop the file from srcs, or the entry from exclude",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the message lacks %q:\n%s", want, out)
		}
	}
}
