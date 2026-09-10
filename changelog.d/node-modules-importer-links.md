### Breaking — node_modules

- **A `node_modules` target is an importer's links into the store.** Its
  outputs are one declared symlink `node_modules/<name>` per package in
  `deps`, into the package's store tree, so one target per Bazel package holds
  them (rename any second one out, or move it into a package of its own), and
  `DefaultInfo.files` is every link and every store tree they reach, where it
  was one tree artifact: a consumer reading `ctx.files.node_modules[0]` reads
  `NodeModulesInfo.dir` instead, and stages the files. New attr `parent`, the
  importer above's target. A workspace member is no longer a dep: write
  `node_modules_member(name = "node_modules/<name>", member = "@npm//:<name>")`
  and name that target where the view was named. A name in `deps` twice with
  two resolutions fails as `'<name>' linked twice`; the two-versions and
  two-peer-sets messages are gone with the flat tree. `ts_codegen`,
  `ts_binary`, `ts_dev_server` and `esbuild_bundle` take the target through
  `NodeModulesInfo`: nothing has to be named `node_modules` any more, and the
  Vite plugin's `target` option and its `*_node_modules` scan under bazel-bin
  are gone; `nodeModules` is the one way. `npm_virtual_store` takes
  `name = "node_modules/.pnpm"`, the directory it declares, so Gazelle can
  write and merge the call.
