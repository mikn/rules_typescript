package typeroots_test

import (
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

type actionConfig struct {
	CompilerOptions map[string]any `json:"compilerOptions"`
}

// The chain's typeRoots bounds automatic inclusion, so the written config
// names no `types` and sets no `typeRoots` of its own.
func TestWrittenConfigLeavesTypesToTheChainsTypeRoots(t *testing.T) {
	var config actionConfig
	verify.New(t).File("tests/npm/type_roots/probe.tsconfig.json").JSON(&config)
	for _, key := range []string{"types", "typeRoots"} {
		if value, ok := config.CompilerOptions[key]; ok {
			t.Errorf("compilerOptions.%s = %v, want unset", key, value)
		}
	}
}
