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
		configureTsConfig(c, rel, f)

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
	walk(&config.Config{
		RepoRoot: repoRoot,
		RepoName: "converge_repo_root",
		Exts:     map[string]any{},
	}, "")
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
