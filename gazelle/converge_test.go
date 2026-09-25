package typescript

// Convergence, not idempotence: a create-if-absent generator passes a
// byte-identical rerun and fails this, since the repairing run emits nothing.

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/bazelbuild/bazel-gazelle/config"
	"github.com/bazelbuild/bazel-gazelle/label"
	"github.com/bazelbuild/bazel-gazelle/language"
	"github.com/bazelbuild/bazel-gazelle/merger"
	"github.com/bazelbuild/bazel-gazelle/resolve"
	"github.com/bazelbuild/bazel-gazelle/rule"
)

// ---- one gazelle invocation -------------------------------------------------

// Post-order walk, pre-merge in memory, resolve, then the writes -- as
// cmd/gazelle does, so no directory sees a BUILD file this pass wrote.
func convergeGazelle(t *testing.T, repoRoot string) {
	t.Helper()

	lang := &tsLang{}
	kinds := lang.Kinds()
	ix := resolve.NewRuleIndex(func(*rule.Rule, string) resolve.Resolver { return lang })

	type dirVisit struct {
		rel     string
		c       *config.Config
		file    *rule.File
		gen     []*rule.Rule
		empty   []*rule.Rule
		imports []any
	}
	var visits []dirVisit

	var walk func(parent *config.Config, rel string)
	walk = func(parent *config.Config, rel string) {
		dir := filepath.Join(repoRoot, filepath.FromSlash(rel))
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		var subdirs, regular []string
		for _, e := range entries {
			name := e.Name()
			switch {
			case strings.HasPrefix(name, "."), strings.HasPrefix(name, "bazel-"):
			case e.IsDir():
				subdirs = append(subdirs, name)
			case name == "BUILD.bazel", name == "BUILD":
			default:
				regular = append(regular, name)
			}
		}
		sort.Strings(subdirs)
		sort.Strings(regular)

		buildPath := filepath.Join(dir, "BUILD.bazel")
		var f *rule.File
		if _, err := os.Stat(buildPath); err == nil {
			// nil rather than an empty file when absent: several generators branch
			// on args.File == nil, and so does the plugin that edits it in place.
			loaded, err := rule.LoadFile(buildPath, rel)
			if err != nil {
				t.Fatal(err)
			}
			f = loaded
		}

		c := parent.Clone()
		// Core's resolve config carries # gazelle:resolve, which the edge
		// resolver reads first, as cmd/gazelle registers it.
		(&resolve.Configurer{}).Configure(c, rel, f)
		lang.Configure(c, rel, f)

		for _, sub := range subdirs {
			walk(c, path.Join(rel, sub))
		}

		res := generateRules(language.GenerateArgs{
			Config:       c,
			Dir:          dir,
			Rel:          rel,
			File:         f,
			Subdirs:      subdirs,
			RegularFiles: regular,
		})
		if f == nil {
			f = rule.EmptyFile(buildPath, rel)
			for _, r := range res.Gen {
				r.Insert(f)
			}
		} else {
			merger.MergeFile(f, res.Empty, res.Gen, merger.PreResolve, kinds, nil)
		}
		for _, r := range f.Rules {
			ix.AddRule(c, r, f)
		}
		visits = append(visits, dirVisit{rel, c, f, res.Gen, res.Empty, res.Imports})
	}
	root := &config.Config{
		RepoRoot:            repoRoot,
		RepoName:            "converge_repo_root",
		ValidBuildFileNames: []string{"BUILD.bazel", "BUILD"},
		Exts:                map[string]any{},
	}
	(&resolve.Configurer{}).RegisterFlags(nil, "", root)
	walk(root, "")
	ix.Finish()

	for _, v := range visits {
		for i, r := range v.gen {
			if i >= len(v.imports) {
				break
			}
			lang.Resolve(v.c, ix, nil, r, v.imports[i],
				label.New(v.c.RepoName, v.rel, r.Name()))
		}
		merger.MergeFile(v.file, v.empty, v.gen, merger.PostResolve, kinds, nil)
	}
	// cmd/gazelle calls this after the last Resolve and before the writes, and
	// a check over the whole target graph has nowhere else to run.
	var asLanguage language.Language = lang
	if life, ok := asLanguage.(language.LifecycleManager); ok {
		life.AfterResolvingDeps(context.Background())
	}
	for _, v := range visits {
		merger.FixLoads(v.file, lang.Loads())
		content := v.file.Format()
		if bytes.Equal(v.file.Content, content) {
			continue
		}
		if err := os.WriteFile(v.file.Path, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// ---- snapshots --------------------------------------------------------------

var convergeNameAttr = regexp.MustCompile(`(?m)^\s*name = "([^"]*)"`)

// Rules sorted by kind and name: a merger append and a fresh insert of the
// same rule set have to compare equal, since rule order carries no meaning.
func convergeSnapshot(t *testing.T, repoRoot string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(repoRoot, func(p string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || (entry.Name() != "BUILD.bazel" && entry.Name() != "BUILD") {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(repoRoot, p)
		if err != nil {
			return err
		}
		text, rules := canonicalBuild(string(data))
		// An emptied BUILD file and no BUILD file both declare no targets; the
		// property is about the rules, so they compare the same.
		if rules > 0 {
			out[filepath.ToSlash(rel)] = text
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func canonicalBuild(text string) (string, int) {
	var blocks []string
	rules := 0
	for _, block := range strings.Split(text, "\n\n") {
		block = strings.Trim(block, "\n")
		if strings.TrimSpace(block) == "" {
			continue
		}
		blocks = append(blocks, block)
		if key := blockKey(block); strings.HasPrefix(key, "1:") {
			rules++
		}
	}
	sort.SliceStable(blocks, func(i, j int) bool { return blockKey(blocks[i]) < blockKey(blocks[j]) })
	return strings.Join(blocks, "\n\n") + "\n", rules
}

func blockKey(block string) string {
	head := ""
	for _, line := range strings.Split(block, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, `"""`) {
			continue
		}
		head = trimmed
		break
	}
	if strings.HasPrefix(head, "load(") {
		return "0:" + block
	}
	kind, _, isCall := strings.Cut(head, "(")
	if m := convergeNameAttr.FindStringSubmatch(block); isCall && m != nil {
		return "1:" + kind + ":" + m[1]
	}
	return "2:" + block
}

// ---- the diff is the failure message ---------------------------------------

func snapshotDiff(want, got map[string]string) string {
	paths := map[string]bool{}
	for p := range want {
		paths[p] = true
	}
	for p := range got {
		paths[p] = true
	}
	ordered := make([]string, 0, len(paths))
	for p := range paths {
		ordered = append(ordered, p)
	}
	sort.Strings(ordered)

	var b strings.Builder
	for _, p := range ordered {
		if want[p] == got[p] {
			continue
		}
		switch {
		case want[p] == "":
			fmt.Fprintf(&b, "\n--- %s (only the two-run tree has it)\n%s", p, indent(got[p]))
		case got[p] == "":
			fmt.Fprintf(&b, "\n--- %s (missing from the two-run tree)\n%s", p, indent(want[p]))
		default:
			fmt.Fprintf(&b, "\n--- %s\n%s", p, lineDiff(want[p], got[p]))
		}
	}
	return b.String()
}

// List elements in order: a label appended to an existing attribute has to
// compare equal to the same label generated in place.
func sortedLists(snapshot map[string]string) map[string]string {
	out := make(map[string]string, len(snapshot))
	for p, text := range snapshot {
		lines := strings.Split(text, "\n")
		sorted := make([]string, 0, len(lines))
		for i := 0; i < len(lines); {
			if !isListElement(lines[i]) {
				sorted = append(sorted, lines[i])
				i++
				continue
			}
			j := i
			for j < len(lines) && isListElement(lines[j]) {
				j++
			}
			run := append([]string(nil), lines[i:j]...)
			sort.Strings(run)
			sorted = append(sorted, run...)
			i = j
		}
		out[p] = strings.Join(sorted, "\n")
	}
	return out
}

func isListElement(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, `"`) && strings.HasSuffix(trimmed, `",`)
}

func indent(text string) string {
	var b strings.Builder
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		b.WriteString("      " + line + "\n")
	}
	return b.String()
}

// lineDiff prints the lines that differ with three lines of context, marking
// the from-scratch side "-" and the two-run side "+".
func lineDiff(want, got string) string {
	a := strings.Split(strings.TrimRight(want, "\n"), "\n")
	b := strings.Split(strings.TrimRight(got, "\n"), "\n")

	lcs := make([][]int, len(a)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}

	type edit struct {
		sign rune
		text string
	}
	var edits []edit
	for i, j := 0, 0; i < len(a) || j < len(b); {
		switch {
		case i < len(a) && j < len(b) && a[i] == b[j]:
			edits = append(edits, edit{' ', a[i]})
			i++
			j++
		case j < len(b) && (i == len(a) || lcs[i][j+1] >= lcs[i+1][j]):
			edits = append(edits, edit{'+', b[j]})
			j++
		default:
			edits = append(edits, edit{'-', a[i]})
			i++
		}
	}

	keep := make([]bool, len(edits))
	for i, e := range edits {
		if e.sign == ' ' {
			continue
		}
		for j := max(0, i-3); j < min(len(edits), i+4); j++ {
			keep[j] = true
		}
	}
	var b2 strings.Builder
	gap := false
	for i, e := range edits {
		if !keep[i] {
			gap = true
			continue
		}
		if gap {
			b2.WriteString("      @@\n")
			gap = false
		}
		fmt.Fprintf(&b2, "    %c %s\n", e.sign, e.text)
	}
	return b2.String()
}

// ---- dangling labels -------------------------------------------------------

// Every in-workspace label no rule declares and no file satisfies: a
// workspace-wide analysis failure no further Gazelle run clears.
func danglingLabels(t *testing.T, repoRoot string) []string {
	t.Helper()
	var out []string
	for _, dir := range convergePackages(t, repoRoot) {
		for _, r := range loadRules(t, repoRoot, dir) {
			for _, attr := range r.AttrKeys() {
				for _, v := range attrValues(r, attr) {
					if !isWorkspaceLabel(v) {
						continue
					}
					abs := absLabel(dir, v)
					pkg, name := splitLabel(abs)
					if pkg == "visibility" || pkg == "conditions" {
						continue
					}
					if labelResolves(t, repoRoot, pkg, name) {
						continue
					}
					out = append(out, fmt.Sprintf("%s named by %s(%s) in %s",
						abs, r.Kind(), r.Name(), path.Join(dir, "BUILD.bazel")))
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// A label resolves to a rule in that package, or to a source file that package
// holds. Not to any file that happens to sit at the path: a directory with no
// BUILD file is not a package at all, so Bazel cannot load `//dir:file` there
// however well the file stats -- which is exactly the dangling label an os.Stat
// on its own says yes to, and the reason this walk once passed a workspace that
// failed at analysis.
func labelResolves(t *testing.T, repoRoot, pkg, name string) bool {
	for _, r := range loadRules(t, repoRoot, pkg) {
		if r.Name() == name {
			return true
		}
		// The store macro declares every target under its name: the trees,
		// their links and the hoist, none a rule this file holds.
		if r.Kind() == "npm_virtual_store" && strings.HasPrefix(name, r.Name()+"/") {
			return true
		}
	}
	full := path.Join(pkg, name)
	info, err := os.Stat(filepath.Join(repoRoot, filepath.FromSlash(full)))
	if err != nil || info.IsDir() {
		return false
	}
	// A source file belongs to the innermost package above it, and no other
	// package can name it as one.
	holder, inPackage := enclosingPackage(repoRoot, full)
	return inPackage && holder == pkg
}

// enclosingPackage is the innermost directory at or above the file's own that
// holds a BUILD file -- the package Bazel reads a source label for it out of --
// and whether any directory up to the root is one.
func enclosingPackage(repoRoot, filePath string) (string, bool) {
	dir := path.Dir(filePath)
	if dir == "." {
		dir = ""
	}
	for {
		for _, name := range []string{"BUILD.bazel", "BUILD"} {
			if _, err := os.Stat(filepath.Join(repoRoot, filepath.FromSlash(dir), name)); err == nil {
				return dir, true
			}
		}
		if dir == "" {
			return "", false
		}
		if parent := path.Dir(dir); parent == "." || parent == dir {
			dir = ""
		} else {
			dir = parent
		}
	}
}

// Every srcs entry naming a file under a directory that is a package of its
// own: Bazel reads a source label out of the innermost package above the file.
func crossesPackageBoundary(t *testing.T, repoRoot string) []string {
	t.Helper()
	var out []string
	for _, dir := range convergePackages(t, repoRoot) {
		for _, r := range loadRules(t, repoRoot, dir) {
			for _, src := range r.AttrStrings("srcs") {
				if strings.HasPrefix(src, "//") || strings.HasPrefix(src, "@") {
					continue
				}
				full := path.Join(dir, strings.TrimPrefix(src, ":"))
				holder, inPackage := enclosingPackage(repoRoot, full)
				if inPackage && holder != dir {
					out = append(out, fmt.Sprintf(
						"%s named by %s(%s) in %s sits in package %s", full,
						r.Kind(), r.Name(), path.Join(dir, "BUILD.bazel"), holder))
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// ---- small helpers ---------------------------------------------------------

func loadRules(t *testing.T, repoRoot, pkg string) []*rule.Rule {
	t.Helper()
	buildPath := filepath.Join(repoRoot, filepath.FromSlash(pkg), "BUILD.bazel")
	if _, err := os.Stat(buildPath); err != nil {
		return nil
	}
	f, err := rule.LoadFile(buildPath, pkg)
	if err != nil {
		t.Fatal(err)
	}
	return f.Rules
}

// convergePackages lists every directory holding a BUILD file, root first.
func convergePackages(t *testing.T, repoRoot string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(repoRoot, func(p string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || entry.Name() != "BUILD.bazel" {
			return err
		}
		rel, err := filepath.Rel(repoRoot, filepath.Dir(p))
		if err != nil {
			return err
		}
		if rel == "." {
			rel = ""
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}

func attrValues(r *rule.Rule, attr string) []string {
	if v := r.AttrString(attr); v != "" {
		return []string{v}
	}
	return r.AttrStrings(attr)
}

func isWorkspaceLabel(v string) bool {
	return strings.HasPrefix(v, "//") || strings.HasPrefix(v, ":")
}

func absLabel(pkg, lbl string) string {
	if after, ok := strings.CutPrefix(lbl, ":"); ok {
		return "//" + pkg + ":" + after
	}
	return lbl
}

func splitLabel(lbl string) (pkg, name string) {
	body := strings.TrimPrefix(lbl, "//")
	if pkg, name, ok := strings.Cut(body, ":"); ok {
		return pkg, name
	}
	return body, path.Base(body)
}

func ruleNamed(rules []*rule.Rule, kind, name string) *rule.Rule {
	for _, r := range rules {
		if r.Kind() == kind && r.Name() == name {
			return r
		}
	}
	return nil
}

func TestForeignJSONImportRemovalPreservesOnlyExplicitKeep(t *testing.T) {
	requireTsgo(t)
	for _, keep := range []bool{false, true} {
		t.Run(fmt.Sprintf("keep=%t", keep), func(t *testing.T) {
			const exports = "exports_files([\"value.json\"], visibility = [\"//app:__pkg__\"])\n"
			const source = "import value from '../fixtures/value.json'; export const answer: number = value.answer;\n"
			root := writeTree(t, map[string]string{
				"app/tsconfig.json":    `{"compilerOptions":{"module":"preserve","moduleResolution":"bundler","resolveJsonModule":true},"files":["index.ts"]}`,
				"app/index.ts":         source,
				"fixtures/value.json":  `{ "answer": 42 }`,
				"fixtures/BUILD.bazel": exports,
			})
			convergeGazelle(t, root)
			buildPath := filepath.Join(root, "app/BUILD.bazel")
			initial, err := os.ReadFile(buildPath)
			if err != nil {
				t.Fatal(err)
			}
			const foreign = "//fixtures:value.json"
			if !strings.Contains(string(initial), foreign) {
				t.Fatalf("missing foreign JSON source: %s", initial)
			}
			retainedLog := captureLog(t, func() { convergeGazelle(t, root) })
			if strings.Contains(retainedLog, `"`+foreign+`" is no longer declared`) {
				t.Fatalf("retained compiler input reported dropped: %s", retainedLog)
			}
			if keep {
				writeFile(t, buildPath, strings.Replace(string(initial), `"`+foreign+`",`, `"`+foreign+`", # keep`, 1))
			}
			writeFile(t, filepath.Join(root, "app/index.ts"), "export const answer: number = 42;\n")
			convergeGazelle(t, root)
			var found bool
			for _, r := range loadRules(t, root, "app") {
				if r.Kind() == "ts_compile" {
					found = slices.Contains(r.AttrStrings("srcs"), foreign)
				}
			}
			if found != keep {
				t.Fatalf("foreign JSON retained=%t, keep=%t", found, keep)
			}
			writeFile(t, filepath.Join(root, "app/index.ts"), source)
			convergeGazelle(t, root)
			var restored bool
			for _, r := range loadRules(t, root, "app") {
				if r.Kind() == "ts_compile" {
					restored = slices.Contains(r.AttrStrings("srcs"), foreign)
				}
			}
			if !restored {
				t.Fatal("restored import did not restore JSON source")
			}
			actual, err := os.ReadFile(filepath.Join(root, "fixtures/BUILD.bazel"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(actual), `visibility = ["//app:__pkg__"]`) {
				t.Fatalf("fixture visibility changed: %s", actual)
			}
		})
	}
}

func TestUnownedSourceClosureConvergesAfterImportRemoval(t *testing.T) {
	requireTsgo(t)
	const source = "import { answer } from '../fixtures/a'; export { answer };\n"
	const exports = `exports_files(
    [
        "a.ts",
        "nested/b.ts",
    ],
    visibility = ["//app:__pkg__"],
)
`
	root := writeTree(t, map[string]string{
		"app/tsconfig.json":     `{"compilerOptions":{"module":"preserve","moduleResolution":"bundler"},"files":["index.ts"]}`,
		"app/index.ts":          source,
		"fixtures/BUILD.bazel":  exports,
		"fixtures/package.json": `{"name":"foreign-project"}`,
		"fixtures/a.ts":         "export { answer } from './nested/b';\n",
		"fixtures/nested/b.ts":  "export const answer = 42;\n",
		"fixtures/unused.ts":    "import 'does-not-exist';\n",
	})
	convergeGazelle(t, root)
	initial := buildFileText(t, root, "app")
	want := []string{"//fixtures:a.ts", "//fixtures:nested/b.ts", "index.ts"}
	assertSources := func(expected []string) {
		t.Helper()
		for _, r := range loadRules(t, root, "app") {
			if r.Kind() == "ts_compile" {
				got := r.AttrStrings("srcs")
				slices.Sort(got)
				wantStrings(t, "srcs", got, expected)
				return
			}
		}
		t.Fatal("missing compile rule")
	}
	assertSources(want)
	logged := captureLog(t, func() { convergeGazelle(t, root) })
	if strings.Contains(logged, "is no longer declared") || buildFileText(t, root, "app") != initial {
		t.Fatalf("retained source closure did not converge: %s", logged)
	}
	buildPath := filepath.Join(root, "app/BUILD.bazel")
	writeFile(t, buildPath, strings.ReplaceAll(initial, "//fixtures:", "@converge_repo_root//fixtures:"))
	logged = captureLog(t, func() { convergeGazelle(t, root) })
	if strings.Contains(logged, "is no longer declared") {
		t.Fatalf("equivalent source labels reported dropped: %s", logged)
	}
	writeFile(t, filepath.Join(root, "app/index.ts"), "export const answer = 42;\n")
	logged = captureLog(t, func() { convergeGazelle(t, root) })
	assertSources([]string{"index.ts"})
	if !strings.Contains(logged, "is no longer declared") {
		t.Fatalf("removed source imports were not reported: %s", logged)
	}
	writeFile(t, filepath.Join(root, "app/index.ts"), source)
	convergeGazelle(t, root)
	assertSources(want)
	actual, err := os.ReadFile(filepath.Join(root, "fixtures/BUILD.bazel"))
	if err != nil || string(actual) != exports {
		t.Fatalf("foreign source owner changed: %s, %v", actual, err)
	}
	if _, err := os.Stat(filepath.Join(root, "fixtures/nested/BUILD.bazel")); !os.IsNotExist(err) {
		t.Fatalf("foreign source directory became a Bazel package: %v", err)
	}
}

func TestGeneratedTreeImportRetainsDirectOwnerWithoutOutputFiles(t *testing.T) {
	requireTsgo(t)
	cases := []struct {
		name, paths, specifier string
		wantGenerator          bool
	}{
		{"inherited alias", `{"#shared/*":["./shared/*"]}`, "#shared/generated/runtime", true},
		{"generated first fallback", `{"#shared/value":["./shared/value"],"#module":["./shared/generated/runtime","./shared/fallback"]}`, "#module", true},
		{"authored first fallback", `{"#shared/value":["./shared/value"],"#module":["./shared/fallback","./shared/generated/runtime"]}`, "#module", false},
		{"exact alias overrides wildcard", `{"#shared/*":["./shared/*"],"#shared/generated/runtime":["./shared/fallback"]}`, "#shared/generated/runtime", false},
		{"tree root candidate", `{"#shared/*":["./shared/*"],"#module":["./shared/generated"]}`, "#module", true},
		{"unowned missing import", `{"#shared/*":["./shared/*"]}`, "#shared/missing/runtime", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeWorkspace(t, root, map[string]string{
				"package.json":              `{"name":"fixture"}`,
				"app/tsconfig.json":         `{"compilerOptions":{"module":"preserve","moduleResolution":"bundler","paths":` + tc.paths + `},"include":["shared/**/*.ts"]}`,
				"app/shared/value.ts":       "export const value = 1;\n",
				"app/shared/fallback.ts":    "export const generated = 'authored';\n",
				"app/plugins/tsconfig.json": `{"extends":"../tsconfig.json","include":["*.ts"]}`,
				"app/plugins/consumer.ts":   "import { value } from '#shared/value';\nimport { generated } from '" + tc.specifier + "';\nexport const result = [value, generated];\n",
				"app/BUILD.bazel": `load("@rules_typescript//ts:defs.bzl", "ts_codegen")
ts_codegen(
    name = "generated",
    generator = "//:generator",
    out_dir = "shared/generated",
    visibility = ["//visibility:public"],
)
`,
			})
			for _, state := range []string{"cold", "materialized", "deleted"} {
				switch state {
				case "materialized":
					for _, name := range []string{"runtime", "index"} {
						writeFile(t, filepath.Join(root, "app/shared/generated/"+name+".d.ts"), "export declare const generated: string;\n")
					}
				case "deleted":
					if err := os.RemoveAll(filepath.Join(root, "app/shared/generated")); err != nil {
						t.Fatal(err)
					}
				}
				for pass := 1; pass <= 2; pass++ {
					captureLog(t, func() { convergeGazelle(t, root) })
					found := false
					for _, r := range loadRules(t, root, "app/plugins") {
						if r.Kind() != "ts_compile" {
							continue
						}
						found = true
						deps := r.AttrStrings("deps")
						if !slices.Contains(deps, "//app") || slices.Contains(deps, "//app:generated") != tc.wantGenerator {
							t.Errorf("%s pass %d: deps = %v, want //app and generator=%t", state, pass, deps, tc.wantGenerator)
						}
					}
					if !found {
						t.Fatal("nested consumer has no ts_compile owner")
					}
				}
			}
		})
	}
}
