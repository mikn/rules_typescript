package main

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mikn/rules_typescript/tests/integration/harness"
)

// The fixture's one node.toolchain() call, removed for the last case.
const declaration = "node.toolchain(\n" +
	"    name = \"nodejs\",\n" +
	"    node_version_from_nvmrc = \"//:.nvmrc\",\n" +
	")\n"

var pinned = regexp.MustCompile(`node_version = "([^"]+)"`)

func nvmrc(it *harness.IT) string {
	return strings.TrimSpace(it.Read(it.Path(".nvmrc")))
}

// The version rules_typescript's own MODULE.bazel pins: what a consumer that
// names none runs under.
func rulesetPin(it *harness.IT) string {
	module := it.Read(filepath.Join(it.RulesTSRoot, "MODULE.bazel"))
	m := pinned.FindStringSubmatch(module)
	if m == nil {
		it.Fail("%s/MODULE.bazel pins no node_version", it.RulesTSRoot)
	}
	return m[1]
}

func runTest(it *harness.IT, logName string) (*harness.Log, error) {
	return it.BazelLog(logName, "test", "//:node_version_test",
		"--test_output=all")
}

// The test's own line naming the version it ran under; node's TAP reporter
// prints it as a comment, the spec reporter as written.
func versionLine(log *harness.Log) string {
	for _, line := range log.Lines() {
		if at := strings.Index(line, "process.version v"); at >= 0 {
			return line[at:]
		}
	}
	return ""
}

func mustRunUnder(
	it *harness.IT, log *harness.Log, err error, version string,
) string {
	if err != nil {
		log.Dump()
		it.Fail("//:node_version_test failed")
	}
	line := versionLine(log)
	if !strings.HasPrefix(line, "process.version v"+version+",") {
		log.Dump()
		it.Fail("the test did not run under v%s: %q", version, line)
	}
	return line
}

func main() {
	harness.Run(harness.Config{
		Name:         "node_version",
		WorkspaceRel: "tests/integration/node_version/workspace",
		Renames:      map[string]string{"BUILD.bazel.tpl": "BUILD.bazel"},
	}, func(it *harness.IT) {
		declared := nvmrc(it)
		pin := rulesetPin(it)
		if declared == pin {
			it.Fail(".nvmrc names %s, the version rules_typescript pins, "+
				"so the cases below prove nothing", pin)
		}

		log, err := runTest(it, "declared.log")
		line := mustRunUnder(it, log, err, declared)
		it.Pass("the test runs under the .nvmrc's v%s, not the ruleset's v%s: %s",
			declared, pin, line)

		it.Write(it.Path(".nvmrc"), pin+"\n")
		log, err = runTest(it, "moved.log")
		line = mustRunUnder(it, log, err, pin)
		it.Pass("an .nvmrc edited to %s moves the runtime with it: %s", pin, line)

		it.Write(it.Path(".nvmrc"), declared+"\n")
		it.Replace(it.Path("MODULE.bazel"), declaration, "")
		log, err = runTest(it, "undeclared.log")
		if err == nil {
			log.Dump()
			it.Fail("with no node.toolchain() call the test passed, "+
				"though .nvmrc says %s", declared)
		}
		line = versionLine(log)
		if !strings.HasPrefix(line, "process.version v"+pin+",") {
			log.Dump()
			it.Fail("with no node.toolchain() call the test did not run "+
				"under the ruleset's v%s: %q", pin, line)
		}
		it.Pass("a consumer that makes no call runs under the ruleset's v%s: %s",
			pin, line)
	})
}
