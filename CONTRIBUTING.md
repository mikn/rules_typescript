# Contributing to rules_typescript

## Table of Contents

- [Development Environment](#development-environment)
- [Code Style](#code-style)
- [Running Tests](#running-tests)
- [Pull Request Process](#pull-request-process)
- [Commit Message Format](#commit-message-format)
- [Reporting Security Issues](#reporting-security-issues)
- [Contributor License Agreement](#contributor-license-agreement)
- [Gazelle Extension Architecture](#gazelle-extension-architecture)

---

## Development Environment

The only prerequisite is **Bazelisk** (or Bazel 9+). Every other dependency (the Rust toolchain, Go toolchain, Node.js, and npm packages) is fetched hermetically by Bazel on the first build.

### Install Bazelisk

```bash
# macOS (Homebrew)
brew install bazelisk

# Linux / macOS (manual)
curl -Lo ~/.local/bin/bazel \
  https://github.com/bazelbuild/bazelisk/releases/latest/download/bazelisk-linux-amd64
chmod +x ~/.local/bin/bazel

# Windows (Scoop)
scoop install bazelisk
```

### Clone and Verify

```bash
git clone https://github.com/mikn/rules_typescript
cd rules_typescript

# Build everything
bazel build //...

# Build with type checking
bazel build //... --output_groups=+_validation

# Run all tests
bazel test //...
```

The first build fetches a Rust toolchain, a Go SDK, Node.js and tsgo, then
compiles `oxc-bazel` and its crate graph from source. The Rust compile takes
minutes. Subsequent builds hit Bazel's content-addressed cache; do not
`bazel clean`.

### Which Binary a Toolchain Resolved

`//ts/toolchain` declares one runnable target per toolchain type. Each runs the
binary the toolchain resolved to for this host, with the arguments after `--`:

```bash
bazel run //ts/toolchain:oxc_resolved -- --help
bazel run //ts/toolchain:tsgo_resolved -- --version
bazel run //ts/toolchain:node_resolved -- --version
```

The second prints `Version 7.0.0-dev.20260311.1` and the third `v22.23.1`, the
versions `MODULE.bazel` pins. `oxc_resolved` builds `oxc-bazel` first. No test
asserts what the three print; `node_resolved` is the node the `tests/dev_server`,
`tests/lsp` and `tests/integration` suites run, and `//tests/toolchain` pins
which platform each toolchain's binary comes from.

### Pre-Push Hook

```bash
git config core.hooksPath .githooks
```

`.githooks/pre-push` refuses a push whose working tree differs from `HEAD`,
tracked edits and untracked files alike; `.gitignore` covers what a working
checkout carries. A push sends commits, so a file edited but never committed is
not in it. The failure names the files and says to commit or stash;
`git push --no-verify` pushes anyway.

It is opt-in per clone, because `core.hooksPath` is repository config and
repository config is not checked in. A linked worktree inherits it from the
clone it was created from.

### Buildifier

See [Starlark](#starlark-build-files-and-bzl-files) under Code Style.

---

## Code Style

One formatter per language. The `lint` CI job checks Starlark and Go; Rust and
TypeScript formatting are local conventions.

### Starlark (BUILD Files and .bzl Files)

Use **buildifier**:

```bash
# buildifier is not a bazel_dep — there is no @buildifier repo to run. Use the
# released binary, which is what CI checks with.
curl -fsSL -o /usr/local/bin/buildifier \
  https://github.com/bazelbuild/buildtools/releases/download/v8.2.1/buildifier-linux-amd64
chmod +x /usr/local/bin/buildifier

buildifier -r . -exclude_patterns='bazel-*,.*'   # format
buildifier --mode=check -r .                     # what CI runs
```

Key conventions (see also AGENTS.md):
- `ctx.actions.run` over `ctx.actions.run_shell` wherever possible
- `depset(order = "postorder")` for transitive file sets
- `args.add_all()` for file lists; never materialize depsets at analysis time
- Private attrs prefixed with `_`
- Public rules exposed from `defs.bzl`; raw implementations in `ts/private/`

### Go (Gazelle Extension)

Use **gofmt**:

```bash
cd gazelle && gofmt -w .
```

The Gazelle extension lives in `gazelle/`. Run its tests with:

```bash
bazel test //gazelle/...
```

### Rust (oxc_cli)

Use **rustfmt**:

```bash
cd oxc_cli && cargo fmt
```

The Rust CLI lives in `oxc_cli/`. Build with:

```bash
bazel build //oxc_cli:oxc-bazel
```

#### Repinning Crate Dependencies

Two `crate_universe` hubs are pinned by checked-in lockfiles. rules_rust
requires a lockfile for a hub declared by a non-root module, and rules_typescript
is a dependency in a consumer's build, where repinning across the module boundary
is not possible:

| Hub          | Rendering            | Cargo resolution      |
| ------------ | -------------------- | --------------------- |
| `@crates`    | `oxc_cli/Cargo.Bazel.lock` | `oxc_cli/Cargo.lock` |
| `@oj_crates` | `oj/Cargo.Bazel.lock`      | `oj/Cargo.lock`      |

After editing `oxc_cli/Cargo.toml` or the `crate.spec` for `oj` in
`MODULE.bazel`, regenerate all four:

```bash
CARGO_BAZEL_REPIN=1 bazel query "@crates//:all + @oj_crates//:all"
```

A rules_rust bump also invalidates the renderings: the digest covers the cargo
and rustc versions rules_rust pins, so repin in the same commit as the bump.
Without it, every build fails with "The current `lockfile` is out of date".

### TypeScript (Test Fixtures and E2E Workspaces)

Use **prettier** (if you have it locally). The TypeScript files in `tests/` and
`e2e/` are fixtures: keep them minimal, illustrating the feature under test. CI
does not lint them.

---

## Running Tests

### Unit Tests and Type Checking (Main Repo)

```bash
# Run all tests
bazel test //...

# Run tests and type-check all targets
bazel build //... --output_groups=+_validation

# Run a specific test suite
bazel test //tests/vitest:math_test
bazel test //gazelle/...
```

### Test Source Coverage

```bash
tools/ci/check_test_sources.sh
```

A Gazelle run that deletes a test target still satisfies `bazel build //...`,
`bazel test //...` and a byte-identical Gazelle rerun. The script compares the
test sources on disk against the srcs of every test target, and again against
only the targets `bazel test //...` runs, so a target tagged `manual` does not
count as coverage.

If a file's only target is `manual`, add the file to `MANUAL_ONLY` inside the
script with the reason it cannot run. The list is exact in both directions:
tagging a test `manual` fails CI until the reason is written down, and untagging
it fails until the entry is removed.

The script is read-only: a loading-phase query and `git ls-files`. It is the
first step of the `test` job in CI. `git ls-files` cannot see an unstaged new
file, so a local run reports green on a test not yet `git add`ed.

### Integration Tests

Integration tests spin up an isolated Bazel workspace each to verify end-to-end
user journeys. They are part of `bazel test //...`, need no environment variable,
and carry the tags `nested-bazel` and `cpu:2`, so Bazel runs as many at once as
the machine has cores for:

```bash
bazel test //tests/integration/...

bazel test //tests/integration:new_project_test --test_output=all
bazel test //tests/integration:existing_project_test --test_output=all
bazel test //tests/integration:npm_deps_test --test_output=all
bazel test //tests/integration:gazelle_roundtrip_test --test_output=all
```

They are slow (each spawns a nested Bazel). To iterate on everything else, use
`--config=fast`, whose `--test_tag_filters=-nested-bazel` drops them:

```bash
bazel test --config=fast //...
```

### End-To-End Workspace Tests

```bash
cd e2e/basic
bazel build //...
bazel test //...
```

### Test Matrix Summary

| Suite | Command | What it covers |
|---|---|---|
| Smoke | `bazel test //tests/smoke/...` | Single-file .ts and .tsx compilation |
| Multi-package | `bazel test //tests/multi/...` | Cross-package deps, .d.ts boundary |
| Vitest | `bazel test //tests/vitest/...` | ts_test + vitest runner |
| Bundle | `bazel test //tests/bundle/...` | ts_binary bundling |
| npm | `bazel test //tests/npm/...` | npm package targets from pnpm-lock.yaml |
| Integration | `bazel test //tests/integration/...` | Full user-journey tests, each in a nested Bazel workspace (tagged `nested-bazel`) |
| LSP | `bazel test //tests/lsp/...` | The tsserver resolution hook against a real tsserver |
| Gazelle | `bazel test //gazelle/...` | Gazelle extension unit tests |
| E2E | `cd e2e/basic && bazel build //...` | Real consumer workspace |
| Test-source coverage | `tools/ci/check_test_sources.sh` | Every tracked test source is claimed by a target that runs |

---

## Pull Request Process

1. **Fork** the repository and create your branch from `main`.
2. **Write tests** for new behaviour, at unit, integration or e2e level.
3. **Run the full test suite** before opening a PR:
   ```bash
   bazel test //...
   bazel build //... --output_groups=+_validation
   ```
4. **Add a changelog entry**: a new file in `changelog.d/`, not an edit to
   `CHANGELOG.md`. Its first line is the `###` section the entry belongs under;
   the rest is the entry:

   ```bash
   cat > changelog.d/ts-binary-js-entry.md <<'EOF'
   ### Added

   - **`ts_binary` takes a plain JavaScript file as its `entry_point`.** The
     attr is polymorphic: a target providing `JsInfo` behaves exactly as before.
   EOF

   bazel run //tools/changelog   # prints the section as it will read
   ```

   `changelog.d/README.md` lists the sections and the rules. A release folds the
   fragments into `CHANGELOG.md`.
5. **Update documentation**: a public-API change (rule attributes, providers,
   directives) lands with its page under `docs/` in the same PR, plus `README.md`
   and `AGENTS.md` where they say the same thing. `mkdocs build --strict` runs in
   the `lint` job, so a nav entry without a page, or a link to a page that does
   not exist, fails CI.
6. **Open the PR** against `main` with the provided pull request template filled in.
7. A maintainer reviews and may request changes. Respond to review comments within two weeks.
8. Once approved, a maintainer will squash-merge your PR.

### What Makes a Good PR

- **One logical change per PR.** Stacked changes are welcome as separate PRs with clear dependency notes.
- **Every breaking change carries a `changelog.d/` entry** under a `### Breaking — <area>` heading, stating the edit a consumer has to make. Pre-1.0 there is no deprecation window and no compatibility shim (see COMPATIBILITY.md).
- **No `bazel clean`** in scripts or documentation. Trust the cache.
- **Never reference `bazel-out/` directly** in Starlark. Use `ctx.bin_dir.path`, `File.path`, `File.dirname`.

---

## Commit Message Format

Use the **Conventional Commits** format:

```
<type>(<scope>): <short description>

[optional body]

[optional footer(s)]
```

**Types:**
- `feat` — a new feature
- `fix` — a bug fix
- `docs` — documentation only
- `refactor` — code change that neither fixes a bug nor adds a feature
- `test` — adding or correcting tests
- `chore` — maintenance (dependency updates, build scripts, toolchain bumps)

**Scopes** (optional, use when helpful):
- `ts_compile`, `ts_test`, `ts_binary`, `ts_bundle` — rule changes
- `gazelle` — Gazelle extension
- `oxc_cli` — Rust CLI
- `npm` — npm/lockfile support
- `toolchain` — toolchain registration
- `runtime` — JS runtime support
- `vite` — Vite bundler integration

**Examples:**

```
feat(gazelle): add ts_path_alias directive support
fix(ts_compile): pass rootDirs to tsgo for bin_dir resolution
docs: update COMPATIBILITY.md for Bazel 9.x support
chore(toolchain): bump oxc to 0.120.0
```

The subject line is 72 characters or fewer. The body carries the reason for the
change.

---

## Reporting Security Issues

**Do not open a public GitHub issue for security vulnerabilities.**

Email security issues to the maintainers directly. Include:
- A description of the vulnerability
- Steps to reproduce or a proof-of-concept
- The affected version(s)

We will acknowledge your report within 72 hours and work with you on a coordinated disclosure timeline.

---

## Contributor License Agreement

Contributions to this project are made under the **MIT License** (the same license as the project itself). By submitting a pull request, you agree that your contribution is licensed under the MIT License and that you have the right to grant that license.

There is no separate CLA to sign.

---

## Gazelle Extension Architecture

The Gazelle extension lives in `gazelle/` and is a standard Gazelle language
extension written in Go.

| File | Role |
|---|---|
| `gazelle/language.go` | Entry point: registers the language, `Kinds()`, `Loads()`, `KnownDirectives()` |
| `gazelle/config.go` | Directive parsing (`# gazelle:ts_*`), framework and codegen detection |
| `gazelle/generate.go` | Rule generation: produces `ts_compile`, `ts_test` and the rest |
| `gazelle/resolve.go` | Import resolution: maps import specifiers to Bazel labels |
| `gazelle/imports.go` | Import extraction from TypeScript sources |
| `gazelle/jsonc/` | JSONC parser, its own Go package, so a commented `tsconfig.json` still yields its `paths` |
| `gazelle/framework_bundle.go` | Vite-based framework bundle targets: TanStack Start and Remix (Next.js and SvelteKit have their own: `framework_next.go`, `sveltekit_bundle.go`) |
| `gazelle/codegen.go` | Auto-detected codegen targets |

**AGENTS.md** is the architectural reference for contributors: package boundary
heuristics, import resolution strategy, and the directive reference.

The extension is compiled into two `gazelle_binary` targets. `//gazelle:gazelle_ts`
is the one this repo runs, through the `gazelle` runner beside it:

```bash
bazel run //gazelle:gazelle
```

It also carries the Go and proto languages, because this repo generates BUILD
files for its own `.go` sources. `//gazelle:gazelle_typescript` is the exported
one and carries TypeScript alone, so it never rewrites a consumer's Go BUILD
files. A consumer workspace declares its own `gazelle` target pointing at
`@rules_typescript//gazelle:gazelle_typescript` (`e2e/basic/BUILD.bazel` is the
worked example) and runs `bazel run //:gazelle`.
