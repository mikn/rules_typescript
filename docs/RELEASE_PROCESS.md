# Release Process for rules_typescript

This document describes the complete release process, from development to Bazel Central Registry submission.

## Prerequisites

Before releasing, ensure:

1. **All tests pass**
   ```bash
   bash scripts/ci.sh
   ```

2. **Determinism is verified**
   ```bash
   bash scripts/verify_determinism.sh
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

## Step 1: Automated Release

Run the release script with your version:

```bash
bash scripts/release.sh 0.2.0
```

This script will:

1. Validate the version format
2. Check for uncommitted changes
3. Check if the git tag already exists
4. Update `MODULE.bazel` with the new version
5. Commit with message "Release v0.2.0"
6. Create an annotated git tag
7. Build a deterministic tarball (excluding build artifacts)
8. Compute SHA256 hash of the tarball
9. Update `.bcr/source.json` with the integrity hash
10. Print next steps

### Example Output

```
=== rules_typescript Release v0.2.0 ===

[1/5] Updating MODULE.bazel version...
✓ Updated MODULE.bazel to version 0.2.0

[2/5] Creating git tag...
✓ Created tag v0.2.0

[3/5] Building tarball...
✓ Created tarball: /tmp/rules_typescript-v0.2.0.tar.gz
-rw-r--r--  1 user  staff  15K Mar 11 20:30 /tmp/rules_typescript-v0.2.0.tar.gz

[4/5] Computing SHA256 hash...
SHA256: 1234567890abcdef...
✓ Computed hash

[5/5] Updating .bcr/source.json...
Updated .bcr/source.json:
{
  "integrity": "sha256-...",
  "strip_prefix": "rules_typescript-v0.2.0",
  "url": "https://github.com/nicholasgasior/rules_typescript/releases/download/v0.2.0/rules_typescript-v0.2.0.tar.gz"
}
✓ Updated source.json

=== Release Complete ===
✓ Version: 0.2.0
✓ Tag: v0.2.0
✓ Tarball: rules_typescript-v0.2.0.tar.gz
✓ SHA256: 1234567890abcdef...

Next steps:
1. Push the tag: git push origin v0.2.0
2. Create a GitHub release: https://github.com/nicholasgasior/rules_typescript/releases/new?tag=v0.2.0
3. Attach tarball: /tmp/rules_typescript-v0.2.0.tar.gz
4. Submit to BCR: https://github.com/bazelbuild/bazel-central-registry/pulls
   - Include .bcr/metadata.json and .bcr/source.json in the PR
```

## Step 2: Push to GitHub

Push the tag to GitHub:

```bash
git push origin v0.2.0
```

Verify the tag is visible:

```bash
git tag -l  # Shows local tags
git ls-remote --tags origin  # Shows remote tags
```

## Step 3: Create GitHub Release

1. Open GitHub: https://github.com/nicholasgasior/rules_typescript/releases/new?tag=v0.2.0

2. Fill in the form:
   - **Tag version**: v0.2.0 (auto-populated)
   - **Release title**: Release v0.2.0 or "TypeScript Rules v0.2.0"
   - **Description**: Include notable changes, bug fixes, and new features
   - **Attach tarball**: Upload `/tmp/rules_typescript-v0.2.0.tar.gz`
   - **Prerelease**: Check if this is a prerelease (rc, alpha, beta)

3. Click "Publish release"

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
- **Non-deterministic build**: Rerun verify_determinism.sh and fix issues
- **Licensing**: Ensure LICENSE file is included in tarball

## Rollback and Fixes

### If Something Goes Wrong Before Push

If you haven't pushed yet, you can undo:

```bash
# Undo the tag
git tag -d v0.2.0

# Undo the commit
git reset --soft HEAD~1

# Restore files
git restore MODULE.bazel .bcr/source.json
```

Then fix the issue and try again.

### If Something Goes Wrong After Push

If you've already pushed:

1. **Don't delete the tag** (others may have fetched it)
2. **Create a new patch release** (e.g., v0.2.1)
3. **Document the issue** in the v0.2.0 release notes

Example:

```bash
# Delete local tag and recreate with updated MODULE.bazel
git tag -d v0.2.0
# Fix the issue...
bash scripts/release.sh 0.2.1
git push origin v0.2.1
```

## Pre-release Workflow

For testing before a major release, use pre-release versions:

```bash
# Release candidate
bash scripts/release.sh 0.2.0-rc.1

# Beta release
bash scripts/release.sh 0.2.0-beta.1

# Alpha release
bash scripts/release.sh 0.2.0-alpha.1
```

These can be published to GitHub with the "Prerelease" checkbox enabled, but do not need BCR submission.

## Verification Checklist

Before declaring release complete:

- [ ] `scripts/ci.sh` passes all tests
- [ ] `scripts/verify_determinism.sh` passes
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
   bash scripts/release.sh 0.2.1
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
bash scripts/release.sh 0.2.1
```

### "Integrity hash is different"

This usually means the tarball is different. Causes:

- Different git commit used
- Timestamps in generated files
- Environment-specific build artifacts

Solution:

```bash
# Verify determinism
bash scripts/verify_determinism.sh

# Check git status
git status

# Recalculate hash
sha256sum /tmp/rules_typescript-v0.2.0.tar.gz
```

### "Module files are not valid YAML"

Your metadata.json or source.json has invalid syntax:

```bash
# Validate JSON
python3 -c "import json; json.load(open('.bcr/metadata.json'))"

# Check for common issues
- Trailing commas
- Unquoted strings
- Missing braces
```

## Next Steps

After BCR submission is approved, the module will be available:

```bash
bazel_dep(name = "rules_typescript", version = "0.2.0")
```

Users can add this to their MODULE.bazel file and use rules_typescript.

---

For questions or issues, see [CI_CD.md](./CI_CD.md) or the main [README.md](../README.md).
