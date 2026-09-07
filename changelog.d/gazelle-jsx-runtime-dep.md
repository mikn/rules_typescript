### Fixed

- **Gazelle writes the dep a JSX source's runtime import needs.** A `ts_compile`
  or `ts_test` got its deps from written import specifiers, the tsconfig's
  `types` list and `ts_runtime_dep` alone, so a `.tsx` file importing nothing
  got no framework dep, while `ts_compile` compiles every source under its
  tsconfig's `jsx`, `react-jsx` by default, and resolves the
  `<jsxImportSource>/jsx-runtime` import every tag makes through the
  node_modules forest built from deps: `TS2875` at the first tag and `TS7026` on
  every element in the sandbox. Every `ts_compile` and `ts_test` with a `.tsx` source now gets the
  dep that specifier resolves to, as a written bare specifier does: the nearest
  tsconfig's `jsxImportSource` through its `extends` chain, else `react`. The extension is
  the whole test: a `.tsx` with no tag in it gets the dep as well, since Gazelle
  reads the srcs list and not the file, while tsc makes the import only for a
  file with a tag. A workspace member of that name answers it through its hub view, a
  `declare module` block in the target's own sources answers it as it does a
  written specifier, and a name the lockfile does not answer gets none, which
  `# gazelle:ts_warn_unresolved true` lists.
  `# gazelle:ts_runtime_dep @npm//:react` for the runtime is no longer needed.
