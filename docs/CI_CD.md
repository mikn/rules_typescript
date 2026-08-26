# CI/CD & Production Readiness

The pipeline this repository runs, and the release path out of it. The remote
cache and RBE sections further down are configurations for a *consumer*
workspace, and nothing here runs them — each says so where it starts.

## GitHub Actions CI

`.github/workflows/ci.yml` runs on every push to `main`/`develop` and every pull
request against `main`. It has seven jobs.

### Workflow Jobs

1. **Unit Tests & Type Checking** (`test`)
   - First, on ubuntu only: `tools/ci/check_test_sources.sh` — every tracked
     test source on disk has to be claimed by a test target that actually runs.
     It goes first because it is a loading-phase query the next step pays for
     anyway, and because it names the cause when a test simply stops existing
     (see [below](#every-test-source-is-claimed-by-a-target))
   - Then `bazel test --config=ci //...`, then
     `bazel build --config=ci //... --output_groups=+_validation`
   - Matrix: `ubuntu-latest` and `macos-latest`

2. **Vite Plugin Type Check** (`vite-typecheck`)
   - `pnpm --dir vite install --frozen-lockfile --ignore-scripts`, then
     `pnpm --dir vite typecheck`. Not a Bazel job: esbuild bundles `vite/src`
     without checking it, and `//vite:plugin_typecheck` is tagged `manual` until
     `@types/node` reaches `//tests/npm:pnpm-lock.yaml`, so this is the only
     thing type-checking the plugin sources

3. **E2E Tests** (`e2e`)
   - Builds and tests `e2e/basic`, a separate workspace
   - Matrix: `ubuntu-latest` and `macos-latest`

4. **Examples Build** (`examples`)
   - One matrix leg per workspace under `examples/` — `basic`, `app`,
     `react-app`, `remix-app`, `tanstack-app`, `nextjs-app` — each a separate
     Bazel invocation of `//...`, `fail-fast: false`

5. **Build Determinism** (`determinism`)
   - `//tests/smoke:hello` and `//tests/css_module:button_module` built from two
     empty output bases, then three outputs compared byte for byte:
     `tests/smoke/hello.js`, `tests/css_module/Button.module.css.exports.json`
     and `tests/css_module/Button.module.css.d.ts`. Two builds cannot be one
     action, so this is the one check that stays a sequence of invocations in
     the workflow rather than a target
   - The `css_module` outputs are there because the scoped class name in
     `.exports.json` is a content hash of the stylesheet, and a path leaking
     into that hash is exactly what two output bases expose
   - Scratch space comes from `/mnt`, the runner's ephemeral disk: two output
     bases are two full toolchain trees and the root disk does not fit both

6. **Integration Tests** (`integration-tests`)
   - `bazelisk test --config=ci-integration //tests/integration/...`
   - Its own job because each target spawns a nested Bazel and they run
     serially (`exclusive`). This is the only job that runs them: `--config=ci`
     in the `test` job expands `--config=fast`, which filters them out so they
     do not run three times per push
   - Each nested Bazel has its own output base, so the harness gives them all one
     **shared repository cache** — it appends `common --repository_cache=<shared>`
     to every staged workspace's `.bazelrc` (`prepare()` in
     `tests/integration/harness/harness.go`). Without it each workspace fetches
     the whole BCR registry for itself, and the resulting lookup failures read as
     flaky tests rather than as a missing cache

7. **Linting & Code Quality** (`lint`)
   - `buildifier --mode=check -r .`, using the released binary downloaded in the
     job. There is no `buildifier` `bazel_dep`, so
     `bazel run @buildifier//:buildifier` does not work — see
     [CONTRIBUTING.md](contributing.md#starlark-build-files-and-bzl-files)
   - `gofmt -l .` (a non-empty result fails) and `go vet` over the Go modules,
     both through `actions/setup-go` rather than Bazel: they read the `go.work`
     module graph straight off the source tree, so nothing about them comes from
     the build graph. `tools/quickstart` is named out of the `go vet` patterns
     rather than excluded, because its `go:embed` target is a genrule output and
     `go list` failing there aborts the whole `./...` pattern
   - `mkdocs build --strict` into a temporary site directory. `docs.yml` builds
     the site only on push to `main`, so until this step existed a PR that broke
     the nav or a page reference was caught after it had merged

### Every test source is claimed by a target

`bazel build //...`, `bazel test //...` and a byte-identical Gazelle rerun are
all satisfied by a Gazelle run that **deletes** a test target — which is how
seven hand-written `go_test` targets once went missing. `check_test_sources.sh`
is not: the set of test files on disk is not something Gazelle writes, so it
cannot be brought back into agreement by damaging the BUILD files.

Tagging a test `manual` is the same regression wearing a disguise — the target
still exists, still claims its srcs, and `bazel test //...` still passes, because
`//...` skips it. So each file's claim is checked twice: once against every test
target, and once against only the targets `bazel test //...` runs. A file with the
first claim but not the second is manual-only and has to be named in the script's
`MANUAL_ONLY` list **with a reason**. That list is exact in both directions:
tagging a test `manual` fails until someone writes down why, and untagging it
fails until the entry is removed.

Directories holding their own `MODULE.bazel`/`WORKSPACE.bazel`, and
`.bazelignore` roots, are out of scope — `//...` does not descend into them, so
their test files are not this workspace's to claim.

The script is read-only: a loading-phase `bazel query` and `git ls-files`. It
never runs Gazelle and never writes to the source tree. One limit that matters
locally: `git ls-files` cannot see an unstaged new file, so a local run reports
green on a test that has no target yet.

### Triggering CI

Pushes to `main` and `develop`, and pull requests against `main`. There is no
`workflow_dispatch` trigger, so the Actions tab offers no "Run workflow" button
— re-run a failed job, or push.

## Running CI Locally

There is no CI driver script: the stages live in `.bazelrc` as `--config=`
groups, so the local command and the workflow step are the same command.

```bash
# The main workspace: tests, then type-checking. --config=ci expands
# --config=fast, so the exclusive nested-Bazel targets are skipped.
bazel test --config=ci //...
bazel build --config=ci //... --output_groups=+_validation

# The nested-Bazel suite, which serializes (~3 minutes per target).
bazel test --config=ci-integration //tests/integration/...

# e2e/ and examples/ are separate workspaces (.bazelignore), so they are
# separate invocations — a --config cannot change workspace.
cd e2e/basic && bazel build //... && bazel test //...
cd examples/basic && bazel build //...
```

## Determinism Verification

A cache is only worth having if the same source produces the same bytes, so this
is the property every cache claim on this page rests on.

Two builds cannot be a single Bazel action, so the check is a sequence of
invocations. The `determinism` job runs this shape over two targets and three
outputs (see [Workflow Jobs](#workflow-jobs) job 5); one target is enough to
reproduce it locally:

```bash
for base in a b; do
  bazel --output_base="$HOME/.cache/det_$base" \
    build --config=determinism //tests/smoke:hello
done
cmp \
  "$(bazel --output_base="$HOME/.cache/det_a" info bazel-bin)/tests/smoke/hello.js" \
  "$(bazel --output_base="$HOME/.cache/det_b" info bazel-bin)/tests/smoke/hello.js"
```

`--config=determinism` turns off the convenience symlinks, which the two builds
would otherwise race for. `bazel info bazel-bin` is asked for the output path
rather than guessing at the `bazel-out` layout. Separate output bases are used
instead of `bazel clean`, which preserves the repository cache.

## Known Sources of Non-Determinism

Eight places non-determinism can enter, what the current answer is for each, and
what a rule of your own has to do to stay out of trouble.

### 1. Build Timestamps in Compiled Output

**Risk**: Some compilers embed the current timestamp in their outputs.
**Status in rules_typescript**: `oxc` does not embed timestamps in compiled `.js` or `.js.map` files. `tsgo` does not embed timestamps in `.d.ts` files. Verified by the `determinism` CI job.
**Mitigation**: If you add a custom `genrule` that runs a tool with `date` or similar, the output will be non-deterministic. Pass `--no-timestamp` or equivalent to that tool.

### 2. File Ordering in Directory Outputs

**Risk**: When a rule uses `ctx.actions.declare_directory`, file ordering inside the directory depends on the filesystem's readdir order. Different kernels or filesystems may return files in different orders.
**Status in rules_typescript**: Affected rules include `ts_bundle` (Vite output directory), `ts_npm_publish` (staging directory), and `node_modules` (tree artifact). These are staging directories, not inputs to further compilation, so ordering only matters if you compare the directories byte-for-byte.
**Mitigation**: Use `diff -r` (which is order-insensitive for directory comparisons) rather than `tar c ... | sha256sum` when checking directory artifacts.

### 3. Vite Bundle Content Hashes

**Risk**: Vite (and Rollup underneath it) generates chunk file names using a content hash. The hash algorithm is deterministic, but the chunk boundaries depend on module graph traversal order, which can change if `import()` statements are added or removed.
**Status**: Vite bundle outputs are deterministic for a fixed source tree. A source change causes all dependent chunk hashes to change, which is expected and correct.
**Mitigation**: None needed — this is correct behaviour. Do not compare Vite output hashes across different source versions.

### 4. npm Package Download Order

**Risk**: parallel npm tarball downloads; if two packages produced the same
output file path, the winner would depend on download order.
**Status**: not possible. Each package is its own external repository with its
own root, so there is no shared output path to race over and no cross-package
ordering dependency.
**Mitigation**: N/A.

### 5. tsgo (TypeScript Native) Internal Parallelism

**Risk**: `tsgo` uses goroutines for type-checking. Internal ordering of diagnostic messages may vary across runs on different hardware.
**Status**: tsgo `.d.ts` outputs are deterministic (Go's `sort.Slice` is not random). Diagnostic message ordering is consistent within a single binary but may differ between tsgo versions.
**Mitigation**: Pin the tsgo version. This repository pins it as `_DEFAULT_TSGO_VERSION` in `ts/extensions.bzl`; a consumer overrides it with `ts.tsgo(version = ...)` on the `ts` module extension.

### 6. Environment Variable Leaks

**Risk**: If an action reads an environment variable that is not declared in its `env` map, the value leaks from the host environment and can cause different outputs on different machines.
**Status**: Bazel's sandbox blocks undeclared env vars for rules that use `use_default_shell_env = False`. All rules in rules_typescript use the sandbox with no default shell env.
**Mitigation**: Run with `--incompatible_strict_action_env` to hard-fail if any action reads an undeclared env var.

### 7. Host Interpreters and Utilities

**Risk**: An action that shells out to whatever interpreter or coreutil the host
happens to have produces whatever that version produces.
**Status**: not applicable to this ruleset. There is no Python anywhere in it —
the house rule is Starlark's `json.decode`/`json.encode` or awk. `package.json`
generation in `ts_npm_publish` runs a JS script through the registered JS runtime
toolchain, its patch is Starlark-encoded JSON, and staging and tarballing are a
checked-in Go binary rather than `install` and `tar`. The one host dependency
left is `bash`, for a handful of build-action wrappers (the Vite bundler,
`next_build`, and the `node_modules` fallback
taken only when no JS runtime toolchain is registered).
**Mitigation**: none needed. If you add a `genrule` of your own, reach for a
toolchain input rather than a host binary.

### 8. Gazelle-Generated BUILD Files

**Risk**: Gazelle updates BUILD files in-place. If two developers run Gazelle on different OS/filesystem configurations (e.g. different file listing order), the generated files may differ.
**Status**: The Gazelle TypeScript extension sorts all generated `srcs`, `deps`, and other list attributes. Generated BUILD files are fully deterministic for a fixed source tree.
**Mitigation**: Enforce a Gazelle check step in CI: `bazel run //:gazelle && git diff --exit-code`. This ensures the checked-in BUILD files always match what Gazelle would generate.

### Summary Table

| Source | Affects | Deterministic? | Notes |
|--------|---------|---------------|-------|
| oxc compiled .js/.js.map | Compilation | Yes | No timestamps |
| tsgo generated .d.ts | Type checking | Yes | Sorted output |
| Vite bundle | Bundling | Yes (per source tree) | Chunk hashes change with source |
| ts_npm_publish package.json | Publishing | Yes | generated by a toolchain JS runtime; key order fixed by the script |
| node_modules tree | Runtime | Yes | per-package isolation |
| Gazelle BUILD generation | Repo structure | Yes | sorted output |

## What this pipeline does and does not guarantee

- **Determinism** is verified by the `determinism` CI job, over the targets it
  names — it is not a blanket property of every rule. `next_build` in particular
  is not byte-reproducible: Next.js bakes the project path into its server
  bundles and mints a random `BUILD_ID`. So "cache hits are safe" holds for what
  the job covers and is an assumption everywhere else.
- **A release tarball** is `git archive` over a tag, so it is a function of the
  commit.
- **Sandbox isolation** is the sandbox's, with no default shell env — see
  [Environment Variable Leaks](#6-environment-variable-leaks).

## Release Process

### Prerequisites

- Clean working tree (no uncommitted changes)
- Git tags properly configured
- Valid semantic version (X.Y.Z or X.Y.Z-prerelease)

### Cutting a Release

```bash
bazel run //tools/release -- 0.2.0 --dry-run   # prints every step, writes nothing
bazel run //tools/release -- 0.2.0 --push
```

The tool validates the version, refuses a dirty tree or an existing tag,
rewrites the version inside `module()` in `MODULE.bazel` (and nowhere else, so
`bazel_dep` versions survive), commits, tags, and optionally pushes. It works on
the checkout you ran `bazel` from, via `BUILD_WORKING_DIRECTORY`.

Everything after the tag is `.github/workflows/release.yml`: `git archive`
tarball, SRI hash, GitHub release with a provenance attestation, and the PR that
fills in `.bcr/source.json`. A locally built tarball would differ from the
published one, and so carry the wrong integrity hash — which is why the tool
does not build one.

Full walkthrough: [Release Process](RELEASE_PROCESS.md).

## BCR (Bazel Central Registry) Publishing

`.bcr/metadata.json` (module-level, one file for every version) and
`.bcr/source.json` (the tarball URL, its SRI hash and `strip_prefix`) are what a
BCR submission carries. Both are computed and committed by the `update-bcr` job
in `.github/workflows/release.yml`; the field-by-field walkthrough, the
`presubmit.yml` matrix and the submission steps are in
[BCR Submission](BCR_SUBMISSION.md).

One detail worth knowing before reading a mismatched hash: `<VERSION>` is the tag
with its leading `v` stripped, which the workflow does once
(`VERSION="${TAG#v}"`) before building all three strings. So the `v` appears in
the release *path* and nowhere else — the tarball is
`rules_typescript-0.2.0.tar.gz` under tag `v0.2.0`, and `strip_prefix` matches
the `git archive --prefix` that produced it.

## Remote Caching

!!! note "Documented, not exercised"
    Nothing in this repository's own CI uses `--remote_cache`, `--disk_cache` or
    RBE. The setups below are configurations we believe are right rather than
    ones we run, and no cache-hit figure on this page was measured here.

A remote cache lets one machine reuse another's action outputs. What makes that
safe rather than merely fast is determinism, which the `determinism` job checks
over the targets it names — see [above](#determinism-verification) for what that
does and does not cover.

### BuildBuddy Setup

[BuildBuddy](https://www.buildbuddy.io) is a hosted remote cache with a free tier.

1. Create a free account at https://app.buildbuddy.io and get your API key.

2. Add to your workspace `.bazelrc`:

```
# Remote cache via BuildBuddy.
build:bb --remote_cache=grpcs://remote.buildbuddy.io
build:bb --remote_header=x-buildbuddy-api-key=<YOUR_API_KEY>
# Optional: upload local results so CI hits also benefit teammates.
build:bb --remote_upload_local_results
# Optional: stream build events to the BuildBuddy UI.
build:bb --bes_backend=grpcs://remote.buildbuddy.io
build:bb --bes_results_url=https://app.buildbuddy.io/invocation/
```

3. Use the flag to activate remote caching:

```bash
bazel build //... --config=bb
```

For CI, add `--config=bb` to all `bazel build` / `bazel test` invocations (see the GitHub Actions template).

### EngFlow Setup

[EngFlow](https://www.engflow.com) is a commercial Bazel cache and RBE provider used by larger teams.

```
# .bazelrc
build:engflow --remote_cache=grpcs://your-cluster.engflow.com
build:engflow --remote_header=Authorization=Bearer <TOKEN>
build:engflow --remote_upload_local_results
```

### Self-Hosted Bazel Cache

For air-gapped or cost-sensitive environments you can run a minimal HTTP cache:

```bash
# Using bazel-remote (open source)
docker run -u 1000:1000 -v /path/to/cache:/data \
  -p 9090:9090 buchgr/bazel-remote-cache \
  --max_size 10
```

Then in `.bazelrc`:

```
build:local-cache --remote_cache=http://localhost:9090
```

### Verifying Hermeticity

All actions in rules_typescript run inside the Bazel sandbox with no network access by default. To confirm there are no hidden external dependencies:

```bash
bazel build //... --sandbox_default_allow_network=false
```

A clean build should succeed without any network errors. If an action fails, it is downloading something it should not be, and the rule needs to declare that dependency explicitly.

Common sources of non-hermeticity to watch for:
- Shell scripts that call `curl` or `wget` without declaring network access.
- Node scripts that call `npm install` at build time.
- Toolchain binaries that phone home on first run (common with some TypeScript tools).

### Cache Hit Rate Tuning

To maximise cache hit rates:

1. **Use `--remote_upload_local_results`**: ensures local developer builds populate the shared cache.
2. **Keep `--workspace_status_command` outputs stable**: stamp variables embedded in binaries bust the cache for every commit. Avoid stamping library targets.
3. **Check for volatile env leaks**: `bazel build //... --action_env` shows every env var that actions see; only variables that affect outputs should be present.

---

## Remote Execution

Remote execution (RBE) runs actions on a pool of workers instead of locally. Same
caveat as remote caching: nothing here is exercised by this repository's CI.

### Prerequisites

Before enabling RBE:

1. A compatible RBE backend (BuildBuddy RBE, EngFlow, Google RBE, or self-hosted).
2. A Docker image containing the build toolchain (oxc-bazel, Node.js, tsgo).
3. Platform constraints declared in your workspace (see below).

### Platform Constraints

RBE requires Bazel to know the execution platform so it can select the correct toolchain binaries. Add a `platforms` target to your workspace:

```python
# platforms/BUILD.bazel
platform(
    name = "linux_x86_64",
    constraint_values = [
        "@platforms//os:linux",
        "@platforms//cpu:x86_64",
    ],
)
```

And reference it in `.bazelrc`:

```
build:rbe --host_platform=//platforms:linux_x86_64
build:rbe --platforms=//platforms:linux_x86_64
```

### Toolchain Binary Compatibility

rules_typescript downloads platform-specific binaries for:

| Tool | Source | Platforms |
|------|--------|-----------|
| `oxc-bazel` | Built from Rust source via rules_rust | linux-x64, linux-arm64, darwin-x64, darwin-arm64 |
| `tsgo` | Downloaded npm package | linux-x64, linux-arm64, darwin-x64, darwin-arm64 |
| Node.js | JS runtime toolchain | linux/mac/win, x86_64/arm64 |

These binaries are statically linked (oxc-bazel, tsgo) or self-contained (Node.js), so they run on any modern Linux RBE worker without additional library dependencies.

### BuildBuddy RBE Setup

BuildBuddy offers managed RBE with a free tier. To enable:

```
# .bazelrc
build:rbe --config=bb
# RBE-specific overrides.
build:rbe --remote_executor=grpcs://remote.buildbuddy.io
build:rbe --jobs=100
build:rbe --remote_instance_name=rules_typescript
```

The one host utility rules_typescript needs on an executor is `bash`, which the
BuildBuddy image has. Everything else an action runs — node, tsgo, oxc, pnpm — is
a toolchain input, and there is no Python anywhere in the ruleset.

### EngFlow RBE Setup

```
# .bazelrc
build:rbe --remote_executor=grpcs://your-cluster.engflow.com
build:rbe --jobs=200
build:rbe --remote_instance_name=default
```

### Custom Executor Image

If you need a custom executor image (e.g. for additional system tools), build one from the minimal image below:

```dockerfile
FROM ubuntu:22.04
# Only a POSIX shell is needed: the Vite bundler and the framework build rules
# (next_build) wraps its action in bash.
# Everything else runs a declared binary — no host tar, no python, no coreutils
# dependency.
RUN apt-get update && apt-get install -y \
    bash \
    && rm -rf /var/lib/apt/lists/*
```

Push to a container registry and configure in EngFlow or your self-hosted RBE cluster.

### Testing RBE Locally

To test RBE connectivity without running your entire build:

```bash
bazel build //tests/smoke:hello --config=rbe --verbose_failures
```

A successful build confirms that the RBE worker can receive actions and the toolchain binaries are executable on the remote platform.

---

## GitLab CI Template

Add this file as `.gitlab-ci.yml` (or import it from a shared template repository):

```yaml
# GitLab CI/CD template for rules_typescript workspaces.
# Adjust the image, cache backend, and registry variables to match your setup.

variables:
  # The Bazel remote cache address. Leave empty to disable remote caching.
  BAZEL_REMOTE_CACHE: ""
  # BuildBuddy API key (or your remote cache auth header).
  BUILDBUDDY_API_KEY: ""

default:
  image: ubuntu:22.04
  before_script:
    - apt-get update -qq && apt-get install -y -qq
        curl git tar unzip
    # Install Bazel using Bazelisk.
    - curl -fsSL https://github.com/bazelbuild/bazelisk/releases/latest/download/bazelisk-linux-amd64 -o /usr/local/bin/bazel
    - chmod +x /usr/local/bin/bazel
    # Warm up the Bazel server and download toolchains once.
    - bazel version

stages:
  - test
  - build

# ── Unit tests ─────────────────────────────────────────────────────────────────
unit-tests:
  stage: test
  script:
    - |
      CACHE_FLAGS=""
      if [[ -n "$BAZEL_REMOTE_CACHE" ]]; then
        CACHE_FLAGS="--remote_cache=$BAZEL_REMOTE_CACHE"
        if [[ -n "$BUILDBUDDY_API_KEY" ]]; then
          CACHE_FLAGS="$CACHE_FLAGS --remote_header=x-buildbuddy-api-key=$BUILDBUDDY_API_KEY"
        fi
        CACHE_FLAGS="$CACHE_FLAGS --remote_upload_local_results"
      fi
      bazel test //... $CACHE_FLAGS --cache_test_results=no
  artifacts:
    reports:
      junit: bazel-testlogs/**/test.xml
    when: always
    expire_in: 7 days
  cache:
    key: bazel-$CI_COMMIT_REF_SLUG
    paths:
      - .cache/bazel/

# ── Build examples ─────────────────────────────────────────────────────────────
build-examples:
  stage: build
  script:
    - |
      CACHE_FLAGS=""
      if [[ -n "$BAZEL_REMOTE_CACHE" ]]; then
        CACHE_FLAGS="--remote_cache=$BAZEL_REMOTE_CACHE"
        if [[ -n "$BUILDBUDDY_API_KEY" ]]; then
          CACHE_FLAGS="$CACHE_FLAGS --remote_header=x-buildbuddy-api-key=$BUILDBUDDY_API_KEY"
        fi
      fi
      bazel build //examples/... $CACHE_FLAGS || true  # non-critical
  allow_failure: true

# ── Determinism check ──────────────────────────────────────────────────────────
determinism:
  stage: build
  script:
    - |
      for base in a b; do
        bazel --output_base="$CI_PROJECT_DIR/.det_$base" \
          build --config=determinism //tests/smoke:hello
      done
      cmp \
        "$(bazel --output_base="$CI_PROJECT_DIR/.det_a" info bazel-bin)/tests/smoke/hello.js" \
        "$(bazel --output_base="$CI_PROJECT_DIR/.det_b" info bazel-bin)/tests/smoke/hello.js"
  allow_failure: false
```

To configure the output_base for the local Bazel cache so GitLab's `cache:` key works correctly, add a `.bazelrc` entry:

```
# .bazelrc  (add to the repo-level file)
startup --output_base=/root/.cache/bazel/output
```

---

## Troubleshooting

### Determinism Failures

`cmp` naming the differing byte offset is the first thing to read: a difference
early in a `.js` is usually a path that leaked in, and one late is usually a
timestamp. Then work through
[Known Sources of Non-Determinism](#known-sources-of-non-determinism) — the two
that reach a plain `ts_compile` target are a `genrule` of your own calling a host
tool, and an undeclared env var.

### Release Tool Issues

- **Dirty working tree**: commit or stash all changes; `--dry-run` reports what
  is uncommitted without touching anything
- **Tag exists**: `git tag -d <tag>` before push, or release the next patch
  version
- **"no rules_typescript MODULE.bazel found"**: `bazel run` was invoked from
  outside the checkout; the tool resolves the repo from
  `BUILD_WORKING_DIRECTORY` upward

### CI Failures

Check logs in GitHub Actions:
1. Click workflow run
2. Expand failed job
3. Review error output
4. Compare with local reproduction: `bazel test --config=ci //...`

## Related Documentation

- [Documentation index](index.md)
- [Release Process](RELEASE_PROCESS.md) — the walkthrough this page summarises
- [AGENTS.md](https://github.com/mikn/rules_typescript/blob/main/AGENTS.md) — architecture, for contributors
- [TODO.md](https://github.com/mikn/rules_typescript/blob/main/TODO.md) — roadmap
