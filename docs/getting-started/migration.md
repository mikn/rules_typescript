# Migrating from rules_ts

`rules_typescript` is a fresh implementation, not a drop-in replacement for `rules_ts` from `aspect-build`. Key differences:

| | rules_ts (aspect-build) | rules_typescript (this) |
|--|------------------------|------------------------|
| Compiler | tsc | Oxc (Rust) |
| Type-checker | tsc (compile + check) | tsgo (check only) |
| .d.ts boundary | requires ts_project per file | automatic per source file |
| npm | aspect_rules_js | built-in pnpm lockfile parsing |
| Gazelle | not included | built-in |

## Migration Steps

1. Replace `ts_project` targets with `ts_compile`.
2. Replace `js_library` / `npm_link_all_packages` with `npm_translate_lock`.
3. Remove `tsconfig.json` from BUILD deps — `ts_compile` generates its tsconfig internally.
4. Run `bazel run //:gazelle` to regenerate BUILD files for new source structure.
5. If your codebase lacks explicit return types on exports, add `# gazelle:ts_isolated_declarations false` to your root `BUILD.bazel` first (see [Quick Start — Path B](quickstart.md#path-b-existing-project)), then migrate package by package.

## Key Conceptual Differences

### No `tsconfig.json` in BUILD files

`ts_compile` generates a `tsconfig.json` for each target internally. You do not manage tsconfig files as Bazel inputs. The generated tsconfig uses:

- `rootDirs` bridging the source tree and `ctx.bin_dir.path`
- `moduleResolution: "Bundler"`
- `paths` entries derived from the direct `deps`

### One `@npm` repository (not per-package)

`rules_ts` with `aspect_rules_js` creates per-package virtual stores. `rules_typescript` creates a single `@npm` repository from a pnpm lockfile. Labels are `@npm//:react`, `@npm//:types_react`, etc.

### Isolated declarations are opt-out, not opt-in

New projects start with `isolated_declarations = True` by default. Existing projects add `# gazelle:ts_isolated_declarations false` to the root BUILD file to opt out, then gradually opt packages back in.
