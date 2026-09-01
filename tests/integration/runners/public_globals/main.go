package main

import (
	"github.com/mikn/rules_typescript/tests/integration/harness"
)

func main() {
	harness.Run(harness.Config{
		Name:         "public_globals",
		WorkspaceRel: "tests/integration/public_globals/workspace",
		Renames:      map[string]string{"BUILD.bazel.tpl": "BUILD.bazel"},
	}, func(it *harness.IT) {
		if err := it.Bazel("build", "//:lib"); err != nil {
			it.Fail("the library did not build; an unexported ambient must still type the target that owns it")
		}
		it.Pass("an unexported ambient still types its own target")

		if err := it.Bazel("build", "//:app"); err != nil {
			it.Fail("a consumer of the library's exported global did not build; public_globals names that src")
		}
		it.Pass("the global public_globals names reaches a consumer")

		unexported, err := it.BazelLog("unexported.log", "build", "//:unexported")
		if err == nil {
			unexported.Dump()
			it.Fail("a consumer resolved `process`, which no public_globals entry exports to it")
		}
		it.Pass("an ambient nobody exported is not in a consumer's scope")

		if !unexported.Matches(`(?i)TS2304|cannot find name 'process'`) {
			unexported.Dump()
			it.Fail("the consumer failed, but not on the undefined identifier")
		}
		it.Pass("the consumer's failure names the identifier")

		lyingPublic, err := it.BazelLog("lying_public.log", "build", "//:lying_public")
		if err == nil {
			lyingPublic.Dump()
			it.Fail("public_globals over a module-scoped .d.ts built; a module has no globals to export")
		}
		it.Pass("public_globals over a module-scoped .d.ts is rejected")

		if !lyingPublic.Matches(`public_globals names`) {
			lyingPublic.Dump()
			it.Fail("the build failed, but not on the public_globals claim")
		}
		it.Pass("the failure names the attribute that is wrong")

		if err := it.Bazel("build", "//:vite_lib"); err != nil {
			it.Fail("vite_types did not type its own target; the shim is a src of it")
		}
		it.Pass("vite_types types the target that sets it")

		viteConsumer, err := it.BazelLog("vite_consumer.log", "build", "//:vite_consumer")
		if err == nil {
			viteConsumer.Dump()
			it.Fail("a consumer resolved `import.meta.env`, which the dep's vite_types shim does not export to it")
		}
		it.Pass("the vite_types shim is not in a consumer's scope")

		if !viteConsumer.Matches(`(?i)TS2339|Property 'env' does not exist`) {
			viteConsumer.Dump()
			it.Fail("the consumer failed, but not on the missing ImportMeta member")
		}
		it.Pass("the consumer's failure names the member ImportMeta is missing")

		if err := it.Bazel("build", "//:vite_consumer_typed"); err != nil {
			it.Fail("the same consumer with vite_types of its own did not build; that is the migration")
		}
		it.Pass("a consumer that sets vite_types itself builds")
	})
}
