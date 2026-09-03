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
		said:  "a label can only stage a file of the directory the tsconfig itself is in",
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

// Four separate call sites write the pair, and the ts_test one was the shape
// that shipped a BUILD file Bazel refuses to load.
func TestRootAmbientTypes_EveryGeneratedKindCarriesThem(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"package.json":              `{"name":"w","dependencies":{"@tanstack/react-router":"1.0.0"}}` + "\n",
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
		"main":     "ts_compile",
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

// The file is not there: a ts_worker_types target in the tsconfig's own BUILD
// file writes it. That target is the label staging it, and the filegroup --
// which could only name a source file -- is not written.
func TestRootAmbientTypes_GeneratedDeclarationNamesItsGenerator(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"package.json":         `{"name":"w"}` + "\n",
		"worker/tsconfig.json": `{"compilerOptions": {"types": ["./worker-configuration.d.ts"]}}` + "\n",
		"worker/BUILD.bazel": `load("@rules_typescript//ts:defs.bzl", "ts_worker_types")

ts_worker_types(
    name = "worker_types",
    config = "wrangler.jsonc",
    node_modules = ":node_modules",
)
`,
		"worker/wrangler.jsonc": `{"name": "w", "main": "src/handler.ts"}` + "\n",
		"worker/src/handler.ts": "export const env = WORKER_ENV;\n",
	})
	logged := captureLog(t, func() { convergeGazelle(t, root) })
	if strings.Contains(logged, "no such file is there") {
		t.Errorf("a file a ts_worker_types target writes was refused as absent: %s", logged)
	}

	below := generated(t, root, "worker", "src", "BUILD.bazel")
	if !strings.Contains(below, `types = ["../worker-configuration.d.ts"]`) {
		t.Errorf("//worker/src names no rebased `types` entry:\n%s", below)
	}
	if !strings.Contains(below, `types_srcs = ["//worker:worker_types"]`) {
		t.Errorf("//worker/src does not name the generating target, so nothing stages the "+
			"declaration:\n%s", below)
	}

	owner := generated(t, root, "worker", "BUILD.bazel")
	if strings.Contains(owner, "tsconfig_types") {
		t.Errorf("a filegroup was written over a file that is not in the source tree:\n%s", owner)
	}
	if !strings.Contains(owner, `name = "worker_types"`) {
		t.Errorf("the hand-written ts_worker_types did not survive the run:\n%s", owner)
	}
}
