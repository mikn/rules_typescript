# Release Checklist

## Pre-Release Checklist (Do This Before Tagging)

### Code Quality
- [ ] All tests pass: `bazel test //... --cache_test_results=no`
- [ ] Type checking passes: `bazel build //... --output_groups=+_validation`
- [ ] E2E tests pass: `cd e2e/basic && bazel test //...`
- [ ] Determinism verified: `bash scripts/verify_determinism.sh`
- [ ] Bootstrap tests pass: `bazel test //tests/bootstrap:...`
- [ ] Examples build: `cd examples/basic && bazel build //...`
- [ ] Linting passes: `bazel run @buildifier//:buildifier -- -r . -l check`

### Documentation
- [ ] README.md is up-to-date
- [ ] CHANGELOG.md or release notes prepared
- [ ] Code comments are accurate
- [ ] No TODOs or FIXMEs in critical paths

### Configuration
- [ ] No uncommitted changes in git: `git status`
- [ ] Working tree is clean: `git diff-index --quiet HEAD --`
- [ ] All changes are pushed to remote
- [ ] No outstanding PRs that should be included

## Release Execution Checklist

### Option A: Using Release Script (Recommended)

```bash
VERSION="0.1.0"
bash scripts/release.sh $VERSION
```

**Verify script output**:
- [ ] MODULE.bazel version updated to 0.1.0
- [ ] Git tag created: v0.1.0
- [ ] Tarball built: rules_typescript-0.1.0.tar.gz
- [ ] SHA256 hash computed
- [ ] .bcr/source.json updated with hash
- [ ] No errors in final summary

**Next**:
```bash
git push origin v0.1.0
```

### Option B: Manual Release

```bash
VERSION="0.1.0"
TAG="v${VERSION}"

# 1. Update version in MODULE.bazel
sed -i 's/version = "[^"]*"/version = "0.1.0"/' MODULE.bazel
git add MODULE.bazel
git commit -m "Release v0.1.0"

# 2. Create tag
git tag -a "$TAG" -m "rules_typescript $VERSION"

# 3. Push tag to trigger workflow
git push origin "$TAG"
```

## GitHub Actions Workflow Checklist

After pushing the tag, monitor the release workflow:

### Release Workflow (`.github/workflows/release.yml`)

1. **Build Release Job**:
   - [ ] Starts on tag push
   - [ ] Extracts version from tag (v0.1.0 → 0.1.0)
   - [ ] Builds tarball with git archive
   - [ ] Computes SHA256 hash (hex format)
   - [ ] Generates SLSA attestation
   - [ ] Creates GitHub Release with tarball
   - [ ] Uploads release artifacts

2. **Update BCR Job**:
   - [ ] Updates .bcr/source.json with integrity hash (SRI format)
   - [ ] Creates PR with updated metadata
   - [ ] Title: "chore: Update BCR metadata for v0.1.0"

### Verify GitHub Release

```bash
# View release
gh release view v0.1.0

# Verify tarball exists
gh release view v0.1.0 --json assets

# Download and verify
wget https://github.com/nicholasgasior/rules_typescript/releases/download/v0.1.0/rules_typescript-0.1.0.tar.gz
sha256sum rules_typescript-0.1.0.tar.gz
```

**Checklist**:
- [ ] GitHub Release created
- [ ] Tarball available for download
- [ ] SLSA attestation present
- [ ] Release notes auto-generated
- [ ] Attestation is valid (can be verified)

### Verify BCR Metadata Update

```bash
# Check PR was created
gh pr list --state open --head "bcr-update-0.1.0"

# Verify .bcr/source.json has integrity hash
cat .bcr/source.json
# Should show:
# {
#   "url": "https://github.com/nicholasgasior/rules_typescript/releases/download/v0.1.0/rules_typescript-0.1.0.tar.gz",
#   "integrity": "sha256-<base64-hash>",
#   "strip_prefix": "rules_typescript-0.1.0"
# }
```

**Checklist**:
- [ ] .bcr/source.json URL points to GitHub release
- [ ] integrity field contains sha256-<base64> hash
- [ ] strip_prefix matches tarball directory name
- [ ] PR created with updated metadata

## BCR Submission Checklist

### Pre-Submission Validation

```bash
# Run publish-to-bcr workflow (optional but recommended)
gh workflow run publish-to-bcr.yml -f version=0.1.0
```

Wait for workflow to complete:
- [ ] Metadata validation passes
- [ ] BCR files are valid JSON/YAML
- [ ] Release exists on GitHub
- [ ] Tarball is downloadable
- [ ] Submission checklist printed

### Manual BCR Submission

1. **Fork and Prepare**:
   ```bash
   # Fork https://github.com/bazelbuild/bazel-central-registry
   git clone https://github.com/YOUR-USERNAME/bazel-central-registry.git
   cd bazel-central-registry
   git checkout -b rules_typescript-v0.1.0
   ```

2. **Create Module Directory**:
   ```bash
   mkdir -p modules/rules_typescript/0.1.0
   ```

3. **Copy Files**:
   ```bash
   cp /path/to/rules_typescript/.bcr/metadata.json modules/rules_typescript/
   cp /path/to/rules_typescript/.bcr/source.json modules/rules_typescript/0.1.0/
   cp /path/to/rules_typescript/.bcr/presubmit.yml modules/rules_typescript/0.1.0/
   ```

4. **Verify Structure**:
   ```
   modules/rules_typescript/
   ├── metadata.json          (shared across all versions)
   └── 0.1.0/
       ├── source.json        (version-specific)
       └── presubmit.yml      (version-specific)
   ```

5. **Validate Files**:
   ```bash
   python3 -c "import json; json.load(open('modules/rules_typescript/metadata.json'))"
   python3 -c "import json; json.load(open('modules/rules_typescript/0.1.0/source.json'))"
   python3 -c "import yaml; yaml.safe_load(open('modules/rules_typescript/0.1.0/presubmit.yml'))"
   ```

   **Checklist**:
   - [ ] metadata.json is valid JSON
   - [ ] source.json is valid JSON with correct URL and hash
   - [ ] presubmit.yml is valid YAML

6. **Commit and Push**:
   ```bash
   git add modules/rules_typescript/
   git commit -m "Add rules_typescript 0.1.0"
   git push origin rules_typescript-v0.1.0
   ```

7. **Create PR**:
   - Go to https://github.com/bazelbuild/bazel-central-registry
   - Click "New Pull Request"
   - Select your fork and branch (rules_typescript-v0.1.0)
   - Fill in title: "Add rules_typescript 0.1.0"
   - Fill in description with:
     - Link to release: https://github.com/nicholasgasior/rules_typescript/releases/tag/v0.1.0
     - Link to commit: https://github.com/bazelbuild/bazel-central-registry/commit/<sha>
     - Any release notes

   **Checklist**:
   - [ ] PR title is clear and includes version
   - [ ] PR description includes release notes
   - [ ] PR description links to release tag

### BCR Pre-Submission Tests

After PR is created, BCR runs tests:
- [ ] Pre-submission tests start (wait for status checks)
- [ ] Tests run on matrix:
  - debian11 + Bazel 8.x
  - debian11 + Bazel 9.x
  - macos_arm64 + Bazel 8.x
  - macos_arm64 + Bazel 9.x
- [ ] All builds pass (build //...)
- [ ] All tests pass (test //...)
- [ ] No timeout or infrastructure failures

**Checklist**:
- [ ] All pre-submission tests pass
- [ ] Status checks show green
- [ ] BCR maintainers approve the PR
- [ ] No conflicts with other PRs
- [ ] Ready for merge

### Post-Submission

After BCR maintainers merge the PR:
- [ ] Version is live on bazel.build
- [ ] Module is indexed by bazel-central-registry
- [ ] Users can depend on rules_typescript 0.1.0
- [ ] Verify by running: `bazel_dep(name = "rules_typescript", version = "0.1.0")`

**Checklist**:
- [ ] PR is merged
- [ ] Commit is visible in BCR repository
- [ ] bazel.build shows the module
- [ ] Documentation is updated

## Post-Release Checklist

### Update Documentation
- [ ] Update main README with latest version
- [ ] Update getting started guide
- [ ] Add release notes to docs
- [ ] Update any version references

### Prepare for Next Release
- [ ] Update MODULE.bazel to next pre-release version (e.g., 0.2.0-dev)
- [ ] Create entry in CHANGELOG for next release
- [ ] Plan next features in issues/projects

### Monitor Release
- [ ] Watch for issues reported by users
- [ ] Respond to GitHub issues promptly
- [ ] Track adoption and feedback

## Automation Commands Reference

### Quick Release

```bash
# Full release with one command
VERSION="0.1.0"
bash scripts/release.sh $VERSION && git push origin v$VERSION
```

### Manual Steps

```bash
# Extract version from tag
TAG="v0.1.0"
VERSION="${TAG#v}"

# Build tarball manually
git archive --format=tar.gz --prefix="rules_typescript-${VERSION}/" \
  --output="rules_typescript-${VERSION}.tar.gz" HEAD

# Compute hash
SHA256_HEX=$(sha256sum "rules_typescript-${VERSION}.tar.gz" | awk '{print $1}')
SHA256_BASE64=$(echo -n "${SHA256_HEX}" | xxd -r -p | base64 -w0)
echo "sha256-${SHA256_BASE64}"
```

### Verify Release

```bash
VERSION="0.1.0"

# Check GitHub release
gh release view v$VERSION --json assets

# Download and verify hash
wget https://github.com/nicholasgasior/rules_typescript/releases/download/v${VERSION}/rules_typescript-${VERSION}.tar.gz
sha256sum rules_typescript-${VERSION}.tar.gz

# Extract and inspect
tar -tzf rules_typescript-${VERSION}.tar.gz | head -20
```

### Check BCR Metadata

```bash
# Validate metadata
python3 -c "import json; print(json.dumps(json.load(open('.bcr/metadata.json')), indent=2))"
python3 -c "import json; print(json.dumps(json.load(open('.bcr/source.json')), indent=2))"
python3 -c "import yaml; print(yaml.dump(yaml.safe_load(open('.bcr/presubmit.yml'))))"
```

## Troubleshooting

### "Tag already exists"
```bash
# Delete local tag
git tag -d v0.1.0

# Delete remote tag
git push origin --delete v0.1.0

# Retag and push
git tag -a v0.1.0 -m "rules_typescript 0.1.0"
git push origin v0.1.0
```

### "Working tree has uncommitted changes"
```bash
# See what's uncommitted
git status
git diff

# Stash changes temporarily
git stash

# Create release, then reapply
git stash pop
```

### "Hash mismatch in source.json"
```bash
# Recalculate correct hash
TARBALL="rules_typescript-0.1.0.tar.gz"
SHA256_HEX=$(sha256sum "$TARBALL" | awk '{print $1}')
SHA256_BASE64=$(echo -n "${SHA256_HEX}" | xxd -r -p | base64 -w0)
echo "sha256-${SHA256_BASE64}"

# Update .bcr/source.json manually
# Then commit and push the fix
```

### "BCR tests fail"
```bash
# Run same tests locally
cd e2e/basic
bazel build //...
bazel test //...

# Check error output
gh workflow run publish-to-bcr.yml -f version=0.1.0
# Wait for workflow to complete and check logs
```

## Support

For help with releases:
1. Check `RELEASE_NOTES.md` for detailed workflow explanation
2. Check `docs/BCR_SUBMISSION.md` for BCR-specific guidance
3. Review `scripts/release.sh` for automated steps
4. Check GitHub Actions logs for workflow errors
5. Open issue in rules_typescript repository

## Quick Links

- **Release Workflows**: `.github/workflows/release.yml`, `.github/workflows/publish-to-bcr.yml`
- **BCR Config**: `.bcr/metadata.json`, `.bcr/source.json`, `.bcr/presubmit.yml`
- **Release Script**: `scripts/release.sh`
- **Documentation**: `RELEASE_NOTES.md`, `docs/BCR_SUBMISSION.md`
- **BCR Repository**: https://github.com/bazelbuild/bazel-central-registry
- **Rules TypeScript**: https://github.com/nicholasgasior/rules_typescript
