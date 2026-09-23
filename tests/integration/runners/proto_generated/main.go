package main

import (
	"github.com/mikn/rules_typescript/tests/integration/harness"
	"path/filepath"
	"strings"
)

func main() {
	harness.Run(harness.Config{Name: "proto_generated", WorkspaceRel: "tests/integration/proto_generated"}, func(it *harness.IT) {
		it.Write(it.Path("testdeps/pnpm-lock.yaml"), it.Read(filepath.Join(it.RulesTSRoot, "tests/npm/pnpm-lock.yaml")))
		it.Write(it.Path("pnpm-lock.yaml"), it.Read(filepath.Join(it.RulesTSRoot, "proto/private/pnpm-lock.yaml")))
		it.Write(it.Path("testdeps/BUILD.bazel"), `load("@npm_test//:defs.bzl", "npm_virtual_store")
npm_virtual_store(name = "node_modules/.pnpm")
exports_files(["pnpm-lock.yaml"])`)
		it.Write(it.Path("schema/BUILD.bazel"), `load("@rules_typescript//proto:defs.bzl", "ts_proto_config")
# gazelle:proto_strip_import_prefix /schema
# gazelle:ts_proto plain json
ts_proto_config(
    name = "plain",
    out_dir = "generated/plain",
    tsconfig = "//:compiler.json",
    node_modules = "//:node_modules",
    deps = ["@npm//:bufbuild_protobuf"],
)
ts_proto_config(
    name = "json",
    out_dir = "generated/json",
    tsconfig = "//:compiler.json",
    node_modules = "//:node_modules",
    deps = ["@npm//:bufbuild_protobuf"],
    options = ["target=ts", "json_types=true"],
)
package(default_visibility = ["//visibility:public"])
`)
		it.Write(it.Path("schema/common/common.proto"), `syntax = "proto3"; package example.common; message Common { string value = 1; }`)
		it.Write(it.Path("schema/messages/message.proto"), `syntax = "proto3"; package example.messages; import "common/common.proto"; import "google/protobuf/timestamp.proto"; message Message { example.common.Common common = 1; google.protobuf.Timestamp at = 2; }`)
		it.MustBazel("run", "//:gazelle", "--", "schema")
		before := it.Read(it.Path("schema/BUILD.bazel"))
		configs := strings.Fields(it.BazelStdout("query", `kind("ts_proto_config rule", //schema:all)`))
		if len(configs) != 2 {
			it.Fail("expected two queryable typed configurations, got %v", configs)
		}
		it.MustBazel("build", "//schema:plain", "//schema:json")
		if files := strings.TrimSpace(it.BazelStdout("cquery", "set(//schema:plain //schema:json)", "--output=files")); files != "" {
			it.Fail("configuration targets declared output files: %s", files)
		}
		it.MustBazel("test", "//consumer:generated_identity_test")
		it.MustBazel("run", "//:gazelle", "--", "schema")
		if after := it.Read(it.Path("schema/BUILD.bazel")); before != after {
			it.Fail("runtime generation changed owner BUILD")
		}
		for _, invalid := range []struct{ name, attrs, diagnostic string }{
			{"missing_label", `tsconfig = "//:absent_config"`, "absent_config"},
			{"javascript", `tsconfig = "//:compiler.json", options = ["target=js"]`, "must select target=ts"},
			{"empty_options", `tsconfig = "//:compiler.json", options = []`, "must select target=ts"},
		} {
			it.Write(it.Path("schema/BUILD.bazel"), before+`
ts_proto_config(name = "invalid", out_dir = "invalid", `+invalid.attrs+`)
`)
			log, err := it.BazelLog(invalid.name+".log", "build", "//schema:invalid")
			if err == nil || !log.Contains(invalid.diagnostic) {
				it.Fail("configuration did not reject %s: %v\n%s", invalid.name, err, log.Text)
			}
			it.Write(it.Path("schema/BUILD.bazel"), before)
		}
		it.Pass("typed configuration targets validate labels and options without outputs; generated identities execute and converge")
	})
}
