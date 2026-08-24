package main

import (
	"regexp"
	"strings"

	"github.com/mikn/rules_typescript/tests/integration/harness"
)

const (
	greetingSrc = "src/lib/greeting.ts"
	goodSig     = "export function greet(name: string): string {"
	badSig      = "export function greet(name: string): number {"
	typeError   = "Type error: Type 'string' is not assignable to type 'number'."
)

var greetingLocation = regexp.MustCompile(`^\./src/lib/greeting\.ts:[0-9]+:[0-9]+$`)

// Next prints the offending file and position, then the diagnostic on a line
// below; matching them together pins the error to the file that was edited.
func reportsTypeErrorInGreeting(log *harness.Log) bool {
	lines := log.Lines()
	for i, line := range lines {
		if !greetingLocation.MatchString(strings.TrimRight(line, "\r")) {
			continue
		}
		for _, following := range lines[i+1 : min(i+3, len(lines))] {
			if strings.Contains(following, typeError) {
				return true
			}
		}
	}
	return false
}

func main() {
	harness.Run(harness.Config{
		Name:         "nextjs",
		WorkspaceRel: "tests/integration/nextjs",
		Lockfile:     "examples/nextjs-app/pnpm-lock.yaml",
	}, func(it *harness.IT) {
		it.MustBazel("build", "//:app")
		it.Pass("bazel build //:app")

		nextOut := it.Bin("app_next_out")
		it.RequireDir(nextOut, "next_build output directory not found: app_next_out/")
		it.Pass("app_next_out/ directory exists")

		for _, subdir := range []string{"server", "static"} {
			it.RequireDir(it.Bin("app_next_out", subdir), ".next/%s/ not found in output", subdir)
			it.Pass(".next/%s/ exists", subdir)
		}

		it.RequireFile(it.Bin("app_next_out", "BUILD_ID"), ".next/BUILD_ID not found")
		it.Pass(".next/BUILD_ID exists")

		// .next/cache/ is non-hermetic and would pollute the remote cache.
		it.RequireNoDir(it.Bin("app_next_out", "cache"), ".next/cache/ must be excluded from output (non-hermetic)")
		it.Pass(".next/cache/ correctly excluded from output")

		it.RequireNoDir(it.Bin("app_next_out", "_staging"), "_staging/ must be cleaned up from output")
		it.Pass("_staging/ correctly absent from output")

		for _, route := range []string{"app/page.js", "app/about/page.js"} {
			it.RequireFile(it.Bin("app_next_out", "server", route), "compiled route missing: .next/server/%s", route)
			it.Pass(".next/server/%s exists", route)
		}

		// `next build` folds greet("Bazel") into the prerendered route, so this
		// exact string is only there if the staged greeting.ts was compiled.
		it.RequireContains(it.Bin("app_next_out", "server/app/page.js"), "Hello, Bazel!",
			"'Hello, Bazel!' not in .next/server/app/page.js — staging_srcs was not compiled into the route")
		it.Pass("greet() output compiled into .next/server/app/page.js (staging_srcs works)")

		// This workspace has no ts_compile target: the only type-checking is the
		// one `next build` runs itself, with the staged tsconfig.json.
		it.Replace(it.Path(greetingSrc), goodSig, badSig)

		typeLog, err := it.BazelLog("type_error.log", "build", "//:app")
		if err == nil {
			typeLog.DumpTail(80)
			it.Fail("//:app built with a type error in a staged source; next build is not type-checking")
		}
		it.Pass("//:app failed to build once a staged source had a type error")

		// Exiting non-zero is not the assertion: a missing toolchain, a bad label
		// or a fetch failure does that too.
		if !typeLog.Contains("INFO: Analyzed target //:app") {
			typeLog.DumpTail(80)
			it.Fail("//:app never got past analysis — the failure is not next build's type check")
		}
		it.Pass("the failure happened during execution, not loading or analysis")

		if !typeLog.Contains("NextBuild //:app failed") {
			typeLog.DumpTail(80)
			it.Fail("no failing NextBuild action for //:app — something else broke the build")
		}
		if !typeLog.Contains("Failed to compile.") {
			typeLog.DumpTail(80)
			it.Fail("next build did not report a compile failure")
		}
		it.Pass("//:app's NextBuild action is what failed")

		if !reportsTypeErrorInGreeting(typeLog) {
			typeLog.DumpTail(80)
			it.Fail("no 'string is not assignable to number' type error reported in ./src/lib/greeting.ts")
		}
		it.Pass("type error reported in src/lib/greeting.ts with the expected message")

		for _, pattern := range []string{
			"ERROR: Analysis of target",
			"no such target",
			"no such package",
			"error loading package",
			"no matching toolchains",
			"Error in fail",
			"Error downloading",
			"Failed to fetch",
			"No space left on device",
			"command not found",
		} {
			if typeLog.Contains(pattern) {
				typeLog.DumpTail(80)
				it.Fail("build log contains '%s' — the build broke for an unrelated reason", pattern)
			}
		}
		it.Pass("no toolchain, loading, analysis or fetch error in the failing build")

		it.Replace(it.Path(greetingSrc), badSig, goodSig)
		restored, err := it.BazelLog("restored.log", "build", "//:app")
		if err != nil {
			restored.DumpTail(80)
			it.Fail("//:app does not build after reverting the type error — the earlier failure was not caused by the edit")
		}
		it.Pass("//:app builds again once the type error is reverted")
	})
}
