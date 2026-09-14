package main

import (
	"strings"

	"github.com/mikn/rules_typescript/tests/integration/harness"
)

const check = "tools/ci/check_tools_lock.sh"

// rows are the TOOLS_INTEGRITY lines the check prints, one per platform.
func rows(log *harness.Log) []string {
	var rows []string
	for _, line := range log.Lines() {
		if strings.HasPrefix(line, `    "`) {
			rows = append(rows, line)
		}
	}
	return rows
}

func runCheck(it *harness.IT, bazel, logName string) string {
	env := []string{
		"BAZEL=" + bazel,
		"BUILD_WORKSPACE_DIRECTORY=" + it.RulesTSRoot,
	}
	log, err := it.Exec(logName, env, check)
	if err != nil {
		log.Dump()
		it.Fail("%s exited non-zero: %v", check, err)
	}
	if !log.Contains("check_tools_lock: the four assets of tools-v") {
		log.Dump()
		it.Fail("%s exited 0 without its closing line", check)
	}
	table := rows(log)
	if len(table) != 4 {
		log.Dump()
		it.Fail("%s printed %d table rows, not four", check, len(table))
	}
	return strings.Join(table, "\n")
}

func main() {
	harness.Run(harness.Config{Name: "tools_lock"}, func(it *harness.IT) {
		bazel := it.BazelExecutable()
		fresh := runCheck(it, bazel, "check_fresh.log")
		it.Pass("on a fresh server the check exits 0 with the table's four rows")

		it.MustBazel("build", "//tests/validation/...")
		warm := runCheck(it, bazel, "check_warm.log")
		if warm != fresh {
			it.Fail("the check's answer changed with what the server analyzed:\n"+
				"%s\n--- on the fresh server ---\n%s", warm, fresh)
		}
		it.Pass("after the server built //tests/validation/... the check " +
			"exits 0 with the same four rows")
	})
}
