package main

import (
	"github.com/mikn/rules_typescript/tests/integration/harness"
)

func main() {
	harness.Run(harness.Config{
		Name:         "oxc_declarations",
		WorkspaceRel: "tests/integration/oxc_declarations",
	}, func(it *harness.IT) {
		it.MustBazel("run", "//:gazelle")
		it.Pass("bazel run //:gazelle")

		for _, dir := range []string{"src/lib", "src/bad"} {
			it.RequireFile(it.Path(dir, "BUILD.bazel"), "Gazelle did not generate %s/BUILD.bazel", dir)
		}

		for _, dir := range []string{"src/lib", "src/bad"} {
			build := it.Path(dir, "BUILD.bazel")
			if it.Contains(build, "declarations") {
				it.Dump(build)
				it.Fail("%s/BUILD.bazel names the emitter; it is the build's --//ts:declarations flag", dir)
			}
			it.Pass("%s/BUILD.bazel carries no declarations attribute", dir)
		}

		// --output_groups=+_validation is explicit, not in the .bazelrc: the next
		// step deliberately builds WITHOUT it.
		err := it.Bazel("build", "//src/lib:all",
			"--output_groups=+_validation,+declarations")
		if err != nil {
			it.Fail("annotated target failed to build or type-check under --//ts:declarations=oxc")
		}
		it.Pass("annotated target builds and type-checks under --//ts:declarations=oxc")

		dts := it.Bin("src/lib/annotated.d.ts")
		it.RequireFile(dts, "oxc did not emit src/lib/annotated.d.ts")
		it.RequireContains(dts, "RegExp", "annotated.d.ts lost the RegExp type")
		it.Pass("oxc emitted annotated.d.ts with its declared types")

		bad, err := it.BazelLog("bad.log", "build", "//src/bad:all")
		if err == nil {
			bad.Dump()
			if badDTS := it.Bin("src/bad/inferred.d.ts"); it.Exists(badDTS) {
				it.Dump(badDTS)
			}
			it.Fail("un-annotated exports built under --//ts:declarations=oxc; they must be rejected, never widened")
		}
		it.Pass("un-annotated exports were rejected under --//ts:declarations=oxc")

		if !bad.Matches(`(?i)isolated declarations|TS901[0-9]|TS90[0-9][0-9]`) {
			bad.Dump()
			it.Fail("build failed, but not with an isolated-declarations diagnostic")
		}
		it.Pass("failure names the isolated-declarations problem")

		// _validation alone is TsgoCheck; the default build's oxc emit rejects
		// the same export first and would hide the check's answer.
		expando, err := it.BazelLog("expando.log", "build",
			"//src/expando:all", "--output_groups=_validation")
		if err == nil {
			expando.Dump()
			it.Fail("TsgoCheck passed //src/expando; its export is " +
				"un-annotated, so --//ts:declarations=oxc must reject it")
		}
		if expando.Contains("panic:") {
			expando.Dump()
			it.Fail("the compiler crashed on //src/expando instead of " +
				"reporting its un-annotated export")
		}
		if !expando.Contains("TS9013") {
			expando.Dump()
			it.Fail("TsgoCheck failed //src/expando, but not with TS9013")
		}
		it.Pass("TsgoCheck reports //src/expando as TS9013 under the " +
			"source-built compiler, where the 7.0.2 release crashes")
	})
}
