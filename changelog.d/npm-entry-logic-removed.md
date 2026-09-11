### Removed

- **npm_import's entry logic and the `@types` assembly.** The repository rule no
  longer reads a package's `exports`, `types`, `typings` and `main` for a
  declaration entry, a module entry, subpath declarations or one-star patterns,
  nor its declarations' `/// <reference types>` headers. `ts_npm_package` loses
  `exports_types`, `module_entry`, `subpath_types`, `subpath_patterns`,
  `type_references` and `is_types_package`; `NpmPackageInfo` loses
  `exports_types_file`, `module_entry_file`, `subpath_types`,
  `subpath_patterns`, `type_references`, `ambient_types_file`,
  `types_package_dir`, `declaration_files` and `json_files`. tsgo resolves a
  bare specifier, an `exports` subpath, a `types` entry and a reference
  directive itself, walking the importer chain's node_modules as it walks a
  pnpm install; the paired `@types/*` package is the importer's link beside
  the runtime one.
