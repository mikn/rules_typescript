# CI/CD & Production Readiness

## Overview

This document describes the CI/CD pipeline and release procedures for rules_typescript.

## GitHub Actions CI

The repository includes a comprehensive GitHub Actions workflow at `.github/workflows/ci.yml` that runs on every push and pull request.

### Workflow Jobs

1. **Unit Tests & Type Checking**
   - Runs all unit tests across the monorepo
   - Validates TypeScript compilation and type checking
   - Platform: Ubuntu latest

2. **E2E Tests**
   - Builds and tests the e2e/basic example
   - Verifies end-to-end functionality

3. **Examples Build**
   - Builds all examples (basic, app)
   - Non-critical; some examples may have incomplete features
   - Fails gracefully if features aren't implemented

4. **Build Determinism**
   - Verifies builds are bit-for-bit reproducible
   - Uses `scripts/verify_determinism.sh`
   - Ensures builds are cacheable and releasable

5. **Linting & Code Quality** (optional)
   - Buildifier formatting checks
   - Can be disabled if buildifier is unavailable

### Triggering CI

The workflow runs automatically on:
- Pushes to `main` and `develop` branches
- Pull requests against `main`

To trigger manually (requires GitHub web UI):
1. Go to Actions tab
2. Select "CI" workflow
3. Click "Run workflow"

## Local CI Script

For local development and CI/CD integration, use `scripts/ci.sh`:

```bash
# Run all tests and validations
bash scripts/ci.sh

# Run with verbose output
bash scripts/ci.sh --verbose

# Continue even if some checks fail
bash scripts/ci.sh --keep-going
```

### What the Script Does

1. Tests main workspace (unit tests + integration tests)
2. Validates TypeScript compilation (type checking)
3. Builds and tests e2e examples
4. Builds all examples (non-critical)

### Exit Code

- `0`: All critical checks passed
- `1`: One or more critical checks failed

## Determinism Verification

Builds must be deterministic for:
- Reliable remote caching
- Reproducible releases
- Cache hit rates

Run the determinism check:

```bash
bash scripts/verify_determinism.sh
```

The script:
1. Builds `//tests/smoke:hello` with output_base_1
2. Builds the same target with output_base_2
3. Compares the output files (byte-for-byte)
4. Reports SHA256 hash

**Note:** Uses `--output_base` instead of `bazel clean` to preserve build cache.

## Release Process

### Prerequisites

- Clean working tree (no uncommitted changes)
- Git tags properly configured
- Valid semantic version (X.Y.Z or X.Y.Z-prerelease)

### Automated Release

```bash
bash scripts/release.sh <version>
```

Example:
```bash
bash scripts/release.sh 0.2.0
```

### What the Script Does

1. **Validates version format** (semantic versioning)
2. **Updates MODULE.bazel** with new version
3. **Commits and tags** the version (e.g., `v0.2.0`)
4. **Builds tarball** excluding build artifacts and git metadata
5. **Computes SHA256** hash of the tarball
6. **Updates `.bcr/source.json`** with the hash

### Generated Artifacts

After running `scripts/release.sh 0.2.0`:

- Git tag: `v0.2.0`
- Tarball: `/tmp/rules_typescript-v0.2.0.tar.gz`
- Updated: `.bcr/source.json` with integrity hash

### Manual Steps After Release

```bash
# 1. Push the tag to GitHub
git push origin v0.2.0

# 2. Create a GitHub Release
# Go to: https://github.com/mikn/rules_typescript/releases/new?tag=v0.2.0
# - Upload the tarball
# - Write release notes

# 3. Submit to Bazel Central Registry (BCR)
# - Fork: https://github.com/bazelbuild/bazel-central-registry
# - Create PR with rules_typescript entry:
#   - Path: modules/rules_typescript/0.2.0/
#   - Include: .bcr/metadata.json, .bcr/source.json
# - See: https://github.com/bazelbuild/bazel-central-registry/blob/main/CONTRIBUTING.md
```

## BCR (Bazel Central Registry) Publishing

### Metadata Configuration

The `.bcr/metadata.json` file contains maintainer information and repository metadata:

```json
{
  "homepage": "https://github.com/mikn/rules_typescript",
  "maintainers": [
    {
      "name": "Nicholas Gasior",
      "email": "nicholas@lovable.app",
      "github": "mikn"
    }
  ],
  "repository": ["github:mikn/rules_typescript"],
  "versions": [],
  "yanked_versions": {}
}
```

### Source Configuration

The `.bcr/source.json` file specifies the release tarball location and integrity:

```json
{
  "integrity": "sha256-<BASE64_SHA256>",
  "strip_prefix": "rules_typescript-{TAG}",
  "url": "https://github.com/mikn/rules_typescript/releases/download/v{TAG}/rules_typescript-v{TAG}.tar.gz"
}
```

The `integrity` field is automatically computed and updated by `scripts/release.sh`.

### BCR Submission Checklist

- [ ] Release script executed (`bash scripts/release.sh <version>`)
- [ ] GitHub release created with tarball uploaded
- [ ] Fork of bazel-central-registry created
- [ ] New version directory created: `modules/rules_typescript/<version>/`
- [ ] `.bcr/metadata.json` copied to version directory
- [ ] `.bcr/source.json` copied to version directory
- [ ] PR created with description of changes
- [ ] Maintainer email verified for first release
- [ ] All BCR checks passed

### Resources

- BCR Repository: https://github.com/bazelbuild/bazel-central-registry
- Contributing Guide: https://github.com/bazelbuild/bazel-central-registry/blob/main/CONTRIBUTING.md
- BCR Format Spec: https://registry.bazel.build/

## Remote Caching

Remote caching allows build artifacts from one machine to be reused by another. All rules_typescript actions are designed to be hermetic and produce deterministic outputs, which maximises cache hit rates.

### Why Remote Caching Matters

- **Team builds**: engineers share a common cache, so the second person to build a target gets it from cache at network speed.
- **CI/CD**: CI builds from pull requests hit the same cache as the main-branch build, dramatically reducing CI time after the first full build.
- **Reproducibility**: deterministic outputs mean the same source always produces the same artifact. The `scripts/verify_determinism.sh` script validates this property.

### BuildBuddy Setup

[BuildBuddy](https://www.buildbuddy.io) is the easiest option — a hosted remote cache that has a generous free tier.

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
# Using bazel-remote (open source, highly recommended)
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

Remote execution (RBE) offloads compilation to a pool of workers, enabling massive parallelism. This section covers setting up RBE for rules_typescript workspaces.

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
| `oxc-bazel` | Built from Rust source via rules_rust | All (linux/mac/win, x86_64/arm64) |
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

The BuildBuddy executor image already includes basic POSIX utilities (`bash`, `install`, `tar`, `python3`) required by the shell actions in rules_typescript.

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
# rules_typescript shell actions use: bash, install (coreutils), tar, python3
RUN apt-get update && apt-get install -y \
    bash coreutils tar python3 \
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
        curl git python3 tar unzip
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
    - bash scripts/verify_determinism.sh
  allow_failure: false
```

To configure the output_base for the local Bazel cache so GitLab's `cache:` key works correctly, add a `.bazelrc` entry:

```
# .bazelrc  (add to the repo-level file)
startup --output_base=/root/.cache/bazel/output
```

---

## Known Sources of Non-Determinism

rules_typescript is designed for deterministic builds, and the `scripts/verify_determinism.sh` script verifies bit-for-bit reproducibility for smoke tests. However, certain configurations or downstream tools can introduce non-determinism. This section documents known causes and how to avoid them.

### 1. Build Timestamps in Compiled Output

**Risk**: Some compilers embed the current timestamp in their outputs.
**Status in rules_typescript**: `oxc` does not embed timestamps in compiled `.js` or `.js.map` files. `tsgo` does not embed timestamps in `.d.ts` files. Verified by `verify_determinism.sh`.
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

**Risk**: `npm_translate_lock` downloads npm tarballs in parallel; if two packages produce the same output file path, the winning tarball depends on download order.
**Status**: rules_typescript's `npm_translate_lock` downloads each package independently into its own directory. There is no cross-package ordering dependency.
**Mitigation**: N/A.

### 5. tsgo (TypeScript Native) Internal Parallelism

**Risk**: `tsgo` uses goroutines for type-checking. Internal ordering of diagnostic messages may vary across runs on different hardware.
**Status**: tsgo `.d.ts` outputs are deterministic (Go's `sort.Slice` is not random). Diagnostic message ordering is consistent within a single binary but may differ between tsgo versions.
**Mitigation**: Pin the tsgo version in your MODULE.bazel (already done via the `TSGO_VERSION` variable in `ts/private/toolchain.bzl`).

### 6. Environment Variable Leaks

**Risk**: If an action reads an environment variable that is not declared in its `env` map, the value leaks from the host environment and can cause different outputs on different machines.
**Status**: Bazel's sandbox blocks undeclared env vars for rules that use `use_default_shell_env = False`. All rules in rules_typescript use the sandbox with no default shell env.
**Mitigation**: Run with `--incompatible_strict_action_env` to hard-fail if any action reads an undeclared env var.

### 7. Python Version Differences

**Risk**: The `ts_npm_publish` rule runs a small Python 3 script to generate `package.json`. Different Python 3 minor versions may produce different JSON output (e.g. ordering of keys in `json.dumps`).
**Status**: Python's `json.dumps` produces stable output for the same input dictionary in any Python 3.7+ version (dictionaries are ordered by insertion order since Python 3.7). The script uses explicit key assignment, so output is stable.
**Mitigation**: Pin `python3` to a specific version in your container/CI image if you require bit-for-bit identical `package.json` files across machines.

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
| ts_npm_publish package.json | Publishing | Yes (Python 3.7+) | key order stable |
| node_modules tree | Runtime | Yes | per-package isolation |
| Gazelle BUILD generation | Repo structure | Yes | sorted output |

## CI/CD Best Practices

### Performance

1. **Parallel builds**: Bazel uses parallelism by default
2. **Incremental builds**: Only rebuild changed files
3. **Cache hits**: All builds are deterministic for consistent caching
4. **Remote caching**: Use remote cache for team builds

### Reliability

1. **Determinism**: Verified by `verify_determinism.sh`
2. **Reproducible releases**: All artifacts are bit-for-bit identical
3. **Sandbox isolation**: No system dependency leaks

### Monitoring

1. **Build metrics**: Track build time and cache hit rate
2. **Deployment frequency**: Monitor releases to BCR
3. **Test coverage**: Maintain comprehensive test suite

## Troubleshooting

### Determinism Failures

If `verify_determinism.sh` fails:

1. Check for timestamp issues in compiled output
2. Verify no non-deterministic sources (e.g., timestamps in generated files)
3. Review oxc and tsgo configuration for timestamp dependencies

### Release Script Issues

- **Dirty working tree**: Commit or stash all changes
- **Tag exists**: Delete and recreate with `git tag -d <tag>` (before push)
- **No jq available**: Script uses `sed` fallback for JSON updates
- **Different OS behavior**: Script handles macOS and Linux differences

### CI Failures

Check logs in GitHub Actions:
1. Click workflow run
2. Expand failed job
3. Review error output
4. Compare with local reproduction: `bash scripts/ci.sh --verbose`

## Related Documentation

- [README.md](../index.md) - Main documentation
- [TODO.md](https://github.com/mikn/rules_typescript/blob/main/TODO.md) - Development roadmap
- [AGENTS.md](https://github.com/mikn/rules_typescript/blob/main/AGENTS.md) - Architecture guide
