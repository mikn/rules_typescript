package typescript

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// What a directory's package.json says each specifier into it means.
//
// A directory carrying a manifest is entered through what that manifest
// declares, which is index.ts only when it happens to say so. Falling straight
// through to index.ts answers the packages whose entry is an index at the root
// and answers every `exports` subpath with a file that is not there -- the same
// gap npm/private/member_paths.bzl closes on the Bazel side, here on the side
// that has to name the target in the first place.

// exportConditions are the keys a lookup descends into, kept to _TYPE_CONDITIONS
// in npm/private/npm_import.bzl. A resolver tries a manifest's conditions in the
// order they are written; Go's decoder does not keep that order, so this fixed
// order stands in for it -- which costs nothing here, where every condition of a
// workspace member names a file in the same Bazel target.
var exportConditions = []string{"types", "typings", "node", "import", "require", "default"}

// entryFields are the fields a package with no `exports` publishes through, in
// tsc's own order, kept to _ENTRY_FIELDS in npm/private/member_paths.bzl.
// `module` is absent from both: no TypeScript resolution mode reads it.
var entryFields = []string{"typings", "types", "main"}

const exportWalkSteps = 64

type packageManifest struct {
	Name    string `json:"name"`
	Main    string `json:"main"`
	Types   string `json:"types"`
	Typings string `json:"typings"`
	Exports any    `json:"exports"`
}

func (m *packageManifest) field(name string) string {
	switch name {
	case "typings":
		return m.Typings
	case "types":
		return m.Types
	case "main":
		return m.Main
	}
	return ""
}

// manifestCache keys on the absolute directory, and caches the absence of a
// manifest too: the resolver asks about every directory a specifier could name,
// and most of them hold none.
var manifestCache sync.Map

// readPackageManifest returns dirRel's package.json, or nil when it has none.
func readPackageManifest(repoRoot, dirRel string) *packageManifest {
	dir := filepath.Join(repoRoot, filepath.FromSlash(dirRel))
	if cached, ok := manifestCache.Load(dir); ok {
		manifest, _ := cached.(*packageManifest)
		return manifest
	}
	var manifest *packageManifest
	if data, err := os.ReadFile(filepath.Join(dir, "package.json")); err == nil {
		var parsed packageManifest
		if json.Unmarshal(data, &parsed) == nil {
			manifest = &parsed
		}
	}
	manifestCache.Store(dir, manifest)
	return manifest
}

// entryModuleExtensions are the extensions a manifest target can carry, longest
// first. A manifest written for a published build names the declaration or the
// emitted .js; both strip to the module path the rule index is keyed on.
var entryModuleExtensions = []string{".d.ts", ".d.mts", ".d.cts", ".tsx", ".ts", ".mjs", ".cjs", ".js"}

// packageEntryModules returns the workspace-relative module paths dirRel's
// manifest designates for subpath -- "" for the package itself, "/wire" for a
// subpath export -- in the order a resolver tries them.
func packageEntryModules(repoRoot, dirRel, subpath string) []string {
	manifest := readPackageManifest(repoRoot, dirRel)
	if manifest == nil {
		return nil
	}

	var targets []string
	if subpath == "" {
		targets = exportTargets(rootExport(manifest.Exports))
		if len(targets) == 0 {
			for _, name := range entryFields {
				if value := manifest.field(name); value != "" {
					targets = []string{value}
					break
				}
			}
		}
	} else {
		targets = subpathExportTargets(manifest.Exports, "."+subpath)
	}

	var modules []string
	seen := make(map[string]struct{})
	for _, target := range targets {
		rel := strings.TrimPrefix(target, "./")
		if rel == "" || strings.HasPrefix(rel, "../") || strings.Contains(rel, "*") {
			continue
		}
		module := path.Join(dirRel, entryModuleBase(rel))
		if _, dup := seen[module]; dup {
			continue
		}
		seen[module] = struct{}{}
		modules = append(modules, module)
	}
	return modules
}

// entryModuleBase drops the extension a manifest target carries, leaving the
// path importsForRule indexes a source under. An extension no compiler emits
// from -- a .css, an asset -- is indexed with it and keeps it.
func entryModuleBase(rel string) string {
	for _, ext := range entryModuleExtensions {
		if strings.HasSuffix(rel, ext) {
			return rel[:len(rel)-len(ext)]
		}
	}
	return rel
}

// rootExport is the `exports` subtree describing the package's own entry point.
// A map with no subpath keys IS that subtree -- npm's shorthand for a package
// that exports nothing but itself.
func rootExport(exports any) any {
	switch e := exports.(type) {
	case string:
		return e
	case map[string]any:
		if root, ok := e["."]; ok {
			return root
		}
		for key := range e {
			if strings.HasPrefix(key, ".") {
				return nil
			}
		}
		return e
	}
	return nil
}

// subpathExportTargets is every file the `exports` map designates for one
// subpath key, with a pattern key's `*` substituted.
func subpathExportTargets(exports any, key string) []string {
	e, ok := exports.(map[string]any)
	if !ok {
		return nil
	}
	if node, ok := e[key]; ok {
		return exportTargets(node)
	}
	pattern, wildcard, ok := matchExportPattern(e, key)
	if !ok {
		return nil
	}
	targets := exportTargets(e[pattern])
	for i, target := range targets {
		targets[i] = strings.Replace(target, "*", wildcard, 1)
	}
	return targets
}

// matchExportPattern picks the pattern key that claims a subpath, and what its
// `*` stands for. Node's own rule: the longest base before the `*` wins, and a
// longer suffix after it breaks a tie.
func matchExportPattern(exports map[string]any, key string) (pattern, wildcard string, ok bool) {
	keys := make([]string, 0, len(exports))
	for candidate := range exports {
		keys = append(keys, candidate)
	}
	sort.Strings(keys)

	var bestBase, bestSuffix string
	for _, candidate := range keys {
		base, suffix, found := strings.Cut(candidate, "*")
		if !found || strings.Contains(suffix, "*") || !strings.HasPrefix(candidate, "./") {
			continue
		}
		if !strings.HasPrefix(key, base) || !strings.HasSuffix(key, suffix) ||
			len(key) < len(base)+len(suffix) {
			continue
		}
		if ok && (len(base) < len(bestBase) ||
			(len(base) == len(bestBase) && len(suffix) <= len(bestSuffix))) {
			continue
		}
		pattern, bestBase, bestSuffix, ok = candidate, base, suffix, true
	}
	if !ok {
		return "", "", false
	}
	return pattern, key[len(bestBase) : len(key)-len(bestSuffix)], true
}

// exportTargets is every file an `exports` subtree can designate, in the order a
// resolver tries them. Children go to the front, so a condition's whole subtree
// is tried before the next condition is looked at.
func exportTargets(node any) []string {
	pending := []any{node}
	var targets []string
	for steps := 0; steps < exportWalkSteps && len(pending) > 0; steps++ {
		head := pending[0]
		pending = pending[1:]
		switch value := head.(type) {
		case string:
			targets = append(targets, value)
		case []any:
			pending = append(append([]any{}, value...), pending...)
		case map[string]any:
			var children []any
			for _, condition := range exportConditions {
				if child, ok := value[condition]; ok {
					children = append(children, child)
				}
			}
			pending = append(children, pending...)
		}
	}
	return targets
}
