### Changed

- **`ts_refresh_tsconfig` writes no nested tsconfig for a package whose targets
  check under the `tsconfig.json` in their own directory.** That file is the
  program tsserver reads for its files, so nothing is generated over it, the
  root program excludes the files it covers, and `nested_tsconfigs` does not
  name it.
