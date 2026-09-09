### Added

- **`ts_test` takes `config_srcs`, the modules `config` imports.** Vite
  bundles a config from its copy beside the runtime `node_modules` tree, the
  one directory whose realpath resolves the config's bare imports, and from
  there a relative import of the package's own module resolved to nothing
  (`Could not resolve './plugins/foo'`, a Startup Error before any test ran).
  `config_srcs` names the modules the config imports and the ones they import,
  and each is staged beside the copy at its path relative to the config's
  package, so `./plugins/foo` resolves there and a bare import inside it walks
  up to the same tree. Gazelle writes the attribute from the config's listing,
  followed through every first-party module it reaches; the closure's npm
  edges are deps. A config whose imports stay bare is unchanged.
