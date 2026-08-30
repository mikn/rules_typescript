package typescript

import (
	"os"
	"path/filepath"
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

	var walked string
	for _, part := range splitRel(rel) {
		if walked == "" {
			walked = part
		} else {
			walked = walked + "/" + part
		}
		configureTsConfig(c, walked, nil)
	}

	dir := filepath.Join(repoRoot, rel)
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
		Config: c, Dir: dir, Rel: rel, RegularFiles: names,
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
