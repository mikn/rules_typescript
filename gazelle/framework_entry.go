package typescript

// The framework's client entry gets a single-file target of its own and leaves
// the directory-wide ts_compile: ts_bundle takes exactly one .js from
// entry_point, and two targets over one source declare the same .js, which
// Bazel rejects as conflicting actions.

import (
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bazelbuild/bazel-gazelle/language"
	"github.com/bazelbuild/bazel-gazelle/rule"
)

// entryTargetName is the target name a single-file ts_compile over file takes:
// "entry.client.tsx" -> "entry_client", "main.tsx" -> "main".
func entryTargetName(file string) string {
	stem := strings.TrimSuffix(strings.TrimSuffix(file, ".tsx"), ".ts")
	return strings.ReplaceAll(stem, ".", "_")
}

func splitPackageLabel(l string) (pkg, name string, ok bool) {
	if !strings.HasPrefix(l, "//") {
		return "", "", false
	}
	pkg, name, found := strings.Cut(strings.TrimPrefix(l, "//"), ":")
	if !found || pkg == "" || name == "" {
		return "", "", false
	}
	return pkg, name, true
}

// frameworkEntrySrc returns the source file in this directory that the detected
// framework's entry_point label names, when this directory is that label's
// package and one of its sources maps to that target name.
func frameworkEntrySrc(rel string, tc *tsConfig, srcs []string) (file, name string, ok bool) {
	target, found := frameworkEntryTargetName(rel, tc)
	if !found {
		return "", "", false
	}
	for _, src := range srcs {
		if entryTargetName(src) == target {
			return src, target, true
		}
	}
	return "", "", false
}

// frameworkEntryTargetName is the target the detected framework's entry_point
// label names, when this directory is that label's package.
func frameworkEntryTargetName(rel string, tc *tsConfig) (string, bool) {
	cfg, found := frameworkConfigs[tc.detectedFramework]
	if !found {
		return "", false
	}
	pkg, target, ok := splitPackageLabel(cfg.EntryPoint)
	if !ok || pkg != rel {
		return "", false
	}
	return target, true
}

// frameworkEntryFileExists reports whether a file mapping to the entry target
// name is still on disk, whatever compiles it -- an excluded file is still
// there, so a target over it is a decision rather than a leftover.
func frameworkEntryFileExists(files []string, name string) bool {
	for _, f := range files {
		if isTypeScriptFile(f) && !isFrameworkGeneratedFile(f) && entryTargetName(f) == name {
			return true
		}
	}
	return false
}

// reportEntryNameCollision names the arrangement that would write two rules of
// one name. It always returns false: leaving the entry in the package target
// makes entry_point name more than one .js, which ts_bundle refuses by name.
func reportEntryNameCollision(args language.GenerateArgs, tc *tsConfig, name string) bool {
	log.Printf("typescript: %s detected: the %s/ target and the bundle's client entry are both "+
		"named %q, so no separate entry target was generated -- one Bazel package cannot hold "+
		"two rules of one name. Rename the directory target with # gazelle:ts_target_name, or "+
		"declare the entry target and entry_point by hand -- entry_point is generated, so it "+
		"needs a \"# keep\" comment above it to survive the next run.",
		frameworkName(tc.detectedFramework), args.Rel, name)
	return false
}

// The pre-0.2 recipe, from before Gazelle wrote the entry target itself: the
// exclusion costs that target's deps maintenance, which strict-deps then fails.
func reportHandMaintainedEntry(args language.GenerateArgs, tc *tsConfig, excluded []string) {
	file, name, ok := frameworkEntrySrc(args.Rel, tc, excluded)
	if !ok {
		return
	}
	log.Printf("typescript: %s detected: a ts_exclude directive drops %s/%s, the bundle's client "+
		"entry, so Gazelle generates no %q target and does not maintain the one you wrote in its "+
		"place -- an import added to the entry never reaches its deps, and ts_compile's "+
		"strict-deps check fails on that import. Drop the directive and the hand-written target: "+
		"Gazelle writes the single-file entry target itself now.",
		frameworkName(tc.detectedFramework), args.Rel, file, name)
}

// frameworkEntryRule builds the single-file ts_compile the generated
// entry_point label names. Its deps come from the resolver, like any other
// generated ts_compile.
//
// Single-file except for the package's ambient declarations: nothing imports
// one, so no dep edge carries it, and splitting the entry out of the package
// target is what took the globals declared beside it out of its program.
func frameworkEntryRule(name, file string, ambient []string, tc *tsConfig) *rule.Rule {
	r := rule.NewRule("ts_compile", name)
	srcs := append([]string{file}, ambient...)
	sort.Strings(srcs)
	r.SetAttr("srcs", srcLabels(srcs))
	r.SetAttr("visibility", []string{"//visibility:public"})
	if tc.declarations != "" && tc.declarations != "tsgo" {
		r.SetAttr("declarations", tc.declarations)
	}
	r.AddComment("# " + frameworkName(tc.detectedFramework) +
		" client entry: ts_bundle takes exactly one .js from entry_point,")
	r.AddComment("# so this file is its own target rather than part of the package's.")
	return r
}

// reportMissingFrameworkEntry warns when nothing in the entry_point label's
// package will carry the target it names: a dangling entry_point fails
// `bazel build //...` for the whole workspace rather than for the bundle alone.
func reportMissingFrameworkEntry(args language.GenerateArgs, tc *tsConfig) {
	cfg, found := frameworkConfigs[tc.detectedFramework]
	if !found {
		return
	}
	pkg, target, ok := splitPackageLabel(cfg.EntryPoint)
	if !ok || entryTargetIsCovered(args, tc, pkg, target) {
		return
	}
	log.Printf("typescript: %s detected: nothing in %s/ declares the client entry target %q "+
		"-- no source file there maps to that name, or a ts_exclude directive drops it -- so no "+
		"%s bundle target was generated: entry_point %s would name nothing, and an unresolvable "+
		"label fails analysis for every target that reaches it. Add the framework's client entry "+
		"there, drop the exclusion, or declare the bundle by hand with a \"# keep\" comment "+
		"above the rule -- without one the next run that does find an entry rewrites it.",
		frameworkName(tc.detectedFramework), pkg, target, cfg.BundleName, cfg.EntryPoint)
}

// entryTargetIsCovered reports whether the entry_point label's package declares
// the target over sources still on disk, or holds one generation will claim.
func entryTargetIsCovered(args language.GenerateArgs, tc *tsConfig, pkg, target string) bool {
	dir := filepath.Join(args.Config.RepoRoot, filepath.FromSlash(pkg))
	lp := readLocalPackage(dir, pkg, tc)
	if lp.ignored {
		return false
	}
	if lp.file != nil {
		for _, r := range lp.file.Rules {
			if r.Kind() == "ts_compile" && r.Name() == target && srcsStillExist(dir, r.AttrStrings("srcs")) {
				return true
			}
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !isTypeScriptFile(name) || isFrameworkGeneratedFile(name) {
			continue
		}
		if isConfiguredExclude(name, lp.tc.excludePatterns) || isConfiguredExclude(name, lp.dropped) {
			continue
		}
		if entryTargetName(name) == target {
			return true
		}
	}
	return false
}

// srcsStillExist reports whether every named source is on disk. An empty list
// is not a target over anything.
func srcsStillExist(dir string, srcs []string) bool {
	if len(srcs) == 0 {
		return false
	}
	for _, src := range srcs {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(src))); err != nil {
			return false
		}
	}
	return true
}

// reportEntryImportCycle warns when the entry target and its package's target
// would import each other: splitting the entry out turns one directory into two
// targets, and Bazel rejects the cycle with nothing in either BUILD file to say
// where it came from.
func reportEntryImportCycle(
	args language.GenerateArgs,
	tc *tsConfig,
	entryFile, entryName string,
	srcFiles, packageImports []string,
) {
	sibling := sameDirImportOf(packageImports, func(name string) bool { return name == entryName })
	if sibling == "" {
		return
	}
	back := sameDirImportOf(importsIn(args.Dir, []string{entryFile}), func(name string) bool {
		for _, src := range srcFiles {
			if entryTargetName(src) == name {
				return true
			}
		}
		return false
	})
	if back == "" {
		return
	}
	log.Printf("typescript: %s detected: %s/%s is its own target for the bundle's entry_point, "+
		"but the package imports it (%q) while it imports the package back (%q) -- the two "+
		"targets would be a dependency cycle. Move what they share into a third file, or "+
		"declare the entry target by hand.",
		frameworkName(tc.detectedFramework), args.Rel, entryFile, sibling, back)
}

// sameDirImportOf returns the first specifier naming a file in its own
// directory whose target name the predicate accepts.
func sameDirImportOf(imports []string, match func(name string) bool) string {
	for _, imp := range imports {
		rest, ok := strings.CutPrefix(imp, "./")
		if !ok || rest == "" || strings.Contains(rest, "/") {
			continue
		}
		stem := strings.TrimSuffix(strings.TrimSuffix(rest, ".jsx"), ".js")
		if match(entryTargetName(stem)) {
			return imp
		}
	}
	return ""
}
