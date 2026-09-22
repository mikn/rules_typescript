package top_level_await_test

import (
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

const pkg = "tests/compiler_options/top_level_await/"

// tsc keeps a top-level await under module esnext and target es2020 and never
// lowers it; the es2021 logical assignment beside it is lowered to the target.
func TestTopLevelAwaitIsKeptAndTheTargetStillLowers(t *testing.T) {
	js := verify.New(t).File(pkg + "awaits.js")
	js.MatchesRE(
		`(?m)^export const settled = await Promise\.resolve\("settled"\);$`)
	js.Excludes("||=")
	js.Contains("fallback || (fallback = settled)")
}
