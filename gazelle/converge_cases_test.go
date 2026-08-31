package typescript

// The fixtures and mutations convergeGazelle is driven with: one workspace per
// framework Gazelle generates for, plus a plain TypeScript one.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bazelbuild/bazel-gazelle/rule"
)

type convergeMutation struct {
	kind   string
	write  map[string]string
	remove []string

	// Workspace paths the app rule's declared inputs must cover after the
	// mutation: undeclared is absent from the build with nothing failing.
	stage []string
}

type convergeCase struct {
	name      string
	files     map[string]string
	mutations []convergeMutation
}

// ---- fixtures ---------------------------------------------------------------

const (
	convergeNextPkg = `{
  "name": "w",
  "dependencies": {"next": "15.3.4", "react": "19.0.0", "react-dom": "19.0.0"},
  "devDependencies": {"typescript": "5.8.2", "@types/node": "22.14.0", "@types/react": "19.0.10"}
}
`
	convergeRemixPkg = `{
  "name": "w",
  "dependencies": {
    "@remix-run/dev": "2.17.4",
    "@remix-run/node": "2.17.4",
    "@remix-run/react": "2.17.4",
    "react": "19.1.0",
    "react-dom": "19.1.0"
  }
}
`
	convergeTanStackPkg = `{
  "name": "w",
  "dependencies": {
    "@tanstack/react-router": "1.166.7",
    "@tanstack/react-start": "1.166.8",
    "react": "19.1.0",
    "react-dom": "19.1.0"
  },
  "devDependencies": {"vite": "8.2.2"}
}
`
	convergeSveltePkg = `{
  "name": "w",
  "type": "module",
  "devDependencies": {
    "@sveltejs/kit": "2.46.4",
    "@sveltejs/vite-plugin-svelte": "6.2.1",
    "svelte": "5.42.2",
    "vite": "8.2.2"
  }
}
`
	convergeSolidPkg = `{"name":"w","dependencies":{"@solidjs/start":"1.0.0","solid-js":"1.9.0"}}` + "\n"
	convergePlainPkg = `{"name":"w","dependencies":{"zod":"3.24.2"}}` + "\n"

	convergeAliasTsConfig = `{"compilerOptions":{"baseUrl":".","paths":{` +
		`"@/*":["./src/*"],"@lib/*":["./src/lib/*"],"@ui/*":["./src/ui/*"]}}}` + "\n"
)

func convergeFixture(t *testing.T, name string) convergeCase {
	t.Helper()
	for _, tc := range convergeCases() {
		if tc.name == name {
			return tc
		}
	}
	t.Fatalf("no %q fixture in convergeCases()", name)
	return convergeCase{}
}

func convergeCases() []convergeCase {
	return []convergeCase{
		{
			name: "plain",
			files: map[string]string{
				"package.json":           convergePlainPkg,
				"tsconfig.json":          `{"compilerOptions":{"strict":true}}` + "\n",
				"src/index.ts":           "export * from \"./lib/helper\";\n",
				"src/routes/index.ts":    "export const routes = 1;\n",
				"src/routes/home.ts":     "export const home = 1;\n",
				"src/lib/helper.ts":      "export const helper = 1;\n",
				"src/lib/helper.test.ts": "export const t = 1;\n",
				"src/lib/helper.doc.ts":  "export * from \"./helper\";\n",
			},
			mutations: []convergeMutation{
				{kind: "add_colocated_module", write: map[string]string{"src/routes/home.data.ts": "export const data = 1;\n"}},
				{kind: "add_folder_route", write: map[string]string{
					"src/routes/panel/index.ts": "export * from \"./view\";\n",
					"src/routes/panel/view.ts":  "export const view = 1;\n",
				}},
				{kind: "add_flat_route", write: map[string]string{"src/routes/about.ts": "export const about = 1;\n"}},
				{kind: "add_nested_route_dir", write: map[string]string{"src/routes/admin/users/list.ts": "export const list = 1;\n"}},
				{kind: "add_root_shared_file", write: map[string]string{"version.ts": "export const version = \"1\";\n"}},
				{kind: "add_shared_dir_no_ts", write: map[string]string{
					"styles/globals.css": ".a{color:red}\n",
					"data/config.json":   `{"a":1}` + "\n",
				}},
				{kind: "add_shared_dir_with_ts", write: map[string]string{"lib2/extra.ts": "export const extra = 1;\n"}},
				{kind: "add_file_to_existing_target", write: map[string]string{"src/lib/format.ts": "export const format = 1;\n"}},
				{kind: "delete_route", remove: []string{"src/routes/home.ts"}},
				{kind: "delete_doc", remove: []string{"src/lib/helper.doc.ts"}},
			},
		},
		{
			// path_aliases is the one attribute Gazelle owns whose value is a
			// dict. No fixture declared compilerOptions.paths, so no fixture
			// generated it, and nothing here asked what a merge does to a dict.
			name: "path_aliases",
			files: map[string]string{
				"package.json":      convergePlainPkg,
				"tsconfig.json":     convergeAliasTsConfig,
				"src/index.ts":      "import { helper } from \"@/lib/helper\";\nexport const a = helper;\n",
				"src/main.ts":       "export const main = 1;\n",
				"src/lib/helper.ts": "export const helper = 1;\n",
				"src/lib/util.ts":   "export const util = 1;\n",
				"src/ui/button.ts":  "export const button = 1;\n",
			},
			mutations: []convergeMutation{
				// The alias map gains an entry: the run that recomputes it has
				// to write the entry, not the map the first run left behind.
				{kind: "add_alias_import", write: map[string]string{
					"src/extra.ts": "import { helper } from \"@lib/helper\";\nexport const b = helper;\n",
				}},
				// And loses its last one, which is the attribute going away
				// rather than a value inside it.
				{kind: "drop_alias_import", write: map[string]string{
					"src/index.ts": "export const a = 1;\n",
				}},
				{kind: "add_alias_import_in_new_dir", write: map[string]string{
					"src/panel/view.ts": "import { button } from \"@ui/button\";\nexport const v = button;\n",
				}},
				{kind: "add_file_to_existing_target", write: map[string]string{
					"src/format.ts": "export const format = 1;\n",
				}},
				{kind: "delete_alias_importing_file", remove: []string{"src/index.ts"}},
			},
		},
		{
			// The standard pnpm workspace-member shape: package.json and
			// tsconfig.json in the member root, sources one directory down. That
			// root classifies no source of its own, so generation there used to
			// stop before anything was written -- and a label naming its
			// tsconfig had nothing to resolve against.
			name: "pnpm_member",
			files: map[string]string{
				"package.json":                   convergePlainPkg,
				"pnpm-workspace.yaml":            "packages:\n  - packages/*\n",
				"tsconfig.json":                  `{"compilerOptions":{"strict":true}}` + "\n",
				"packages/core/package.json":     `{"name":"@w/core","version":"1.0.0"}` + "\n",
				"packages/core/tsconfig.json":    `{"compilerOptions":{"lib":["es2022"]}}` + "\n",
				"packages/core/src/index.ts":     "export * from \"./util\";\n",
				"packages/core/src/util.ts":      "export const util = 1;\n",
				"packages/core/src/util.test.ts": "export const t = 1;\n",
				"packages/app/package.json":      `{"name":"@w/app","version":"1.0.0"}` + "\n",
				"packages/app/src/main.ts":       "export const main = 1;\n",
			},
			mutations: []convergeMutation{
				{kind: "add_file_to_existing_target", write: map[string]string{"packages/core/src/format.ts": "export const format = 1;\n"}},
				{kind: "add_member_with_tsconfig", write: map[string]string{
					"packages/ui/package.json":  `{"name":"@w/ui","version":"1.0.0"}` + "\n",
					"packages/ui/tsconfig.json": `{"compilerOptions":{"jsx":"react-jsx"}}` + "\n",
					"packages/ui/src/button.ts": "export const button = 1;\n",
				}},
				{kind: "add_tsconfig_to_member", write: map[string]string{
					"packages/app/tsconfig.json": `{"compilerOptions":{"strict":false}}` + "\n",
				}},
				{kind: "delete_member_tsconfig", remove: []string{"packages/core/tsconfig.json"}},
				{kind: "add_nested_source_dir", write: map[string]string{"packages/core/src/nested/leaf.ts": "export const leaf = 1;\n"}},
			},
		},
		{
			name: "next",
			files: map[string]string{
				"package.json":           convergeNextPkg,
				"tsconfig.json":          `{"compilerOptions":{"strict":true}}` + "\n",
				"next.config.mjs":        "export default {};\n",
				"app/layout.tsx":         "export default function L() { return null; }\n",
				"app/page.tsx":           "export default function P() { return null; }\n",
				"app/dashboard/page.tsx": "export default function D() { return null; }\n",
				"lib/greeting.ts":        "export const hi = 1;\n",
			},
			mutations: []convergeMutation{
				{kind: "add_colocated_module", write: map[string]string{"app/dashboard/chart.tsx": "export const Chart = null;\n"}},
				{kind: "add_folder_route", write: map[string]string{"app/settings/page.tsx": "export default function S() { return null; }\n"}},
				{
					kind:  "add_flat_route",
					write: map[string]string{"pages/about.tsx": "export default function A() { return null; }\n"},
					stage: []string{"pages/about.tsx"},
				},
				{kind: "add_nested_route_dir", write: map[string]string{"app/dashboard/reports/page.tsx": "export default function R() { return null; }\n"}},
				{
					kind:  "add_root_shared_file",
					write: map[string]string{"version.ts": "export const version = \"1\";\n"},
					stage: []string{"version.ts"},
				},
				{
					kind:  "add_shared_dir_no_ts",
					write: map[string]string{"styles/globals.css": ".a{color:red}\n"},
					stage: []string{"styles/globals.css"},
				},
				{
					kind:  "add_shared_dir_with_ts",
					write: map[string]string{"lib2/extra.ts": "export const extra = 1;\n"},
					stage: []string{"lib2/extra.ts"},
				},
				{kind: "add_file_to_existing_target", write: map[string]string{"lib/format.ts": "export const format = 1;\n"}},
				{
					kind:  "add_doc_beside_shared_ts",
					write: map[string]string{"lib/greeting.doc.ts": "export * from \"./greeting\";\n"},
					stage: []string{"lib/greeting.doc.ts"},
				},
				{
					kind:  "add_doc_only_dir",
					write: map[string]string{"gallery/tour.doc.ts": "export * from \"../lib/greeting\";\n"},
					stage: []string{"gallery/tour.doc.ts"},
				},
				{kind: "delete_route", remove: []string{"app/dashboard"}},
			},
		},
		{
			name: "remix",
			files: map[string]string{
				"package.json":               convergeRemixPkg,
				"index.html":                 "<html></html>\n",
				"remix-vite.config.mjs":      "export default {};\n",
				"app/entry.client.tsx":       "export {};\n",
				"app/root.tsx":               "export default function Root() { return null; }\n",
				"app/routes/_index.tsx":      "export default function Index() { return null; }\n",
				"app/routes/panel/route.tsx": "export default function Panel() { return null; }\n",
				"app/routes/panel/helper.ts": "export const helper = 1;\n",
				"lib/greeting.ts":            "export const hi = 1;\n",
			},
			mutations: []convergeMutation{
				{
					kind:  "add_colocated_module",
					write: map[string]string{"app/routes/panel/subtitle.ts": "export const subtitle = 1;\n"},
					stage: []string{"app/routes/panel/subtitle.ts"},
				},
				{
					kind: "add_folder_route",
					write: map[string]string{
						"app/routes/later/route.tsx": "export default function Later() { return null; }\n",
						"app/routes/later/bit.ts":    "export const bit = 1;\n",
					},
					stage: []string{"app/routes/later/route.tsx"},
				},
				{
					kind:  "add_flat_route",
					write: map[string]string{"app/routes/about.tsx": "export default function About() { return null; }\n"},
					stage: []string{"app/routes/about.tsx"},
				},
				{kind: "add_nested_route_dir", write: map[string]string{"app/routes/panel/nested/thing.ts": "export const thing = 1;\n"}},
				{
					kind:  "add_root_shared_file",
					write: map[string]string{"version.ts": "export const version = \"1\";\n"},
					stage: []string{"version.ts"},
				},
				{
					kind:  "add_shared_dir_no_ts",
					write: map[string]string{"styles/globals.css": ".a{color:red}\n"},
					stage: []string{"styles/globals.css"},
				},
				{
					kind:  "add_shared_dir_with_ts",
					write: map[string]string{"lib2/extra.ts": "export const extra = 1;\n"},
					stage: []string{"lib2/extra.ts"},
				},
				{kind: "add_file_to_existing_target", write: map[string]string{"app/helpers.ts": "export const helpers = 1;\n"}},
				{kind: "change_entry_imports", write: map[string]string{"app/entry.client.tsx": "import \"zod\";\nexport {};\n"}},
				{
					kind:   "rename_entry",
					write:  map[string]string{"app/bootstrap.tsx": "export {};\n"},
					remove: []string{"app/entry.client.tsx"},
				},
				{kind: "delete_route", remove: []string{"app/routes/panel"}},
			},
		},
		{
			name: "tanstack",
			files: map[string]string{
				"package.json":                  convergeTanStackPkg,
				"index.html":                    "<html></html>\n",
				"tanstack-vite.config.mjs":      "export default {};\n",
				"src/app/main.tsx":              "export {};\n",
				"src/routes/__root.tsx":         "export const Route = null;\n",
				"src/routes/index.tsx":          "export const Route = null;\n",
				"src/routes/users.tsx":          "export const Route = null;\n",
				"src/routes/settings/index.tsx": "export const Route = null;\n",
				"src/routes/settings/panel.ts":  "export const panel = 1;\n",
				"src/lib/params.ts":             "export const params = 1;\n",
				"src/components/Layout.tsx":     "export const Layout = null;\n",
			},
			mutations: []convergeMutation{
				{
					kind:  "add_colocated_module",
					write: map[string]string{"src/routes/-shared.ts": "export const shared = 1;\n"},
					stage: []string{"src/routes/-shared.ts"},
				},
				{
					kind:  "add_folder_route",
					write: map[string]string{"src/routes/posts/index.tsx": "export const Route = null;\n"},
					stage: []string{"src/routes/posts/index.tsx"},
				},
				{
					kind:  "add_flat_route",
					write: map[string]string{"src/routes/about.tsx": "export const Route = null;\n"},
					stage: []string{"src/routes/about.tsx"},
				},
				{
					kind:  "add_nested_route_dir",
					write: map[string]string{"src/routes/admin/users/list.tsx": "export const Route = null;\n"},
					stage: []string{"src/routes/admin/users/list.tsx"},
				},
				{
					kind:  "add_root_shared_file",
					write: map[string]string{"version.ts": "export const version = \"1\";\n"},
					stage: []string{"version.ts"},
				},
				{
					kind:  "add_shared_dir_no_ts",
					write: map[string]string{"styles/globals.css": ".a{color:red}\n"},
					stage: []string{"styles/globals.css"},
				},
				{
					kind:  "add_shared_dir_with_ts",
					write: map[string]string{"lib2/extra.ts": "export const extra = 1;\n"},
					stage: []string{"lib2/extra.ts"},
				},
				{kind: "add_file_to_existing_target", write: map[string]string{"src/lib/format.ts": "export const format = 1;\n"}},
				{kind: "change_entry_imports", write: map[string]string{"src/app/main.tsx": "import \"zod\";\nexport {};\n"}},
				{
					kind:   "rename_entry",
					write:  map[string]string{"src/app/bootstrap.tsx": "export {};\n"},
					remove: []string{"src/app/main.tsx"},
				},
				{kind: "delete_route", remove: []string{"src/routes/settings"}},
			},
		},
		{
			name: "sveltekit",
			files: map[string]string{
				"package.json":                 convergeSveltePkg,
				"svelte.config.js":             "export default {};\n",
				"vite.config.mjs":              "export default {};\n",
				"src/app.html":                 "<html>%sveltekit.head%</html>\n",
				"src/routes/+page.svelte":      "<h1>home</h1>\n",
				"src/routes/+page.server.ts":   "export const load = () => ({});\n",
				"src/routes/api/+server.ts":    "export const GET = () => new Response();\n",
				"src/routes/blog/+page.svelte": "<h1>blog</h1>\n",
				"src/lib/greeting.ts":          "export const hi = 1;\n",
				"lib/shared.ts":                "export const shared = 1;\n",
			},
			mutations: []convergeMutation{
				{kind: "add_colocated_module", write: map[string]string{"src/routes/api/helpers.ts": "export const helpers = 1;\n"}},
				{kind: "add_folder_route", write: map[string]string{"src/routes/about/+page.svelte": "<h1>about</h1>\n"}},
				{kind: "add_flat_route", write: map[string]string{"src/routes/+layout.svelte": "<slot />\n"}},
				{kind: "add_nested_route_dir", write: map[string]string{"src/routes/blog/[slug]/+page.svelte": "<h1>post</h1>\n"}},
				{
					kind:  "add_root_shared_file",
					write: map[string]string{"version.ts": "export const version = \"1\";\n"},
					stage: []string{"version.ts"},
				},
				{
					kind: "add_shared_dir_no_ts",
					write: map[string]string{
						"static/favicon.svg": "<svg></svg>\n",
						"styles/globals.css": ".a{color:red}\n",
					},
					stage: []string{"static/favicon.svg", "styles/globals.css"},
				},
				{
					kind:  "add_shared_dir_with_ts",
					write: map[string]string{"lib2/extra.ts": "export const extra = 1;\n"},
					stage: []string{"lib2/extra.ts"},
				},
				{kind: "add_file_to_existing_target", write: map[string]string{"lib/format.ts": "export const format = 1;\n"}},
				{kind: "delete_route", remove: []string{"src/routes/blog"}},
			},
		},
		{
			name: "solidstart",
			files: map[string]string{
				"package.json":         convergeSolidPkg,
				"src/app.tsx":          "export default function App() { return null; }\n",
				"src/routes/index.tsx": "export default function Index() { return null; }\n",
				"src/routes/about.tsx": "export default function About() { return null; }\n",
				"src/lib/greeting.ts":  "export const hi = 1;\n",
			},
			mutations: []convergeMutation{
				{kind: "add_colocated_module", write: map[string]string{"src/routes/index.data.ts": "export const data = 1;\n"}},
				{kind: "add_folder_route", write: map[string]string{"src/routes/posts/index.tsx": "export default function P() { return null; }\n"}},
				{kind: "add_flat_route", write: map[string]string{"src/routes/contact.tsx": "export default function C() { return null; }\n"}},
				{kind: "add_nested_route_dir", write: map[string]string{"src/routes/admin/users/list.tsx": "export default function L() { return null; }\n"}},
				{kind: "add_root_shared_file", write: map[string]string{"version.ts": "export const version = \"1\";\n"}},
				{kind: "add_shared_dir_no_ts", write: map[string]string{"styles/globals.css": ".a{color:red}\n"}},
				{kind: "add_shared_dir_with_ts", write: map[string]string{"lib2/extra.ts": "export const extra = 1;\n"}},
				{kind: "add_file_to_existing_target", write: map[string]string{"src/lib/format.ts": "export const format = 1;\n"}},
				{kind: "delete_route", remove: []string{"src/routes/about.tsx"}},
			},
		},
	}
}

// ---- the property ----------------------------------------------------------

// For every framework and for a plain workspace: generate, mutate, generate
// again, and require what one generation over the mutated tree produces.
func TestConvergeAfterMutation(t *testing.T) {
	for _, tc := range convergeCases() {
		t.Run(tc.name, func(t *testing.T) {
			for _, mut := range tc.mutations {
				t.Run(mut.kind, func(t *testing.T) { runConvergeCase(t, tc, mut) })
			}
		})
	}
}

func runConvergeCase(t *testing.T, tc convergeCase, mut convergeMutation) {
	t.Helper()

	scratch := t.TempDir()
	writeWorkspace(t, scratch, tc.files)
	applyMutation(t, scratch, mut)
	captureLog(t, func() { convergeGazelle(t, scratch) })
	want := convergeSnapshot(t, scratch)
	wantInputs := collectAppInputs(t, scratch)

	twoRun := t.TempDir()
	writeWorkspace(t, twoRun, tc.files)
	captureLog(t, func() { convergeGazelle(t, twoRun) })
	applyMutation(t, twoRun, mut)
	logged := captureLog(t, func() { convergeGazelle(t, twoRun) })
	got := convergeSnapshot(t, twoRun)
	gotInputs := collectAppInputs(t, twoRun)

	diff := snapshotDiff(want, got)
	switch {
	case snapshotDiff(sortedLists(want), sortedLists(got)) != "":
		t.Errorf("%s/%s does not converge.\n"+
			"  \"-\" is one generation over the mutated tree; \"+\" is generate, mutate, generate.\n%s\n"+
			"second run said:\n%s",
			tc.name, mut.kind, diff, indentLog(logged))
	case diff != "":
		t.Errorf("%s/%s converges on the same targets in a different attribute order, so a "+
			"from-scratch checkout and an incremental one disagree:\n%s", tc.name, mut.kind, diff)
	}

	for _, staged := range mut.stage {
		if gotInputs.covers(staged) {
			continue
		}
		t.Errorf("%s/%s: %s is not among the declared inputs of %s, so the build never sees it "+
			"(from scratch: covered=%v; after generate/mutate/generate: covered=false)",
			tc.name, mut.kind, staged, orNone(gotInputs.rule), wantInputs.covers(staged))
	}

	if dangling := danglingLabels(t, twoRun); len(dangling) > 0 {
		t.Errorf("%s/%s left %d label(s) no target satisfies, which fails analysis for the whole workspace:\n      %s",
			tc.name, mut.kind, len(dangling), strings.Join(dangling, "\n      "))
	}
}

func applyMutation(t *testing.T, root string, mut convergeMutation) {
	t.Helper()
	if len(mut.write) > 0 {
		writeWorkspace(t, root, mut.write)
	}
	for _, rel := range mut.remove {
		if err := os.RemoveAll(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			t.Fatal(err)
		}
	}
}

func indentLog(logged string) string {
	logged = strings.TrimRight(logged, "\n")
	if logged == "" {
		return "      (nothing)"
	}
	var b strings.Builder
	for _, line := range strings.Split(logged, "\n") {
		fmt.Fprintf(&b, "      %s\n", line)
	}
	return strings.TrimRight(b.String(), "\n")
}

func orNone(s string) string {
	if s == "" {
		return "(no app rule at the workspace root)"
	}
	return s
}

// ---- hand-authored values in Gazelle-managed attributes ---------------------

// The generators recompute these attributes from the tree on every run, so a
// value a user writes into one survives only with a "# keep" -- and a run that
// drops one has to name it. Every case above starts from a generated tree and
// mutates sources; none of them authors a value, which is why the deletion went
// unseen.
type handAuthoredCase struct {
	workspace string
	extra     map[string]string
	drop      []string

	pkg    string
	kind   string
	target string
	attr   string
	shape  string // "list", "glob" or "scalar"
	value  string
}

const handVendorPackage = `# gazelle:ts_ignore

filegroup(
    name = "vendor_hand",
    srcs = ["legacy.js"],
    visibility = ["//visibility:public"],
)
`

func handAuthoredCases() []handAuthoredCase {
	vendor := map[string]string{
		"vendor/BUILD.bazel": handVendorPackage,
		"vendor/legacy.js":   "export const legacy = 1;\n",
	}
	return []handAuthoredCase{
		{
			workspace: "next", kind: "next_build", target: "app",
			attr: "staging_srcs", shape: "list", value: "//vendor:vendor_hand",
			extra: vendor,
		},
		{
			workspace: "next", kind: "next_build", target: "app",
			attr: "srcs", shape: "glob", value: "content/**",
			extra: map[string]string{"content/post.mdx": "# post\n"},
		},
		{
			workspace: "next", kind: "next_build", target: "app",
			attr: "config", shape: "scalar", value: "custom.next.config.mjs",
			drop:  []string{"next.config.mjs"},
			extra: map[string]string{"custom.next.config.mjs": "export default {};\n"},
		},
		{
			workspace: "next", kind: "next_build", target: "app",
			attr: "tsconfig", shape: "scalar", value: "tsconfig.build.json",
			extra: map[string]string{"tsconfig.build.json": `{"compilerOptions":{"strict":true}}` + "\n"},
		},
		{
			workspace: "next", kind: "node_modules", target: "node_modules",
			attr: "deps", shape: "list", value: "@npm//:sharp",
		},
		{
			workspace: "next", kind: "next_dev_server", target: "dev",
			attr: "node_modules", shape: "scalar", value: ":dev_only_nm",
		},
		{
			workspace: "remix", kind: "ts_bundle", target: "app_remix",
			attr: "staging_srcs", shape: "list", value: "//vendor:vendor_hand",
			extra: vendor,
		},
		{
			workspace: "remix", kind: "node_modules", target: "node_modules",
			attr: "deps", shape: "list", value: "@npm//:sharp",
		},
		{
			workspace: "tanstack", kind: "ts_bundle", target: "app",
			attr: "staging_srcs", shape: "list", value: "//vendor:vendor_hand",
			extra: vendor,
		},
		{
			workspace: "tanstack", pkg: "src/routes", kind: "filegroup", target: "sources",
			attr: "srcs", shape: "list", value: "//vendor:vendor_hand",
			extra: vendor,
		},
		{
			workspace: "sveltekit", kind: "sveltekit_build", target: "app",
			attr: "srcs", shape: "glob", value: "content/**",
			extra: map[string]string{"content/post.md": "# post\n"},
		},
		{
			workspace: "sveltekit", kind: "sveltekit_build", target: "app",
			attr: "svelte_config", shape: "scalar", value: "svelte.config.mjs",
			extra: map[string]string{"svelte.config.mjs": "export default {};\n"},
		},
	}
}

// TestHandAuthoredAttrValue pins both halves of the contract: a value carrying
// "# keep" survives the next run, and one without it is replaced out loud.
func TestHandAuthoredAttrValue(t *testing.T) {
	fixtures := map[string]convergeCase{}
	for _, tc := range convergeCases() {
		fixtures[tc.name] = tc
	}

	for _, hc := range handAuthoredCases() {
		tc, ok := fixtures[hc.workspace]
		if !ok {
			t.Fatalf("no %q fixture for %s(%s).%s", hc.workspace, hc.kind, hc.target, hc.attr)
		}
		name := fmt.Sprintf("%s/%s.%s", hc.workspace, hc.kind, hc.attr)

		// Twice: a run that keeps the value but loses the "# keep" that held it
		// has only moved the deletion one run out.
		t.Run(name+"/keep", func(t *testing.T) {
			root, logged := handAuthoredRun(t, tc, hc, true)
			for run := 2; run <= 3; run++ {
				if !contains(declaredAttrValues(t, root, hc), hc.value) {
					t.Fatalf("%s(%s).%s lost the hand-authored %q on run %d even though it "+
						"carries a \"# keep\", so a declared build input disappeared:\n%s\nthe run "+
						"said:\n%s", hc.kind, hc.target, hc.attr, hc.value, run,
						indent(buildFileText(t, root, hc.pkg)), indentLog(logged))
				}
				logged = captureLog(t, func() { convergeGazelle(t, root) })
			}
		})

		t.Run(name+"/replaced", func(t *testing.T) {
			root, logged := handAuthoredRun(t, tc, hc, false)
			switch {
			case contains(declaredAttrValues(t, root, hc), hc.value):
				t.Errorf("%s(%s).%s kept the hand-authored %q with no \"# keep\". Gazelle owns "+
					"the attribute, so either the generator merges and the docs say so, or it "+
					"replaces and this case is wrong.",
					hc.kind, hc.target, hc.attr, hc.value)
			case !strings.Contains(logged, hc.value):
				t.Errorf("%s(%s).%s dropped the hand-authored %q and said nothing about it. A "+
					"declared build input disappearing with no diagnostic is the defect this "+
					"generator exists to remove.\nthe run said:\n%s",
					hc.kind, hc.target, hc.attr, hc.value, indentLog(logged))
			}
		})
	}
}

func handAuthoredRun(t *testing.T, tc convergeCase, hc handAuthoredCase, keep bool) (string, string) {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{}
	for rel, body := range tc.files {
		files[rel] = body
	}
	for rel, body := range hc.extra {
		files[rel] = body
	}
	for _, rel := range hc.drop {
		delete(files, rel)
	}
	writeWorkspace(t, root, files)
	captureLog(t, func() { convergeGazelle(t, root) })
	handAuthorAttr(t, root, hc, keep)
	logged := captureLog(t, func() { convergeGazelle(t, root) })
	return root, logged
}

// handAuthorAttr edits the generated BUILD file the way a user would: the value
// joins a list or replaces a scalar, marked "# keep" on its own element line
// and, for a scalar, on the line above the attribute.
func handAuthorAttr(t *testing.T, root string, hc handAuthoredCase, keep bool) {
	t.Helper()
	buildPath := filepath.Join(root, filepath.FromSlash(hc.pkg), "BUILD.bazel")
	data, err := os.ReadFile(buildPath)
	if err != nil {
		t.Fatal(err)
	}

	var target *rule.Rule
	for _, r := range loadRules(t, root, hc.pkg) {
		if r.Kind() == hc.kind && r.Name() == hc.target {
			target = r
		}
	}
	if target == nil {
		t.Fatalf("generation wrote no %s(%s) in %s, so there is nothing to hand-edit:\n%s",
			hc.kind, hc.target, buildPath, data)
	}

	const indent = "    "
	var replacement []string
	switch hc.shape {
	case "glob":
		glob, ok := rule.ParseGlobExpr(target.Attr(hc.attr))
		if !ok {
			t.Fatalf("%s(%s).%s is not a glob() call", hc.kind, hc.target, hc.attr)
		}
		replacement = []string{
			indent + hc.attr + " = glob([",
			handListLines(append(glob.Patterns, hc.value), hc.value, keep, indent),
			indent + "]),",
		}
	case "scalar":
		if keep {
			replacement = append(replacement, indent+"# keep")
		}
		replacement = append(replacement, fmt.Sprintf("%s%s = %q,", indent, hc.attr, hc.value))
	default:
		replacement = []string{
			indent + hc.attr + " = [",
			handListLines(append(attrValues(target, hc.attr), hc.value), hc.value, keep, indent),
			indent + "],",
		}
	}

	blocks := strings.Split(string(data), "\n\n")
	edited := false
	for i, block := range blocks {
		if !strings.Contains(block, hc.kind+"(") || !strings.Contains(block, fmt.Sprintf("name = %q", hc.target)) {
			continue
		}
		lines := replaceAttrLines(strings.Split(block, "\n"), hc.attr, replacement)
		if lines == nil {
			t.Fatalf("could not place %s in the %s(%s) block:\n%s", hc.attr, hc.kind, hc.target, block)
		}
		blocks[i] = strings.Join(lines, "\n")
		edited = true
		break
	}
	if !edited {
		t.Fatalf("no %s(%s) block in %s:\n%s", hc.kind, hc.target, buildPath, data)
	}
	if err := os.WriteFile(buildPath, []byte(strings.Join(blocks, "\n\n")), 0o644); err != nil {
		t.Fatal(err)
	}
}

// replaceAttrLines swaps the attribute's whole assignment -- however many lines
// it spans -- for replacement, or inserts it after name when it is absent.
func replaceAttrLines(lines []string, attr string, replacement []string) []string {
	splice := func(at, through int) []string {
		out := append([]string(nil), lines[:at]...)
		out = append(out, replacement...)
		return append(out, lines[through+1:]...)
	}
	for i, line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), attr+" = ") {
			continue
		}
		depth := 0
		for j := i; j < len(lines); j++ {
			// Braces too: path_aliases is a dict, and counting only brackets
			// ends the assignment at its opening line.
			depth += strings.Count(lines[j], "[") + strings.Count(lines[j], "(") +
				strings.Count(lines[j], "{")
			depth -= strings.Count(lines[j], "]") + strings.Count(lines[j], ")") +
				strings.Count(lines[j], "}")
			if depth <= 0 {
				return splice(i, j)
			}
		}
		return nil
	}
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "name = ") {
			return splice(i+1, i)
		}
	}
	return nil
}

func handListLines(values []string, marked string, keep bool, indent string) string {
	var b strings.Builder
	for i, v := range values {
		suffix := ""
		if keep && v == marked {
			suffix = "  # keep"
		}
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "%s    %q,%s", indent, v, suffix)
	}
	return b.String()
}

func declaredAttrValues(t *testing.T, root string, hc handAuthoredCase) []string {
	t.Helper()
	for _, r := range loadRules(t, root, hc.pkg) {
		if r.Kind() != hc.kind || r.Name() != hc.target {
			continue
		}
		if glob, ok := rule.ParseGlobExpr(r.Attr(hc.attr)); ok {
			return glob.Patterns
		}
		return attrValues(r, hc.attr)
	}
	return nil
}

func buildFileText(t *testing.T, root, pkg string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(pkg), "BUILD.bazel"))
	if err != nil {
		return "(no BUILD file)"
	}
	return string(data)
}
