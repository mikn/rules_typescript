package typescript

import (
	"bytes"
	"context"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/bazelbuild/bazel-gazelle/config"
	"github.com/bazelbuild/bazel-gazelle/language"
	"github.com/bazelbuild/bazel-gazelle/rule"
	"github.com/bazelbuild/bazel-gazelle/walk"
)

func sourceInventoryWalk(t *testing.T, root string, l language.Language) map[string]language.GenerateResult {
	t.Helper()
	requireTsgo(t)
	c := config.New()
	c.RepoRoot = root
	fs := flag.NewFlagSet("source-inventory", flag.ContinueOnError)
	wc := &walk.Configurer{}
	extensions := []config.Configurer{wc, l}
	for _, extension := range extensions {
		extension.RegisterFlags(fs, "update", c)
	}
	for _, extension := range extensions {
		if err := extension.CheckFlags(fs, c); err != nil {
			t.Fatal(err)
		}
	}
	result := map[string]language.GenerateResult{}
	originals := map[*rule.File][]byte{}
	err := walk.Walk2(c, extensions, []string{root}, walk.VisitAllUpdateSubdirsMode, func(args walk.Walk2FuncArgs) walk.Walk2FuncResult {
		if args.File != nil {
			originals[args.File] = append([]byte(nil), args.File.Format()...)
		}
		result[args.Rel] = l.GenerateRules(language.GenerateArgs{Config: args.Config, Dir: args.Dir, Rel: args.Rel, File: args.File, Subdirs: args.Subdirs, RegularFiles: args.RegularFiles, GenFiles: args.GenFiles})
		return walk.Walk2FuncResult{}
	})
	if err != nil {
		t.Fatal(err)
	}
	if lifecycle, ok := l.(language.LifecycleManager); ok {
		lifecycle.AfterResolvingDeps(context.Background())
	}
	if _, inventory := l.(*sourceInventory); inventory {
		for file, original := range originals {
			if !bytes.Equal(file.Format(), original) {
				t.Fatalf("source inventory mutated existing attributes in %s", file.Path)
			}
		}
	}
	return result
}

func TestSourceInventory_UpdateOnlyRetainsNestedSourcesWithoutInventingPackages(t *testing.T) {
	root := writeTree(t, map[string]string{
		"BUILD.bazel":  "# gazelle:generation_mode update_only\n",
		"package.json": rootManifest,
		"pkg/BUILD.bazel": `ts_compile(name = "pkg", srcs = ["old.ts"], deps = ["//:runtime"], emit = False, tsconfig = ":tsconfig")
ts_config(name = "tsconfig", src = "tsconfig.json")
ts_binary(name = "run", entry_point = ":pkg")
`,
		"pkg/tsconfig.json":             `{"compilerOptions":{"lib":["es2022"]},"include":["**/*"]}`,
		"pkg/main.ts":                   "export const main = 1;\n",
		"pkg/nested/tsconfig.json":      includeTs,
		"pkg/nested/deep/added.ts":      "export const added = 1;\n",
		"pkg/nested/deep/runtime.d.mts": "export declare const value: number;\n",
		"pkg/nested/deep/runtime.mjs":   "export const value = 1;\n",
		"pkg/nested/deep/fixture.json":  "{}\n",
		"pkg/child/BUILD.bazel": `ts_compile(name = "child", srcs = ["child.ts"], tsconfig = ":tsconfig")
ts_config(name = "tsconfig", src = "tsconfig.json")
`,
		"pkg/child/tsconfig.json": includeTs,
		"pkg/child/child.ts":      "export const child = 1;\n",
		"pkg/manual/BUILD.bazel": `exports_files(["owned.ts"])
`,
		"pkg/manual/owned.ts": "export const manual = 1;\n",
		"pkg/kept/BUILD.bazel": `# keep
ts_compile(name = "kept", srcs = glob(["**/*.ts"]), tsconfig = ":custom")
`,
		"pkg/kept/owned.ts": "export const kept = 1;\n",
		"pkg/kept_srcs/BUILD.bazel": `ts_compile(
    name = "kept_srcs",
    srcs = glob(["**/*.ts"]), # keep
    tsconfig = ":custom",
)
`,
		"pkg/kept_srcs/owned.ts": "export const keptSrcs = 1;\n",
	})
	generate := func() []string {
		results := sourceInventoryWalk(t, root, NewSourceInventoryLanguage())
		if _, ok := results["pkg/nested"]; ok {
			t.Fatal("update_only invented a package from a child tsconfig")
		}
		for _, dir := range []string{"", "pkg/manual", "pkg/kept", "pkg/kept_srcs"} {
			if len(results[dir].Gen) != 0 {
				t.Fatalf("new target in %s: %v", dir, results[dir].Gen)
			}
		}
		parent := mustRule(t, results["pkg"], "ts_compile", "pkg")
		if got := parent.AttrKeys(); !reflect.DeepEqual(got, []string{"name", "srcs"}) {
			t.Fatalf("source refresh rewrites non-source attributes: %v", got)
		}
		child := mustRule(t, results["pkg/child"], "ts_compile", "child")
		wantStrings(t, "child srcs", child.AttrStrings("srcs"), []string{"child.ts"})
		return parent.AttrStrings("srcs")
	}
	want := []string{"main.ts", "nested/deep/added.ts", "nested/deep/fixture.json", "nested/deep/runtime.d.mts", "nested/deep/runtime.mjs", "nested/tsconfig.json"}
	wantStrings(t, "parent srcs", generate(), want)
	wantStrings(t, "stable parent srcs", generate(), want)
	if err := os.Remove(filepath.Join(root, "pkg/nested/deep/added.ts")); err != nil {
		t.Fatal(err)
	}
	wantStrings(t, "removed source", generate(), []string{"main.ts", "nested/deep/fixture.json", "nested/deep/runtime.d.mts", "nested/deep/runtime.mjs", "nested/tsconfig.json"})
}

func TestGenerate_UpdateOnlyDoesNotDropNestedCompilerInputs(t *testing.T) {
	root := writeTree(t, map[string]string{
		"BUILD.bazel":       "# gazelle:generation_mode update_only\n",
		"package.json":      rootManifest,
		"pkg/BUILD.bazel":   "",
		"pkg/tsconfig.json": `{"compilerOptions":{"lib":["es2022"]},"include":["**/*"]}`,
		"pkg/deep/file.ts":  "export const value = 1;\n",
	})
	result := sourceInventoryWalk(t, root, NewLanguage())
	compile := mustRule(t, result["pkg"], "ts_compile", "pkg")
	wantStrings(t, "folded compiler inputs", compile.AttrStrings("srcs"), []string{"deep/file.ts"})
}
