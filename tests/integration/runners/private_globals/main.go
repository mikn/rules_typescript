package main

import (
	"github.com/mikn/rules_typescript/tests/integration/harness"
)

func main() {
	harness.Run(harness.Config{
		Name:         "private_globals",
		WorkspaceRel: "tests/integration/private_globals/workspace",
		Renames:      map[string]string{"BUILD.bazel.tpl": "BUILD.bazel"},
	}, func(it *harness.IT) {
		if err := it.Bazel("build", "//:lib"); err != nil {
			it.Fail("the library did not build; a withheld ambient must still type the target that owns it")
		}
		it.Pass("a withheld ambient still types its own target")

		if err := it.Bazel("build", "//:app"); err != nil {
			it.Fail("a consumer of the library's public global did not build; only the named src is withheld")
		}
		it.Pass("the global that was not named still reaches a consumer")

		withheld, err := it.BazelLog("withheld.log", "build", "//:withheld")
		if err == nil {
			withheld.Dump()
			it.Fail("a consumer resolved `process`, which private_globals withholds from it")
		}
		it.Pass("the withheld global is not in a consumer's scope")

		if !withheld.Matches(`(?i)TS2304|cannot find name 'process'`) {
			withheld.Dump()
			it.Fail("the consumer failed, but not on the undefined identifier")
		}
		it.Pass("the consumer's failure names the identifier")

		lying, err := it.BazelLog("lying.log", "build", "//:lying")
		if err == nil {
			lying.Dump()
			it.Fail("private_globals over a module-scoped .d.ts built; it withholds nothing and must say so")
		}
		it.Pass("private_globals over a module-scoped .d.ts is rejected")

		if !lying.Matches(`private_globals names`) {
			lying.Dump()
			it.Fail("the build failed, but not on the private_globals claim")
		}
		it.Pass("the failure names the attribute that is wrong")
	})
}
