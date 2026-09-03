package worker_types_test

import (
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

func TestGeneratedDeclarations(t *testing.T) {
	tree := verify.New(t)
	tree.File("tests/worker_types/worker-configuration.d.ts").Contains(
		"interface Env extends __BaseEnv_Env {}",
		"CACHE: KVNamespace;",
		"GREETING: string;",
		"// Begin runtime types",
		"interface KVNamespace",
	)
}
