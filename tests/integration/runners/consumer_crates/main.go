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
		it.MustBazel("build", "//:hello")
		it.Pass("a ts_compile builds beside the workspace's own hub named crates")

		graph := it.BazelStdout("mod", "graph", "--depth=1")
		if !strings.Contains(graph, "rules_rust@0.70.0") {
			it.Fail("bazel mod graph names no rules_rust@0.70.0:\n%s", graph)
		}
		it.Pass("rules_rust is the workspace's 0.70.0, not the ruleset's")

		rustc := strings.TrimSpace(it.BazelStdout("run",
			"@rules_rust//tools/upstream_wrapper:rustc", "--", "--version"))
		if !strings.HasPrefix(rustc, "rustc 1.91.0") {
			it.Fail("the registered rustc is %q, not the workspace's 1.91.0",
				rustc)
		}
		it.Pass("the registered Rust toolchain is the workspace's: %s", rustc)

		hub := strings.TrimSpace(it.BazelStdout("query", "@crates//:all"))
		if !regexp.MustCompile(`crates//:cfg[-_]if`).MatchString(hub) {
			it.Fail("@crates//:all lists %q, not the workspace's cfg-if", hub)
		}
		it.Pass("@crates is the workspace's hub:\n%s", hub)
	})
}
