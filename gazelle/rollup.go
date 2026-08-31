package typescript

import (
	"log"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// rolledUpSrcs returns the TypeScript sources of every non-boundary descendant
// of dir, as paths relative to dir, split into sources and tests.
//
// In index-only mode a directory is a package only when it has an index file,
// so the files in a plain subdirectory belong to the nearest ancestor that is
// one. Giving them their own target instead is what turns an ordinary shape --
// a barrel re-exporting ./rules, and ./rules importing ../utils -- into a
// dependency cycle between two Bazel packages, because at file granularity
// there is no cycle at all.
//
// A descendant stops the walk when it is a package in its own right: it has an
// index file, or it has a BUILD file, which is either a package already or a
// deliberate statement that it should be one.
func rolledUpSrcs(dir string, excludes []string) (srcs, tests []string) {
	r := rolledUp(dir, excludes)
	return r.srcs, r.tests
}

// rolledUpFiles is everything a rolled-up subtree contributes to the package
// that claims it, split by the kind of target each group needs.
type rolledUpFiles struct {
	srcs  []string
	tests []string
	docs  []string
	// ambient names the srcs entries that declare globals. Nothing imports one,
	// so every target that needs it needs it in its own srcs.
	ambient    []string
	css        []string
	cssModules []string
	assets     []string
	json       []string
}

// rolledUp is rolledUpSrcs plus the files that are not TypeScript. A stylesheet
// beside a rolled-up source is imported by it, so leaving it behind gives that
// import nothing to resolve to and the specifier becomes a label for a package
// that cannot exist.
func rolledUp(dir string, excludes []string) rolledUpFiles {
	return rolledUpIn(boundaryIndexOnly, dir, excludes)
}

// rolledUpIn is rolledUp for a named boundary mode: what stops the walk is
// whatever makes a directory a package in that mode.
func rolledUpIn(mode string, dir string, excludes []string) rolledUpFiles {
	var out rolledUpFiles
	stops := func(d string) bool { return dirIsItsOwnPackageIn(mode, d) }
	var walk func(rel string)
	walk = func(rel string) {
		entries, err := os.ReadDir(filepath.Join(dir, rel))
		if err != nil {
			return
		}
		var subdirs []string
		var files []string
		for _, e := range entries {
			if e.IsDir() {
				subdirs = append(subdirs, e.Name())
				continue
			}
			files = append(files, e.Name())
		}
		for _, name := range files {
			joined := filepath.ToSlash(filepath.Join(rel, name))
			// Excludes are written relative to the directory declaring them, so
			// both spellings have to match: the pattern as written and the path
			// this walk reached the file by.
			if isConfiguredExclude(name, excludes) || isConfiguredExclude(joined, excludes) {
				continue
			}
			switch {
			case isTypeScriptFile(name):
				if isFrameworkGeneratedFile(name) {
					continue
				}
				if isTestFile(name) {
					out.tests = append(out.tests, joined)
					continue
				}
				if isDocFile(name) {
					out.docs = append(out.docs, joined)
					continue
				}
				out.srcs = append(out.srcs, joined)
				if isAmbientDeclaration(filepath.Join(dir, rel), name) {
					out.ambient = append(out.ambient, joined)
				}
			case isCSSModuleFile(name):
				out.cssModules = append(out.cssModules, joined)
			case isCSSFile(name):
				out.css = append(out.css, joined)
			case isJSONFile(name):
				out.json = append(out.json, joined)
			case isAssetFile(name):
				out.assets = append(out.assets, joined)
			}
		}
		for _, sub := range subdirs {
			subRel := filepath.ToSlash(filepath.Join(rel, sub))
			if skipRolledUpDir(sub) {
				continue
			}
			if isConfiguredExclude(sub, excludes) || isConfiguredExclude(subRel, excludes) {
				continue
			}
			if stops(filepath.Join(dir, subRel)) {
				continue
			}
			walk(subRel)
		}
	}
	for _, sub := range subdirsOf(dir) {
		if skipRolledUpDir(sub) {
			continue
		}
		if isConfiguredExclude(sub, excludes) {
			continue
		}
		if stops(filepath.Join(dir, sub)) {
			continue
		}
		walk(sub)
	}
	for _, g := range [][]string{out.srcs, out.tests, out.docs, out.ambient, out.css, out.cssModules, out.assets, out.json} {
		sort.Strings(g)
	}
	return out
}

func subdirsOf(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// skipRolledUpDir names directories that are never part of a target's sources,
// whatever mode is in force.
func skipRolledUpDir(name string) bool {
	return strings.HasPrefix(name, ".") ||
		name == "node_modules" ||
		name == "dist" ||
		name == "bazel-out"
}

// dirIsItsOwnPackage reports whether dir already is, or is meant to be, a
// package: an index file makes it a boundary in index-only mode, and a BUILD
// file makes it a Bazel package regardless.
func dirIsItsOwnPackage(dir string) bool {
	return dirIsItsOwnPackageIn(boundaryIndexOnly, dir)
}

// dirIsItsOwnPackageIn answers the same question for a named boundary mode. A
// BUILD file settles it either way; what else counts is the mode's own rule.
func dirIsItsOwnPackageIn(mode string, dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if name == "BUILD" || name == "BUILD.bazel" {
			return true
		}
		if mode == boundaryTsConfig {
			if name == "tsconfig.json" {
				return true
			}
			continue
		}
		if isTypeScriptFile(name) && isIndexFile(name) {
			return true
		}
	}
	return false
}

// dirHasTsConfig reports whether dir holds the tsconfig.json that makes it a
// TypeScript project root.
func dirHasTsConfig(dir string) bool {
	st, err := os.Stat(filepath.Join(dir, "tsconfig.json"))
	return err == nil && !st.IsDir()
}

// rebaseRelative rewrites a relative specifier as written inside a file into the
// equivalent specifier from the root of the target that claims that file.
//
// Resolution happens against the target's package, not against the importing
// file, which is identical for a file sitting directly in that directory and
// wrong for a rolled-up one: `../utils.js` written in rules/ means src/utils,
// and read as if written in src/ it means the directory above.
func rebaseRelative(spec, dirRel string) string {
	if dirRel == "" || dirRel == "." {
		return spec
	}
	if !strings.HasPrefix(spec, "./") && !strings.HasPrefix(spec, "../") {
		return spec
	}
	joined := path.Join(dirRel, spec)
	if joined == "" || joined == "." {
		return spec
	}
	// A specifier that still escapes the package after rebasing genuinely points
	// outside it, and "../" is how resolution has to keep seeing that.
	if strings.HasPrefix(joined, "../") {
		return joined
	}
	return "./" + joined
}

// importsIn returns every specifier the given files name, each rebased onto the
// directory that claims them.
func importsIn(dir string, files []string) []string {
	var all []string
	for _, f := range files {
		filePath := filepath.Join(dir, f)
		imps, err := extractImports(filePath)
		if err != nil {
			log.Printf("typescript: error reading %s: %v", filePath, err)
			continue
		}
		dirRel := path.Dir(filepath.ToSlash(f))
		for _, imp := range imps {
			all = append(all, rebaseRelative(imp, dirRel))
		}
	}
	return all
}
