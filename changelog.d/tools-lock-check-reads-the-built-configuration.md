### Fixed

- **`tools/ci/check_tools_lock.sh` reads the files of the configuration it
  built, and the four tools are `pure = "on"`.** A `cquery` names every
  configuration the server holds a target in, so on a server that had
  analyzed the four tools as build tools -- the suite's, a developer's -- the
  check handed the packer paths no build produced and exited 1 with the table
  right; a fresh server passed. Its two cqueries are `config(..., target)`,
  the configuration the build before them used. And `tsaction` reached
  `os/user`, so its linux_amd64 binary was cgo-linked against the builder's C
  library and three machines printed three SRIs for one commit; `pure =
  "on"` on the four `go_binary` targets makes each asset the Go SDK's bytes
  alone, and the table's linux_amd64 row is the static build's.
  `//tests/integration/tools_lock` runs the check over the checkout on a
  fresh nested server and again after that server built
  `//tests/validation/...`.
