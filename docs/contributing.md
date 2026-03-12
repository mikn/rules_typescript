# Contributing

Thank you for your interest in contributing. This document covers everything you need to know to get started.

## Development Environment

The only prerequisite is **Bazelisk** (or Bazel 9+). All other dependencies — the Rust toolchain, Go toolchain, Node.js, and npm packages — are fetched hermetically by Bazel on the first build.

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

## Code Style

### Starlark (BUILD files and .bzl files)

Use **buildifier**:

```bash
bazel run @buildifier//:buildifier -- -r . -exclude_patterns='bazel-*,.*'
```

Key conventions:
- `ctx.actions.run` over `ctx.actions.run_shell` wherever possible
- `depset(order = "postorder")` for transitive file sets
- `args.add_all()` for file lists — never materialize depsets at analysis time
- Private attrs prefixed with `_`
- Public rules exposed from `defs.bzl`; raw implementations in `ts/private/`

### Go (Gazelle extension)

```bash
cd gazelle && gofmt -w .
bazel test //gazelle/...
```

### Rust (oxc_cli)

```bash
cd oxc_cli && cargo fmt
bazel build //oxc_cli:oxc_cli
```

## Running Tests

```bash
# Run all tests
bazel test //...

# Run tests and type-check all targets
bazel build //... --output_groups=+_validation

# Run a specific test suite
bazel test //tests/vitest:math_test
bazel test //gazelle/...
```

### Bootstrap Integration Tests

```bash
export RULES_TYPESCRIPT_ROOT=$(pwd)
bazel test //tests/bootstrap:test_new_project --test_output=all --test_strategy=local
```

### End-to-End Workspace Tests

```bash
cd e2e/basic
bazel build //...
bazel test //...
```

### Test Matrix

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

## Pull Request Process

1. **Fork** the repository and create your branch from `main`.
2. **Write tests** for new behaviour.
3. **Run the full test suite** before opening a PR.
4. **Update CHANGELOG.md** — add an entry under `[Unreleased]`.
5. **Update documentation** — if you're changing the public API, update `docs/` and `AGENTS.md`.
6. **Open the PR** against `main`.

## Commit Message Format

Use the **Conventional Commits** format:

```
<type>(<scope>): <short description>
```

Types: `feat`, `fix`, `docs`, `refactor`, `test`, `chore`

Scopes: `ts_compile`, `ts_test`, `gazelle`, `oxc_cli`, `npm`, `toolchain`, `runtime`, `vite`

## Security Issues

**Do not open a public GitHub issue for security vulnerabilities.** Email the maintainers directly. We will acknowledge within 72 hours.

## Contributor License Agreement

Contributions are made under the MIT License. By submitting a pull request, you agree your contribution is licensed under the MIT License.
