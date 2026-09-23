package typeroots_test

import (
	"strings"
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

type actionConfig struct {
	CompilerOptions map[string]any `json:"compilerOptions"`
}

func TestWrittenConfigPreservesCustomTypeRootsWithoutImplicitTypes(t *testing.T) {
	var config actionConfig
	verify.New(t).File("tests/npm/type_roots/probe.tsconfig.json").JSON(&config)
	if value, ok := config.CompilerOptions["types"]; ok {
		t.Errorf("compilerOptions.types = %v, want no implicit type package inclusion", value)
	}
	roots, _ := config.CompilerOptions["typeRoots"].([]any)
	if len(roots) != 1 {
		t.Fatalf("typeRoots = %v, want the one source configuration root", roots)
	}
	root, _ := roots[0].(string)
	if !strings.HasPrefix(root, "../") || !strings.HasSuffix(root, "/tests/npm/type_roots/node_modules/@types") {
		t.Errorf("typeRoots = %q, want the native resolved source-package root", root)
	}
}
