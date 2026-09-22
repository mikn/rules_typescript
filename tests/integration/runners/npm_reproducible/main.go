package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/mikn/rules_typescript/tests/integration/harness"
)

const lockTemplate = `lockfileVersion: '9.0'
patchedDependencies:
  sample@1.0.0:
    hash: %s
    path: sample@1.0.0.patch
importers:
  .: {}
  member: {}
packages:
  sample@1.0.0:
    resolution: {integrity: sha512-c2FtcGxl}
%s
snapshots:
  sample@1.0.0: {}
%s
`

func main() {
	harness.Run(harness.Config{
		Name:         "npm_reproducible",
		WorkspaceRel: "tests/integration/npm_reproducible/workspace",
		Renames:      map[string]string{"BUILD.bazel.tpl": "BUILD.bazel"},
	}, func(it *harness.IT) {
		patch := "--- a/package.json\n+++ b/package.json\n@@ -1 +1 @@\n-old\n+new\n"
		digest := fmt.Sprintf("%x", sha256.Sum256([]byte(patch)))
		it.Write(it.Path("sample@1.0.0.patch"), patch)
		it.Write(it.Path("pnpm-lock.yaml"), fmt.Sprintf(lockTemplate, digest, "", ""))
		it.Write(it.Path("member/BUILD.bazel"), "filegroup(name = \"member\")\n")
		query := func() string {
			result := it.BazelStdout("query", "@npm//:all", "--output=build", "--lockfile_mode=update")
			var lock struct {
				ModuleExtensions map[string]json.RawMessage `json:"moduleExtensions"`
			}
			if err := json.Unmarshal([]byte(it.Read(it.Path("MODULE.bazel.lock"))), &lock); err != nil {
				it.Fail("cannot decode Bazel lockfile: %v", err)
			}
			for extension := range lock.ModuleExtensions {
				if strings.Contains(extension, "//npm:extensions.bzl") && strings.HasSuffix(extension, "%npm") {
					it.Fail("reproducible npm extension still persisted metadata: %s", extension)
				}
			}
			return result
		}
		require := func(output, want string, present bool) {
			if strings.Contains(output, want) != present {
				it.Fail("generated hub presence of %q should be %t; got:\n%s", want, present, output)
			}
		}
		require(query(), `name = "sample"`, true)
		it.Pass("npm repository evaluates without persisting extension metadata")

		updatedLock := fmt.Sprintf(lockTemplate, digest, "  added@2.0.0:\n    resolution: {integrity: sha512-YWRkZWQ=}", "  added@2.0.0: {}")
		it.Write(it.Path("pnpm-lock.yaml"), updatedLock)
		require(query(), `name = "added"`, true)
		it.Pass("lockfile edit regenerates the hub")

		require(query(), `name = "member-first"`, false)
		it.Write(it.Path("member/package.json"), `{"name":"member-first","version":"1.0.0"}`)
		require(query(), `name = "member-first"`, true)
		it.Write(it.Path("member/package.json"), `{"name":"member-second","version":"1.0.0"}`)
		output := query()
		require(output, `name = "member-first"`, false)
		require(output, `name = "member-second"`, true)
		if err := os.Remove(it.Path("member/package.json")); err != nil {
			it.Fail("remove member manifest: %v", err)
		}
		require(query(), `name = "member-second"`, false)
		it.Pass("member manifest appearance, edit and removal regenerate the hub")

		patch += "\n"
		it.Write(it.Path("sample@1.0.0.patch"), patch)
		failed, err := it.BazelLog("stale-patch.log", "query", "@npm//:all", "--lockfile_mode=update")
		if err == nil || !failed.Contains("sha256 disagrees") {
			failed.Dump()
			it.Fail("edited patch must fail with stale lockfile digest")
		}
		newDigest := fmt.Sprintf("%x", sha256.Sum256([]byte(patch)))
		it.Write(it.Path("pnpm-lock.yaml"), strings.ReplaceAll(updatedLock, digest, newDigest))
		require(query(), `name = "added"`, true)
		it.Pass("patch mutation invalidates evaluation and matching lock digest recovers")
	})
}
