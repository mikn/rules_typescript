"""Unit tests for the lockfile reader behind ts.tsgo(pnpm_lock = ...).

The reader is text -> struct with no fetch in it, so every message a consumer
can get out of a lockfile is pinned here; //tests/integration/tsgo_lockfile
pins the fetch those structs drive and the fail() the extension turns an error
into.
"""

load("@bazel_skylib//lib:unittest.bzl", "asserts", "unittest")
load("//npm/private:npm_translate_lock.bzl", "npmrc_registries")
load("//ts/private:toolchain.bzl", "TSGO_PLATFORMS")
load("//ts/private:tsgo_lock.bzl", "tsgo_from_pnpm_lock", "tsgo_from_version")

_LOCK = "//:pnpm-lock.yaml"

# typescript@7.0.2 as pnpm writes it, trimmed to TSGO_PLATFORMS: the same text
# as tests/integration/tsgo_lockfile/workspace/pnpm-lock.yaml.
_STABLE_PACKAGES = """
  '@typescript/typescript-darwin-arm64@7.0.2':
    resolution: {integrity: sha512-gowzar9MwS/aRWp6f3a4KUqzRjAZjOsmGNCM6LcTgXum+dBfgsBVMN+AgvOCCbguXyick6LJhpBszxMebJ8syA==}
    engines: {node: '>=16.20.0'}
    cpu: [arm64]
    os: [darwin]

  '@typescript/typescript-darwin-x64@7.0.2':
    resolution: {integrity: sha512-SZ9xZInqApNlNGc9s0W1VSsktYSOe9cFqNOIqmN1Gs8SmkjKZYFt017G4VwPxASInODuAdbTW7sXiFUf893RgA==}
    engines: {node: '>=16.20.0'}
    cpu: [x64]
    os: [darwin]

  '@typescript/typescript-linux-arm64@7.0.2':
    resolution: {integrity: sha512-Qh4eU4/y3yDjnfjjyPYihMj5/ODIlmt+Bzu17OI+fiSRDW57QmU5SiN63exPRNJPKUzcc1INa1NXdrJ+MqHjUQ==}
    engines: {node: '>=16.20.0'}
    cpu: [arm64]
    os: [linux]

  '@typescript/typescript-linux-x64@7.0.2':
    resolution: {integrity: sha512-EYdf2cNg7rgCWJnxCdJ+F3V39O8ihb37eHAu1LK8oAFizgTQbPOK7zHHXbPt8rX24COqODXeI3sIf0fCXG7H/A==}
    engines: {node: '>=16.20.0'}
    cpu: [x64]
    os: [linux]

  typescript@7.0.2:
    resolution: {integrity: sha512-8FYau96o3NKOhbjKi/qNvG/W5jhzxkbdm5sj9AbZ/5T5sWqn3hJgLfGx27sRKZWTvyzCP8dLRBTf5tBTSRVUNA==}
    engines: {node: '>=16.20.0'}
    hasBin: true
"""

_STABLE_SNAPSHOTS = """
  '@typescript/typescript-darwin-arm64@7.0.2':
    optional: true

  '@typescript/typescript-darwin-x64@7.0.2':
    optional: true

  '@typescript/typescript-linux-arm64@7.0.2':
    optional: true

  '@typescript/typescript-linux-x64@7.0.2':
    optional: true

  typescript@7.0.2:
    optionalDependencies:
      '@typescript/typescript-darwin-arm64': 7.0.2
      '@typescript/typescript-darwin-x64': 7.0.2
      '@typescript/typescript-linux-arm64': 7.0.2
      '@typescript/typescript-linux-x64': 7.0.2
"""

_STABLE = """lockfileVersion: '9.0'

importers:

  .:
    devDependencies:
      typescript:
        specifier: 'catalog:'
        version: 7.0.2

packages:
""" + _STABLE_PACKAGES + """
snapshots:
""" + _STABLE_SNAPSHOTS

_INTEGRITY = {
    "darwin_arm64": "sha512-gowzar9MwS/aRWp6f3a4KUqzRjAZjOsmGNCM6LcTgXum+dBfgsBVMN+AgvOCCbguXyick6LJhpBszxMebJ8syA==",
    "darwin_amd64": "sha512-SZ9xZInqApNlNGc9s0W1VSsktYSOe9cFqNOIqmN1Gs8SmkjKZYFt017G4VwPxASInODuAdbTW7sXiFUf893RgA==",
    "linux_arm64": "sha512-Qh4eU4/y3yDjnfjjyPYihMj5/ODIlmt+Bzu17OI+fiSRDW57QmU5SiN63exPRNJPKUzcc1INa1NXdrJ+MqHjUQ==",
    "linux_amd64": "sha512-EYdf2cNg7rgCWJnxCdJ+F3V39O8ihb37eHAu1LK8oAFizgTQbPOK7zHHXbPt8rX24COqODXeI3sIf0fCXG7H/A==",
}

_NPM_ARCH = {
    "darwin_arm64": "darwin-arm64",
    "darwin_amd64": "darwin-x64",
    "linux_arm64": "linux-arm64",
    "linux_amd64": "linux-x64",
}

# The nightly, as the trial monorepo's lockfile carries it.
_NIGHTLY = """lockfileVersion: '9.0'

importers:

  .:
    devDependencies:
      '@typescript/native-preview':
        specifier: 7.0.0-dev.20251229.1
        version: 7.0.0-dev.20251229.1

packages:

  '@typescript/native-preview-darwin-arm64@7.0.0-dev.20251229.1':
    resolution: {integrity: sha512-AVo6J3Lrh0v900q9QdTXtzcOdKPuAuZ7U2hUEv/H6+yFoBcvrebRxsCbCuzBxqr8/RhIsE4Ue9m9RvbqRKNJTA==}
    cpu: [arm64]
    os: [darwin]

  '@typescript/native-preview-darwin-x64@7.0.0-dev.20251229.1':
    resolution: {integrity: sha512-+wtv9YvKYH1hjoj+5Ixc6aCaarUkPL5hSRtW1H0RRweoEnxU7O/3+/qr57W5NQ/mkQLFPeboOG7oUM1zW/X/Yg==}
    cpu: [x64]
    os: [darwin]

  '@typescript/native-preview-linux-arm64@7.0.0-dev.20251229.1':
    resolution: {integrity: sha512-e+RN9DdKJEOU7LQV8XcgxKwHaKX5V2eYDabJTcaRF1zan9C1YKB7tOVXC9iXmwLHhybOujr4yu8RI/gGb2cxcw==}
    cpu: [arm64]
    os: [linux]

  '@typescript/native-preview-linux-x64@7.0.0-dev.20251229.1':
    resolution: {integrity: sha512-/gW7OMxYU2O7fFV2qMwkA/d9gHFk1po+ufFucB719eunn84MtxHxO6SyJoK+nGlKPYRFMVAniB+0JLQlG2j4eg==}
    cpu: [x64]
    os: [linux]

  '@typescript/native-preview@7.0.0-dev.20251229.1':
    resolution: {integrity: sha512-hjACA2wU89fwpTn3MNFFnRMtMUZfqnSsyHQCQzSF39d+vahzqVrWvBpBZRO4TaSKYot4Bbi9Bu3BHaZKpnlIlA==}
    hasBin: true

snapshots:

  '@typescript/native-preview-darwin-arm64@7.0.0-dev.20251229.1':
    optional: true

  '@typescript/native-preview-darwin-x64@7.0.0-dev.20251229.1':
    optional: true

  '@typescript/native-preview-linux-arm64@7.0.0-dev.20251229.1':
    optional: true

  '@typescript/native-preview-linux-x64@7.0.0-dev.20251229.1':
    optional: true

  '@typescript/native-preview@7.0.0-dev.20251229.1':
    optionalDependencies:
      '@typescript/native-preview-darwin-arm64': 7.0.0-dev.20251229.1
      '@typescript/native-preview-darwin-x64': 7.0.0-dev.20251229.1
      '@typescript/native-preview-linux-arm64': 7.0.0-dev.20251229.1
      '@typescript/native-preview-linux-x64': 7.0.0-dev.20251229.1
"""

# The root pins nothing; one member does. The catalog echo above `importers:` is
# the trap: read globally, it looks like a root pin of 6.0.2.
_MEMBER_ONLY = """lockfileVersion: '9.0'

catalogs:
  default:
    typescript:
      specifier: 6.0.2
      version: 6.0.2

importers:

  .:
    devDependencies:
      lodash:
        specifier: 4.18.1
        version: 4.18.1

  packages/app:
    devDependencies:
      typescript:
        specifier: 7.0.2
        version: 7.0.2

packages:

  lodash@4.18.1:
    resolution: {integrity: sha512-TwqaxBS4nGHIRNYLe/T0sRCDfy+hcrhmxwbaE0GBhVAuWqwgg9sSL1jMZLDdSbKUoLqR0c5F1bxhoYQPGbs8qA==}
""" + _STABLE_PACKAGES + """
snapshots:

  lodash@4.18.1: {}
""" + _STABLE_SNAPSHOTS

# The root's `typescript` is the ts6 catalog's alias, as a member of the trial
# monorepo has it.
_ALIASED = """lockfileVersion: '9.0'

importers:

  .:
    devDependencies:
      typescript:
        specifier: npm:@typescript/typescript6@6.0.2
        version: '@typescript/typescript6@6.0.2'

packages:

  '@typescript/typescript6@6.0.2':
    resolution: {integrity: sha512-hjACA2wU89fwpTn3MNFFnRMtMUZfqnSsyHQCQzSF39d+vahzqVrWvBpBZRO4TaSKYot4Bbi9Bu3BHaZKpnlIlA==}
    hasBin: true

snapshots:

  '@typescript/typescript6@6.0.2': {}
"""

_NO_TYPESCRIPT = """lockfileVersion: '9.0'

importers:

  .:
    dependencies:
      lodash:
        specifier: 4.18.1
        version: 4.18.1

packages:

  lodash@4.18.1:
    resolution: {integrity: sha512-TwqaxBS4nGHIRNYLe/T0sRCDfy+hcrhmxwbaE0GBhVAuWqwgg9sSL1jMZLDdSbKUoLqR0c5F1bxhoYQPGbs8qA==}

snapshots:

  lodash@4.18.1: {}
"""

_TYPESCRIPT_5 = """lockfileVersion: '9.0'

importers:

  .:
    devDependencies:
      typescript:
        specifier: 5.9.2
        version: 5.9.2

packages:

  typescript@5.9.2:
    resolution: {integrity: sha512-CWBzXQrc/qOkhidw1OzBTQuYRbfyxDXJMVJ1XNwUHGROVmuaeiEm3OslpZ1RV96d7SKKjZKrSJu3+t/xlw3R9A==}
    engines: {node: '>=14.17'}
    hasBin: true

snapshots:

  typescript@5.9.2: {}
"""

# Two members on two real versions, no root pin.
_TWO_VERSIONS = """lockfileVersion: '9.0'

importers:

  packages/app:
    devDependencies:
      typescript:
        specifier: 7.0.2
        version: 7.0.2

  packages/legacy:
    devDependencies:
      typescript:
        specifier: 7.0.1
        version: 7.0.1

packages:
""" + _STABLE_PACKAGES + """
  typescript@7.0.1:
    resolution: {integrity: sha512-hjACA2wU89fwpTn3MNFFnRMtMUZfqnSsyHQCQzSF39d+vahzqVrWvBpBZRO4TaSKYot4Bbi9Bu3BHaZKpnlIlA==}
    hasBin: true

snapshots:
""" + _STABLE_SNAPSHOTS + """
  typescript@7.0.1:
    optionalDependencies:
      '@typescript/typescript-linux-x64': 7.0.2
"""

def _replace_resolution(lockfile, package_id, resolution):
    """Swaps one `packages:` entry's resolution line for another."""
    head, sep, tail = lockfile.partition("  '{}':\n    resolution: ".format(package_id))
    if not sep:
        fail("no packages: entry for " + package_id)
    _, _, rest = tail.partition("\n")
    return head + sep + resolution + "\n" + rest

def _stable_test(ctx):
    env = unittest.begin(ctx)
    spec = tsgo_from_pnpm_lock(_STABLE, "typescript", TSGO_PLATFORMS, _LOCK)

    asserts.equals(env, "", spec.error)
    asserts.equals(env, "7.0.2", spec.version)
    asserts.equals(env, "tsc", spec.binary)
    asserts.equals(env, sorted(TSGO_PLATFORMS), sorted(spec.platforms.keys()))
    for platform in TSGO_PLATFORMS:
        resolved = spec.platforms[platform]
        arch = _NPM_ARCH[platform]
        asserts.equals(env, "@typescript/typescript-" + arch, resolved.package)
        asserts.equals(env, _INTEGRITY[platform], resolved.integrity)
        asserts.equals(
            env,
            "https://registry.npmjs.org/@typescript/typescript-{arch}/-/typescript-{arch}-7.0.2.tgz".format(arch = arch),
            resolved.url,
        )

    return unittest.end(env)

stable_test = unittest.make(_stable_test)

def _nightly_test(ctx):
    env = unittest.begin(ctx)
    spec = tsgo_from_pnpm_lock(_NIGHTLY, "@typescript/native-preview", TSGO_PLATFORMS, _LOCK)

    asserts.equals(env, "", spec.error)
    asserts.equals(env, "7.0.0-dev.20251229.1", spec.version)
    asserts.equals(env, "tsgo", spec.binary)
    linux = spec.platforms["linux_amd64"]
    asserts.equals(env, "@typescript/native-preview-linux-x64", linux.package)
    asserts.equals(env, "sha512-/gW7OMxYU2O7fFV2qMwkA/d9gHFk1po+ufFucB719eunn84MtxHxO6SyJoK+nGlKPYRFMVAniB+0JLQlG2j4eg==", linux.integrity)
    asserts.equals(
        env,
        "https://registry.npmjs.org/@typescript/native-preview-linux-x64/-/native-preview-linux-x64-7.0.0-dev.20251229.1.tgz",
        linux.url,
    )

    # The stable package is not in this lockfile, and the nightly is not it.
    absent = tsgo_from_pnpm_lock(_NIGHTLY, "typescript", TSGO_PLATFORMS, _LOCK)
    asserts.true(env, absent.error.startswith("no typescript in //:pnpm-lock.yaml"), absent.error)

    return unittest.end(env)

nightly_test = unittest.make(_nightly_test)

def _member_pin_is_found_test(ctx):
    env = unittest.begin(ctx)
    spec = tsgo_from_pnpm_lock(_MEMBER_ONLY, "typescript", TSGO_PLATFORMS, _LOCK)

    asserts.equals(env, "", spec.error)
    asserts.equals(env, "7.0.2", spec.version)
    asserts.equals(env, _INTEGRITY["linux_amd64"], spec.platforms["linux_amd64"].integrity)

    return unittest.end(env)

member_pin_is_found_test = unittest.make(_member_pin_is_found_test)

def _alias_is_refused_test(ctx):
    env = unittest.begin(ctx)
    spec = tsgo_from_pnpm_lock(_ALIASED, "typescript", TSGO_PLATFORMS, _LOCK)

    asserts.true(env, spec.error.startswith(
        "typescript in //:pnpm-lock.yaml is an alias for @typescript/typescript6@6.0.2, not the typescript package.",
    ), spec.error)
    asserts.equals(env, {}, spec.platforms)

    return unittest.end(env)

alias_is_refused_test = unittest.make(_alias_is_refused_test)

def _absent_test(ctx):
    env = unittest.begin(ctx)
    spec = tsgo_from_pnpm_lock(_NO_TYPESCRIPT, "typescript", TSGO_PLATFORMS, "@@//:pnpm-lock.yaml")

    asserts.true(env, spec.error.startswith("no typescript in @@//:pnpm-lock.yaml"), spec.error)
    asserts.true(env, "pnpm add -D typescript@7" in spec.error, spec.error)
    asserts.true(env, "ts.tsgo(version = " in spec.error, spec.error)

    return unittest.end(env)

absent_test = unittest.make(_absent_test)

def _typescript_5_test(ctx):
    env = unittest.begin(ctx)
    spec = tsgo_from_pnpm_lock(_TYPESCRIPT_5, "typescript", TSGO_PLATFORMS, _LOCK)

    asserts.true(env, spec.error.startswith(
        "typescript@5.9.2 in //:pnpm-lock.yaml ships no native compiler (no @typescript/typescript-<os>-<cpu> optional dependencies)",
    ), spec.error)
    asserts.true(env, "TypeScript 7 or later" in spec.error, spec.error)

    return unittest.end(env)

typescript_5_test = unittest.make(_typescript_5_test)

def _two_versions_without_root_pin_test(ctx):
    env = unittest.begin(ctx)
    spec = tsgo_from_pnpm_lock(_TWO_VERSIONS, "typescript", TSGO_PLATFORMS, _LOCK)

    asserts.true(env, spec.error.startswith(
        "typescript in //:pnpm-lock.yaml resolves to several versions (typescript@7.0.1, typescript@7.0.2) and the root importer pins none",
    ), spec.error)

    return unittest.end(env)

two_versions_without_root_pin_test = unittest.make(_two_versions_without_root_pin_test)

def _missing_platform_test(ctx):
    env = unittest.begin(ctx)
    lockfile = _STABLE.replace("      '@typescript/typescript-darwin-x64': 7.0.2\n", "")
    spec = tsgo_from_pnpm_lock(lockfile, "typescript", TSGO_PLATFORMS, _LOCK)

    asserts.true(env, spec.error.startswith(
        "typescript@7.0.2 in //:pnpm-lock.yaml has no @typescript/typescript-darwin-x64 among its optional dependencies, so there is no compiler to fetch for darwin_amd64.",
    ), spec.error)

    return unittest.end(env)

missing_platform_test = unittest.make(_missing_platform_test)

def _no_integrity_test(ctx):
    env = unittest.begin(ctx)
    lockfile = _replace_resolution(
        _STABLE,
        "@typescript/typescript-linux-x64@7.0.2",
        "{tarball: https://mirror.example/typescript-linux-x64-7.0.2.tgz}",
    )
    spec = tsgo_from_pnpm_lock(lockfile, "typescript", TSGO_PLATFORMS, _LOCK)

    asserts.true(env, spec.error.startswith(
        "@typescript/typescript-linux-x64@7.0.2 in //:pnpm-lock.yaml carries no usable integrity (resolution keys: tarball)",
    ), spec.error)

    # A digest Bazel cannot check is as unusable as none.
    sha1 = _replace_resolution(_STABLE, "@typescript/typescript-linux-x64@7.0.2", "{integrity: sha1-2jmj7l5rSw0yVb/vlWAYkK/YBwk=}")
    asserts.true(env, "no usable integrity (resolution keys: integrity)" in tsgo_from_pnpm_lock(sha1, "typescript", TSGO_PLATFORMS, _LOCK).error)

    return unittest.end(env)

no_integrity_test = unittest.make(_no_integrity_test)

def _tarball_resolution_wins_test(ctx):
    env = unittest.begin(ctx)
    lockfile = _replace_resolution(
        _STABLE,
        "@typescript/typescript-linux-x64@7.0.2",
        "{integrity: " + _INTEGRITY["linux_amd64"] + ", tarball: https://mirror.example/typescript-linux-x64-7.0.2.tgz}",
    )
    spec = tsgo_from_pnpm_lock(lockfile, "typescript", TSGO_PLATFORMS, _LOCK)

    asserts.equals(env, "", spec.error)
    asserts.equals(env, "https://mirror.example/typescript-linux-x64-7.0.2.tgz", spec.platforms["linux_amd64"].url)
    asserts.equals(env, _INTEGRITY["linux_amd64"], spec.platforms["linux_amd64"].integrity)
    asserts.equals(
        env,
        "https://registry.npmjs.org/@typescript/typescript-linux-arm64/-/typescript-linux-arm64-7.0.2.tgz",
        spec.platforms["linux_arm64"].url,
    )

    return unittest.end(env)

tarball_resolution_wins_test = unittest.make(_tarball_resolution_wins_test)

def _platform_constraint_is_checked_test(ctx):
    env = unittest.begin(ctx)
    lockfile = _STABLE.replace(
        "    cpu: [x64]\n    os: [linux]\n",
        "    cpu: [x64]\n    os: [linux]\n    libc: [musl]\n",
    )
    spec = tsgo_from_pnpm_lock(lockfile, "typescript", TSGO_PLATFORMS, _LOCK)

    asserts.true(env, spec.error.startswith(
        "@typescript/typescript-linux-x64@7.0.2 in //:pnpm-lock.yaml is constrained to os [\"linux\"] cpu [\"x64\"] libc [\"musl\"], which does not admit linux_amd64 (linux-x64, libc glibc).",
    ), spec.error)

    return unittest.end(env)

platform_constraint_is_checked_test = unittest.make(_platform_constraint_is_checked_test)

def _npmrc_registry_test(ctx):
    env = unittest.begin(ctx)
    registries = npmrc_registries("registry=https://npm.example/default/\n@typescript:registry=https://npm.example/ms/\n")
    spec = tsgo_from_pnpm_lock(_STABLE, "typescript", TSGO_PLATFORMS, _LOCK, registries)

    asserts.equals(env, "", spec.error)
    asserts.equals(
        env,
        "https://npm.example/ms/@typescript/typescript-linux-x64/-/typescript-linux-x64-7.0.2.tgz",
        spec.platforms["linux_amd64"].url,
    )
    asserts.equals(env, _INTEGRITY["linux_amd64"], spec.platforms["linux_amd64"].integrity)

    return unittest.end(env)

npmrc_registry_test = unittest.make(_npmrc_registry_test)

def _unknown_package_test(ctx):
    env = unittest.begin(ctx)

    for spec in [
        tsgo_from_pnpm_lock(_STABLE, "@typescript/typescript6", TSGO_PLATFORMS, _LOCK),
        tsgo_from_version("@typescript/typescript6", "6.0.2", TSGO_PLATFORMS),
    ]:
        asserts.true(env, spec.error.startswith("ts.tsgo(package = \"@typescript/typescript6\"): the tsgo toolchain comes from \"typescript\""), spec.error)

    return unittest.end(env)

unknown_package_test = unittest.make(_unknown_package_test)

def _from_version_test(ctx):
    env = unittest.begin(ctx)

    stable = tsgo_from_version("typescript", "7.0.2", TSGO_PLATFORMS)
    asserts.equals(env, "", stable.error)
    asserts.equals(env, "7.0.2", stable.version)
    asserts.equals(env, "tsc", stable.binary)
    asserts.equals(
        env,
        "https://registry.npmjs.org/@typescript/typescript-darwin-arm64/-/typescript-darwin-arm64-7.0.2.tgz",
        stable.platforms["darwin_arm64"].url,
    )
    for platform in TSGO_PLATFORMS:
        asserts.equals(env, "", stable.platforms[platform].integrity)

    nightly = tsgo_from_version("@typescript/native-preview", "7.0.0-dev.20260311.1", TSGO_PLATFORMS, {"@typescript": "https://npm.example/ms"})
    asserts.equals(env, "tsgo", nightly.binary)
    asserts.equals(
        env,
        "https://npm.example/ms/@typescript/native-preview-linux-x64/-/native-preview-linux-x64-7.0.0-dev.20260311.1.tgz",
        nightly.platforms["linux_amd64"].url,
    )

    return unittest.end(env)

from_version_test = unittest.make(_from_version_test)

def tsgo_lock_test_suite(name):
    unittest.suite(
        name,
        stable_test,
        nightly_test,
        member_pin_is_found_test,
        alias_is_refused_test,
        absent_test,
        typescript_5_test,
        two_versions_without_root_pin_test,
        missing_platform_test,
        no_integrity_test,
        tarball_resolution_wins_test,
        platform_constraint_is_checked_test,
        npmrc_registry_test,
        unknown_package_test,
        from_version_test,
    )
