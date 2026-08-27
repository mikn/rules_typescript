package typescript

// Gazelle owns the attributes it declares mergeable and recomputes them on
// every run. It replaces a shape its merger cannot reconcile value by value --
// a variable, a concatenation, a select() -- the same way Gazelle-for-Go does.
// Nothing here changes that; what it adds is saying so, since Go's merger does
// it in silence. docs/gazelle/directives.md.

import (
	"log"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/bazelbuild/bazel-gazelle/language"
	"github.com/bazelbuild/bazel-gazelle/rule"
	bzl "github.com/bazelbuild/buildtools/build"
)

// Derived from Kinds() rather than listed: MergeableAttrs is already the
// declaration of what Gazelle recomputes, and a second list of the same thing
// is a list that goes stale when a rule kind gains an attribute.
func managedAttrs(kind string) (mergeable, resolved []string) {
	info, ok := (&tsLang{}).Kinds()[kind]
	if !ok {
		return nil, nil
	}
	for attr := range info.MergeableAttrs {
		mergeable = append(mergeable, attr)
		if info.ResolveAttrs[attr] {
			resolved = append(resolved, attr)
		}
	}
	sort.Strings(mergeable)
	sort.Strings(resolved)
	return mergeable, resolved
}

// Before the merge, the one point where the value on disk and the recomputed
// one are both in hand.
func reportManagedAttrDrops(args language.GenerateArgs, gen []*rule.Rule) {
	if args.File == nil {
		return
	}
	for _, want := range gen {
		mergeable, resolved := managedAttrs(want.Kind())
		for _, have := range args.File.Rules {
			if have.Kind() != want.Kind() || have.Name() != want.Name() || have.ShouldKeep() {
				continue
			}
			for _, attr := range mergeable {
				if attrKept(have, attr) {
					continue
				}
				expr := have.Attr(attr)
				if expr == nil {
					continue
				}
				if !isLiteralAttrValue(expr) {
					reportUnmergeableExpr(args.File.Path, have, attr, expr)
					continue
				}
				// A resolved attribute is filled after generation, so the
				// candidate carries nothing to compare against yet. Its shape
				// is all that can be checked here.
				if slices.Contains(resolved, attr) {
					continue
				}
				reportDroppedValues(args, have, attr, droppedAttrValues(args, have, want, attr))
			}
		}
	}
}

// The two shapes rule.MergeRules reconciles value by value. Anything else it
// replaces or drops from.
func isLiteralAttrValue(e bzl.Expr) bool {
	switch v := e.(type) {
	case nil:
		return true
	case *bzl.StringExpr:
		return true
	case *bzl.ListExpr:
		for _, el := range v.List {
			if _, ok := el.(*bzl.StringExpr); !ok {
				return false
			}
		}
		return true
	}
	return false
}

// Which of the two the merger picks depends on the shape, and reproducing that
// decision here means reproducing rule/merge.go's internals: an Ident is
// replaced with its contents lost, a "list + list" is refused and left alone.
// Naming both outcomes is the part that stays true.
func reportUnmergeableExpr(path string, r *rule.Rule, attr string, expr bzl.Expr) {
	start, _ := expr.Span()
	log.Printf("typescript: %s:%d: %s(%s) declares %s as an expression Gazelle's merger cannot "+
		"reconcile value by value, so %s is no longer an attribute Gazelle maintains: it either "+
		"replaces the whole expression, losing what it computed, or leaves it untouched and stops "+
		"updating it. A \"# keep\" comment above the attribute makes that yours deliberately.",
		path, start.Line, r.Kind(), r.Name(), attr, attr)
}

// Mirrors rule.MergeRules, which is what actually removes them.
func droppedAttrValues(args language.GenerateArgs, have, want *rule.Rule, attr string) []string {
	if s := have.AttrString(attr); s != "" {
		if want.AttrString(attr) == s || !derivableValue(args, s) {
			return nil
		}
		return []string{s}
	}
	carried := map[string]struct{}{}
	for _, v := range want.AttrStrings(attr) {
		carried[v] = struct{}{}
	}
	var dropped []string
	for _, e := range listElements(have.Attr(attr)) {
		s, ok := e.(*bzl.StringExpr)
		if !ok || rule.ShouldKeep(e) {
			continue
		}
		if _, held := carried[s.Value]; held || !derivableValue(args, s.Value) {
			continue
		}
		dropped = append(dropped, s.Value)
	}
	return dropped
}

var fileExtension = regexp.MustCompile(`\.[A-Za-z0-9]{1,6}$`)

// A value naming a file or package that is no longer there was dropped because
// the tree changed, and telling the user to hold it with "# keep" is telling
// them to name a source nothing provides -- which fails analysis rather than
// surviving the run. Only a path-shaped value is checked: "app" is a mode, not
// a missing directory.
func derivableValue(args language.GenerateArgs, value string) bool {
	switch {
	case value == "", strings.HasPrefix(value, "@"), strings.HasPrefix(value, ":"):
		return true
	case strings.HasPrefix(value, "//"):
		pkg, _, _ := strings.Cut(strings.TrimPrefix(value, "//"), ":")
		return pkg == "" || pathExists(args.Config.RepoRoot, pkg)
	case strings.Contains(value, ":"):
		return true
	case strings.Contains(value, "/"), fileExtension.MatchString(value):
		return pathExists(args.Dir, value)
	}
	return true
}

func pathExists(base, rel string) bool {
	_, err := os.Stat(filepath.Join(base, filepath.FromSlash(rel)))
	return err == nil
}

func reportDroppedValues(args language.GenerateArgs, r *rule.Rule, attr string, dropped []string) {
	if len(dropped) == 0 {
		return
	}
	quoted := make([]string, 0, len(dropped))
	for _, v := range dropped {
		quoted = append(quoted, `"`+v+`"`)
	}
	log.Printf("typescript: %s(%s) in %s: Gazelle generates %s and recomputed it from the tree, "+
		"so %s is no longer declared. A value Gazelle cannot derive needs a \"# keep\" comment on "+
		"its own line to survive the next run; \"# keep\" above the attribute hands the whole "+
		"attribute back to you.",
		r.Kind(), r.Name(), args.File.Path, attr, strings.Join(quoted, ", "))
}

// ---- attributes the merger cannot reconcile --------------------------------

// Written onto the rule in the BUILD file too, since the merger cannot merge a
// glob() call -- so keep is honoured here rather than by the merger.
func setGeneratedGlob(args language.GenerateArgs, gen *rule.Rule, patterns []string) {
	gen.SetAttr("srcs", rule.GlobValue{Patterns: patterns})
	if args.File == nil {
		return
	}
	for _, r := range args.File.Rules {
		if r.Kind() != gen.Kind() || r.Name() != gen.Name() {
			continue
		}
		if r.ShouldKeep() || attrKept(r, "srcs") {
			return
		}
		expr := r.Attr("srcs")
		if expr == nil {
			// Nothing on disk to leave alone: the merger copies in an attribute
			// the existing rule does not carry.
			return
		}
		have, ok := rule.ParseGlobExpr(expr)
		if !ok || !isLiteralGlobExpr(expr) {
			start, end := expr.Span()
			log.Printf("typescript: %s:%d.%d-%d.%d: could not merge expression -- %s(%s) declares "+
				"srcs that is not a glob() of plain strings, so Gazelle left it alone. It now has "+
				"to cover %s by hand: a file srcs does not name is absent from the staged tree "+
				"and does not resolve.",
				args.File.Path, start.Line, start.LineRune, end.Line, end.LineRune,
				r.Kind(), r.Name(), strings.Join(patterns, ", "))
			return
		}
		kept, dropped := globPatternsKept(expr, patterns)
		reportDroppedValues(args, r, "srcs", dropped)
		merged := append(append([]string(nil), patterns...), keptPatternValues(kept)...)
		if sameValues(have.Patterns, merged) {
			return
		}
		r.SetAttr("srcs", globKeeping(patterns, have.Excludes, kept))
		return
	}
}

// ParseGlobExpr skips an argument it cannot read rather than refusing the call,
// so a rewrite from what it did read drops everything it did not.
func isLiteralGlobExpr(e bzl.Expr) bool {
	call, ok := e.(*bzl.CallExpr)
	if !ok {
		return false
	}
	for _, arg := range call.List {
		if assign, named := arg.(*bzl.AssignExpr); named {
			arg = assign.RHS
		}
		if !isLiteralAttrValue(arg) {
			return false
		}
	}
	return true
}

// Kept patterns come back as expressions, comment and all, so the mark itself
// survives the rewrite.
func globPatternsKept(expr bzl.Expr, patterns []string) (kept []bzl.Expr, dropped []string) {
	for _, e := range listElements(globPatternList(expr)) {
		s, ok := e.(*bzl.StringExpr)
		if !ok || slices.Contains(patterns, s.Value) {
			continue
		}
		if rule.ShouldKeep(e) {
			kept = append(kept, e)
			continue
		}
		dropped = append(dropped, s.Value)
	}
	return kept, dropped
}

// globKeeping is the glob() call the recomputed and the kept patterns make up.
func globKeeping(patterns, excludes []string, kept []bzl.Expr) bzl.Expr {
	expr := rule.GlobValue{Patterns: patterns, Excludes: excludes}.BzlExpr()
	if len(kept) == 0 {
		return expr
	}
	list, ok := globPatternList(expr).(*bzl.ListExpr)
	if !ok {
		return expr
	}
	list.List = append(list.List, kept...)
	list.ForceMultiLine = true
	return expr
}

func keptPatternValues(kept []bzl.Expr) []string {
	out := make([]string, 0, len(kept))
	for _, e := range kept {
		if s, ok := e.(*bzl.StringExpr); ok {
			out = append(out, s.Value)
		}
	}
	return out
}

func globPatternList(expr bzl.Expr) bzl.Expr {
	call, ok := expr.(*bzl.CallExpr)
	if !ok || len(call.List) == 0 {
		return nil
	}
	return call.List[0]
}

func listElements(e bzl.Expr) []bzl.Expr {
	list, ok := e.(*bzl.ListExpr)
	if !ok {
		return nil
	}
	return list.List
}

// As sets: both sides are label-sorted on the way into the file.
func sameValues(a, b []string) bool {
	x := append([]string(nil), a...)
	y := append([]string(nil), b...)
	sort.Strings(x)
	sort.Strings(y)
	return slices.Equal(x, y)
}

// attrKept reports a "# keep" on the attribute, which the merger checks itself
// but the direct write path has to ask about.
func attrKept(r *rule.Rule, key string) bool {
	comments := r.AttrComments(key)
	if comments == nil {
		return false
	}
	for _, comment := range append(comments.Before, comments.Suffix...) {
		text := strings.TrimSpace(strings.TrimPrefix(comment.Token, "#"))
		if text == "keep" || strings.HasPrefix(text, "keep: ") {
			return true
		}
	}
	return false
}
