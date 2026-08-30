package typescript

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"

	"github.com/bazelbuild/bazel-gazelle/config"
	"github.com/bazelbuild/bazel-gazelle/language"
	"github.com/bazelbuild/bazel-gazelle/rule"
)

// generateTree writes a whole tree, configures from the root down to rel with
// the given root directives, and generates rules for rel -- which is what a
// boundary mode needs: the answer depends on directories other than rel.
func generateTree(t *testing.T, files map[string]string, directives []rule.Directive, rel string) language.GenerateResult {
	t.Helper()
	return generateTreeWithBuild(t, files, directives, rel, "")
}

// generateTreeWithBuild is generateTree with a BUILD file in the generated
// directory. A directive that names a target has to be read there: the pattern
// carries the directory it was declared in, and one target belongs in one
// package.
func generateTreeWithBuild(t *testing.T, files map[string]string, directives []rule.Directive, rel, build string) language.GenerateResult {
	t.Helper()
	repoRoot := t.TempDir()
	for name, content := range files {
		full := filepath.Join(repoRoot, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	c := &config.Config{RepoRoot: repoRoot, Exts: make(map[string]interface{})}
	rootFile := rule.EmptyFile("BUILD.bazel", "")
	rootFile.Directives = directives
	configureTsConfig(c, "", rootFile)

	dir := filepath.Join(repoRoot, rel)
	buildFile, err := rule.LoadData(filepath.Join(dir, "BUILD.bazel"), rel, []byte(build))
	if err != nil {
		t.Fatal(err)
	}

	var walked string
	for _, part := range splitRel(rel) {
		if walked == "" {
			walked = part
		} else {
			walked = walked + "/" + part
		}
		if walked == rel {
			configureTsConfig(c, walked, buildFile)
			continue
		}
		configureTsConfig(c, walked, nil)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	return generateRules(language.GenerateArgs{
		Config: c, Dir: dir, Rel: rel, File: buildFile, RegularFiles: names,
	})
}

func splitRel(rel string) []string {
	if rel == "" {
		return nil
	}
	var out []string
	cur := ""
	for _, ch := range rel {
		if ch == '/' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(ch)
	}
	return append(out, cur)
}

// The tree a Cloudflare worker has after `wrangler types`: one tsconfig, an
// ambient declaration beside it, and the sources a directory down. The
// declaration types the sources, and refers back to them -- legal in one
// TypeScript program, a cycle between two Bazel packages.
var workerTree = map[string]string{
	"worker/package.json": `{"name":"w"}`,
	"worker/tsconfig.json": `{
  "compilerOptions": { "strict": true },
  "include": ["src/**/*.ts", "worker-configuration.d.ts"]
}`,
	"worker/worker-configuration.d.ts": `declare namespace Cloudflare {
  interface GlobalProps { mainModule: typeof import("./src/index"); }
}
declare type ExportedHandler = { fetch(): Response };
`,
	"worker/src/index.ts": `const handler: ExportedHandler = { fetch: () => new Response() };
export default handler;
`,
}

func TestBoundaryTsConfig_ProjectIsOneTarget(t *testing.T) {
	res := generateTree(t, workerTree,
		[]rule.Directive{directive(directivePackageBoundary, "tsconfig")}, "worker")

	var compile *rule.Rule
	for _, r := range res.Gen {
		if r.Kind() == "ts_compile" {
			compile = r
		}
	}
	if compile == nil {
		t.Fatalf("no ts_compile generated for the project root: %v", generatedNames(t, res))
	}
	srcs := compile.AttrStrings("srcs")
	want := map[string]bool{"worker-configuration.d.ts": false, "src/index.ts": false}
	for _, s := range srcs {
		if _, ok := want[s]; ok {
			want[s] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("srcs %v: missing %s", srcs, name)
		}
	}
}

// A directory inside the project is not a package, so it must claim nothing --
// otherwise the roll-up above and a target here compile the same file twice and
// the labels cross a boundary that TypeScript does not have.
func TestBoundaryTsConfig_SubdirectoryClaimsNothing(t *testing.T) {
	res := generateTree(t, workerTree,
		[]rule.Directive{directive(directivePackageBoundary, "tsconfig")}, "worker/src")

	if len(res.Gen) != 0 {
		t.Errorf("subdirectory generated %v, want nothing", generatedNames(t, res))
	}
}

// The default is unchanged: without the directive every directory is its own
// package, which is what every existing BUILD file in a repo was generated for.
func TestBoundaryTsConfig_DefaultStillSplitsEveryDirectory(t *testing.T) {
	res := generateTree(t, workerTree, nil, "worker/src")

	if len(res.Gen) == 0 {
		t.Errorf("every-dir mode generated nothing for a directory with sources")
	}
}

// The same worker tree with the declaration a directory down, where the
// roll-up rather than this directory's own file list is what finds it. Being
// rolled up changes nothing about why the test target needs it: an ambient
// declaration has no export for a dep edge to carry.
func TestBoundaryTsConfig_RolledUpAmbientDeclarationReachesTheTest(t *testing.T) {
	res := generateTree(t, map[string]string{
		"worker/package.json": `{"name":"w"}`,
		"worker/tsconfig.json": `{
  "compilerOptions": { "strict": true },
  "include": ["src/**/*.ts"]
}`,
		"worker/src/worker-configuration.d.ts": "declare type ZzEnv = { KV: string };\n",
		"worker/src/index.ts":                  "export const handler = (env: ZzEnv) => env.KV;\n",
		"worker/src/index.test.ts":             "import { handler } from \"./index\";\nexport const t = handler;\n",
	}, []rule.Directive{directive(directivePackageBoundary, "tsconfig")}, "worker")

	var compile, test *rule.Rule
	for _, r := range res.Gen {
		switch r.Kind() {
		case "ts_compile":
			compile = r
		case "ts_test":
			test = r
		}
	}
	if compile == nil || test == nil {
		t.Fatalf("want a ts_compile and a ts_test, got %v", generatedNames(t, res))
	}
	// The declaration is in the roll-up's srcs group and named again in its
	// ambient group, so counting is what catches it being listed twice.
	const decl = "src/worker-configuration.d.ts"
	for _, r := range []*rule.Rule{compile, test} {
		srcs := r.AttrStrings("srcs")
		n := 0
		for _, s := range srcs {
			if s == decl {
				n++
			}
		}
		if n != 1 {
			t.Errorf("%s srcs %v: %s listed %d times, want exactly 1", r.Name(), srcs, decl, n)
		}
	}
}

// The roll-up and a ts_codegen's outs both decide what a target's srcs contain,
// and a file claimed by both is a source and an output of one package -- which
// Bazel rejects outright. The out stays checked in until the day it does not,
// so the claim has to reach every kind the walk collects, not the TypeScript
// ones alone.
func TestBoundaryTsConfig_RolledUpCodegenOutIsNotAlsoASrc(t *testing.T) {
	res := generateTreeWithBuild(t, map[string]string{
		"worker/tsconfig.json":      `{"include": ["src/**/*.ts"]}`,
		"worker/src/schema.graphql": "type Query { a: Int }\n",
		"worker/src/schema.gen.ts":  "export type S = number;\n",
		"worker/src/schema.json":    `{"a": 1}`,
		"worker/src/worker.ts":      "export const w = 1;\n",
	}, []rule.Directive{directive(directivePackageBoundary, "tsconfig")}, "worker",
		"# gazelle:ts_codegen schema_gen //tools:schemagen src/schema.gen.ts,src/schema.json srcs:src/schema.graphql --out {out}\n")

	compile := generatedRule(res, "worker")
	if compile == nil {
		t.Fatalf("no ts_compile named worker; got %v", generatedNames(t, res))
	}
	if got := compile.AttrStrings("srcs"); slices.Contains(got, "src/schema.gen.ts") {
		t.Errorf("ts_compile srcs = %v: the declared out is also a source", got)
	}
	for _, r := range res.Gen {
		if r.Kind() != "json_library" {
			continue
		}
		if got := r.AttrStrings("srcs"); slices.Contains(got, "src/schema.json") {
			t.Errorf("json_library %s srcs = %v: the declared out is also a source", r.Name(), got)
		}
	}
}

// A generator that names no srcs reads the sources of the target it sits beside,
// and in a boundary mode that target covers the whole subtree. Read before the
// roll-up it would get the one directory the BUILD file is in -- for a worker
// root, nothing at all, and the rule disappears with only a log line.
func TestBoundaryTsConfig_CodegenWithoutSrcsReadsTheRolledUpSources(t *testing.T) {
	res := generateTreeWithBuild(t, map[string]string{
		"worker/tsconfig.json": `{"include": ["src/**/*.ts"]}`,
		"worker/src/worker.ts": "export const w = 1;\n",
		"worker/src/util.ts":   "export const u = 2;\n",
	}, []rule.Directive{directive(directivePackageBoundary, "tsconfig")}, "worker",
		"# gazelle:ts_codegen types_gen //tools:typegen types.gen.ts --out {out}\n")

	gen := generatedRule(res, "types_gen")
	if gen == nil {
		t.Fatalf("no ts_codegen named types_gen; got %v", generatedNames(t, res))
	}
	got := gen.AttrStrings("srcs")
	for _, want := range []string{"src/util.ts", "src/worker.ts"} {
		if !slices.Contains(got, want) {
			t.Errorf("ts_codegen srcs = %v, missing %s", got, want)
		}
	}
}
