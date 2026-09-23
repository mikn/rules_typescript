package typescript

import (
	"context"
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/bazelbuild/bazel-gazelle/label"
	"github.com/bazelbuild/bazel-gazelle/rule"
)

type emissionGraph struct {
	roots   map[string]bool
	outputs map[string]bool
	rules   map[string]*rule.Rule
	files   map[string]*rule.File
	lock    *npmLock
}

func emissionLabel(pkg, name string) string {
	if name == "" {
		return ""
	}
	l, err := label.Parse(name)
	if err != nil {
		return ""
	}
	return l.Abs("", pkg).String()
}

func (s *programStore) recordEmissionConsumers(root, pkg string, f *rule.File) {
	if s.emission == nil {
		s.emission = &emissionGraph{roots: map[string]bool{}, outputs: map[string]bool{}, rules: map[string]*rule.Rule{}, files: map[string]*rule.File{}}
	}
	g := s.emission
	if f != nil {
		g.files[pkg] = f
		for _, r := range f.Rules {
			g.rules[emissionLabel(pkg, ":"+r.Name())] = r
		}
	}
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(pkg), "package.json"))
	if err != nil {
		return
	}
	var manifest map[string]any
	if json.Unmarshal(raw, &manifest) != nil {
		return
	}
	var recordEntry func(any)
	recordEntry = func(v any) {
		switch x := v.(type) {
		case string:
			for _, suffix := range []string{".js", ".jsx", ".mjs", ".cjs", ".d.ts", ".d.mts", ".d.cts"} {
				if strings.HasSuffix(x, suffix) {
					g.outputs[path.Join(pkg, x)] = true
					break
				}
			}
		case map[string]any:
			for _, entry := range x {
				recordEntry(entry)
			}
		case []any:
			for _, entry := range x {
				recordEntry(entry)
			}
		}
	}
	for _, key := range []string{"main", "module", "types", "typings", "exports", "bin"} {
		recordEntry(manifest[key])
	}
}

func (l *tsLang) AfterResolvingDeps(_ context.Context) {
	if l.programs == nil || l.programs.emission == nil {
		return
	}
	g := l.programs.emission
	for pkg, f := range g.files {
		for _, r := range f.Rules {
			g.rules[emissionLabel(pkg, ":"+r.Name())] = r
		}
	}
	outputOwners := map[string]string{}
	for pkg, files := range l.programs.packages {
		for source := range files {
			if isDeclarationFile(source) || l.programs.owner(source) != pkg {
				continue
			}
			ext := path.Ext(source)
			var outputs []string
			switch ext {
			case ".ts":
				outputs = []string{".js", ".d.ts"}
			case ".tsx":
				outputs = []string{".js", ".jsx", ".d.ts"}
			case ".mts":
				outputs = []string{".mjs", ".d.mts"}
			case ".cts":
				outputs = []string{".cjs", ".d.cts"}
			case ".js":
				outputs = []string{".d.ts"}
			case ".mjs":
				outputs = []string{".d.mts"}
			case ".cjs":
				outputs = []string{".d.cts"}
			}
			for _, suffix := range outputs {
				outputOwners[strings.TrimSuffix(source, ext)+suffix] = emissionLabel(pkg, ":"+packageName(pkg))
			}
		}
	}
	for output := range g.outputs {
		if owner := outputOwners[output]; owner != "" {
			g.roots[owner] = true
		}
		if strings.Contains(output, "*") {
			for file, owner := range outputOwners {
				if matchesExportOutput(output, file) {
					g.roots[owner] = true
				}
			}
		}
	}
	members := map[string]string{}
	if g.lock != nil {
		for name, dir := range g.lock.members {
			members["@npm//:"+npmPackageToLabelName(name)] = emissionLabel(dir, ":"+packageName(dir))
		}
	}
	nodeRunner := func(key string) bool {
		seen := map[string]bool{}
		for key != "" && !seen[key] {
			seen[key] = true
			if strings.HasSuffix(key, "//ts/runners:node_test") {
				return true
			}
			r := g.rules[key]
			if r == nil || r.Kind() != "alias" {
				return false
			}
			own, err := label.Parse(key)
			if err != nil {
				return false
			}
			key = emissionLabel(own.Pkg, r.AttrString("actual"))
		}
		return false
	}
	for key, r := range g.rules {
		own, err := label.Parse(key)
		if err != nil {
			continue
		}
		if r.Kind() == "ts_binary" {
			g.roots[emissionLabel(own.Pkg, r.AttrString("entry_point"))] = true
		}
		if r.Kind() == "ts_test" && (r.AttrString("wrangler_config") != "" || nodeRunner(emissionLabel(own.Pkg, r.AttrString("runner")))) {
			g.roots[key] = true
		}
	}
	seen := map[string]bool{}
	var visit func(string)
	visit = func(key string) {
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		if member := members[key]; member != "" {
			visit(member)
			return
		}
		r := g.rules[key]
		if r == nil {
			return
		}
		own, err := label.Parse(key)
		if err != nil {
			return
		}
		if r.Kind() == "ts_compile" || r.Kind() == "ts_test" {
			if !r.ShouldKeep() && !attrKept(r, "emit") {
				r.SetAttr("emit", true)
			}
		}
		for _, dep := range r.AttrStrings("deps") {
			visit(emissionLabel(own.Pkg, dep))
		}
		for _, attr := range []string{"member", "target", "actual"} {
			visit(emissionLabel(own.Pkg, r.AttrString(attr)))
		}
	}
	for root := range g.roots {
		visit(root)
	}
}

// Node replaces every RHS star with the same subpath, including slashes: https://nodejs.org/api/packages.html#subpath-patterns.
func matchesExportOutput(pattern, file string) bool {
	stars := strings.Count(pattern, "*")
	if stars == 0 {
		return pattern == file
	}
	fixed := len(pattern) - stars
	width := len(file) - fixed
	if width < 0 || width%stars != 0 {
		return false
	}
	start := strings.IndexByte(pattern, '*')
	capture := file[start : start+width/stars]
	return strings.ReplaceAll(pattern, "*", capture) == file
}
