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

// anchoredExclude is one "./"-spelled ts_exclude: the workspace-relative
// pattern it resolved to, and the directive as it was written, which is the
// line a report has to name.
type anchoredExclude struct {
	pattern string
	written string
}

// addExcludePattern files one ts_exclude value under the reading its spelling
// asks for. rel is the directory that declared it, which is what an anchored
// pattern is relative to.
func (tc *tsConfig) addExcludePattern(rel, pattern string) {
	switch {
	case pattern == "":
	case !strings.HasPrefix(pattern, anchoredExcludePrefix):
		tc.excludePatterns = append(tc.excludePatterns, pattern)
	default:
		rest := strings.TrimPrefix(pattern, anchoredExcludePrefix)
		if strings.Trim(rest, "/") == "" {
			log.Printf("typescript: %s: # gazelle:ts_exclude %s names the directory itself and "+
				"no file in it, so nothing is excluded. An anchored pattern needs a path after "+
				"the %q -- to drop a whole directory, name it: # gazelle:ts_exclude <dir>.",
				orRepoRoot(rel), pattern, anchoredExcludePrefix)
			return
		}
		tc.anchoredExcludes = append(tc.anchoredExcludes, anchoredExclude{
			pattern: path.Join(rel, rest),
			written: pattern,
		})
	}
}

// excludeSet is the ts_exclude patterns in force for one package plus where
// that package sits, which is what an anchored pattern is compared against.
type excludeSet struct {
	bare     []string
	anchored []anchoredExclude
	rel      string
}

func (tc *tsConfig) excludesIn(rel string) excludeSet {
	return excludeSet{bare: tc.excludePatterns, anchored: tc.anchoredExcludes, rel: rel}
}

// drops reports whether a pattern takes the file or directory the package
// reaches by pkgPath: a basename for something sitting directly in the package,
// a joined path for something a rollup walk reached below it.
func (e excludeSet) drops(pkgPath string) bool {
	return e.dropsBy(pkgPath) != ""
}

// dropsBy is drops plus the directive that did it, as written, so a report can
// name the line to edit.
//
// Both spellings of a bare pattern are tried, which is what lets `plugins/*.ts`
// match a rolled-up path while `*.generated.ts` matches a name at any depth.
func (e excludeSet) dropsBy(pkgPath string) string {
	if p := matchingExclude(path.Base(pkgPath), e.bare); p != "" {
		return p
	}
	if p := matchingExclude(pkgPath, e.bare); p != "" {
		return p
	}
	wsPath := path.Join(e.rel, pkgPath)
	for _, a := range e.anchored {
		if matched, err := filepath.Match(a.pattern, wsPath); err == nil && matched {
			return a.written
		}
	}
	return ""
}

// matchingExclude is isConfiguredExclude plus which pattern matched.
func matchingExclude(name string, patterns []string) string {
	for _, pattern := range patterns {
		if matched, err := filepath.Match(pattern, name); err == nil && matched {
			return pattern
		}
	}
	return ""
}

// excludedSrc is one TypeScript source a ts_exclude pattern took out of a
// package, and the directive that took it.
type excludedSrc struct {
	path    string
	pattern string
}

// reportExcludedSrcs says what left the program and which directive took it.
// Without it a pattern matching more than it meant to removes files silently,
// which is the failure the exclusion mechanism exists to control.
//
// One line per pattern that dropped something, never one per file: the run that
// does what the directive was written for is the common one, and forty lines
// there is how a diagnostic stops being read. A pattern that matched nothing in
// this package says nothing, so a root-level directive is quiet in every
// package it does not reach.
//
// Deliberately not reported: a pattern matching a directory, which stops the
// rollup walk before it reads what is inside. Counting those files means
// walking the subtree the exclusion exists to skip, and naming a directory is
// the one spelling that cannot match more than it says.
func reportExcludedSrcs(args language.GenerateArgs, dropped []excludedSrc) {
	byPattern := map[string][]string{}
	for _, d := range dropped {
		byPattern[d.pattern] = append(byPattern[d.pattern], d.path)
	}
	patterns := make([]string, 0, len(byPattern))
	for pattern := range byPattern {
		patterns = append(patterns, pattern)
	}
	sort.Strings(patterns)

	for _, pattern := range patterns {
		paths := byPattern[pattern]
		sort.Strings(paths)
		log.Printf("typescript: %s: # gazelle:ts_exclude %s keeps %s out of every generated "+
			"target's srcs -- %s -- so nothing in the build compiles them.%s",
			orRepoRoot(args.Rel), pattern, countOf(len(paths), "TypeScript source"),
			firstFew(paths, 3), anchoringAdvice(pattern))
	}
}

// A pattern with no path in it is the one shape that cannot say which file it
// means, so it is the one the anchored spelling is advice for.
func anchoringAdvice(pattern string) string {
	if strings.Contains(pattern, "/") {
		return ""
	}
	return fmt.Sprintf(" The pattern names no path, so it drops that name at every depth of this "+
		"tree; %q anchors it to this directory.", anchoredExcludePrefix+pattern)
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
