package typescript

// The other half of TestHandAuthoredAttrValue: a value on a managed attribute
// that is not a plain string or a plain list of them, which is the only shape
// rule.MergeRules reconciles rather than rewrites. Gazelle-for-Go replaces the
// rest, so this extension does too -- what it asserts is that the replacement
// is announced and that "# keep" holds the expression.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/bazelbuild/bazel-gazelle/rule"
	bzl "github.com/bazelbuild/buildtools/build"
)

// nonLiteralCase is one managed attribute, in the workspace whose generator
// writes it. class picks the expression shapes that can stand in for its value.
type nonLiteralCase struct {
	workspace string
	extra     map[string]string
	pkg       string
	kind      string
	target    string
	attr      string
	class     string // "list", "scalar", "glob" or "dict"
}

// managedAttrCases names every attribute in keep.go's managedAttrs, in a
// workspace where generation writes that rule -- so a managed attribute that
// stops being covered here is one this test stops asking about.
func managedAttrCases() []nonLiteralCase {
	return []nonLiteralCase{
		{workspace: "next", kind: "next_build", target: "app", attr: "srcs", class: "glob"},
		{workspace: "next", kind: "next_build", target: "app", attr: "staging_srcs", class: "list"},
		{workspace: "next", kind: "next_build", target: "app", attr: "config", class: "scalar"},
		{workspace: "next", kind: "next_build", target: "app", attr: "tsconfig", class: "scalar"},
		{workspace: "next", kind: "next_build", target: "app", attr: "node_modules", class: "scalar"},
		{workspace: "next", kind: "next_dev_server", target: "dev", attr: "node_modules", class: "scalar"},
		{workspace: "next", kind: "node_modules", target: "node_modules", attr: "deps", class: "list"},

		{workspace: "sveltekit", kind: "sveltekit_build", target: "app", attr: "srcs", class: "glob"},
		{workspace: "sveltekit", kind: "sveltekit_build", target: "app", attr: "staging_srcs", class: "list"},
		{workspace: "sveltekit", kind: "sveltekit_build", target: "app", attr: "config", class: "scalar"},
		{workspace: "sveltekit", kind: "sveltekit_build", target: "app", attr: "svelte_config", class: "scalar"},
		{workspace: "sveltekit", kind: "sveltekit_build", target: "app", attr: "node_modules", class: "scalar"},

		{workspace: "remix", kind: "ts_bundle", target: "app_remix", attr: "staging_srcs", class: "list"},
		{workspace: "remix", kind: "ts_bundle", target: "app_remix", attr: "entry_point", class: "scalar"},
		{workspace: "remix", kind: "ts_bundle", target: "app_remix", attr: "html", class: "scalar"},
		{workspace: "remix", kind: "ts_bundle", target: "app_remix", attr: "vite_config", class: "scalar"},
		{workspace: "remix", kind: "ts_bundle", target: "app_remix", attr: "mode", class: "scalar"},
		{workspace: "remix", kind: "ts_bundle", target: "app_remix", attr: "bundler", class: "scalar"},
		{workspace: "remix", kind: "vite_bundler", target: "vite", attr: "vite", class: "scalar"},
		{workspace: "remix", kind: "vite_bundler", target: "vite", attr: "node_modules", class: "scalar"},

		{workspace: "path_aliases", pkg: "src", kind: "ts_compile", target: "src",
			attr: "path_aliases", class: "dict"},
		{workspace: "path_aliases", pkg: "src", kind: "ts_compile", target: "src",
			attr: "path_alias_srcs", class: "list",
			extra: map[string]string{"src/main.ts": "import { button } from \"@ui/button\";\nexport const main = button;\n"}},
		{workspace: "path_aliases", pkg: "e2e", kind: "ts_test", target: "e2e_test",
			attr: "path_aliases", class: "dict"},
		{workspace: "path_aliases", pkg: "e2e", kind: "ts_test", target: "e2e_test",
			attr: "path_alias_srcs", class: "list"},

		{workspace: "plain", pkg: "src", kind: "ts_compile", target: "src", attr: "srcs", class: "list"},
		{workspace: "plain", pkg: "src", kind: "ts_compile", target: "src", attr: "deps", class: "list"},
		{workspace: "plain", pkg: "src", kind: "ts_compile", target: "src", attr: "visibility", class: "list"},
		{workspace: "plain", pkg: "src", kind: "ts_compile", target: "src", attr: "tsconfig", class: "scalar"},
		{workspace: "plain", pkg: "src/lib", kind: "ts_test", target: "lib_test", attr: "srcs", class: "list"},
		{workspace: "plain", pkg: "src/lib", kind: "ts_test", target: "lib_test", attr: "deps", class: "list"},
		{workspace: "plain", pkg: "src/lib", kind: "ts_test", target: "lib_test", attr: "tsconfig", class: "scalar"},
		{workspace: "plain", kind: "ts_config", target: "tsconfig", attr: "src", class: "scalar"},
		{workspace: "plain", kind: "ts_config", target: "tsconfig", attr: "visibility", class: "list"},

		{workspace: "pnpm_member", pkg: "packages/core/src", kind: "ts_test", target: "src_test", attr: "deps", class: "list"},

		{workspace: "tanstack", kind: "ts_bundle", target: "app", attr: "staging_srcs", class: "list"},
		{workspace: "tanstack", pkg: "src/routes", kind: "filegroup", target: "sources", attr: "srcs", class: "list"},
		{workspace: "tanstack", pkg: "src/routes", kind: "filegroup", target: "sources", attr: "visibility", class: "list"},
	}
}

// The expression shapes that can stand in for a value of each class.
var nonLiteralShapes = map[string][]string{
	"list":   {"ident", "concat", "select", "mixed"},
	"scalar": {"ident"},
	"glob":   {"glob_ident", "glob_mixed"},
	"dict":   {"ident", "dict_mixed"},
}

// TestNonLiteralAttrValue: whichever way rule.MergeRules goes on a shape it
// cannot reconcile -- an Ident it replaces with the contents lost, a
// "list + list" it refuses and leaves alone -- the run says the attribute is no
// longer Gazelle's to maintain, and "# keep" holds it. Which of the two it
// picks is Gazelle's business, the same as it is for Go; doing either in
// silence is the defect, because the user is left believing a value they wrote
// is still declared.
func TestNonLiteralAttrValue(t *testing.T) {
	fixtures := map[string]convergeCase{}
	for _, tc := range convergeCases() {
		fixtures[tc.name] = tc
	}

	for _, nc := range managedAttrCases() {
		tc, ok := fixtures[nc.workspace]
		if !ok {
			t.Fatalf("no %q fixture for %s(%s).%s", nc.workspace, nc.kind, nc.target, nc.attr)
		}
		for _, shape := range nonLiteralShapes[nc.class] {
			name := fmt.Sprintf("%s/%s.%s/%s", nc.workspace, nc.kind, nc.attr, shape)
			t.Run(name, func(t *testing.T) { runNonLiteralCase(t, tc, nc, shape) })
		}
	}
}

func runNonLiteralCase(t *testing.T, tc convergeCase, nc nonLiteralCase, shape string) {
	t.Helper()

	root := t.TempDir()
	files := map[string]string{}
	for rel, body := range tc.files {
		files[rel] = body
	}
	for rel, body := range nc.extra {
		files[rel] = body
	}
	writeWorkspace(t, root, files)
	captureLog(t, func() { convergeGazelle(t, root) })

	authored := writeNonLiteralAttr(t, root, nc, shape, false)
	buildPath := filepath.Join(root, filepath.FromSlash(nc.pkg), "BUILD.bazel")

	if nc.class == "glob" {
		// srcs is not mergeable on these kinds: setGeneratedGlob writes it, so
		// the expression is this extension's to leave alone rather than the
		// merger's to replace.
		assertGlobLeftAlone(t, root, nc, shape, authored, buildPath)
		return
	}

	logged := captureLog(t, func() { convergeGazelle(t, root) })
	text := buildFileText(t, root, nc.pkg)

	if declaredAttrExpr(t, root, nc) == nil {
		t.Fatalf("%s(%s).%s is gone after the merge: the attribute was deleted rather than "+
			"replaced or left alone, so nothing declares the inputs it named.\n%s",
			nc.kind, nc.target, nc.attr, indent(text))
	}
	if !rewriteReported(logged, buildPath, nc) {
		t.Fatalf("%s(%s).%s held a %s expression the merger cannot reconcile and the run said "+
			"nothing -- it has to name the file, the rule, the attribute and \"# keep\". "+
			"Whether Gazelle replaced the value or stopped maintaining it, the user is left "+
			"believing it is still recomputed.\n%s\nthe run said:\n%s",
			nc.kind, nc.target, nc.attr, shape, indent(text), indentLog(logged))
	}
	if missing := missingFrom(declaredStrings(t, root, nc.pkg), authored.values); len(missing) > 0 &&
		exprShape(declaredAttrExpr(t, root, nc)) == shape {
		t.Fatalf("%s(%s).%s kept its %s shape but lost %v: a value vanished out of an expression "+
			"the merge reported it would leave alone.\n%s\nthe run said:\n%s",
			nc.kind, nc.target, nc.attr, shape, missing, indent(text), indentLog(logged))
	}

	// The report has to track what is actually in the file: repeated on a shape
	// Gazelle left alone, silent once the value is Gazelle's own. A warning that
	// outlives its cause is one the user learns to skip.
	logged = captureLog(t, func() { convergeGazelle(t, root) })
	stillUnmergeable := !isLiteralAttrValue(declaredAttrExpr(t, root, nc))
	if got := rewriteReported(logged, buildPath, nc); got != stillUnmergeable {
		t.Fatalf("%s(%s).%s is %s after the merge and the next run reported it: %v. The "+
			"diagnostic and the file disagree.\n%s\nthe run said:\n%s",
			nc.kind, nc.target, nc.attr, exprShape(declaredAttrExpr(t, root, nc)), got,
			indent(buildFileText(t, root, nc.pkg)), indentLog(logged))
	}

	// "# keep" is what the diagnostic tells the user to reach for, so it has to
	// hold every shape, whichever way the merger would have gone.
	assertKeepHoldsExpr(t, tc, nc, shape)
}

// assertGlobLeftAlone: a glob() of anything but plain strings is one
// rule.ParseGlobExpr reads only part of, so rewriting from what it read would
// drop the rest. setGeneratedGlob leaves it and says what it now has to cover.
func assertGlobLeftAlone(t *testing.T, root string, nc nonLiteralCase, shape string, authored authoredExpr, buildPath string) {
	t.Helper()
	for run := 2; run <= 3; run++ {
		logged := captureLog(t, func() { convergeGazelle(t, root) })
		text := buildFileText(t, root, nc.pkg)
		if got := exprShape(declaredAttrExpr(t, root, nc)); got != shape {
			t.Fatalf("%s(%s).%s was authored as %s and is %s after run %d. ParseGlobExpr skips "+
				"an argument it cannot read, so a rewrite from what it did read drops "+
				"everything it did not.\n%s\nthe run said:\n%s",
				nc.kind, nc.target, nc.attr, shape, got, run, indent(text), indentLog(logged))
		}
		if missing := missingFrom(declaredStrings(t, root, nc.pkg), authored.values); len(missing) > 0 {
			t.Fatalf("%s(%s).%s lost %v on run %d: a staged source disappeared from a glob "+
				"Gazelle does not rewrite.\n%s\nthe run said:\n%s",
				nc.kind, nc.target, nc.attr, missing, run, indent(text), indentLog(logged))
		}
		if !unmergeableReported(logged, buildPath) {
			t.Fatalf("%s(%s).%s holds a %s expression Gazelle stopped maintaining and run %d "+
				"did not say so. The user is left believing an attribute Gazelle has given up "+
				"on is still recomputed.\n%s\nthe run said:\n%s",
				nc.kind, nc.target, nc.attr, shape, run, indent(text), indentLog(logged))
		}
	}
}

// assertKeepHoldsExpr: the same shape, marked, across two runs and in silence.
func assertKeepHoldsExpr(t *testing.T, tc convergeCase, nc nonLiteralCase, shape string) {
	t.Helper()

	root := t.TempDir()
	files := map[string]string{}
	for rel, body := range tc.files {
		files[rel] = body
	}
	for rel, body := range nc.extra {
		files[rel] = body
	}
	writeWorkspace(t, root, files)
	captureLog(t, func() { convergeGazelle(t, root) })

	authored := writeNonLiteralAttr(t, root, nc, shape, true)
	buildPath := filepath.Join(root, filepath.FromSlash(nc.pkg), "BUILD.bazel")

	for run := 2; run <= 3; run++ {
		logged := captureLog(t, func() { convergeGazelle(t, root) })
		text := buildFileText(t, root, nc.pkg)
		if got := exprShape(declaredAttrExpr(t, root, nc)); got != shape {
			t.Fatalf("%s(%s).%s carries \"# keep\" and was authored as %s, but is %s after run "+
				"%d. \"# keep\" above the attribute is the one thing the rewrite diagnostic "+
				"tells the user to do, so it has to work on every shape.\n%s\nthe run "+
				"said:\n%s", nc.kind, nc.target, nc.attr, shape, got, run, indent(text),
				indentLog(logged))
		}
		if missing := missingFrom(declaredStrings(t, root, nc.pkg), authored.values); len(missing) > 0 {
			t.Fatalf("%s(%s).%s carries \"# keep\" and lost %v on run %d.\n%s\nthe run "+
				"said:\n%s", nc.kind, nc.target, nc.attr, missing, run, indent(text),
				indentLog(logged))
		}
		if rewriteReported(logged, buildPath, nc) {
			t.Fatalf("%s(%s).%s carries \"# keep\", so Gazelle is not maintaining it and has "+
				"nothing to announce, yet run %d reported a rewrite. Advice that keeps warning "+
				"after it is followed reads as advice that did not work.\nthe run said:\n%s",
				nc.kind, nc.target, nc.attr, run, indentLog(logged))
		}
	}
}

// The rewrite line: the file, the rule, the attribute and the way out. Every
// part is load-bearing -- a diagnostic missing any of them cannot be acted on.
func rewriteReported(logged, buildPath string, nc nonLiteralCase) bool {
	for _, line := range strings.Split(logged, "\n") {
		if !strings.Contains(line, buildPath) {
			continue
		}
		if !strings.Contains(line, fmt.Sprintf("%s(%s)", nc.kind, nc.target)) {
			continue
		}
		if strings.Contains(line, nc.attr) && strings.Contains(line, `"# keep"`) {
			return true
		}
	}
	return false
}

// setGeneratedGlob's own line for a glob() it will not rewrite, in the file and
// span rule.MergeRules would name it by.
var unmergeableLine = regexp.MustCompile(`(?m)^(typescript: )?(\S+):\d+\.\d+-\d+\.\d+: could not merge expression`)

func unmergeableReported(logged, buildPath string) bool {
	for _, m := range unmergeableLine.FindAllStringSubmatch(logged, -1) {
		if m[2] == buildPath {
			return true
		}
	}
	return false
}

// ---- authoring the expression ----------------------------------------------

type authoredExpr struct {
	values []string
}

// writeNonLiteralAttr rewrites the generated attribute as the named shape,
// carrying the values generation derived plus one only the user knows about.
func writeNonLiteralAttr(t *testing.T, root string, nc nonLiteralCase, shape string, keep bool) authoredExpr {
	t.Helper()

	buildPath := filepath.Join(root, filepath.FromSlash(nc.pkg), "BUILD.bazel")
	var target *rule.Rule
	for _, r := range loadRules(t, root, nc.pkg) {
		if r.Kind() == nc.kind && r.Name() == nc.target {
			target = r
		}
	}
	if target == nil {
		t.Fatalf("generation wrote no %s(%s) in %s, so there is nothing to hand-edit:\n%s",
			nc.kind, nc.target, buildPath, buildFileText(t, root, nc.pkg))
	}

	hand := handValueFor(nc)
	generated, excludes := generatedValues(t, target, nc)
	values := append(append([]string(nil), generated...), hand)

	const indent = "    "
	quoted := func(vs []string) string { return strings.Join(quotedEach(vs), ", ") }
	entries := renderDictEntries(target.Attr(nc.attr))

	var prelude, replacement string
	switch shape {
	case "ident":
		if nc.class == "dict" {
			prelude = fmt.Sprintf("_HAND = {%s%q: %q}", entries, handAliasKey, hand)
			replacement = indent + nc.attr + " = _HAND,"
			break
		}
		if nc.class == "scalar" {
			prelude = fmt.Sprintf("_HAND = %q", hand)
			values = []string{hand}
		} else {
			prelude = fmt.Sprintf("_HAND = [%s]", quoted(values))
		}
		replacement = indent + nc.attr + " = _HAND,"
	case "concat":
		replacement = fmt.Sprintf("%s%s = [%s] + [%q],", indent, nc.attr, quoted(generated), hand)
	case "select":
		replacement = fmt.Sprintf("%s%s = select({\"//conditions:default\": [%s]}),",
			indent, nc.attr, quoted(values))
	case "mixed":
		prelude = fmt.Sprintf("_HAND = %q", hand)
		replacement = fmt.Sprintf("%s%s = [%s],", indent, nc.attr,
			strings.Join(append(quotedEach(generated), "_HAND"), ", "))
	case "dict_mixed":
		prelude = fmt.Sprintf("_HAND = %q", hand)
		replacement = fmt.Sprintf("%s%s = {%s%q: _HAND},", indent, nc.attr, entries, handAliasKey)
	case "glob_ident":
		prelude = fmt.Sprintf("_HAND = [%s]", quoted(values))
		replacement = indent + nc.attr + " = glob(_HAND),"
	case "glob_mixed":
		prelude = fmt.Sprintf("_HAND = %q", hand)
		replacement = fmt.Sprintf("%s%s = glob([%s, _HAND]%s),", indent, nc.attr, quoted(generated), excludes)
	default:
		t.Fatalf("no expression for shape %q", shape)
	}

	data, err := os.ReadFile(buildPath)
	if err != nil {
		t.Fatal(err)
	}
	blocks := strings.Split(string(data), "\n\n")
	edited := false
	for i, block := range blocks {
		if !strings.Contains(block, nc.kind+"(") || !strings.Contains(block, fmt.Sprintf("name = %q", nc.target)) {
			continue
		}
		if keep {
			replacement = indent + "# keep\n" + replacement
		}
		lines := replaceAttrLines(strings.Split(block, "\n"), nc.attr, []string{replacement})
		if lines == nil {
			t.Fatalf("could not place %s in the %s(%s) block:\n%s", nc.attr, nc.kind, nc.target, block)
		}
		blocks[i] = strings.Join(lines, "\n")
		edited = true
		break
	}
	if !edited {
		t.Fatalf("no %s(%s) block in %s:\n%s", nc.kind, nc.target, buildPath, data)
	}
	body := strings.Join(blocks, "\n\n")
	if prelude != "" {
		body = prelude + "\n\n" + body
	}
	if err := os.WriteFile(buildPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return authoredExpr{values: values}
}

// handValueFor is the one value in the expression generation cannot derive: the
// element whose disappearance is the defect.
func handValueFor(nc nonLiteralCase) string {
	switch {
	case nc.class == "dict":
		return handAliasDir
	case nc.attr == "visibility":
		return "//vendor:__pkg__"
	case nc.attr == "srcs" && nc.class == "glob":
		return "content/**"
	case nc.attr == "srcs":
		return "hand_extra.ts"
	case nc.attr == "deps":
		return "@npm//:sharp"
	case nc.class == "scalar":
		return "hand.config.mjs"
	default:
		return "//vendor:vendor_hand"
	}
}

// generatedValues are the values already in the attribute, plus the exclude
// argument a generated glob() carries, which the rewrite has to keep too.
func generatedValues(t *testing.T, r *rule.Rule, nc nonLiteralCase) (values []string, excludes string) {
	t.Helper()
	if nc.class == "dict" {
		return dictValues(r.Attr(nc.attr)), ""
	}
	if nc.class != "glob" {
		return attrValues(r, nc.attr), ""
	}
	glob, ok := rule.ParseGlobExpr(r.Attr(nc.attr))
	if !ok {
		t.Fatalf("%s(%s).%s is not a glob() call", nc.kind, nc.target, nc.attr)
	}
	if len(glob.Excludes) > 0 {
		quoted := make([]string, 0, len(glob.Excludes))
		for _, v := range glob.Excludes {
			quoted = append(quoted, fmt.Sprintf("%q", v))
		}
		excludes = ", exclude = [" + strings.Join(quoted, ", ") + "]"
	}
	return glob.Patterns, excludes
}

// ---- reading the expression back -------------------------------------------

func declaredAttrExpr(t *testing.T, root string, nc nonLiteralCase) bzl.Expr {
	t.Helper()
	for _, r := range loadRules(t, root, nc.pkg) {
		if r.Kind() == nc.kind && r.Name() == nc.target {
			return r.Attr(nc.attr)
		}
	}
	return nil
}

// exprShape names the shape of an expression in the vocabulary the cases are
// written in, so a rewrite into a plain list is a shape change rather than a
// value comparison.
func exprShape(e bzl.Expr) string {
	switch v := e.(type) {
	case *bzl.Ident:
		return "ident"
	case *bzl.BinaryExpr:
		return "concat"
	case *bzl.ListExpr:
		for _, el := range v.List {
			if _, ok := el.(*bzl.StringExpr); !ok {
				return "mixed"
			}
		}
		return "literal list"
	case *bzl.StringExpr:
		return "string"
	case *bzl.DictExpr:
		if !isStringDict(v) {
			return "dict_mixed"
		}
		return "dict"
	case *bzl.CallExpr:
		callee, ok := v.X.(*bzl.Ident)
		if !ok {
			return "call"
		}
		if callee.Name == "select" {
			return "select"
		}
		if callee.Name != "glob" || len(v.List) == 0 {
			return callee.Name + "()"
		}
		switch shape := exprShape(v.List[0]); shape {
		case "literal list":
			return "glob"
		case "ident":
			return "glob_ident"
		default:
			return "glob_" + shape
		}
	}
	return fmt.Sprintf("%T", e)
}

// declaredStrings is every string literal in the BUILD file, so a value the
// attribute reaches through a module-level variable counts as declared.
func declaredStrings(t *testing.T, root, pkg string) []string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(pkg), "BUILD.bazel")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := bzl.ParseBuild(path, data)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	bzl.Walk(parsed, func(x bzl.Expr, _ []bzl.Expr) {
		if s, ok := x.(*bzl.StringExpr); ok {
			out = append(out, s.Value)
		}
	})
	return out
}

// handAliasKey and handAliasDir are the path_aliases entry generation cannot
// derive: the entry whose disappearance is the defect. The directory is a real
// one, so it is not suppressed as a path the tree no longer holds.
const (
	handAliasKey = "@hand/"
	handAliasDir = "src/ui/"
)

// renderDictEntries is the dict's own entries as Starlark, ready for another
// entry to be appended -- "" for anything that is not a dict.
func renderDictEntries(e bzl.Expr) string {
	d, ok := e.(*bzl.DictExpr)
	if !ok {
		return ""
	}
	var out []string
	for _, kv := range d.List {
		k, keyOK := kv.Key.(*bzl.StringExpr)
		v, valueOK := kv.Value.(*bzl.StringExpr)
		if !keyOK || !valueOK {
			continue
		}
		out = append(out, fmt.Sprintf("%q: %q", k.Value, v.Value))
	}
	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, ", ") + ", "
}

func dictValues(e bzl.Expr) []string {
	d, ok := e.(*bzl.DictExpr)
	if !ok {
		return nil
	}
	var out []string
	for _, kv := range d.List {
		if v, valueOK := kv.Value.(*bzl.StringExpr); valueOK {
			out = append(out, v.Value)
		}
	}
	return out
}

func quotedEach(vs []string) []string {
	out := make([]string, 0, len(vs))
	for _, v := range vs {
		out = append(out, fmt.Sprintf("%q", v))
	}
	return out
}

func missingFrom(have, want []string) []string {
	var missing []string
	for _, v := range want {
		if !contains(have, v) {
			missing = append(missing, v)
		}
	}
	return missing
}

// ---- the drop diagnostic ---------------------------------------------------

// TestHandAuthoredAttrValue/*/replaced is the half of the drop diagnostic that
// has to fire: a value generation cannot derive disappears, and the run names
// it. TestDeletedPathIsNotReportedAsDropped is the half that has to stay quiet.
// Every fixture mutation that removes something is an ordinary deletion, and
// the advice -- hold it with "# keep" -- would name a source nothing provides,
// which fails analysis rather than surviving the run.
func TestDeletedPathIsNotReportedAsDropped(t *testing.T) {
	for _, tc := range convergeCases() {
		for _, mut := range tc.mutations {
			if len(mut.remove) == 0 {
				continue
			}
			t.Run(tc.name+"/"+mut.kind, func(t *testing.T) {
				root := t.TempDir()
				writeWorkspace(t, root, tc.files)
				captureLog(t, func() { convergeGazelle(t, root) })
				applyMutation(t, root, mut)

				logged := captureLog(t, func() { convergeGazelle(t, root) })
				for _, line := range strings.Split(logged, "\n") {
					if !strings.Contains(line, "is no longer declared") {
						continue
					}
					for _, gone := range mut.remove {
						if !strings.Contains(line, gone) {
							continue
						}
						t.Fatalf("%s was deleted and the run told the user to hold it with "+
							"\"# keep\". Doing that names a source nothing provides, so the "+
							"advice fails analysis instead of surviving the run:\n%s",
							gone, indentLog(line))
					}
				}
			})
		}
	}
}

// TestManagedAttrCasesCoverGeneratedAttrs discovers the (kind, attribute) pairs
// generation actually writes across the fixtures and requires a case for each.
// Hand-listing them is how ts_compile.deps came to be uncovered while four
// framework rules were: the list and the generators drifted apart.
func TestManagedAttrCasesCoverGeneratedAttrs(t *testing.T) {
	covered := map[string]struct{}{}
	for _, nc := range managedAttrCases() {
		covered[nc.kind+"."+nc.attr] = struct{}{}
	}

	// ts_dev_server is written once and then left: generation skips the rule
	// when one already exists, so no candidate ever reaches the merger and
	// there is no rewrite to ask about.
	exempt := map[string]string{
		"ts_dev_server.entry_point":  "created only when absent",
		"ts_dev_server.port":         "created only when absent",
		"ts_dev_server.plugin":       "created only when absent",
		"ts_dev_server.visibility":   "created only when absent",
		"ts_dev_server.node_modules": "created only when absent",
		"ts_dev_server.host":         "created only when absent",
		"ts_dev_server.open":         "created only when absent",
		"ts_dev_server.bundler":      "created only when absent",
	}

	kinds := (&tsLang{}).Kinds()
	var missing []string
	seen := map[string]struct{}{}

	for _, tc := range convergeCases() {
		root := t.TempDir()
		writeWorkspace(t, root, tc.files)
		captureLog(t, func() { convergeGazelle(t, root) })

		for _, pkg := range convergePackages(t, root) {
			for _, r := range loadRules(t, root, pkg) {
				info, known := kinds[r.Kind()]
				if !known {
					continue
				}
				for attr := range info.MergeableAttrs {
					if r.Attr(attr) == nil {
						continue
					}
					pair := r.Kind() + "." + attr
					if _, ok := covered[pair]; ok {
						continue
					}
					if _, ok := exempt[pair]; ok {
						continue
					}
					if _, ok := seen[pair]; ok {
						continue
					}
					seen[pair] = struct{}{}
					missing = append(missing, fmt.Sprintf("%s (%s fixture, %s(%s))",
						pair, tc.name, r.Kind(), r.Name()))
				}
			}
		}
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("generation writes %d mergeable attribute(s) no case in managedAttrCases() asks "+
			"about, so nothing checks what happens to a hand-authored value there:\n      %s",
			len(missing), strings.Join(missing, "\n      "))
	}
}

// ---- path_aliases, the one dict ---------------------------------------------

// TestPathAliasesSurviveTheMerge: path_aliases is the only attribute Gazelle
// owns whose value is a dict, and rule.MergeRules has no case for one --
// extractPlatformStringsExprs matches neither a list nor a select() and returns
// an empty result with no error, so the pre-resolve merge deletes the attribute
// and the post-resolve pass writes the generated dict back whole. Three things
// that round trip cannot do, in the order a user meets them: leave a run over
// an unchanged tree silent, recompute the map when the tree moves, and keep the
// one entry a "# keep" holds while naming the one it drops.
func TestPathAliasesSurviveTheMerge(t *testing.T) {
	tc := convergeFixture(t, "path_aliases")
	root := t.TempDir()
	writeWorkspace(t, root, tc.files)
	captureLog(t, func() { convergeGazelle(t, root) })

	assertPathAliases(t, root, "src", map[string]string{"@/": "src/"})
	before := buildFileText(t, root, "src")

	// Gazelle wrote this attribute, so a diagnostic about its shape is a
	// diagnostic about Gazelle's own output: one warning on the run that
	// generates it and one per generated target on every run after.
	logged := captureLog(t, func() { convergeGazelle(t, root) })
	if logged != "" {
		t.Errorf("the second run over an unchanged tree reported path_aliases, which Gazelle "+
			"generated itself. A warning nobody can act on is one every reader learns to "+
			"skip:\n%s", indentLog(logged))
	}
	if after := buildFileText(t, root, "src"); after != before {
		t.Errorf("the second run over an unchanged tree rewrote src/BUILD.bazel:\n%s",
			lineDiff(before, after))
	}

	// The tree moves: an import through a second alias.
	writeWorkspace(t, root, map[string]string{
		"src/extra.ts": "import { helper } from \"@lib/helper\";\nexport const b = helper;\n",
	})
	captureLog(t, func() { convergeGazelle(t, root) })
	assertPathAliases(t, root, "src", map[string]string{"@/": "src/", "@lib/": "src/lib/"})

	// "# keep" on one entry, nothing on the other. Both directories exist, so
	// neither is suppressed as a path the tree no longer holds.
	addAliasEntries(t, root, "src",
		`        "@kept/": "src/ui/",  # keep`,
		`        "@bare/": "src/ui/",`)

	for run := 2; run <= 3; run++ {
		logged = captureLog(t, func() { convergeGazelle(t, root) })
		got := declaredPathAliases(t, root, "src")
		if _, held := got["@kept/"]; !held {
			t.Fatalf("path_aliases lost the hand-authored \"@kept/\" on run %d even though it "+
				"carries a \"# keep\", so a declared alias disappeared:\n%s\nthe run said:\n%s",
				run, indent(buildFileText(t, root, "src")), indentLog(logged))
		}
		if _, held := got["@bare/"]; held {
			t.Fatalf("path_aliases kept \"@bare/\" with no \"# keep\" on run %d. Gazelle owns "+
				"the attribute, so either it merges entry by entry and the docs say so, or "+
				"this case is wrong:\n%s", run, indent(buildFileText(t, root, "src")))
		}
		if run == 2 && !strings.Contains(logged, `"@bare/"`) {
			t.Fatalf("path_aliases dropped the hand-authored \"@bare/\" and said nothing about "+
				"it. A declared build input disappearing with no diagnostic is the defect "+
				"keep.go exists to remove.\nthe run said:\n%s", indentLog(logged))
		}
		if run == 3 && logged != "" {
			t.Fatalf("run 3 reported path_aliases again, so the drop notice outlived its "+
				"cause or the \"# keep\" is being re-announced:\n%s", indentLog(logged))
		}
	}
}

func assertPathAliases(t *testing.T, root, pkg string, want map[string]string) {
	t.Helper()
	got := declaredPathAliases(t, root, pkg)
	if len(got) != len(want) {
		t.Fatalf("ts_compile(%s).path_aliases = %v, want %v -- Gazelle recomputes the map from "+
			"the tree on every run, so a stale one is an alias map no run corrects:\n%s",
			pkg, got, want, indent(buildFileText(t, root, pkg)))
	}
	for prefix, dir := range want {
		if got[prefix] != dir {
			t.Fatalf("ts_compile(%s).path_aliases = %v, want %v:\n%s",
				pkg, got, want, indent(buildFileText(t, root, pkg)))
		}
	}
}

func declaredPathAliases(t *testing.T, root, pkg string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, r := range loadRules(t, root, pkg) {
		d, isDict := r.Attr("path_aliases").(*bzl.DictExpr)
		if !isDict {
			continue
		}
		for _, kv := range d.List {
			k, keyOK := kv.Key.(*bzl.StringExpr)
			v, valueOK := kv.Value.(*bzl.StringExpr)
			if keyOK && valueOK {
				out[k.Value] = v.Value
			}
		}
	}
	return out
}

// addAliasEntries edits the generated dict the way a user would, by writing
// entries into it verbatim -- comment and all.
func addAliasEntries(t *testing.T, root, pkg string, entries ...string) {
	t.Helper()
	buildPath := filepath.Join(root, filepath.FromSlash(pkg), "BUILD.bazel")
	data, err := os.ReadFile(buildPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) != "path_aliases = {" {
			continue
		}
		edited := append(append([]string(nil), lines[:i+1]...), entries...)
		edited = append(edited, lines[i+1:]...)
		if err := os.WriteFile(buildPath, []byte(strings.Join(edited, "\n")), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Fatalf("no multi-line path_aliases dict in %s:\n%s", buildPath, data)
}
