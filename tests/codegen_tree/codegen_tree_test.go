// A ts_codegen tree consumed by a ts_compile, checked at the boundary the two
// share: the consumer's declaration is what the tree's types produced, so a
// tree that never reached the program cannot pass.
package codegen_tree_test

import (
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

func TestTheTreeHoldsFilesOnlyTheGeneratorNamed(t *testing.T) {
	tree := verify.New(t)
	dir := tree.Dir("tests/codegen_tree/compiled")
	dir.File("messages/greeting.d.ts").Contains("greeting: (count: number) => string")
	dir.File("index.d.ts").Contains("./messages/greeting.js", "./messages/farewell.js")
}

func TestTheConsumerTypeChecksAgainstTheTree(t *testing.T) {
	tree := verify.New(t)
	tree.File("tests/codegen_tree/consumer.d.ts").
		Contains("shout(count: number): string")
}
