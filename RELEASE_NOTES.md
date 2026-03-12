# Release and BCR Publishing Automation

This document summarizes the automated release and Bazel Central Registry (BCR) publishing workflow for rules_typescript.

## Workflow Overview

```
Git Tag (v0.1.0)
       ↓
[Release Workflow] (.github/workflows/release.yml)
  - Build tarball from git archive
  - Generate SLSA attestation
  - Create GitHub Release
  - Calculate SHA256 integrity hash
  - Update .bcr/source.json
  - Create PR with updated metadata
       ↓
GitHub Release Available
       ↓
[Publish to BCR Workflow] (.github/workflows/publish-to-bcr.yml)
  - Validate BCR metadata
  - Verify release artifacts
  - Generate submission checklist
  - Provide detailed instructions
       ↓
Manual BCR PR Submission
       ↓
BCR Pre-submission Tests
       ↓
Merged to Bazel Central Registry
```

## Files Created/Modified

### GitHub Workflows

#### `.github/workflows/release.yml` (NEW)
- **Trigger**: Git tag push matching `v*` (e.g., `v0.1.0`)
- **Purpose**: Automated release creation with attestation
- **Key steps**:
  1. Extract version from git tag
  2. Build tarball using `git archive`
  3. Compute SHA256 hash (hex and SRI format)
  4. Generate SLSA build provenance attestation (actions/attest-build-provenance)
  5. Create GitHub Release with tarball and attestation
  6. Update .bcr/source.json with integrity hash
  7. Create PR to rules_typescript repo with updated metadata

**Outputs**:
- `version`: Semantic version extracted from tag
- `tarball`: Filename of release tarball
- `sha256`: Integrity hash in SRI format (sha256-<base64>)

#### `.github/workflows/publish-to-bcr.yml` (NEW)
- **Trigger**: Manual workflow dispatch or automatic on release publication
- **Purpose**: Validate metadata and guide BCR submission
- **Key steps**:
  1. Determine version from workflow input or release event
  2. Validate all BCR metadata files exist and are properly formatted
  3. Verify release exists on GitHub
  4. Check tarball download availability
  5. Generate submission summary with step-by-step instructions
  6. Upload metadata artifacts for archival

**Inputs** (for manual dispatch):
- `version`: Version to publish (e.g., 0.1.0)

#### `.github/workflows/ci.yml` (UPDATED)
- **Change**: Added multi-platform matrix for unit tests and E2E tests
- **Platforms**: ubuntu-latest, macos-latest
- **Purpose**: Ensure rules_typescript works across different operating systems

### BCR Configuration Files

#### `.bcr/metadata.json` (VERIFIED)
Contains module-level metadata shared across all versions:
- **homepage**: https://github.com/nicholasgasior/rules_typescript
- **maintainers**: Array with name, email, github username
- **repository**: GitHub repository reference
- **versions**: Maintained by BCR system (should be empty)
- **yanked_versions**: Deprecated versions (should be empty)

#### `.bcr/source.json` (UPDATED)
Version-specific release information:
- **url**: Direct link to GitHub release tarball
- **integrity**: SRI-format hash (sha256-<base64>) - set by release workflow
- **strip_prefix**: Directory name in tarball to strip during extraction

#### `.bcr/presubmit.yml` (UPDATED)
Testing configuration for BCR's CI:
- **matrix.platform**: [debian11, macos_arm64]
- **matrix.bazel**: [8.x, 9.x] - tests on Bazel 8 and 9
- **tasks**: Build and test commands to run on e2e/basic module
- **build_flags**: --keep_going (continue on errors)
- **test_flags**: --test_output=short (concise output)

### GitHub Configuration

#### `.github/dependabot.yml` (NEW)
Automated dependency updates:
- **github-actions**: Update workflow actions weekly
- **npm**: Update npm dependencies in eslint-plugin weekly
- **schedule**: Monday 3:00 AM UTC
- **labels**: Auto-applied to PRs (dependencies, ci, npm)

## Release Process

### Creating a Release

#### Using the Release Script (Recommended)

```bash
bash scripts/release.sh 0.1.0
git push origin v0.1.0
```

This script:
1. Validates version format
2. Checks working tree is clean
3. Updates MODULE.bazel version
4. Creates git commit and tag
5. Builds tarball
6. Computes SHA256
7. Updates .bcr/source.json

Then push the tag to trigger the GitHub Actions release workflow.

#### Manual Release

```bash
# 1. Update MODULE.bazel version
sed -i 's/version = "[^"]*"/version = "0.1.0"/' MODULE.bazel

# 2. Commit and tag
git add MODULE.bazel
git commit -m "Release v0.1.0"
git tag -a v0.1.0 -m "rules_typescript 0.1.0"

# 3. Push to trigger workflow
git push origin v0.1.0
```

### Release Workflow Execution

When you push a tag matching `v*`:

1. **Build Release** job runs on ubuntu-latest:
   - Extracts version from tag
   - Creates tarball via `git archive`
   - Computes SHA256 hash
   - Generates SLSA attestation
   - Creates GitHub Release
   - Uploads artifacts

2. **Update BCR** job runs after Build Release:
   - Updates .bcr/source.json with integrity hash
   - Creates PR with updated metadata

### Verifying the Release

After the workflow completes:

```bash
# Check GitHub Releases
gh release view v0.1.0

# Download and verify tarball
wget https://github.com/nicholasgasior/rules_typescript/releases/download/v0.1.0/rules_typescript-0.1.0.tar.gz
sha256sum rules_typescript-0.1.0.tar.gz

# Verify SLSA attestation
gh release view v0.1.0 --json assets
```

## BCR Submission

### Prerequisites

- [x] Release tag exists on GitHub
- [x] Tarball is available in GitHub Releases
- [x] SHA256 integrity hash in .bcr/source.json
- [x] SLSA attestation generated
- [x] All CI tests pass
- [x] .bcr/metadata.json is valid
- [x] .bcr/presubmit.yml is valid

### Submission Steps

#### Option 1: Manual Submission (Recommended for first submission)

1. Fork https://github.com/bazelbuild/bazel-central-registry

2. Clone and create branch:
   ```bash
   git clone https://github.com/YOUR-USERNAME/bazel-central-registry.git
   cd bazel-central-registry
   git checkout -b rules_typescript-v0.1.0
   ```

3. Create module directory:
   ```bash
   mkdir -p modules/rules_typescript/0.1.0
   ```

4. Copy files from rules_typescript repo:
   ```bash
   cp /path/to/rules_typescript/.bcr/metadata.json modules/rules_typescript/
   cp /path/to/rules_typescript/.bcr/source.json modules/rules_typescript/0.1.0/
   cp /path/to/rules_typescript/.bcr/presubmit.yml modules/rules_typescript/0.1.0/
   ```

5. Verify files:
   ```bash
   cat modules/rules_typescript/metadata.json
   cat modules/rules_typescript/0.1.0/source.json
   cat modules/rules_typescript/0.1.0/presubmit.yml
   ```

6. Commit and push:
   ```bash
   git add modules/rules_typescript/
   git commit -m "Add rules_typescript 0.1.0"
   git push origin rules_typescript-v0.1.0
   ```

7. Create PR on GitHub
   - Title: "Add rules_typescript 0.1.0"
   - Description: Include release notes and link to release tag

#### Option 2: Manual Trigger of Publish Workflow

```bash
gh workflow run publish-to-bcr.yml \
  -f version=0.1.0 \
  -R nicholasgasior/rules_typescript
```

This workflow generates detailed submission instructions and validates all metadata.

### What Happens in BCR

Once the PR is merged:

1. BCR system indexes the new module version
2. Pre-submission tests run with matrix:
   - Platforms: debian11, macos_arm64
   - Bazel versions: 8.x, 9.x
3. Tests run e2e/basic module from tarball
4. If all pass, version becomes available
5. Users can depend on it:
   ```python
   bazel_dep(name = "rules_typescript", version = "0.1.0")
   ```

## Automation Details

### Release Workflow Details

**File**: `.github/workflows/release.yml`

**When triggered**:
- Git tag push matching pattern `v*`
- Example: `git push origin v0.1.0`

**Environment**:
- runs-on: ubuntu-latest
- Permissions: contents:write, id-token:write

**Key Actions**:
- `actions/checkout@v4` - Clone repository
- `actions/attest-build-provenance@v2` - Generate SLSA attestation
- `softprops/action-gh-release@v2` - Create GitHub release
- `actions/upload-artifact@v4` - Upload for next job

**Process**:
1. Extract version from tag (v0.1.0 → 0.1.0)
2. Build tarball with `git archive --format=tar.gz`
3. Compute SHA256 hash and convert to SRI format
4. Generate SLSA v1 attestation (supply chain security)
5. Create GitHub Release with tarball attached
6. Compute integrity hash for BCR
7. Upload release info artifact
8. Update BCR metadata files
9. Create PR to update .bcr/source.json

**Outputs available to other jobs**:
- version: 0.1.0
- tarball: rules_typescript-0.1.0.tar.gz
- sha256: sha256-<base64-hash>

### Publish to BCR Workflow Details

**File**: `.github/workflows/publish-to-bcr.yml`

**Triggers**:
- Manual workflow dispatch (specify version)
- Automatic on GitHub release publication

**Steps**:
1. Check if triggered by workflow_dispatch or release event
2. Determine version from input or release tag
3. Validate .bcr/metadata.json exists and is valid JSON
4. Validate .bcr/source.json exists and is valid JSON
5. Validate .bcr/presubmit.yml exists and is valid YAML
6. Verify release exists on GitHub using gh CLI
7. Check tarball download availability with curl
8. Generate submission summary and checklist
9. Print detailed step-by-step instructions
10. Comment on release with BCR info (if release event)
11. Upload metadata artifacts for archival

**Artifacts uploaded**:
- bcr-metadata-<version>/ containing:
  - .bcr/metadata.json
  - .bcr/source.json
  - .bcr/presubmit.yml

## Security & Supply Chain

### SLSA Attestation

The release workflow generates SLSA v1 build provenance using:
```yaml
- uses: actions/attest-build-provenance@v2
  with:
    subject-path: rules_typescript-0.1.0.tar.gz
```

This creates a signed attestation proving:
- The tarball was built from the GitHub repository
- Build ran on GitHub Actions infrastructure
- No tampering occurred between build and release

Attestation is available on the release page and verifiable with:
```bash
gh release view v0.1.0 --json assets
```

### Hash Verification

SHA256 integrity hash is computed and stored in:
- `sha256sum rules_typescript-0.1.0.tar.gz`
- `.bcr/source.json` (SRI format: sha256-<base64>)

Users can verify:
```bash
wget https://github.com/nicholasgasior/rules_typescript/releases/download/v0.1.0/rules_typescript-0.1.0.tar.gz
echo "sha256-HASH_FROM_SOURCE_JSON" | xxd -r -p | base64 -w0
# Should match computed hash
```

## Continuous Integration

### CI Matrix

The `.github/workflows/ci.yml` now tests on:
- **ubuntu-latest**: Primary Linux testing environment
- **macos-latest**: macOS testing (validates Darwin support)

For each platform:
- Unit tests
- Type checking (tsgo validation)
- E2E tests
- Determinism checks
- Bootstrap tests
- Linting

### Dependabot Integration

`.github/dependabot.yml` automates:
- **GitHub Actions**: Weekly updates to workflow action versions
- **npm dependencies**: Weekly updates to eslint-plugin dependencies
- **Schedule**: Monday 3:00 AM UTC
- **PR labels**: Automatically adds "dependencies" and platform labels

## Key Features

1. **Fully Automated**: Tag push triggers entire release and metadata update
2. **SLSA Attestation**: Cryptographically signed build provenance
3. **Multi-platform**: Tests on Linux and macOS
4. **Integrity Verification**: SHA256 SRI hashes for verification
5. **BCR Metadata**: Automatically computed and versioned
6. **Documentation**: Comprehensive submission guide included
7. **Validation**: All metadata validated before submission
8. **Audit Trail**: All steps logged and traceable
9. **No Manual Hashing**: Hash computed automatically by workflows
10. **PR Automation**: Metadata PR created automatically

## Testing the Workflow

### Dry Run (Without Creating Release)

View workflow file structure:
```bash
cat .github/workflows/release.yml
```

Validate YAML:
```bash
python3 -c "import yaml; yaml.safe_load(open('.github/workflows/release.yml'))"
```

### First Real Release

When ready to release v0.1.0:

1. Ensure all changes are committed to main
2. Run release script or manually tag
3. Push tag: `git push origin v0.1.0`
4. Monitor workflow on Actions tab
5. Wait for "Release" and "Update BCR" jobs to complete
6. Verify GitHub Release was created
7. Check .bcr/source.json was updated with integrity hash
8. Follow BCR submission steps in docs/BCR_SUBMISSION.md

## Troubleshooting

### Workflow Fails

Check GitHub Actions logs:
1. Go to repository Actions tab
2. Select failed workflow
3. Click job to see step details
4. Look for error messages

Common issues:
- **Tag not matching pattern**: Use `v*` format (e.g., `v0.1.0`)
- **Module.bazel version**: Ensure version field matches tag
- **Dirty working tree**: Commit all changes before tagging
- **Permission issues**: Workflows need contents:write, id-token:write

### Hash Mismatch

If .bcr/source.json has wrong hash:

1. Check workflow output for computed hash
2. Manually verify with:
   ```bash
   wget https://github.com/nicholasgasior/rules_typescript/releases/download/v0.1.0/rules_typescript-0.1.0.tar.gz
   sha256sum rules_typescript-0.1.0.tar.gz
   ```
3. Update .bcr/source.json with correct value
4. Commit and push

### BCR Tests Fail

If BCR pre-submission tests fail:

1. Run same test locally:
   ```bash
   cd e2e/basic
   bazel build //...
   bazel test //...
   ```
2. Fix issues in the code
3. Tag new version and retry
4. Or contact BCR maintainers with detailed error info

## References

- **Release Workflow**: `.github/workflows/release.yml`
- **BCR Publish Workflow**: `.github/workflows/publish-to-bcr.yml`
- **BCR Configuration**: `.bcr/metadata.json`, `.bcr/source.json`, `.bcr/presubmit.yml`
- **Submission Guide**: `docs/BCR_SUBMISSION.md`
- **Release Script**: `scripts/release.sh`

## Next Steps

1. Review `.github/workflows/release.yml` and `.github/workflows/publish-to-bcr.yml`
2. Read `docs/BCR_SUBMISSION.md` for detailed BCR submission process
3. When ready, create first release:
   ```bash
   bash scripts/release.sh 0.1.0
   git push origin v0.1.0
   ```
4. Monitor workflow execution
5. Verify GitHub Release and .bcr/source.json update
6. Submit to BCR following the guide
