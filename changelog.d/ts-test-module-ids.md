### Changed

- **Under vitest a module's id is its runfiles path where the runfiles hold the
  file and its realpath otherwise.** The Bazel layer set
  `resolve.preserveSymlinks`, so every id was a runfiles path, a package's
  included, and the Workers pool's layer turned it off again to give vitest's
  modules one identity. Vite now realpaths every id, and one plugin in the
  layer gives a file the runfiles hold -- a test, a compiled module, a source
  file, a setup file -- its runfiles path back: `import.meta.url` and a
  relative import stay in the runfiles tree, and a file under `node_modules`
  keeps its realpath, so a package's own imports resolve from its place in the
  tree. An id resolving to a file the runfiles do not hold is refused, as the
  pool's layer refused a build output. `server.fs.allow` names the workspace's
  runfiles and `bazel-bin`'s realpath, so a DOM environment, which loads
  through Vite's server, is served both. The pool's layer and the plugin
  serving a `setupFiles` entry from its staged path are gone; a
  `resolve.preserveSymlinks` a config sets still wins.
