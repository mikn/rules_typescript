### Added

- **`//ts/toolchain/tsgo_source`: the compiler built from source by rules_go.**
  `ts/private/tsgo_source/go.mod` names
  `github.com/microsoft/typescript-go/cmd/tsgo` in a `tool` directive and
  requires the module at the commit the lockfile's `typescript` release was
  built from; the `ts` extension declares that module and every module its
  build reaches as Gazelle-written `go_repository` repositories from the
  go.mod and its go.sum, and `@tsgo_source//:tsgo` is the module's `cmd/tsgo`
  built for the exec platform. The toolchain is outside `//ts/toolchain:all`:
  a consumer registers `@rules_typescript//ts/toolchain/tsgo_source` ahead of
  it to run the ruleset's build of the compiler, and the lockfile toolchains
  stay the default. `//ts/private/tsgo:tsgo_test` holds the pin to the
  revision the lockfile's binary embeds. rules_go's apparent name in this
  module is `io_bazel_rules_go`, the name Gazelle writes into a repository
  with no MODULE.bazel of its own.
