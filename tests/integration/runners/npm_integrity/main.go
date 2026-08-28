package main

import (
	"fmt"

	"github.com/mikn/rules_typescript/tests/integration/harness"
)

const lockfile = `lockfileVersion: '9.0'

importers:

  .:
    dependencies:
      unverified:
        specifier: 1.0.0
        version: 1.0.0

packages:

  unverified@1.0.0:
    resolution: {%s}

snapshots:

  unverified@1.0.0: {}
`

func main() {
	harness.Run(harness.Config{
		Name:         "npm_integrity",
		WorkspaceRel: "tests/integration/npm_integrity/workspace",
		Renames:      map[string]string{"BUILD.bazel.tpl": "BUILD.bazel"},
	}, func(it *harness.IT) {
		// A remote tarball pnpm could not hash: the one shape that downloads
		// successfully today, with nothing to compare the bytes against.
		it.Write(it.Path("pnpm-lock.yaml"), fmt.Sprintf(lockfile,
			"tarball: https://example.invalid/unverified-1.0.0.tgz"))

		log, err := it.BazelLog("missing_integrity.log", "build", "@npm//:unverified")
		if err == nil {
			log.Dump()
			it.Fail("the build succeeded on a lockfile entry with no integrity")
		}
		for _, want := range []string{
			"no usable integrity",
			"unverified@1.0.0",
			"resolution keys: tarball",
			"pnpm-lock.yaml",
		} {
			if !log.Contains(want) {
				log.Dump()
				it.Fail("the extension's error does not mention %q", want)
			}
		}
		it.Pass("an entry with no integrity fails at extension evaluation, naming the package")

		// The same entry with a digest gets past the gate. The URL resolves to
		// nothing, so the build still fails -- on the fetch, which is the point:
		// the gate is what must stop saying anything.
		it.Write(it.Path("pnpm-lock.yaml"), fmt.Sprintf(lockfile,
			"integrity: sha512-Z2l2ZW4gYSBkaWdlc3QsIHRoZSBnYXRlIGlzIHNpbGVudA=="))

		fetched, err := it.BazelLog("with_integrity.log", "build", "@npm//:unverified")
		if err == nil {
			it.Pass("an entry carrying a digest is accepted")
		} else if fetched.Contains("no usable integrity") {
			fetched.Dump()
			it.Fail("an entry carrying a sha512 digest was still reported as unverifiable")
		} else {
			it.Pass("an entry carrying a digest reaches the fetch, not the gate")
		}
	})
}
