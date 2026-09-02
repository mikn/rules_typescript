package main

import (
	"github.com/mikn/rules_typescript/tests/integration/harness"
)

func main() {
	harness.Run(harness.Config{
		Name:         "untyped_packages",
		WorkspaceRel: "tests/integration/untyped_packages",
		Lockfile:     "tests/workers/pnpm-lock.yaml",
		Renames:      map[string]string{"BUILD.bazel.tpl": "BUILD.bazel"},
	}, func(it *harness.IT) {
		if err := it.Bazel("build", "//:no_import"); err != nil {
			it.Fail("the control did not build; nothing here loads a module and Element.append is lib.dom's")
		}
		it.Pass("the same dep and the same DOM call build with nothing loading a module")

		leaks, err := it.BazelLog("leaks.log", "build", "//:leaks")
		if err == nil {
			leaks.Dump()
			it.Fail("one import() more built; wrangler's declarations pull in a global script that widens Element")
		}
		it.Pass("a dynamic import of wrangler alone breaks an unrelated DOM call")

		if !leaks.Matches(`(?i)TS2769|TS2345|ReadableStream`) {
			leaks.Dump()
			it.Fail("it failed, but not on the widened Element.append")
		}
		it.Pass("the failure names the signature the global script merged in")

		if err := it.Bazel("build", "//:excluded"); err != nil {
			it.Fail("untyped_packages did not keep the global script out; the source is the same one that just failed")
		}
		it.Pass("untyped_packages on the global-script package builds the same source")

		noShim, err := it.BazelLog("no_shim.log", "build", "//:no_shim")
		if err == nil {
			noShim.Dump()
			it.Fail("excluding a package this target imports itself built; the specifier has no key to resolve through")
		}
		it.Pass("an import of an excluded package resolves to nothing")

		if !noShim.Matches(`(?i)TS2307|Cannot find module 'wrangler'`) {
			noShim.Dump()
			it.Fail("it failed, but not on the unresolved specifier")
		}
		it.Pass("the failure is the unresolved specifier, not a widening")

		if err := it.Bazel("build", "//:shimmed"); err != nil {
			it.Fail("a `declare module` src did not answer the import the exclusion left unresolved")
		}
		it.Pass("a declare module src composes with the exclusion")

		reachable, err := it.BazelLog("reachable.log", "build", "//:reachable_declared")
		if err == nil {
			reachable.Dump()
			it.Fail("an import of a reachable-only package built; strict deps should have named it")
		}
		if !reachable.Matches(`(?i)strict|add "?@npm//:cloudflare_workers-types`) {
			reachable.Dump()
			it.Fail("it failed, but not on strict deps naming the dep to add")
		}
		it.Pass("without the attribute, strict deps names the reachable dep and what to add")

		untypedReachable, err := it.BazelLog("reachable_untyped.log", "build", "//:reachable_untyped")
		if err == nil {
			untypedReachable.Dump()
			it.Fail("excluding a package this target imports itself built")
		}
		if untypedReachable.Matches(`(?i)add "?@npm//:cloudflare_workers-types`) {
			untypedReachable.Dump()
			it.Fail("strict deps still told the reader to add a dep the exclusion would ignore")
		}
		if !untypedReachable.Matches(`(?i)TS2307`) {
			untypedReachable.Dump()
			it.Fail("it failed, but not on the unresolved specifier")
		}
		it.Pass("the exclusion drops it from the reachable set too, so the answer is TS2307")

		typo, err := it.BazelLog("typo.log", "build", "//:typo")
		if err == nil {
			typo.Dump()
			it.Fail("an untyped_packages entry matching no package in the closure built, keeping nothing out")
		}
		it.Pass("an entry that matches nothing is refused")

		if !typo.Matches(`no dep of`) {
			typo.Dump()
			it.Fail("it failed, but not on the unmatched name")
		}
		it.Pass("the failure names the entry and what it could have meant")
	})
}
