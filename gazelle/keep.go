package typescript

// Gazelle owns the attributes it declares mergeable and recomputes them on
// every run. It replaces a shape its merger cannot reconcile value by value --
// a variable, a concatenation, a select() -- the same way Gazelle-for-Go does.
// Nothing here changes that; what it adds is saying so, since Go's merger does
// it in silence. docs/gazelle/directives.md.

import (
	"log"
	"os"
	"path"
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

// The shapes a merge reconciles value by value: the two rule.MergeRules knows,
// plus the string dict pathAliasMap.Merge reconciles entry by entry. Anything
// else is replaced or dropped from.
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
	case *bzl.DictExpr:
		return isStringDict(v)
	}
	return false
}

// isStringDict reports a dict whose every key and value is a plain string,
// which is the whole of what pathAliasMap.Merge reads.
func isStringDict(d *bzl.DictExpr) bool {
	for _, kv := range d.List {
		if _, ok := kv.Key.(*bzl.StringExpr); !ok {
			return false
		}
		if _, ok := kv.Value.(*bzl.StringExpr); !ok {
			return false
		}
	}
	return true
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
	if d, isDict := have.Attr(attr).(*bzl.DictExpr); isDict {
		return droppedDictKeys(args, d, want.Attr(attr))
	}
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

// Mirrors pathAliasMap.Merge: an entry the recomputed map does not carry and no
// "# keep" holds is gone. The keys are what the user reads the map by, so the
// keys are what the report names. An entry whose directory has been deleted was
// dropped because the tree changed, and its value is repo-relative rather than
// package-relative -- which is why derivableValue cannot answer for it.
func droppedDictKeys(args language.GenerateArgs, have *bzl.DictExpr, want bzl.Expr) []string {
	carried := map[string]struct{}{}
	if w, isDict := want.(*bzl.DictExpr); isDict {
		for _, kv := range w.List {
			if k, ok := kv.Key.(*bzl.StringExpr); ok {
				carried[k.Value] = struct{}{}
			}
		}
	}
	var dropped []string
	for _, kv := range have.List {
		k, isString := kv.Key.(*bzl.StringExpr)
		if !isString || rule.ShouldKeep(kv) {
			continue
		}
		if _, held := carried[k.Value]; held {
			continue
		}
		if dir, ok := kv.Value.(*bzl.StringExpr); ok && !pathExists(args.Config.RepoRoot, dir.Value) {
			continue
		}
		dropped = append(dropped, k.Value)
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

// ---- path_aliases, the one dict Gazelle owns -------------------------------

// rule.attrValue states the contract: "If the attribute is mergeable the value
// must implement the Merger interface." A map[string]string does not, and
// rule.MergeRules has no case for a dict at all -- extractPlatformStringsExprs
// matches neither a list nor a select() and returns an empty result with no
// error, so the pre-resolve merge deletes the attribute and the post-resolve
// pass writes the generated dict back whole. That round trip is why an entry
// carrying "# keep" does not survive, and why keep.go warned about Gazelle's own
// output on every run after the first.
type pathAliasMap map[string]string

var (
	_ rule.BzlExprValue = pathAliasMap(nil)
	_ rule.Merger       = pathAliasMap(nil)
)

func (m pathAliasMap) BzlExpr() bzl.Expr {
	prefixes := make([]string, 0, len(m))
	for prefix := range m {
		prefixes = append(prefixes, prefix)
	}
	sort.Strings(prefixes)
	entries := make([]*bzl.KeyValueExpr, 0, len(prefixes))
	for _, prefix := range prefixes {
		entries = append(entries, &bzl.KeyValueExpr{
			Key:   &bzl.StringExpr{Value: prefix},
			Value: &bzl.StringExpr{Value: m[prefix]},
		})
	}
	return &bzl.DictExpr{List: entries, ForceMultiLine: true}
}

// Merge is entry by entry, as rule.MergeList is element by element: a
// recomputed entry wins, an entry carrying "# keep" survives with its comment,
// and an entry the tree no longer accounts for goes. A nil result is how a
// merger asks rule.MergeRules to delete the attribute, which is what an alias
// map recomputed down to nothing has to do.
func (m pathAliasMap) Merge(other bzl.Expr) bzl.Expr {
	merged := m.BzlExpr().(*bzl.DictExpr)
	if dict, isDict := other.(*bzl.DictExpr); isDict {
		for _, kv := range dict.List {
			key, isString := kv.Key.(*bzl.StringExpr)
			if !isString || !rule.ShouldKeep(kv) {
				continue
			}
			if _, recomputed := m[key.Value]; recomputed {
				continue
			}
			merged.List = append(merged.List, kv)
		}
	}
	if len(merged.List) == 0 {
		return nil
	}
	return merged
}

// setPathAliases hands the merger a value it can reconcile. The empty map never
// goes on the generated rule: rule.MergeRules re-adds, in its post-resolve pass,
// every attribute the generated rule carries that the merged file does not, an
// empty dict included. So a map recomputed down to nothing is applied to the
// rule on disk here, the way setGeneratedGlob applies a glob().
func setPathAliases(args language.GenerateArgs, gen *rule.Rule, used map[string]string) {
	if len(used) > 0 {
		gen.SetAttr("path_aliases", pathAliasMap(used))
		return
	}
	have := matchingRule(args, gen)
	if have == nil || have.ShouldKeep() || attrKept(have, "path_aliases") {
		return
	}
	// reportManagedAttrDrops names a shape no merge reconciles, so this one
	// leaves it alone rather than saying the same thing twice.
	if expr := have.Attr("path_aliases"); expr == nil || !isLiteralAttrValue(expr) {
		return
	}
	reportDroppedValues(args, have, "path_aliases",
		droppedAttrValues(args, have, gen, "path_aliases"))
	if merged := pathAliasMap(nil).Merge(have.Attr("path_aliases")); merged != nil {
		have.SetAttr("path_aliases", merged)
		return
	}
	have.DelAttr("path_aliases")
}

func matchingRule(args language.GenerateArgs, gen *rule.Rule) *rule.Rule {
	if args.File == nil {
		return nil
	}
	for _, r := range args.File.Rules {
		if r.Kind() == gen.Kind() && r.Name() == gen.Name() {
			return r
		}
	}
	return nil
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

// ---- declaration_type, the dict a directive owns ---------------------------

// A string dict like pathAliasMap, and reconciled the same way, but left out of
// Kinds() -- rule.MergeRules deletes a mergeable attribute the generated rule
// does not carry, and a tree with no ts_asset_declaration_type directive carries
// none, so declaring it mergeable would delete the hand-written declaration_type
// of every repo that adopted the attribute before the directive existed.
//
// An entry whose value is empty is one a directive named and left blank: the
// map owns the extension and declares nothing for it, which is how a nested
// directive returns a subtree to asset_library's string default.
type declarationTypeMap map[string]string

var (
	_ rule.BzlExprValue = declarationTypeMap(nil)
	_ rule.Merger       = declarationTypeMap(nil)
)

func (m declarationTypeMap) BzlExpr() bzl.Expr {
	return &bzl.DictExpr{List: m.entries(), ForceMultiLine: true}
}

func (m declarationTypeMap) entries() []*bzl.KeyValueExpr {
	exts := make([]string, 0, len(m))
	for ext, typeExpr := range m {
		if typeExpr != "" {
			exts = append(exts, ext)
		}
	}
	sort.Strings(exts)
	out := make([]*bzl.KeyValueExpr, 0, len(exts))
	for _, ext := range exts {
		out = append(out, &bzl.KeyValueExpr{
			Key:   &bzl.StringExpr{Value: ext},
			Value: &bzl.StringExpr{Value: m[ext]},
		})
	}
	return out
}

// Merge is entry by entry, as pathAliasMap.Merge is, over a narrower claim: the
// directives name the extensions Gazelle owns, and an extension no directive
// named is carried across untouched however it got there. Within what it owns
// the directive wins, and a "# keep" on the entry is what hands one back.
func (m declarationTypeMap) Merge(other bzl.Expr) bzl.Expr {
	held := map[string]bool{}
	var carried []*bzl.KeyValueExpr
	if dict, isDict := other.(*bzl.DictExpr); isDict {
		for _, kv := range dict.List {
			key, isString := kv.Key.(*bzl.StringExpr)
			if !isString {
				continue
			}
			_, owned := m[key.Value]
			if owned && !rule.ShouldKeep(kv) {
				continue
			}
			held[key.Value] = owned
			carried = append(carried, kv)
		}
	}
	merged := &bzl.DictExpr{ForceMultiLine: true}
	for _, kv := range m.entries() {
		if !held[kv.Key.(*bzl.StringExpr).Value] {
			merged.List = append(merged.List, kv)
		}
	}
	merged.List = append(merged.List, carried...)
	sort.SliceStable(merged.List, func(i, j int) bool {
		return dictKey(merged.List[i]) < dictKey(merged.List[j])
	})
	if len(merged.List) == 0 {
		return nil
	}
	return merged
}

func dictKey(kv *bzl.KeyValueExpr) string {
	if key, isString := kv.Key.(*bzl.StringExpr); isString {
		return key.Value
	}
	return ""
}

// declarationTypeFor narrows the directives in force to the extensions srcs
// actually has. One asset_library holds one asset file, so a tree declaring
// .svg and .png writes one entry per target rather than both on each.
func declarationTypeFor(tc *tsConfig, srcs []string) declarationTypeMap {
	if len(tc.assetDeclarationType) == 0 {
		return nil
	}
	out := declarationTypeMap{}
	for _, src := range srcs {
		ext := strings.ToLower(path.Ext(src))
		if typeExpr, owned := tc.assetDeclarationType[ext]; owned {
			out[ext] = typeExpr
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// setAssetDeclarationType decorates a rule the generator is writing for the
// first time. A rule already in the BUILD file never reaches here: its srcs are
// claimed, so the generator emits nothing for that asset at all --
// applyAssetDeclarationType is what reaches those.
func setAssetDeclarationType(gen *rule.Rule, want declarationTypeMap) {
	if len(want.entries()) > 0 {
		gen.SetAttr("declaration_type", want)
	}
}

// applyAssetDeclarationType reconciles the asset_library rules the BUILD file
// already holds, which is every one of them after the run that wrote it.
func applyAssetDeclarationType(args language.GenerateArgs, tc *tsConfig) {
	if args.File == nil || len(tc.assetDeclarationType) == 0 {
		return
	}
	for _, have := range args.File.Rules {
		if have.Kind() != "asset_library" || have.ShouldKeep() {
			continue
		}
		want := declarationTypeFor(tc, have.AttrStrings("srcs"))
		if len(want) == 0 || attrKept(have, "declaration_type") {
			continue
		}
		existing := have.Attr("declaration_type")
		if existing != nil && !isLiteralAttrValue(existing) {
			reportUnmergeableExpr(args.File.Path, have, "declaration_type", existing)
			continue
		}
		merged := want.Merge(existing)
		if merged == nil {
			have.DelAttr("declaration_type")
			continue
		}
		have.SetAttr("declaration_type", merged)
	}
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
