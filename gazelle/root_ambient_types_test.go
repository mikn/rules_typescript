package typescript

// A project tsconfig names a declaration file that belongs in every program
// under it -- `"types": ["./worker-configuration.d.ts"]` is how wrangler writes
// one. TypeScript resolves a relative `types` entry against the config the
// program was invoked with, and the generated per-directory config is in
// bazel-out, so the name reaches nothing: every global that file declares is
// TS2304 in every directory below.
//
// Only `compilerOptions.types` is read. `files`, `include` and `exclude` do not
// survive `extends` into the generated config -- it states its own -- so a
// declaration named only in `include` makes no claim this can act on.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bazelbuild/bazel-gazelle/rule"
)

func generated(t *testing.T, root string, parts ...string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(append([]string{root}, parts...)...))
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

const workerAmbient = "declare const WORKER_ENV: string;\n"

func TestRootAmbientTypes_ReachesADirectoryBelow(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"package.json":                     `{"name":"w"}` + "\n",
		"worker/tsconfig.json":             `{"compilerOptions": {"types": ["./worker-configuration.d.ts"]}}` + "\n",
		"worker/worker-configuration.d.ts": workerAmbient,
		"worker/src/handler.ts":            "export const env = WORKER_ENV;\n",
	})
	captureLog(t, func() { convergeGazelle(t, root) })

	below := generated(t, root, "worker", "src", "BUILD.bazel")
	if !strings.Contains(below, `types = ["../worker-configuration.d.ts"]`) {
		t.Errorf("//worker/src names no rebased `types` entry, so nothing resolves the "+
			"declaration its tsconfig asked for:\n%s", below)
	}
	if !strings.Contains(below, `types_srcs = ["//worker:tsconfig_types"]`) {
		t.Errorf("//worker/src names no label staging the declaration, so the entry "+
			"resolves to nothing:\n%s", below)
	}

	owner := generated(t, root, "worker", "BUILD.bazel")
	if !strings.Contains(owner, `name = "tsconfig_types"`) {
		t.Errorf("the tsconfig's own package supplies no label for the file it names:\n%s", owner)
	}
	if !strings.Contains(owner, "filegroup(") {
		t.Errorf("the label staging the declaration is not a filegroup, so it compiles "+
			"something and publishes it:\n%s", owner)
	}
}

// The tsconfig's own directory: the file is a src of the target there, and the
// label it names is a filegroup, so naming it is not a dep on itself.
func TestRootAmbientTypes_OwnDirectoryDoesNotDepItself(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"package.json":                     `{"name":"w"}` + "\n",
		"worker/tsconfig.json":             `{"compilerOptions": {"types": ["./worker-configuration.d.ts"]}}` + "\n",
		"worker/worker-configuration.d.ts": workerAmbient,
		"worker/index.ts":                  "export const env = WORKER_ENV;\n",
	})
	captureLog(t, func() { convergeGazelle(t, root) })

	owner := generated(t, root, "worker", "BUILD.bazel")
	if !strings.Contains(owner, `types = ["./worker-configuration.d.ts"]`) {
		t.Errorf("the target beside the tsconfig lost the entry the tsconfig states:\n%s", owner)
	}
	if strings.Contains(owner, `deps = [":tsconfig_types"]`) {
		t.Errorf("the declaration arrives as a dep, which is the propagating shape:\n%s", owner)
	}
	if !strings.Contains(owner, `types_srcs = [":tsconfig_types"]`) {
		t.Errorf("the target beside the tsconfig names no staging label:\n%s", owner)
	}
}

// A `types` list is one key, and `extends` replaces it whole, so a subset
// written onto the target drops the packages the author asked for.
func TestRootAmbientTypes_PackageEntriesStayInTheList(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"package.json":                     `{"name":"w"}` + "\n",
		"worker/tsconfig.json":             `{"compilerOptions": {"types": ["node", "./worker-configuration.d.ts"]}}` + "\n",
		"worker/worker-configuration.d.ts": workerAmbient,
		"worker/src/handler.ts":            "export const env = WORKER_ENV;\n",
	})
	captureLog(t, func() { convergeGazelle(t, root) })

	below := generated(t, root, "worker", "src", "BUILD.bazel")
	if !strings.Contains(below, `"node"`) || !strings.Contains(below, `"../worker-configuration.d.ts"`) {
		t.Errorf("//worker/src does not carry both entries the tsconfig states:\n%s", below)
	}
}

// The case changelog.d/globals-private-by-default.md was written to close: a
// `declare const process` shim named in `include`. `include` does not survive
// `extends`, so it states nothing about the tree below and nothing fires.
func TestRootAmbientTypes_IncludeAloneFiresNothing(t *testing.T) {
	for _, decl := range []string{"process-shim.d.ts", "vite-env.d.ts"} {
		t.Run(decl, func(t *testing.T) {
			root := t.TempDir()
			writeWorkspace(t, root, map[string]string{
				"package.json":                 `{"name":"w"}` + "\n",
				"packages/ui/tsconfig.json":    `{"include": ["src", "` + decl + `"]}` + "\n",
				"packages/ui/" + decl:          "declare const process: { env: Record<string, string> };\n",
				"packages/ui/src/component.ts": "export const ok = 1;\n",
			})
			captureLog(t, func() { convergeGazelle(t, root) })

			owner := generated(t, root, "packages", "ui", "BUILD.bazel")
			if strings.Contains(owner, "tsconfig_types") {
				t.Errorf("a declaration named only in `include` got a staging label:\n%s", owner)
			}
			if strings.Contains(owner, "public_globals") {
				t.Errorf("a declaration named only in `include` was published as globals:\n%s", owner)
			}
			below := generated(t, root, "packages", "ui", "src", "BUILD.bazel")
			if strings.Contains(below, "types = ") || strings.Contains(below, "types_srcs") {
				t.Errorf("a declaration named only in `include` reached a target below it:\n%s", below)
			}
		})
	}
}

// The opt-out has to survive a converge. types and types_srcs are outside
// ts_compile's MergeableAttrs, and rule.MergeRules re-adds a generated
// attribute the file does not carry at all -- so deleting the lines does not
// opt out. Two things stick: an empty value on both, and a "# keep" on the
// rule.
func TestRootAmbientTypes_EmptyValueIsTheOptOut(t *testing.T) {
	root, build := optOutWorkspace(t)
	before := generated(t, root, "worker", "src", "BUILD.bazel")

	deleted := strings.NewReplacer(
		"    types = [\"../worker-configuration.d.ts\"],\n", "",
		"    types_srcs = [\"//worker:tsconfig_types\"],\n", "",
	).Replace(before)
	if err := os.WriteFile(build, []byte(deleted), 0o644); err != nil {
		t.Fatal(err)
	}
	captureLog(t, func() { convergeGazelle(t, root) })
	if back := generated(t, root, "worker", "src", "BUILD.bazel"); !strings.Contains(back, "types_srcs") {
		t.Fatalf("deleting the lines opted out, so the empty value below is not the "+
			"opt-out the docs name:\n%s", back)
	}
	if err := os.WriteFile(build, []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	edited := strings.NewReplacer(
		`types = ["../worker-configuration.d.ts"]`, "types = []",
		`types_srcs = ["//worker:tsconfig_types"]`, "types_srcs = []",
	).Replace(before)
	if edited == before {
		t.Fatalf("the attributes to empty are not in:\n%s", before)
	}
	if err := os.WriteFile(build, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	captureLog(t, func() { convergeGazelle(t, root) })

	after := generated(t, root, "worker", "src", "BUILD.bazel")
	if !strings.Contains(after, "types = []") || !strings.Contains(after, "types_srcs = []") {
		t.Errorf("the empty opt-out did not survive a converge:\n%s", after)
	}
	if strings.Contains(after, "worker-configuration.d.ts") {
		t.Errorf("the entry came back over the opt-out:\n%s", after)
	}
}

// The other one, which keeps the inherited list rather than replacing it with
// nothing: hand the whole rule back.
func TestRootAmbientTypes_KeptRuleIsTheOtherOptOut(t *testing.T) {
	root, build := optOutWorkspace(t)
	before := generated(t, root, "worker", "src", "BUILD.bazel")
	edited := strings.NewReplacer(
		"ts_compile(", "# keep\nts_compile(",
		"    types = [\"../worker-configuration.d.ts\"],\n", "",
		"    types_srcs = [\"//worker:tsconfig_types\"],\n", "",
	).Replace(before)
	if err := os.WriteFile(build, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	captureLog(t, func() { convergeGazelle(t, root) })

	after := generated(t, root, "worker", "src", "BUILD.bazel")
	if strings.Contains(after, "types_srcs") || strings.Contains(after, "worker-configuration.d.ts") {
		t.Errorf("a \"# keep\" on the rule did not stop the attributes coming back:\n%s", after)
	}
}

// The generated pair, so that neither opt-out test can pass on a tree where
// nothing was generated in the first place.
func optOutWorkspace(t *testing.T) (root, build string) {
	t.Helper()
	root = t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"package.json":                     `{"name":"w"}` + "\n",
		"worker/tsconfig.json":             `{"compilerOptions": {"types": ["./worker-configuration.d.ts"]}}` + "\n",
		"worker/worker-configuration.d.ts": workerAmbient,
		"worker/src/handler.ts":            "export const env = 1;\n",
	})
	captureLog(t, func() { convergeGazelle(t, root) })
	build = filepath.Join(root, "worker", "src", "BUILD.bazel")
	body := generated(t, root, "worker", "src", "BUILD.bazel")
	if !strings.Contains(body, "types_srcs") {
		t.Fatalf("nothing was generated to opt out of:\n%s", body)
	}
	return root, build
}

// Two shapes produce nothing rather than a guess, each said once: a path no
// label of the tsconfig's own package can stage, and a file that is not there.
func TestRootAmbientTypes_UnstageableShapesAreRefused(t *testing.T) {
	cases := []struct {
		name  string
		types string
		files map[string]string
		said  string
	}{{
		name:  "below the tsconfig",
		types: `["./types/globals.d.ts"]`,
		files: map[string]string{"worker/types/globals.d.ts": workerAmbient},
		said:  "a label stages a file of its own directory alone",
	}, {
		name:  "no such file",
		types: `["./missing.d.ts"]`,
		said:  "no such file is there",
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			files := map[string]string{
				"package.json":          `{"name":"w"}` + "\n",
				"worker/tsconfig.json":  `{"compilerOptions": {"types": ` + tc.types + `}}` + "\n",
				"worker/src/handler.ts": "export const env = 1;\n",
			}
			for rel, body := range tc.files {
				files[rel] = body
			}
			writeWorkspace(t, root, files)
			logged := captureLog(t, func() { convergeGazelle(t, root) })

			if !strings.Contains(logged, tc.said) {
				t.Errorf("the refusal was not said: %s", logged)
			}
			below := generated(t, root, "worker", "src", "BUILD.bazel")
			if strings.Contains(below, "types_srcs") {
				t.Errorf("a shape no label can answer produced one anyway:\n%s", below)
			}
			owner := generated(t, root, "worker", "BUILD.bazel")
			if strings.Contains(owner, "tsconfig_types") {
				t.Errorf("a shape no label can answer got a filegroup:\n%s", owner)
			}
		})
	}
}

// Three separate call sites write the pair, and the ts_test one was the shape
// that shipped a BUILD file Bazel refuses to load.
func TestRootAmbientTypes_EveryGeneratedKindCarriesThem(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"package.json":              `{"name":"w"}` + "\n",
		"BUILD.bazel":               "# gazelle:ts_package_boundary every-dir\n",
		"tsconfig.json":             `{"compilerOptions": {"types": ["./globals.d.ts"]}}` + "\n",
		"globals.d.ts":              workerAmbient,
		"index.html":                "<html></html>\n",
		"src/app/main.tsx":          "export const main = WORKER_ENV;\n",
		"src/app/panel.ts":          "export const panel = 1;\n",
		"src/app/panel.test.ts":     "export const t = 1;\n",
		"src/app/panel.stories.tsx": "export const story = 1;\n",
	})
	captureLog(t, func() { convergeGazelle(t, root) })

	want := map[string]string{
		"app":      "ts_compile",
		"app_test": "ts_test",
		"app_doc":  "ts_compile",
	}
	for _, r := range loadRules(t, root, "src/app") {
		kind, ours := want[r.Name()]
		if !ours || r.Kind() != kind {
			continue
		}
		delete(want, r.Name())
		if got := r.AttrStrings("types"); len(got) != 1 || got[0] != "../../globals.d.ts" {
			t.Errorf("%s(%q) carries types = %q, not the entry rebased onto its package",
				r.Kind(), r.Name(), got)
		}
		if got := r.AttrStrings("types_srcs"); len(got) != 1 || got[0] != "//:tsconfig_types" {
			t.Errorf("%s(%q) carries types_srcs = %q, so its entry resolves to nothing",
				r.Kind(), r.Name(), got)
		}
	}
	for name, kind := range want {
		t.Errorf("no %s named %q was generated, so this test says nothing about that kind", kind, name)
	}
}

// The file is not there: the ts_codegen in the tsconfig's own BUILD file names
// it in outs, so that target is the label and no filegroup is written.
func TestRootAmbientTypes_GeneratedDeclarationNamesItsGenerator(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"package.json":         `{"name":"w"}` + "\n",
		"worker/tsconfig.json": `{"compilerOptions": {"types": ["./worker-configuration.d.ts"]}}` + "\n",
		"worker/BUILD.bazel": `load("@rules_typescript//ts:defs.bzl", "ts_codegen")

ts_codegen(
    name = "worker_types",
    srcs = ["wrangler.jsonc"],
    outs = ["worker-configuration.d.ts"],
    args = [
        "--config",
        "wrangler.jsonc",
        "--out",
        "{out}",
        "--srcs",
        "{srcs}",
    ],
    generator = "@rules_typescript//tools/codegen:wrangler_types",
    node_modules = ":node_modules",
)
`,
		"worker/wrangler.jsonc": `{"name": "w", "main": "src/handler.ts"}` + "\n",
		"worker/index.ts":       "export const env = WORKER_ENV;\n",
		"worker/index.test.ts":  "export const t = WORKER_ENV;\n",
		"worker/src/handler.ts": "export const env = WORKER_ENV;\n",
	})
	logged := captureLog(t, func() { convergeGazelle(t, root) })
	if strings.Contains(logged, "no such file is there") {
		t.Errorf("a file a ts_codegen writes was refused as absent: %s", logged)
	}

	own := loadRules(t, root, "worker")
	requireTypesPair(t, own, "ts_compile", "worker", []string{"./worker-configuration.d.ts"}, []string{":worker_types"})
	requireTypesPair(t, own, "ts_test", "worker_test", []string{"./worker-configuration.d.ts"}, []string{":worker_types"})
	requireTypesPair(t, loadRules(t, root, "worker/src"), "ts_compile", "src", []string{"../worker-configuration.d.ts"}, []string{"//worker:worker_types"})

	owner := generated(t, root, "worker", "BUILD.bazel")
	if strings.Contains(owner, "tsconfig_types") {
		t.Errorf("a filegroup was written over a file that is not in the source tree:\n%s", owner)
	}
	if !strings.Contains(owner, `name = "worker_types"`) {
		t.Errorf("the hand-written ts_codegen did not survive the run:\n%s", owner)
	}
}

// A test directory's tsconfig names the worker's declaration one directory up;
// the label staging it is the one the ancestor's tsconfig already produced.
func TestRootAmbientTypes_ParentEntryNamesTheAncestorsLabel(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"package.json":                     `{"name":"w"}` + "\n",
		"worker/tsconfig.json":             `{"compilerOptions": {"types": ["./worker-configuration.d.ts"]}}` + "\n",
		"worker/worker-configuration.d.ts": workerAmbient,
		"worker/src/handler.ts":            "export const env = WORKER_ENV;\n",
		"worker/test/tsconfig.json": `{"extends": "../tsconfig.json", "compilerOptions": {"types": ` +
			`["@cloudflare/vitest-pool-workers/types", "../worker-configuration.d.ts"]}}` + "\n",
		"worker/test/index.spec.ts": "export const env = WORKER_ENV;\n",
		"worker/test/mocks/env.ts":  "export const env = WORKER_ENV;\n",
	})
	logged := captureLog(t, func() { convergeGazelle(t, root) })
	if strings.Contains(logged, "compilerOptions.types") {
		t.Errorf("an entry the ancestor stages was refused: %s", logged)
	}

	requireTypesPair(t, loadRules(t, root, "worker/test"), "ts_test", "test_test",
		[]string{"@cloudflare/vitest-pool-workers/types", "../worker-configuration.d.ts"},
		[]string{"//worker:tsconfig_types"})
	requireTypesPair(t, loadRules(t, root, "worker/test/mocks"), "ts_compile", "mocks",
		[]string{"@cloudflare/vitest-pool-workers/types", "../../worker-configuration.d.ts"},
		[]string{"//worker:tsconfig_types"})

	leaf := generated(t, root, "worker", "test", "BUILD.bazel")
	if strings.Contains(leaf, `name = "tsconfig_types"`) {
		t.Errorf("the leaf got a filegroup over a file it does not hold:\n%s", leaf)
	}
	owner := generated(t, root, "worker", "BUILD.bazel")
	if n := strings.Count(owner, `name = "tsconfig_types"`); n != 1 {
		t.Errorf("the ancestor carries %d filegroups named tsconfig_types, want 1:\n%s", n, owner)
	}
}

// The label exists only where the tsconfig at the directory the hops name
// names the file; a tsconfig at the root has nothing above it to name.
func TestRootAmbientTypes_ParentEntryNothingAboveStagesIsRefused(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		leaf  string
		said  string
	}{{
		name: "the tsconfig above names other entries",
		files: map[string]string{
			"worker/tsconfig.json":             `{"compilerOptions": {"types": ["node"]}}` + "\n",
			"worker/worker-configuration.d.ts": workerAmbient,
		},
		leaf: "worker/test",
		said: `a tsconfig there has to name "./worker-configuration.d.ts" in its own compilerOptions.types`,
	}, {
		name: "no tsconfig above",
		files: map[string]string{
			"worker/worker-configuration.d.ts": workerAmbient,
		},
		leaf: "worker/test",
		said: "nothing in worker stages that file",
	}, {
		name: "above the workspace root",
		leaf: "",
		said: "a path above the workspace root",
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			files := map[string]string{
				"package.json":                          `{"name":"w"}` + "\n",
				filepath.Join(tc.leaf, "tsconfig.json"): `{"compilerOptions": {"types": ["../worker-configuration.d.ts"]}}` + "\n",
				filepath.Join(tc.leaf, "index.spec.ts"): "export const env = 1;\n",
			}
			for rel, body := range tc.files {
				files[rel] = body
			}
			writeWorkspace(t, root, files)
			logged := captureLog(t, func() { convergeGazelle(t, root) })

			if !strings.Contains(logged, tc.said) {
				t.Errorf("the refusal was not said: %s", logged)
			}
			if n := strings.Count(logged, "typescript: the tsconfig in"); n != 1 {
				t.Errorf("the refusal was said %d times, want once: %s", n, logged)
			}
			below := generated(t, root, filepath.FromSlash(tc.leaf), "BUILD.bazel")
			if strings.Contains(below, "types_srcs") || strings.Contains(below, "worker-configuration") {
				t.Errorf("an entry nothing stages produced the pair anyway:\n%s", below)
			}
			if _, err := os.Stat(filepath.Join(root, "worker", "BUILD.bazel")); err == nil {
				if owner := generated(t, root, "worker", "BUILD.bazel"); strings.Contains(owner, "tsconfig_types") {
					t.Errorf("a directory whose tsconfig names no file got a filegroup:\n%s", owner)
				}
			}
		})
	}
}

// The tsconfig at the directory the hops name decides, whatever sits between:
// the leaf's list replaces the one between whole, as `extends` does.
func TestRootAmbientTypes_ParentEntryClimbsPastTheTsconfigsBetween(t *testing.T) {
	cases := []struct {
		name        string
		files       map[string]string
		between     []string
		betweenSrcs []string
	}{{
		name: "the tsconfig between names other entries",
		files: map[string]string{
			"worker/test/tsconfig.json":      `{"extends": "../tsconfig.json", "compilerOptions": {"types": ["node"]}}` + "\n",
			"worker/test/deep/tsconfig.json": `{"extends": "../tsconfig.json", "compilerOptions": {"types": ["../../worker-configuration.d.ts"]}}` + "\n",
		},
		between: []string{"node"},
	}, {
		name: "the tsconfig between carries the entry one hop shorter",
		files: map[string]string{
			"worker/test/tsconfig.json":      `{"extends": "../tsconfig.json", "compilerOptions": {"types": ["../worker-configuration.d.ts"]}}` + "\n",
			"worker/test/deep/tsconfig.json": `{"extends": "../tsconfig.json", "compilerOptions": {"types": ["../../worker-configuration.d.ts"]}}` + "\n",
		},
		between:     []string{"../worker-configuration.d.ts"},
		betweenSrcs: []string{"//worker:tsconfig_types"},
	}, {
		name: "no tsconfig between",
		files: map[string]string{
			"worker/test/deep/tsconfig.json": `{"extends": "../../tsconfig.json", "compilerOptions": {"types": ["../../worker-configuration.d.ts"]}}` + "\n",
		},
		between:     []string{"../worker-configuration.d.ts"},
		betweenSrcs: []string{"//worker:tsconfig_types"},
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			files := map[string]string{
				"package.json":                     `{"name":"w"}` + "\n",
				"worker/tsconfig.json":             `{"compilerOptions": {"types": ["./worker-configuration.d.ts"]}}` + "\n",
				"worker/worker-configuration.d.ts": workerAmbient,
				"worker/test/index.spec.ts":        "export const env = 1;\n",
				"worker/test/deep/index.spec.ts":   "export const env = WORKER_ENV;\n",
			}
			for rel, body := range tc.files {
				files[rel] = body
			}
			writeWorkspace(t, root, files)
			logged := captureLog(t, func() { convergeGazelle(t, root) })
			if strings.Contains(logged, "compilerOptions.types") {
				t.Errorf("an entry the tsconfig two directories up stages was refused: %s", logged)
			}

			requireTypesPair(t, loadRules(t, root, "worker/test/deep"), "ts_test", "deep_test",
				[]string{"../../worker-configuration.d.ts"}, []string{"//worker:tsconfig_types"})
			requireTypesPair(t, loadRules(t, root, "worker/test"), "ts_test", "test_test", tc.between, tc.betweenSrcs)
			for _, dir := range []string{"worker/test", "worker/test/deep"} {
				if build := generated(t, root, filepath.FromSlash(dir), "BUILD.bazel"); strings.Contains(build, `name = "tsconfig_types"`) {
					t.Errorf("%s got a filegroup over a file it does not hold:\n%s", dir, build)
				}
			}
		})
	}
}

// A list with no file entry is the same one key that `extends` replaces whole;
// the rule resolves a package entry from deps, so no types_srcs and no filegroup.
func TestRootAmbientTypes_PackageOnlyListIsWrittenWhole(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"package.json":      `{"name":"w"}` + "\n",
		"app/tsconfig.json": `{"compilerOptions": {"types": ["vite/client"]}}` + "\n",
		"app/index.ts":      "export const mode = import.meta.env.MODE;\n",
		"app/src/main.ts":   "export const mode = import.meta.env.MODE;\n",
	})
	captureLog(t, func() { convergeGazelle(t, root) })

	for pkg, name := range map[string]string{"app": "app", "app/src": "src"} {
		requireTypesPair(t, loadRules(t, root, pkg), "ts_compile", name, []string{"vite/client"}, nil)
		for _, r := range loadRules(t, root, pkg) {
			if r.Kind() == "ts_compile" && r.Name() == name && !hasLabel(r.AttrStrings("deps"), "@npm//:vite") {
				t.Errorf("//%s:%s names vite/client in types and has no dep the rule can resolve it from: %q",
					pkg, name, r.AttrStrings("deps"))
			}
		}
	}
	if owner := generated(t, root, "app", "BUILD.bazel"); strings.Contains(owner, "tsconfig_types") {
		t.Errorf("a list naming no file got a filegroup:\n%s", owner)
	}
}

// `"types": []` names no file and the generated config inherits the empty list
// through `extends`: nothing to write.
func TestRootAmbientTypes_EmptyListWritesNothing(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"package.json":      `{"name":"w"}` + "\n",
		"app/tsconfig.json": `{"compilerOptions": {"types": []}}` + "\n",
		"app/index.ts":      "export const ok = 1;\n",
	})
	captureLog(t, func() { convergeGazelle(t, root) })

	if owner := generated(t, root, "app", "BUILD.bazel"); strings.Contains(owner, "types =") {
		t.Errorf("an empty list wrote a types attribute:\n%s", owner)
	}
}

func requireTypesPair(t *testing.T, rules []*rule.Rule, kind, name string, types, typesSrcs []string) {
	t.Helper()
	for _, r := range rules {
		if r.Kind() != kind || r.Name() != name {
			continue
		}
		if got := r.AttrStrings("types"); strings.Join(got, " ") != strings.Join(types, " ") {
			t.Errorf("%s(%q) carries types = %q, want %q", kind, name, got, types)
		}
		if got := r.AttrStrings("types_srcs"); strings.Join(got, " ") != strings.Join(typesSrcs, " ") {
			t.Errorf("%s(%q) carries types_srcs = %q, want %q", kind, name, got, typesSrcs)
		}
		return
	}
	t.Errorf("no %s named %q was generated, so this test says nothing about it", kind, name)
}

func ruleNamed(rules []*rule.Rule, kind, name string) *rule.Rule {
	for _, r := range rules {
		if r.Kind() == kind && r.Name() == name {
			return r
		}
	}
	return nil
}

// The declaration leaves for a ts_codegen's outs and the filegroup goes with it;
// neither attribute merges, so every rule below kept a label no target satisfied.
func TestRootAmbientTypes_StagingLabelFollowsTheDeclaration(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"package.json":                     `{"name":"w"}` + "\n",
		"worker/tsconfig.json":             `{"compilerOptions": {"types": ["./worker-configuration.d.ts"]}}` + "\n",
		"worker/worker-configuration.d.ts": workerAmbient,
		"worker/wrangler.jsonc":            `{"name": "w", "main": "src/handler.ts"}` + "\n",
		"worker/src/handler.ts":            "export const env = WORKER_ENV;\n",
		"worker/test/tsconfig.json":        `{"extends": "../tsconfig.json", "compilerOptions": {"types": ["../worker-configuration.d.ts"]}}` + "\n",
		"worker/test/index.spec.ts":        "export const t = WORKER_ENV;\n",
	})
	captureLog(t, func() { convergeGazelle(t, root) })
	requireTypesPair(t, loadRules(t, root, "worker/src"), "ts_compile", "src",
		[]string{"../worker-configuration.d.ts"}, []string{"//worker:tsconfig_types"})

	// The migration commit: the file goes, the target writing it lands.
	if err := os.Remove(filepath.Join(root, "worker", "worker-configuration.d.ts")); err != nil {
		t.Fatal(err)
	}
	appendFile(t, root, "worker/BUILD.bazel", convergeWorkerTypesCodegen)
	logged := captureLog(t, func() { convergeGazelle(t, root) })

	requireTypesPair(t, loadRules(t, root, "worker/src"), "ts_compile", "src",
		[]string{"../worker-configuration.d.ts"}, []string{"//worker:worker_types"})
	requireTypesPair(t, loadRules(t, root, "worker/test"), "ts_test", "test_test",
		[]string{"../worker-configuration.d.ts"}, []string{"//worker:worker_types"})
	if !strings.Contains(logged, "//worker:tsconfig_types") {
		t.Errorf("a label was replaced on two rules and the run did not say so:\n%s", logged)
	}

	owner := generated(t, root, "worker", "BUILD.bazel")
	if strings.Contains(owner, "tsconfig_types") {
		t.Errorf("the filegroup outlived the file it staged:\n%s", owner)
	}
	if strings.Contains(owner, "ts_compile(") {
		t.Errorf("the ts_compile whose only source was the declaration outlived it:\n%s", owner)
	}
}

// The file goes and nothing writes it: the tsconfig entry is refused and said,
// and the label leaves the rules below rather than naming a withdrawn filegroup.
func TestRootAmbientTypes_StagingLabelIsWithdrawnWhenNothingStages(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"package.json":                     `{"name":"w"}` + "\n",
		"worker/tsconfig.json":             `{"compilerOptions": {"types": ["./worker-configuration.d.ts"]}}` + "\n",
		"worker/worker-configuration.d.ts": workerAmbient,
		"worker/src/handler.ts":            "export const env = WORKER_ENV;\n",
	})
	captureLog(t, func() { convergeGazelle(t, root) })
	if err := os.Remove(filepath.Join(root, "worker", "worker-configuration.d.ts")); err != nil {
		t.Fatal(err)
	}
	logged := captureLog(t, func() { convergeGazelle(t, root) })
	if !strings.Contains(logged, "no such file is there") {
		t.Errorf("the entry naming a file that is gone was not refused:\n%s", logged)
	}

	src := ruleNamed(loadRules(t, root, "worker/src"), "ts_compile", "src")
	if src == nil {
		t.Fatal("no ts_compile src in worker/src")
	}
	if got := src.AttrStrings("types_srcs"); len(got) > 0 {
		t.Errorf("//worker/src still names %q in types_srcs, and nothing stages the declaration", got)
	}
	if owner := generated(t, root, "worker", "BUILD.bazel"); strings.Contains(owner, "tsconfig_types") {
		t.Errorf("the filegroup outlived the file it staged:\n%s", owner)
	}
}

// A label Gazelle did not write is not its to rewrite, and a "# keep" holds even
// one it did; a hand-written label beside Gazelle's stays when Gazelle's goes.
func TestRootAmbientTypes_HandWrittenStagingLabelStays(t *testing.T) {
	const handTypes = `# gazelle:ts_ignore

filegroup(
    name = "hand_types",
    srcs = ["hand.d.ts"],
    visibility = ["//visibility:public"],
)
`
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"package.json":                     `{"name":"w"}` + "\n",
		"vendor/BUILD.bazel":               handTypes,
		"vendor/hand.d.ts":                 "declare const HAND: string;\n",
		"worker/tsconfig.json":             `{"compilerOptions": {"types": ["./worker-configuration.d.ts"]}}` + "\n",
		"worker/worker-configuration.d.ts": workerAmbient,
		"worker/wrangler.jsonc":            `{"name": "w", "main": "src/handler.ts"}` + "\n",
		"worker/src/handler.ts":            "export const env = WORKER_ENV;\n",
		"worker/src/BUILD.bazel": `load("@rules_typescript//ts:defs.bzl", "ts_compile")

ts_compile(
    name = "src",
    srcs = ["handler.ts"],
    tsconfig = "//worker:tsconfig",
    types = ["../worker-configuration.d.ts"],
    types_srcs = ["//vendor:hand_types"],
    visibility = ["//visibility:public"],
)
`,
		"worker/test/tsconfig.json": `{"extends": "../tsconfig.json", "compilerOptions": {"types": ["../worker-configuration.d.ts"]}}` + "\n",
		"worker/test/index.spec.ts": "export const t = WORKER_ENV;\n",
		"worker/test/BUILD.bazel": `load("@rules_typescript//ts:defs.bzl", "ts_test")

ts_test(
    name = "test_test",
    srcs = ["index.spec.ts"],
    tsconfig = ":tsconfig",
    types = ["../worker-configuration.d.ts"],
    # keep
    types_srcs = ["//worker:tsconfig_types"],
)
`,
		"worker/test/mocks/env.ts": "export const env = WORKER_ENV;\n",
		"worker/test/mocks/BUILD.bazel": `load("@rules_typescript//ts:defs.bzl", "ts_compile")

ts_compile(
    name = "mocks",
    srcs = ["env.ts"],
    tsconfig = "//worker/test:tsconfig",
    types = ["../../worker-configuration.d.ts"],
    types_srcs = [
        "//vendor:hand_types",
        "//worker:tsconfig_types",
    ],
    visibility = ["//visibility:public"],
)
`,
	})
	captureLog(t, func() { convergeGazelle(t, root) })
	requireTypesPair(t, loadRules(t, root, "worker/src"), "ts_compile", "src",
		[]string{"../worker-configuration.d.ts"}, []string{"//vendor:hand_types"})

	if err := os.Remove(filepath.Join(root, "worker", "worker-configuration.d.ts")); err != nil {
		t.Fatal(err)
	}
	appendFile(t, root, "worker/BUILD.bazel", convergeWorkerTypesCodegen)
	captureLog(t, func() { convergeGazelle(t, root) })

	requireTypesPair(t, loadRules(t, root, "worker/src"), "ts_compile", "src",
		[]string{"../worker-configuration.d.ts"}, []string{"//vendor:hand_types"})
	requireTypesPair(t, loadRules(t, root, "worker/test"), "ts_test", "test_test",
		[]string{"../worker-configuration.d.ts"}, []string{"//worker:tsconfig_types"})
	requireTypesPair(t, loadRules(t, root, "worker/test/mocks"), "ts_compile", "mocks",
		[]string{"../../worker-configuration.d.ts"}, []string{"//vendor:hand_types", "//worker:worker_types"})
}

// Gazelle writes a tsconfig_types label from the rule's package or one above it,
// so one naming another package's filegroup is hand-written and not judged.
func TestRootAmbientTypes_StagingLabelFromAnotherPackageStays(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"package.json":    `{"name":"w"}` + "\n",
		"a/tsconfig.json": `{"compilerOptions": {"types": ["./a.d.ts"]}}` + "\n",
		"a/a.d.ts":        "declare const A: string;\n",
		"a/src/x.ts":      "export const x = A;\n",
		"b/tsconfig.json": `{"compilerOptions": {"strict": true}}` + "\n",
		"b/src/y.ts":      "export const y = A;\n",
		"b/src/BUILD.bazel": `load("@rules_typescript//ts:defs.bzl", "ts_compile")

ts_compile(
    name = "src",
    srcs = ["y.ts"],
    tsconfig = "//b:tsconfig",
    types = ["../../a/a.d.ts"],
    types_srcs = ["//a:tsconfig_types"],
    visibility = ["//visibility:public"],
)
`,
	})
	logged := captureLog(t, func() { convergeGazelle(t, root) })

	if owner := generated(t, root, "a", "BUILD.bazel"); !strings.Contains(owner, `name = "tsconfig_types"`) {
		t.Fatalf("a/ stages nothing, so this test says nothing about a live label:\n%s", owner)
	}
	requireTypesPair(t, loadRules(t, root, "b/src"), "ts_compile", "src",
		[]string{"../../a/a.d.ts"}, []string{"//a:tsconfig_types"})
	if strings.Contains(logged, "//a:tsconfig_types") {
		t.Errorf("the run spoke of a label it never wrote:\n%s", logged)
	}
}

// The rewrite edits the list in place, so a "# keep" on an entry beside the
// stale one survives, and one on the stale entry itself holds it.
func TestRootAmbientTypes_ValueKeepSurvivesTheRewrite(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"package.json": `{"name":"w"}` + "\n",
		"vendor/BUILD.bazel": `# gazelle:ts_ignore

filegroup(
    name = "hand_types",
    srcs = ["hand.d.ts"],
    visibility = ["//visibility:public"],
)
`,
		"vendor/hand.d.ts":                 "declare const HAND: string;\n",
		"worker/tsconfig.json":             `{"compilerOptions": {"types": ["./worker-configuration.d.ts"]}}` + "\n",
		"worker/worker-configuration.d.ts": workerAmbient,
		"worker/wrangler.jsonc":            `{"name": "w", "main": "src/handler.ts"}` + "\n",
		"worker/src/handler.ts":            "export const env = WORKER_ENV;\n",
		"worker/src/BUILD.bazel": `load("@rules_typescript//ts:defs.bzl", "ts_compile")

ts_compile(
    name = "src",
    srcs = ["handler.ts"],
    tsconfig = "//worker:tsconfig",
    types = ["../worker-configuration.d.ts"],
    types_srcs = [
        "//vendor:hand_types",  # keep
        "//worker:tsconfig_types",
    ],
    visibility = ["//visibility:public"],
)
`,
		"worker/test/tsconfig.json": `{"extends": "../tsconfig.json", "compilerOptions": {"types": ["../worker-configuration.d.ts"]}}` + "\n",
		"worker/test/index.spec.ts": "export const t = WORKER_ENV;\n",
		"worker/test/BUILD.bazel": `load("@rules_typescript//ts:defs.bzl", "ts_test")

ts_test(
    name = "test_test",
    srcs = ["index.spec.ts"],
    tsconfig = ":tsconfig",
    types = ["../worker-configuration.d.ts"],
    types_srcs = [
        "//worker:tsconfig_types",  # keep
    ],
)
`,
	})
	captureLog(t, func() { convergeGazelle(t, root) })
	if err := os.Remove(filepath.Join(root, "worker", "worker-configuration.d.ts")); err != nil {
		t.Fatal(err)
	}
	appendFile(t, root, "worker/BUILD.bazel", convergeWorkerTypesCodegen)
	captureLog(t, func() { convergeGazelle(t, root) })

	if src := generated(t, root, "worker", "src", "BUILD.bazel"); !strings.Contains(src, `"//vendor:hand_types",  # keep`) {
		t.Errorf("the # keep on the entry beside the rewritten one was dropped:\n%s", src)
	}
	requireTypesPair(t, loadRules(t, root, "worker/src"), "ts_compile", "src",
		[]string{"../worker-configuration.d.ts"}, []string{"//vendor:hand_types", "//worker:worker_types"})
	requireTypesPair(t, loadRules(t, root, "worker/test"), "ts_test", "test_test",
		[]string{"../worker-configuration.d.ts"}, []string{"//worker:tsconfig_types"})
}

// The entry is judged by the filegroup it names, not by this rule's recomputed
// value: a hand-written rule whose own tsconfig names no file may still name it.
func TestRootAmbientTypes_StagingLabelOfALiveAncestorFilegroupStays(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"package.json":                     `{"name":"w"}` + "\n",
		"worker/tsconfig.json":             `{"compilerOptions": {"types": ["./worker-configuration.d.ts"]}}` + "\n",
		"worker/worker-configuration.d.ts": workerAmbient,
		"worker/src/handler.ts":            "export const env = WORKER_ENV;\n",
		"worker/tools/tsconfig.json":       `{"compilerOptions": {"strict": true}}` + "\n",
		"worker/tools/build.ts":            "export const b = WORKER_ENV;\n",
		"worker/tools/BUILD.bazel": `load("@rules_typescript//ts:defs.bzl", "ts_compile")

ts_compile(
    name = "tools",
    srcs = ["build.ts"],
    tsconfig = ":tsconfig",
    types = ["../worker-configuration.d.ts"],
    types_srcs = ["//worker:tsconfig_types"],
    visibility = ["//visibility:public"],
)
`,
	})
	logged := captureLog(t, func() { convergeGazelle(t, root) })

	if owner := generated(t, root, "worker", "BUILD.bazel"); !strings.Contains(owner, `name = "tsconfig_types"`) {
		t.Fatalf("worker/ stages nothing, so this test says nothing about a live label:\n%s", owner)
	}
	requireTypesPair(t, loadRules(t, root, "worker/tools"), "ts_compile", "tools",
		[]string{"../worker-configuration.d.ts"}, []string{"//worker:tsconfig_types"})
	if strings.Contains(logged, "//worker:tsconfig_types") {
		t.Errorf("the run spoke of a label naming a filegroup it still writes:\n%s", logged)
	}
}

// The label is live while the filegroup it names is on disk after the run: one
// held by a "# keep" under the reserved name, in a package that stages nothing.
func TestRootAmbientTypes_KeptFilegroupByTheReservedNameIsLive(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"package.json":         `{"name":"w"}` + "\n",
		"worker/tsconfig.json": `{"compilerOptions": {"strict": true}}` + "\n",
		"worker/hand.d.ts":     "declare const HAND: string;\n",
		"worker/BUILD.bazel": `# keep
filegroup(
    name = "tsconfig_types",
    srcs = ["hand.d.ts"],
    visibility = ["//visibility:public"],
)
`,
		"worker/src/handler.ts": "export const env = HAND;\n",
		"worker/src/BUILD.bazel": `load("@rules_typescript//ts:defs.bzl", "ts_compile")

ts_compile(
    name = "src",
    srcs = ["handler.ts"],
    tsconfig = "//worker:tsconfig",
    types = ["../hand.d.ts"],
    types_srcs = ["//worker:tsconfig_types"],
    visibility = ["//visibility:public"],
)
`,
	})
	logged := captureLog(t, func() { convergeGazelle(t, root) })

	if owner := generated(t, root, "worker", "BUILD.bazel"); !strings.Contains(owner, `name = "tsconfig_types"`) {
		t.Fatalf("the # keep did not hold the filegroup, so this test says nothing about a live label:\n%s", owner)
	}
	requireTypesPair(t, loadRules(t, root, "worker/src"), "ts_compile", "src",
		[]string{"../hand.d.ts"}, []string{"//worker:tsconfig_types"})
	if strings.Contains(logged, "//worker:tsconfig_types") {
		t.Errorf("the run spoke of a label naming a filegroup it leaves in place:\n%s", logged)
	}
}
