package typescript

import (
	"fmt"
	"log"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bazelbuild/bazel-gazelle/language"
)

// anchoredExcludePrefix is what marks a ts_exclude pattern as a path rather
// than a name. A bare pattern is matched against a basename, so it drops a file
// of that name at every depth of the tree below the declaration and has no way
// to say which one it meant; "./" resolves the rest against the directory whose
// build file declares it.
const anchoredExcludePrefix = "./"

// excludeRule is one ts_exclude value: the pattern as it was written, the
// directory whose build file declared it, and -- for an anchored pattern -- the
// workspace-relative pattern that "./" resolved to.
//
// declaredIn is carried because directives are inherited: the package a drop
// fires in is usually not the package holding the line to edit, and a report
// that names the wrong build file sends the reader to a file the directive is
// not in.
type excludeRule struct {
	written    string
	declaredIn string
	anchored   string
}

// addExcludePattern files one ts_exclude value under the reading its spelling
// asks for. rel is the directory that declared it, which is what an anchored
// pattern is relative to.
func (tc *tsConfig) addExcludePattern(rel, pattern string) {
	switch {
	case pattern == "":
	case !strings.HasPrefix(pattern, anchoredExcludePrefix):
		tc.excludePatterns = append(tc.excludePatterns,
			excludeRule{written: pattern, declaredIn: rel})
	default:
		rest := strings.TrimPrefix(pattern, anchoredExcludePrefix)
		if strings.Trim(rest, "/") == "" {
			log.Printf("typescript: %s: # gazelle:ts_exclude %s resolves to the declaring "+
				"directory's own path, and no file or subdirectory is ever compared against "+
				"that, so the pattern excludes nothing at all. An anchored pattern needs a "+
				"path after the %q.",
				orRepoRoot(rel), pattern, anchoredExcludePrefix)
			return
		}
		tc.excludePatterns = append(tc.excludePatterns, excludeRule{
			written:    pattern,
			declaredIn: rel,
			anchored:   path.Join(rel, rest),
		})
	}
}

// excludeSet is the ts_exclude rules in force for one package plus where that
// package sits, which is what an anchored pattern is compared against.
type excludeSet struct {
	rules []excludeRule
	rel   string
}

func (tc *tsConfig) excludesIn(rel string) excludeSet {
	return excludeSet{rules: tc.excludePatterns, rel: rel}
}

// drops reports whether a pattern takes the file or directory the package
// reaches by pkgPath: a basename for something sitting directly in the package,
// a joined path for something a rollup walk reached below it.
func (e excludeSet) drops(pkgPath string) bool {
	_, dropped := e.dropsBy(pkgPath)
	return dropped
}

// dropsBy is drops plus the rule that did it, so a report can name the
// directive and the build file holding it.
//
// Both spellings of a bare pattern are tried, which is what lets `plugins/*.ts`
// match a rolled-up path while `*.generated.ts` matches a name at any depth.
func (e excludeSet) dropsBy(pkgPath string) (excludeRule, bool) {
	for _, against := range []string{path.Base(pkgPath), pkgPath} {
		for _, r := range e.rules {
			if r.anchored == "" && globMatches(r.written, against) {
				return r, true
			}
		}
	}
	wsPath := path.Join(e.rel, pkgPath)
	for _, r := range e.rules {
		if r.anchored != "" && globMatches(r.anchored, wsPath) {
			return r, true
		}
	}
	return excludeRule{}, false
}

func globMatches(pattern, name string) bool {
	matched, err := filepath.Match(pattern, name)
	return err == nil && matched
}

// excludedSrc is one TypeScript source a ts_exclude rule took out of a
// package's generated targets, and the rule that took it.
type excludedSrc struct {
	path string
	rule excludeRule
}

// reportExcludedSrcs says what left the generated targets and which directive
// took it. Without it a pattern matching more than it meant to removes files
// silently, which is the failure the exclusion mechanism exists to control.
//
// One line per rule that dropped something, never one per file: the run that
// does what the directive was written for is the common one, and forty lines
// there is how a diagnostic stops being read. A rule that matched nothing in
// this package says nothing, so a root-level directive is quiet in every
// package it does not reach.
//
// The claim is about this run's srcs and no more. A srcs entry carrying a
// "# keep" comment survives rule.MergeList, so a hand-kept ts_compile goes on
// compiling an excluded file -- exclusion runs at generation time and never
// sees that merge.
func reportExcludedSrcs(args language.GenerateArgs, dropped []excludedSrc) {
	byRule := map[excludeRule][]string{}
	for _, d := range dropped {
		byRule[d.rule] = append(byRule[d.rule], path.Join(args.Rel, d.path))
	}
	rules := make([]excludeRule, 0, len(byRule))
	for r := range byRule {
		rules = append(rules, r)
	}
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].written != rules[j].written {
			return rules[i].written < rules[j].written
		}
		return rules[i].declaredIn < rules[j].declaredIn
	})

	for _, r := range rules {
		paths := byRule[r]
		sort.Strings(paths)
		log.Printf("typescript: %s: # gazelle:ts_exclude %s%s leaves %s out of the srcs "+
			"generated here: %s.%s",
			orRepoRoot(args.Rel), r.written, declaredElsewhere(r, args.Rel),
			countOf(len(paths), "TypeScript file"), firstFew(paths, 3),
			anchoringAdvice(r, args.Rel))
	}
}

// declaredElsewhere names the build file holding the directive when that is not
// the one being generated, and says nothing when it is: the line already opens
// with this package.
func declaredElsewhere(r excludeRule, rel string) string {
	if r.declaredIn == rel {
		return ""
	}
	return fmt.Sprintf(", declared in %s,", orRepoRoot(r.declaredIn))
}

// anchoringAdvice spells the anchored form of a bare pattern for the reader,
// resolved so that writing it in the build file that declares the directive
// names this package. A pattern with a path in it already says which directory
// it means, so it gets no advice.
//
// The anchored form covers this package's own files. It is not a restatement of
// this drop: under a rolled-up boundary the same bare pattern also took files
// out of subdirectories, and an anchored pattern does not reach those.
func anchoringAdvice(r excludeRule, rel string) string {
	if strings.Contains(r.written, "/") {
		return ""
	}
	under, ok := relativeTo(r.declaredIn, rel)
	if !ok {
		return ""
	}
	anchored := anchoredExcludePrefix + path.Join(under, r.written)
	if under == "" {
		return fmt.Sprintf(" It names no path, so it matches that basename at every depth "+
			"below this directory; %q anchors it here.", anchored)
	}
	return fmt.Sprintf(" It names no path, so it matches that basename at every depth below "+
		"%s; %q in that build file matches %s's own files only.",
		orRepoRoot(r.declaredIn), anchored, rel)
}

// relativeTo is target's path from base, and whether base contains target at
// all: a directive reaches a package by inheritance, so the declaring directory
// is an ancestor, but nothing in the type system says so.
func relativeTo(base, target string) (string, bool) {
	switch {
	case base == "":
		return target, true
	case base == target:
		return "", true
	case strings.HasPrefix(target, base+"/"):
		return strings.TrimPrefix(target, base+"/"), true
	default:
		return "", false
	}
}

func countOf(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// firstFew is a bounded rendering of a list: enough of it to recognise what
// happened and a count for the rest, so the line does not grow with the drop.
func firstFew(items []string, n int) string {
	if len(items) <= n {
		return strings.Join(items, ", ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(items[:n], ", "), len(items)-n)
}
