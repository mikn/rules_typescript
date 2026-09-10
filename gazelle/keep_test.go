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
	class     string // "list" or "scalar"
}

// managedAttrCases names every attribute in keep.go's managedAttrs, in a
// workspace where generation writes that rule -- so a managed attribute that
// stops being covered here is one this test stops asking about.
func managedAttrCases() []nonLiteralCase {
	managed := func(workspace, pkg, kind, target, attr, class string,
	) nonLiteralCase {
		return nonLiteralCase{workspace: workspace, pkg: pkg, kind: kind,
			target: target, attr: attr, class: class}
	}
	return []nonLiteralCase{
		managed("plain", "src", "ts_compile", "src", "srcs", "list"),
		managed("plain", "src", "ts_compile", "src", "deps", "list"),
		managed("plain", "src", "ts_compile", "src", "visibility", "list"),
		managed("plain", "src", "ts_compile", "src", "tsconfig", "scalar"),
		managed("plain", "src", "ts_test", "src_test", "srcs", "list"),
		managed("plain", "src", "ts_test", "src_test", "deps", "list"),
		managed("plain", "src", "ts_test", "src_test", "tsconfig", "scalar"),
		managed("plain", "src", "ts_test", "src_test", "config", "scalar"),
		managed("plain", "", "ts_config", "tsconfig", "src", "scalar"),
		managed("plain", "", "ts_config", "tsconfig", "visibility", "list"),
		managed("plain", "src", "ts_config", "tsconfig", "deps", "list"),

		managed("pnpm_member", "packages/core", "ts_compile", "core",
			"node_modules", "scalar"),
		managed("pnpm_member", "packages/core", "ts_test", "core_test", "deps",
			"list"),
		managed("pnpm_member", "packages/core", "ts_test", "core_test",
			"node_modules", "scalar"),
		managed("pnpm_member", "packages/core", "node_modules", "node_modules",
			"deps", "list"),
		managed("pnpm_member", "packages/core", "node_modules", "node_modules",
			"parent", "scalar"),
		managed("pnpm_member", "packages/core", "node_modules", "node_modules",
			"visibility", "list"),
		managed("pnpm_member", "", "node_modules_member", "node_modules/@w/core",
			"member", "scalar"),
		managed("pnpm_member", "", "node_modules_member", "node_modules/@w/core",
			"visibility", "list"),

		managed("worker", "worker", "filegroup", "vitest_config", "srcs", "list"),
		managed("worker", "worker", "filegroup", "vitest_config", "visibility",
			"list"),
	}
}

// The expression shapes that can stand in for a value of each class.
var nonLiteralShapes = map[string][]string{
	"list":   {"ident", "concat", "select", "mixed"},
	"scalar": {"ident"},
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
	generated := attrValues(target, nc.attr)
	values := append(append([]string(nil), generated...), hand)

	const indent = "    "
	quoted := func(vs []string) string { return strings.Join(quotedEach(vs), ", ") }

	var prelude, replacement string
	switch shape {
	case "ident":
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
	case nc.attr == "visibility":
		return "//vendor:__pkg__"
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
	case *bzl.CallExpr:
		callee, ok := v.X.(*bzl.Ident)
		if !ok {
			return "call"
		}
		if callee.Name == "select" {
			return "select"
		}
		return callee.Name + "()"
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
