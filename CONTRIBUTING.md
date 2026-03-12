# Contributing to rules_typescript

Thank you for your interest in contributing. This document covers everything you need to know to get started.

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

The only prerequisite is **Bazelisk** (or Bazel 9+). All other dependencies — the Rust toolchain, Go toolchain, Node.js, and npm packages — are fetched hermetically by Bazel on the first build.

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

### Clone and verify

```bash
git clone https://github.com/nicholasgasior/rules_typescript
cd rules_typescript

# Build everything
bazel build //...

# Build with type checking
bazel build //... --output_groups=+_validation

# Run all tests
bazel test //...
```

The first build downloads several toolchains (Rust, Go, Node.js, oxc, tsgo) and may take a few minutes. Subsequent builds are fast via Bazel's content-addressed cache.

### Optional: buildifier for Starlark formatting

```bash
# Format all BUILD and .bzl files
bazel run @buildifier//:buildifier -- -r . -exclude_patterns='bazel-*,.*'
```

---

## Code Style

The project enforces consistent style across its four languages:

### Starlark (BUILD files and .bzl files)

Use **buildifier**:

```bash
bazel run @buildifier//:buildifier -- -r . -exclude_patterns='bazel-*,.*'
```

Key conventions (see also AGENTS.md):
- `ctx.actions.run` over `ctx.actions.run_shell` wherever possible
- `depset(order = "postorder")` for transitive file sets
- `args.add_all()` for file lists — never materialize depsets at analysis time
- Private attrs prefixed with `_`
- Public rules exposed from `defs.bzl`; raw implementations in `ts/private/`

### Go (Gazelle extension)

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
bazel build //oxc_cli:oxc_cli
```

### TypeScript (test fixtures and e2e workspaces)

Use **prettier** (if you have it locally). The TypeScript files in `tests/` and `e2e/` are fixtures — prefer minimal, readable code that illustrates the feature under test. No linter enforcement is enforced in CI for these files.

---

## Running Tests

### Unit tests and type checking (main repo)

```bash
# Run all tests
bazel test //...

# Run tests and type-check all targets
bazel build //... --output_groups=+_validation

# Run a specific test suite
bazel test //tests/vitest:math_test
bazel test //gazelle/...
```

### Bootstrap integration tests

Bootstrap tests spin up isolated Bazel workspaces to verify end-to-end user journeys. They require the `RULES_TYPESCRIPT_ROOT` environment variable to point to the repository root:

```bash
export RULES_TYPESCRIPT_ROOT=$(pwd)

bazel test //tests/bootstrap:test_new_project --test_output=all --test_strategy=local
bazel test //tests/bootstrap:test_existing_project --test_output=all --test_strategy=local
bazel test //tests/bootstrap:test_npm_deps --test_output=all --test_strategy=local
bazel test //tests/bootstrap:test_gazelle_roundtrip --test_output=all --test_strategy=local
```

### End-to-end workspace tests

```bash
cd e2e/basic
bazel build //...
bazel test //...
```

### Test matrix summary

| Suite | Command | What it covers |
|---|---|---|
| Smoke | `bazel test //tests/smoke/...` | Single-file .ts and .tsx compilation |
| Multi-package | `bazel test //tests/multi/...` | Cross-package deps, .d.ts boundary |
| Vitest | `bazel test //tests/vitest/...` | ts_test + vitest runner |
| Bundle | `bazel test //tests/bundle/...` | ts_binary bundling |
| npm | `bazel test //tests/npm/...` | npm package targets from pnpm-lock.yaml |
| Bootstrap | `bazel test //tests/bootstrap/...` | Full user-journey integration tests |
| Gazelle | `bazel test //gazelle/...` | Gazelle extension unit tests |
| E2E | `cd e2e/basic && bazel build //...` | Real consumer workspace |

---

## Pull Request Process

1. **Fork** the repository and create your branch from `main`.
2. **Write tests** for new behaviour. The project has coverage at multiple levels (unit, integration, e2e) — add tests at the appropriate level.
3. **Run the full test suite** before opening a PR:
   ```bash
   bazel test //...
   bazel build //... --output_groups=+_validation
   ```
4. **Update CHANGELOG.md** — add an entry under `[Unreleased]` describing your change.
5. **Update documentation** — if you're changing the public API (rule attributes, providers, directives), update both `README.md` and `AGENTS.md`.
6. **Open the PR** against `main` with the provided pull request template filled in.
7. A maintainer will review and may request changes. Please respond to review comments within a reasonable time (two weeks is a good guideline).
8. Once approved, a maintainer will squash-merge your PR.

### What makes a good PR

- **One logical change per PR.** Stacked changes are welcome as separate PRs with clear dependency notes.
- **No breaking changes without a deprecation path** (see COMPATIBILITY.md).
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

The subject line should be 72 characters or fewer. Use the body to explain *why*, not *what*.

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

The Gazelle extension lives in `gazelle/` and is a standard Gazelle language extension written in Go. A brief map for newcomers:

| File | Role |
|---|---|
| `gazelle/ts.go` | Entry point — registers the language with Gazelle |
| `gazelle/config.go` | Directive parsing (`# gazelle:ts_*`) and per-package config |
| `gazelle/generate.go` | Rule generation — produces `ts_compile` and `ts_test` targets |
| `gazelle/resolve.go` | Import resolution — maps import specifiers to Bazel labels |
| `gazelle/fix.go` | Fix/clean pass — removes stale targets |

For a deeper walkthrough of the design (package boundary heuristics, import resolution strategy, directive reference, `gazelle_ts.json` migration notes), read **AGENTS.md** — it is the authoritative architectural document for contributors working on the codebase.

The Gazelle extension is built as a Go binary and registered via `MODULE.bazel`. Consumers run it with:

```bash
bazel run //:gazelle
```
