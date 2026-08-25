# Release Process for rules_typescript

This document describes the complete release process, from development to Bazel
Central Registry submission.

**Nothing has been released yet.** There are no git tags and no GitHub releases,
so every version number below (`0.2.0`, `0.2.1`, …) is an example of the shape a
release takes, not a record of one. The first release will be `0.1.0`. See
[BCR Submission](BCR_SUBMISSION.md) for current status.

## Prerequisites

Before releasing, ensure:

1. **All tests pass** — the lane CI runs, named in `.bazelrc`:
   ```bash
   bazel test --config=ci //...
   bazel build --config=ci //... --output_groups=+_validation
   ```
   `e2e/` and `examples/` are separate workspaces, so they are separate
   invocations: `cd e2e/basic && bazel test //...`, then `bazel build //...`
   in each `examples/*` directory.

2. **Determinism is verified** — build one target from two empty output bases
   and compare, which is what the `determinism` job in
   `.github/workflows/ci.yml` does:
   ```bash
   for base in a b; do
     bazel --output_base="$HOME/.cache/det_$base" \
       build --config=determinism //tests/smoke:hello
   done
   cmp \
     "$(bazel --output_base="$HOME/.cache/det_a" info bazel-bin)/tests/smoke/hello.js" \
     "$(bazel --output_base="$HOME/.cache/det_b" info bazel-bin)/tests/smoke/hello.js"
   ```

3. **Working tree is clean**
   ```bash
   git status  # Should show nothing or only untracked files
   ```

4. **Decide on version number** (follow semantic versioning)
   - Major version: Breaking changes
   - Minor version: New features (backward compatible)
   - Patch version: Bug fixes
   - Pre-release: X.Y.Z-rc.1, X.Y.Z-alpha, etc.

## Step 1: Bump, Commit, Tag

```bash
bazel run //tools/release -- 0.2.0 --dry-run   # prints every step, writes nothing
bazel run //tools/release -- 0.2.0
```

The tool acts on the checkout you ran `bazel` from (`BUILD_WORKING_DIRECTORY`)
and:

1. Validates the version format
2. Refuses if the tag already exists or the working tree is dirty
3. Rewrites the version inside `module()` in `MODULE.bazel` — and only there,
   so `bazel_dep` versions are untouched
4. Commits `MODULE.bazel` as `chore: release v0.2.0`
5. Creates the annotated tag `v0.2.0`

It stops there. Everything downstream of the tag belongs to
`.github/workflows/release.yml`: it builds the tarball with `git archive`,
computes the SRI hash, publishes the GitHub release with a build-provenance
attestation, and opens the PR that fills in `.bcr/source.json`. Producing a
tarball locally would produce a *different* archive from the published one, and
so a wrong integrity hash.

### Example Output

```
Repository: /home/you/src/rules_typescript
Release:    v0.2.0

[1/3] MODULE.bazel: module version 0.1.0 -> 0.2.0
[2/3] commit MODULE.bazel
[3/3] tag v0.2.0

Nothing has been pushed. To publish:

  git push origin v0.2.0

That starts .github/workflows/release.yml: tarball, GitHub release, and the
.bcr/source.json PR. To undo instead: git tag -d v0.2.0 && git reset --hard HEAD~1
```

## Step 2: Push to GitHub

Pushing the tag is what starts the Release workflow:

```bash
git push origin v0.2.0
```

`bazel run //tools/release -- 0.2.0 --push` does the bump, tag, and push in one
go.

Verify the tag is visible:

```bash
git tag -l  # Shows local tags
git ls-remote --tags origin  # Shows remote tags
```

## Step 3: Watch the Release Workflow

The push in Step 2 triggers `.github/workflows/release.yml`, which creates the
GitHub release, attaches the `git archive` tarball, and opens the
`.bcr/source.json` PR. Nothing here is manual:

```bash
gh run list --workflow=release.yml
gh release view v0.2.0
```

Mark the release as a prerelease by hand if the version carries an `-rc.N`,
`-alpha.N`, or `-beta.N` suffix — the workflow publishes with
`prerelease: false`.

## Step 4: Submit to Bazel Central Registry

### 4.1 Fork the BCR

1. Go to: https://github.com/bazelbuild/bazel-central-registry
2. Click "Fork" and create your fork

### 4.2 Create Release Directory

Clone your fork and create the version directory:

```bash
git clone https://github.com/YOUR_USERNAME/bazel-central-registry.git
cd bazel-central-registry
mkdir -p modules/rules_typescript/0.2.0
```

### 4.3 Copy Files

Copy the metadata and source files:

```bash
# From your rules_typescript repo
cp .bcr/metadata.json ../bazel-central-registry/modules/rules_typescript/
cp .bcr/source.json ../bazel-central-registry/modules/rules_typescript/0.2.0/
```

### 4.4 Create Additional Files (Optional)

For first-time submissions, you may need to add:

```
modules/rules_typescript/0.2.0/
├── source.json              (required)
├── MODULE.bazel            (optional)
└── presubmit.yml           (optional, for CI checks)
```

See examples in the BCR repo for format.

### 4.5 Commit and Push

```bash
cd bazel-central-registry
git checkout -b rules_typescript-v0.2.0
git add modules/rules_typescript/
git commit -m "Add rules_typescript v0.2.0"
git push origin rules_typescript-v0.2.0
```

### 4.6 Create Pull Request

1. Open: https://github.com/bazelbuild/bazel-central-registry/pulls
2. Click "New pull request"
3. Select your fork and branch (rules_typescript-v0.2.0)
4. Fill in the description:

```
Add rules_typescript v0.2.0

## Summary
Brief description of changes and improvements in this release.

## Changes
- Feature 1
- Feature 2
- Bug fix 1

## Related Issues
- Fixes #123 (if applicable)

## BCR Compliance
- [x] Module files are valid YAML
- [x] source.json integrity hash is computed
- [x] Tarball is reproducible
- [x] All tests pass
```

5. Click "Create pull request"

## Step 5: Respond to BCR Feedback

The BCR maintainers will review your submission. They may:

1. **Request changes** to metadata or configuration
2. **Verify the integrity hash** by downloading and hashing the tarball
3. **Ask about compatibility** with their build system
4. **Request documentation** updates

Common issues:

- **Integrity mismatch**: Recalculate hash and update source.json
- **Missing metadata**: Add required fields to metadata.json
- **Non-deterministic build**: rerun the determinism check in Prerequisites and fix what differs
- **Licensing**: Ensure LICENSE file is included in tarball

## Rollback and Fixes

### If Something Goes Wrong Before Push

If you haven't pushed yet, you can undo:

```bash
# Undo the tag
git tag -d v0.2.0

# Undo the commit (the tool only ever touches MODULE.bazel)
git reset --hard HEAD~1
```

Then fix the issue and try again.

### If Something Goes Wrong After Push

If you've already pushed:

1. **Don't delete the tag** (others may have fetched it)
2. **Create a new patch release** (e.g., v0.2.1)
3. **Document the issue** in the v0.2.0 release notes

Example:

```bash
# Fix the issue, then cut the next patch version.
bazel run //tools/release -- 0.2.1 --push
```

## Pre-release Workflow

For testing before a major release, use pre-release versions:

```bash
bazel run //tools/release -- 0.2.0-rc.1
bazel run //tools/release -- 0.2.0-beta.1
bazel run //tools/release -- 0.2.0-alpha.1
```

These do not need BCR submission. `release.yml` publishes every tag with
`prerelease: false`, so tick "Set as a pre-release" on the GitHub release
afterwards.

## Verification Checklist

Before declaring release complete:

- [ ] `bazel test --config=ci //...` passes, and the `e2e/` and `examples/`
      workspaces build
- [ ] The determinism check in Prerequisites passes
- [ ] Git tag is created and pushed
- [ ] GitHub release is published with tarball
- [ ] Tarball is downloadable from GitHub
- [ ] SHA256 hash in source.json is correct
- [ ] BCR PR is created with metadata
- [ ] No uncommitted changes remain
- [ ] Version number is incremented in next development cycle

## Development Workflow After Release

After releasing v0.2.0, prepare for v0.2.1:

1. Update MODULE.bazel to next development version:
   ```
   version = "0.2.1-dev"
   ```

2. Continue development normally

3. When ready for next release:
   ```bash
   bazel run //tools/release -- 0.2.1
   ```

## Resources

- BCR Contributing Guide: https://github.com/bazelbuild/bazel-central-registry/blob/main/CONTRIBUTING.md
- Semantic Versioning: https://semver.org/
- Bazel Module Specification: https://bazel.build/external/module_registry
- GitHub Releases Help: https://docs.github.com/en/repositories/releasing-projects-on-github/about-releases

## Troubleshooting

### "Tag v0.2.0 already exists"

Someone has already released this version:

```bash
# Check existing tags
git tag -l | grep v0.2.0

# Create a patch version instead
bazel run //tools/release -- 0.2.1
```

### "Integrity hash is different"

This usually means the tarball is different. Causes:

- Different git commit used
- Timestamps in generated files
- Environment-specific build artifacts

Solution:

```bash
# Verify determinism (see Prerequisites for the two-output-base sequence)
bazel build --config=determinism //tests/smoke:hello

# Check git status
git status

# Recalculate hash
sha256sum /tmp/rules_typescript-v0.2.0.tar.gz
```

### "Module files are not valid YAML"

Your metadata.json or source.json has invalid syntax. Usual causes: a trailing
comma, an unquoted string, a missing brace. A stdlib-only local check:

```bash
python3 -m json.tool .bcr/metadata.json > /dev/null
python3 -m json.tool .bcr/source.json   > /dev/null
```

A maintainer's convenience, not a dependency — nothing in the ruleset itself uses
Python.

## Next Steps

After BCR submission is approved, the module will be available:

```bash
bazel_dep(name = "rules_typescript", version = "0.2.0")
```

Users can add this to their MODULE.bazel file and use rules_typescript.

---

For questions or issues, see [CI_CD.md](./CI_CD.md) or the main [the documentation index](index.md).
