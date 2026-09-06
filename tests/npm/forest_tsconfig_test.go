package npm_test

import (
	"reflect"
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

type actionConfig struct {
	CompilerOptions map[string]any `json:"compilerOptions"`
	Include         []string       `json:"include"`
	Files           []string       `json:"files"`
}

// The tsconfig the tsgo action reads names no npm package: bare specifiers
// resolve through node_modules alone, `types` is the direct @types deps, and
// typeRoots stays unset so that tsgo's own roots and its node_modules walk
// answer a `types` entry the way they do outside Bazel.
func TestWrittenConfigNamesNoPackage(t *testing.T) {
	tree := verify.New(t)
	for _, c := range []struct {
		target string
		types  []string
	}{
		{"app", []string{}},
		{"direct_types_probe", []string{"node"}},
		{"transitive_types_probe", []string{}},
	} {
		var config actionConfig
		tree.File("tests/npm/" + c.target + ".tsconfig.json").JSON(&config)
		opts := config.CompilerOptions
		if _, ok := opts["paths"]; ok {
			t.Errorf("%s: compilerOptions.paths = %v, want none: the forest answers every bare specifier", c.target, opts["paths"])
		}
		if _, ok := opts["typeRoots"]; ok {
			t.Errorf("%s: compilerOptions.typeRoots = %v, want unset", c.target, opts["typeRoots"])
		}
		got, _ := opts["types"].([]any)
		types := make([]string, 0, len(got))
		for _, entry := range got {
			types = append(types, entry.(string))
		}
		if !reflect.DeepEqual(types, c.types) {
			t.Errorf("%s: compilerOptions.types = %v, want %v", c.target, types, c.types)
		}
		if opts["preserveSymlinks"] != true {
			t.Errorf("%s: preserveSymlinks = %v, want true", c.target, opts["preserveSymlinks"])
		}
		if len(config.Files) != 0 {
			t.Errorf("%s: files = %v, want []: a global reaches the program through `types`", c.target, config.Files)
		}
		if len(config.Include) != 1 {
			t.Errorf("%s: include = %v, want the one src", c.target, config.Include)
		}
	}
}
