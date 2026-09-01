# Release Process for rules_typescript

Cutting a tag through to a Bazel Central Registry submission.

Nothing has been released yet: there are no git tags and no GitHub releases, so
every version number below (`0.2.0`, `0.2.1`, …) shows the shape a release takes.
`MODULE.bazel` reads `0.2.0` and every install snippet on the site names it, so
that is the version a first release cuts. See
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

5. **Fold the changelog** — entries since the last release live in
   `changelog.d/`, one file per PR, and are not in `CHANGELOG.md` until this
   runs:
   ```bash
   bazel run //tools/changelog                              # preview
   bazel run //tools/changelog -- --version 0.2.0 --write   # write and clear
   git add CHANGELOG.md changelog.d
   git commit -m "docs(changelog): assemble v0.2.0"
   ```
   It inserts the assembled section above the newest release and deletes the
   fragments it consumed. It belongs here rather than after Step 1:
   `//tools/release` refuses to run against a dirty working tree, and the tag
   has to carry the changelog.

## Step 1: Bump, Commit, Tag

```bash
bazel run //tools/release -- 0.2.0 --dry-run   # prints every step, writes nothing
bazel run //tools/release -- 0.2.0
```

The tool acts on the checkout you ran `bazel` from (`BUILD_WORKING_DIRECTORY`)
and:

1. Validates the version format
2. Stops if the tag already exists or the working tree is dirty
3. Rewrites the version inside `module()` in `MODULE.bazel` — and only there,
   so `bazel_dep` versions are untouched
4. Commits `MODULE.bazel` as `chore: release v0.2.0`
5. Creates the annotated tag `v0.2.0`

It stops there. Everything downstream of the tag belongs to
`.github/workflows/release.yml`: it builds the tarball with `git archive`,
computes the SRI hash, publishes the GitHub release with a build-provenance
attestation, and opens the PR that fills in `.bcr/source.json`. A tarball built
locally is a different archive from the published one, and so carries a wrong
integrity hash.

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

Pushing the tag starts the Release workflow:

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

The workflow publishes with `prerelease: false`, so mark the release as a
prerelease by hand if the version carries an `-rc.N`, `-alpha.N`, or `-beta.N`
suffix.

## Step 4: Submit to Bazel Central Registry

This is the manual half, written out once in
[BCR Submission](BCR_SUBMISSION.md#submission-steps): fork the registry, create
`modules/rules_typescript/<version>/`, copy `.bcr/metadata.json`,
`.bcr/source.json` and `.bcr/presubmit.yml` into it, push, open the PR. Follow
that page.

Check one thing before starting: the `.bcr/source.json` PR the release workflow
opened has merged, so the file you are copying carries the real integrity hash
and not the empty placeholder.

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

The tarball differs. Causes:

- Different git commit used
- Timestamps in generated files
- Environment-specific build artifacts

Solution:

```bash
# Verify determinism (see Prerequisites for the two-output-base sequence)
bazel build --config=determinism //tests/smoke:hello

# Check git status
git status

# Recalculate hash. The tarball name carries no `v` -- the workflow strips it
# from the tag before naming the archive.
sha256sum rules_typescript-0.2.0.tar.gz
```

### "Module files are not valid YAML"

Your metadata.json or source.json has invalid syntax. Usual causes: a trailing
comma, an unquoted string, a missing brace. A stdlib-only local check:

```bash
python3 -m json.tool .bcr/metadata.json > /dev/null
python3 -m json.tool .bcr/source.json   > /dev/null
```

This is a maintainer's local convenience. No rule, action or toolchain in
rules_typescript uses Python.

## Next Steps

After BCR submission is approved, the module will be available:

```bash
bazel_dep(name = "rules_typescript", version = "0.2.0")
```

Users can add this to their MODULE.bazel file and use rules_typescript.

---

For questions or issues, see [CI_CD.md](./CI_CD.md) or [the documentation index](index.md).
