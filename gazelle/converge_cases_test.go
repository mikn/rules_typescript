package typescript

// The fixtures and mutations convergeGazelle is driven with: one workspace per
// framework Gazelle generates for, plus a plain TypeScript one.

import (
	"bytes"
	"fmt"
	"log"
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
}

type convergeCase struct {
	name      string
	files     map[string]string
	mutations []convergeMutation
}

// ---- fixtures ---------------------------------------------------------------

func writeWorkspace(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// captureLog collects what the generator writes to the standard logger, which
// is where Gazelle's own diagnostics go.
func captureLog(t *testing.T, body func()) string {
	t.Helper()
	var buf bytes.Buffer
	flags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(os.Stderr)
		log.SetFlags(flags)
	})
	body()
	return buf.String()
}

func contains(haystack []string, needle string) bool {
	for _, entry := range haystack {
		if entry == needle {
			return true
		}
	}
	return false
}

const (
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
				"src/lib/tokens.json":    `{"a":1}` + "\n",
				"src/icons/logo.svg":     "<svg/>\n",
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
				// A data-file rule is read back as a claim on its file, so the
				// run after the deletion has nothing to regenerate over it.
				{kind: "delete_json_beside_sources", remove: []string{"src/lib/tokens.json"}},
				{kind: "delete_only_asset_in_dir", remove: []string{"src/icons/logo.svg"}},
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
				// One test under the alias directory, which validates the alias on its
				// own srcs, and one outside it, which needs path_alias_srcs.
				"src/lib/helper.test.ts": "import type { helper } from \"@lib/helper\";\nexport const t = typeof helper;\n",
				"e2e/smoke.test.ts":      "import { button } from \"@ui/button\";\nexport const t = button;\n",
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
	}
}

// ---- the property ----------------------------------------------------------

// For every fixture workspace: generate, mutate, generate again, and require
// what one generation over the mutated tree produces.
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

	twoRun := t.TempDir()
	writeWorkspace(t, twoRun, tc.files)
	captureLog(t, func() { convergeGazelle(t, twoRun) })
	applyMutation(t, twoRun, mut)
	logged := captureLog(t, func() { convergeGazelle(t, twoRun) })
	got := convergeSnapshot(t, twoRun)

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
	shape  string // "list" or "scalar"
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
	return []handAuthoredCase{
		{
			workspace: "plain", pkg: "src", kind: "ts_compile", target: "src",
			attr: "srcs", shape: "list", value: "legacy.js",
			extra: map[string]string{"src/legacy.js": "export const legacy = 1;\n"},
		},
		{
			workspace: "plain", pkg: "src", kind: "ts_compile", target: "src",
			attr: "visibility", shape: "list", value: "//vendor:__pkg__",
			extra: map[string]string{
				"vendor/BUILD.bazel": handVendorPackage,
				"vendor/legacy.js":   "export const legacy = 1;\n",
			},
		},
		{
			workspace: "plain", pkg: "src", kind: "ts_compile", target: "src",
			attr: "tsconfig", shape: "scalar", value: "//:tsconfig_build",
		},
		{
			workspace: "plain", pkg: "", kind: "filegroup", target: "tsconfig_types",
			attr: "srcs", shape: "list", value: "//vendor:vendor_hand",
			extra: map[string]string{
				"tsconfig.json":      `{"compilerOptions":{"strict":true,"types":["./globals.d.ts"]}}` + "\n",
				"globals.d.ts":       "declare const GLOBAL_FLAG: boolean;\n",
				"vendor/BUILD.bazel": handVendorPackage,
				"vendor/legacy.js":   "export const legacy = 1;\n",
			},
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
