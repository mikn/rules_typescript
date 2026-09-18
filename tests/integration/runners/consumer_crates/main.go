package main

import (
	"regexp"
	"strings"

	"github.com/mikn/rules_typescript/tests/integration/harness"
)

func main() {
	harness.Run(harness.Config{
		Name:         "consumer_crates",
		WorkspaceRel: "tests/integration/consumer_crates/workspace",
		Renames:      map[string]string{"BUILD.bazel.tpl": "BUILD.bazel"},
	}, func(it *harness.IT) {
		it.MustBazel("build", "//:hello", "//:hello_commonjs", "--output_groups=+declarations,+_validation")
		it.Pass("a ts_compile builds beside the workspace's own hub named crates")

		if !strings.Contains(it.Read(it.Bin("src/commonjs.js")), "exports.first") {
			it.Fail("the prebuilt compiler did not emit CommonJS exports")
		}
		if !strings.Contains(it.Read(it.Bin("src/commonjs.d.ts")), "ReadonlyArray<string>") {
			it.Fail("the prebuilt compiler did not emit the standard-library declaration")
		}
		it.Pass("the prebuilt compiler finds its libraries during config, CommonJS emit, declaration emit and typecheck")

		graph := it.BazelStdout("mod", "graph", "--depth=1")
		if !strings.Contains(graph, "rules_rs@0.0.111") {
			it.Fail("bazel mod graph names no rules_rs@0.0.111:\n%s", graph)
		}
		it.Pass("rules_rs is pinned by the consumer")

		hub := strings.TrimSpace(it.BazelStdout("query", "@crates//:all"))
		if !regexp.MustCompile(`crates//:cfg[-_]if`).MatchString(hub) {
			it.Fail("@crates//:all lists %q, not the workspace's cfg-if", hub)
		}
		it.Pass("@crates is the workspace's hub:\n%s", hub)
	})
}
