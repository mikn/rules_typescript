# Bazel Central Registry (BCR) Submission Guide

This document describes how to submit rules_typescript to the Bazel Central Registry and manage releases.

## Overview

The release and BCR publishing process is fully automated using GitHub Actions:

1. **Release Workflow** (`.github/workflows/release.yml`)
   - Triggered on git tag push (e.g., `git tag v0.1.0`)
   - Creates GitHub release with tarball and attestation
   - Generates SHA256 integrity hash
   - Prepares BCR metadata

2. **Publish to BCR Workflow** (`.github/workflows/publish-to-bcr.yml`)
   - Validates BCR metadata
   - Verifies release artifacts
   - Provides submission checklist
   - Can be triggered manually or automatically after release

## Release Process

### 1. Create a Release

Release versions follow semantic versioning: `X.Y.Z`

```bash
# Set version variable
VERSION="0.1.0"

# Create and push tag (optionally use scripts/release.sh)
git tag -a "v${VERSION}" -m "rules_typescript ${VERSION}"
git push origin "v${VERSION}"
```

Or use the provided release script:

```bash
bash scripts/release.sh 0.1.0
git push origin v0.1.0
```

The script will:
- Update MODULE.bazel version
- Create git tag
- Build tarball
- Compute SHA256 hash
- Update .bcr/source.json

### 2. GitHub Release Creation

When you push the tag, GitHub Actions automatically:

- Builds the release tarball
- Generates SLSA attestation for supply chain security
- Creates a GitHub Release
- Updates BCR metadata files

The release workflow output includes:
- **version**: Semantic version (e.g., 0.1.0)
- **tarball**: Compressed archive (e.g., rules_typescript-0.1.0.tar.gz)
- **sha256**: Integrity hash in SRI format (sha256-...)

### 3. Verify Release Artifacts

After the workflow completes:

1. Check GitHub Releases: https://github.com/mikn/rules_typescript/releases
2. Verify tarball download works
3. Check SHA256 hash matches the build output

```bash
# Verify integrity
wget https://github.com/mikn/rules_typescript/releases/download/v0.1.0/rules_typescript-0.1.0.tar.gz
sha256sum rules_typescript-0.1.0.tar.gz
```

## BCR Submission Process

### Prerequisites

- Release tag exists on GitHub with tarball and attestation
- All CI checks pass (tests, validation, examples)
- MODULE.bazel version is updated
- .bcr/source.json has correct integrity hash

### Submission Steps

The BCR submission must be done via GitHub PR to https://github.com/bazelbuild/bazel-central-registry

#### Option A: Manual Submission (Recommended)

1. Fork https://github.com/bazelbuild/bazel-central-registry

2. Clone your fork:
   ```bash
   git clone https://github.com/YOUR-USERNAME/bazel-central-registry.git
   cd bazel-central-registry
   ```

3. Create a feature branch:
   ```bash
   git checkout -b rules_typescript-v0.1.0
   ```

4. Create the module directory:
   ```bash
   mkdir -p modules/rules_typescript/0.1.0
   ```

5. Copy metadata files from rules_typescript repo:
   ```bash
   # Get the files
   cp /path/to/rules_typescript/.bcr/metadata.json modules/rules_typescript/
   cp /path/to/rules_typescript/.bcr/source.json modules/rules_typescript/0.1.0/
   cp /path/to/rules_typescript/.bcr/presubmit.yml modules/rules_typescript/0.1.0/
   ```

6. Verify the files:
   ```bash
   cat modules/rules_typescript/metadata.json
   cat modules/rules_typescript/0.1.0/source.json
   cat modules/rules_typescript/0.1.0/presubmit.yml
   ```

7. Commit and push:
   ```bash
   git add modules/rules_typescript/
   git commit -m "Add rules_typescript 0.1.0"
   git push origin rules_typescript-v0.1.0
   ```

8. Create PR on GitHub:
   - Go to https://github.com/bazelbuild/bazel-central-registry
   - Click "New Pull Request"
   - Select your fork and branch
   - Fill PR title: "Add rules_typescript 0.1.0"
   - Fill PR description with details from release notes

#### Option B: Automated Submission (via GitHub Actions)

Trigger the publish-to-bcr workflow manually:

```bash
gh workflow run publish-to-bcr.yml \
  -f version=0.1.0 \
  -R mikn/rules_typescript
```

This workflow:
- Validates all metadata files
- Verifies release availability
- Generates submission checklist
- Provides detailed instructions

### BCR Metadata Files

#### .bcr/metadata.json

Contains module-level information (shared across all versions):

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

**Note**: The `versions` and `yanked_versions` arrays are maintained by the BCR system and should not be manually edited.

#### .bcr/source.json

Version-specific release information:

```json
{
  "url": "https://github.com/mikn/rules_typescript/releases/download/v0.1.0/rules_typescript-0.1.0.tar.gz",
  "integrity": "sha256-<base64-hash>",
  "strip_prefix": "rules_typescript-0.1.0"
}
```

Fields:
- **url**: Direct link to release tarball (must be GitHub Releases)
- **integrity**: SRI-format hash (sha256-base64)
- **strip_prefix**: Top-level directory in tarball (removes prefix when extracting)

#### .bcr/presubmit.yml

Testing configuration for BCR's CI system:

```yaml
bcr_test_module:
  module_path: "e2e/basic"
  matrix:
    platform:
      - debian11
      - macos_arm64
    bazel:
      - "8.x"
      - "9.x"
  tasks:
    run_tests:
      build_targets:
        - "//..."
      test_targets:
        - "//..."
```

Defines:
- **module_path**: Path to test module within the repository
- **matrix**: Combinations of platforms and Bazel versions to test
- **tasks**: Build and test commands to run

### Adding SOURCE.md (Optional)

For complex build requirements, add `.bcr/SOURCE.md`:

```markdown
# Building rules_typescript from source

## Prerequisites
- Bazel 8+
- Rust 1.94+
- Go 1.26+

## Build Instructions

### Building oxc_cli

The oxc Rust CLI requires the Rust toolchain. It's automatically built by Bazel's crate_universe extension.

```

Include this if the standard tarball extraction and build procedure needs documentation.

## Automating with Release Script

The `scripts/release.sh` automates most of the release process:

```bash
bash scripts/release.sh 0.1.0
```

This script:
1. Validates the version format
2. Checks the git working tree is clean
3. Updates MODULE.bazel version
4. Commits the change
5. Creates a git tag
6. Builds the release tarball
7. Computes SHA256 hash
8. Updates .bcr/source.json

**After running the script:**
```bash
git push origin v0.1.0
```

This triggers the GitHub Actions release workflow.

## CI/CD Workflows

### Release Workflow

**File**: `.github/workflows/release.yml`

**Triggered by**: Git tag push matching `v*`

**Steps**:
1. Extract version from tag
2. Build release tarball from git archive
3. Compute SHA256 hash (both hex and SRI format)
4. Generate SLSA build provenance attestation
5. Create GitHub Release with tarball
6. Upload release info for BCR workflow
7. Update BCR metadata files
8. Create PR with updated .bcr/source.json

**Outputs**:
- GitHub Release with tarball and attestation
- PR updating .bcr/source.json with correct hash

### Publish to BCR Workflow

**File**: `.github/workflows/publish-to-bcr.yml`

**Triggered by**:
- Manual workflow dispatch with version
- Automatic on release publication

**Steps**:
1. Determine version (from workflow input or release tag)
2. Validate BCR metadata files exist
3. Validate JSON format of metadata files
4. Verify release exists on GitHub
5. Check tarball download availability
6. Generate submission summary and checklist
7. Comment on release with BCR info
8. Upload metadata artifacts for reference

## Release Checklist

Before cutting a release:

- [ ] All commits are properly reviewed and merged
- [ ] CI tests pass (unit tests, E2E, examples)
- [ ] Type checking passes (validation)
- [ ] Determinism check passes
- [ ] Bootstrap tests pass
- [ ] README is up-to-date
- [ ] CHANGELOG or release notes prepared
- [ ] MODULE.bazel version is in main branch (can be done by release script)

For the actual release:

- [ ] Tag version using `git tag v0.1.0` or `bash scripts/release.sh 0.1.0`
- [ ] Push tag: `git push origin v0.1.0`
- [ ] Verify GitHub Actions workflows complete successfully
- [ ] Download tarball and verify integrity
- [ ] Check .bcr/metadata.json is correct
- [ ] Check .bcr/source.json has correct hash
- [ ] Submit to BCR following the steps above

For BCR submission:

- [ ] Fork bazel-central-registry
- [ ] Create feature branch
- [ ] Copy metadata files to correct location
- [ ] Verify file format and contents
- [ ] Commit and push
- [ ] Create PR with descriptive title and body
- [ ] Wait for BCR pre-submission tests to pass
- [ ] Respond to reviewer feedback if any

## Troubleshooting

### Release Workflow Fails

Check the workflow logs:
- Go to Actions tab in GitHub
- Click on the failed "Release" workflow
- Check step output for error details

Common issues:
- **Module.bazel version mismatch**: Ensure release script was run or version was manually updated
- **Tarball creation failed**: Check git archive command and directory structure
- **Hash computation failed**: Verify sha256sum and xxd are available (should be on Ubuntu)

### BCR Metadata Issues

Validate files locally:

```bash
# Validate JSON
python3 -c "import json; json.load(open('.bcr/metadata.json'))"
python3 -c "import json; json.load(open('.bcr/source.json'))"

# Validate YAML
python3 -c "import yaml; yaml.safe_load(open('.bcr/presubmit.yml'))"
```

### Integrity Hash Mismatch

Recalculate the correct hash:

```bash
VERSION="0.1.0"
TARBALL="rules_typescript-${VERSION}.tar.gz"

# Download from GitHub release
wget https://github.com/mikn/rules_typescript/releases/download/v${VERSION}/${TARBALL}

# Calculate SHA256 hash in SRI format
SHA256_HEX=$(sha256sum ${TARBALL} | awk '{print $1}')
SHA256_BASE64=$(echo -n "${SHA256_HEX}" | xxd -r -p | base64 -w0)

echo "sha256-${SHA256_BASE64}"
```

Update .bcr/source.json with the correct integrity value.

## BCR Submission Best Practices

1. **Use GitHub Releases**: The tarball must be hosted on GitHub Releases, not arbitrary URLs
2. **Include attestation**: Always generate SLSA provenance attestation for supply chain security
3. **Test before submission**: Run the e2e/basic tests to ensure module works
4. **Clear descriptions**: Provide detailed release notes and PR descriptions
5. **Respond quickly**: BCR maintainers may request changes or clarifications
6. **Semantic versioning**: Follow SemVer for version numbering (e.g., 0.1.0, not v0.1.0)
7. **Minimal files**: Only include necessary files in tarball, exclude test artifacts and caches

## References

- [Bazel Central Registry Documentation](https://github.com/bazelbuild/bazel-central-registry)
- [Module Metadata Schema](https://github.com/bazelbuild/bazel-central-registry#metadata)
- [Source Configuration](https://github.com/bazelbuild/bazel-central-registry#source-configuration)
- [Presubmit Testing](https://github.com/bazelbuild/bazel-central-registry#testing-your-module)
- [SLSA Framework](https://slsa.dev/)
