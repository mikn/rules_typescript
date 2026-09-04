package typescript

import (
	"log"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

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
	// excluded names the TypeScript sources a ts_exclude pattern took out of
	// the subtree, each with the pattern that took it. Nothing downstream reads
	// it; it is what the run reports, since a file dropped here is a file
	// dropped from the program.
	excluded []excludedSrc
}

// rolledUp is everything under dir that belongs to dir's own target, TypeScript
// and otherwise: in tsconfig mode a directory holding no tsconfig.json is not a
// package, so its files belong to the project above it. Giving them their own
// target instead is what turns an ordinary shape -- a barrel re-exporting
// ./rules, and ./rules importing ../utils -- into a dependency cycle between
// two Bazel packages, when at file granularity there is no cycle at all. A
// stylesheet beside a rolled-up source is imported by it, so leaving it behind
// gives that import nothing to resolve to.
//
// The walk stops at a descendant that is a package in its own right, which
// dirIsItsOwnPackage decides, and at a directory in skip (a ts_codegen out_dir).
func rolledUp(dir string, excludes excludeSet, jsSrcExts []string, skip []string) rolledUpFiles {
	var out rolledUpFiles
	stops := func(subRel string) bool {
		return slices.Contains(skip, subRel) || dirIsItsOwnPackage(filepath.Join(dir, subRel))
	}
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
			if r, isDropped := excludes.dropsBy(joined); isDropped {
				if isCompileSrcFile(name, jsSrcExts) && !isFrameworkGeneratedFile(name) {
					out.excluded = append(out.excluded, excludedSrc{path: joined, rule: r})
				}
				continue
			}
			switch {
			case isCompileSrcFile(name, jsSrcExts):
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
			if excludes.drops(subRel) {
				continue
			}
			if stops(subRel) {
				continue
			}
			walk(subRel)
		}
	}
	for _, sub := range subdirsOf(dir) {
		if skipRolledUpDir(sub) {
			continue
		}
		if excludes.drops(sub) {
			continue
		}
		if stops(sub) {
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
// package under a rolled-up boundary: its own tsconfig.json makes it one, and a
// BUILD file makes it a Bazel package regardless.
func dirIsItsOwnPackage(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		switch e.Name() {
		case "BUILD", "BUILD.bazel", "tsconfig.json":
			return true
		}
	}
	return false
}

// dirIsRolledUpIn reports whether the boundary mode in force rolls dir's files
// into the package above it instead of making it a package of its own. Only
// tsconfig mode rolls anything up; under every-dir a directory with sources is
// a package whatever else it holds.
func dirIsRolledUpIn(mode, dir string) bool {
	return mode == boundaryTsConfig && !dirIsItsOwnPackage(dir)
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
