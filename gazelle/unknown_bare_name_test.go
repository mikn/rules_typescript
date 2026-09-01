package typescript

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/bazelbuild/bazel-gazelle/config"
	"github.com/bazelbuild/bazel-gazelle/label"
	"github.com/bazelbuild/bazel-gazelle/rule"
)

// A workspace whose lockfile carries the three shapes the refusal set has to
// tell apart: an installed package, a package built for one platform (which the
// inventory drops on purpose), and a workspace link. `@anthropic-ai/sdk` is the
// name nothing here installs -- a package.json under tools/ asked npm for it,
// so the pnpm hub has no target and never will.
const unknownNameLock = `lockfileVersion: '9.0'

importers:

  .:
    dependencies:
      '@acme/lib':
        specifier: workspace:*
        version: link:packages/lib
      zod:
        specifier: ^3.0.0
        version: 3.24.2

packages:

  zod@3.24.2:
    resolution: {integrity: sha512-aaa}

  fsevents@2.3.3:
    resolution: {integrity: sha512-bbb}
    os: [darwin]

snapshots:

  zod@3.24.2: {}

  fsevents@2.3.3:
    optional: true
`

func unknownNameConfig(t *testing.T) *tsConfig {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, pnpmLockfileName), unknownNameLock)
	c := &config.Config{RepoRoot: root, Exts: make(map[string]interface{})}
	configureTsConfig(c, "", nil)
	return getConfig(c)
}

// The defect: a bare specifier nothing installed still took the hub convention,
// and `@npm//:anthropic-ai_sdk` is a target the hub does not declare. Bazel
// answers that with `no such target` during analysis, which fails every target
// in the build rather than the one import that asked for it.
func TestResolveNpmPackage_NameTheLockfileNeverMentions(t *testing.T) {
	tc := unknownNameConfig(t)

	for _, imp := range []string{
		"@anthropic-ai/sdk",
		"@anthropic-ai/sdk/resources/messages",
		"react-native",
		"@/integrations/supabase/client",
	} {
		if got := resolveNpmPackage(tc, imp); got != "" {
			t.Errorf("resolveNpmPackage(%q) = %q, want no dep", imp, got)
		}
	}
}

// The refusal reads the names the lockfile mentions, not the inventory, because
// the inventory under-claims on purpose: a package carrying `os:`/`cpu:`/`libc:`
// is dropped rather than matched against platforms.bzl. Gating on the inventory
// would turn every one of those into a dropped dep.
func TestResolveNpmPackage_PlatformRestrictedNameKeepsItsLabel(t *testing.T) {
	tc := unknownNameConfig(t)

	if hasNpmPackage(tc, "fsevents") {
		t.Fatal("fsevents reached the inventory; this test no longer covers the under-claim")
	}
	for imp, want := range map[string]string{
		"fsevents":  "@npm//:fsevents",
		"zod":       "@npm//:zod",
		"@acme/lib": "@npm//:acme_lib",
	} {
		if got := resolveNpmPackage(tc, imp); got != want {
			t.Errorf("resolveNpmPackage(%q) = %q, want %q", imp, got, want)
		}
	}
}

// The lockfile this reader found is the default hub's. A ts_npm_hub tree
// resolves against a second lockfile nobody read here, so it is not refused.
func TestResolveNpmPackage_OtherHubIsNotRefused(t *testing.T) {
	tc := unknownNameConfig(t)
	tc.npmHub = "@npm_eslint"

	if got := resolveNpmPackage(tc, "eslint-plugin-import"); got != "@npm_eslint//:eslint-plugin-import" {
		t.Errorf("resolveNpmPackage = %q, want the other hub's label", got)
	}
}

// No lockfile is no information, not an empty workspace: every name keeps the
// label it had before this gate existed.
func TestResolveNpmPackage_NoLockfileRefusesNothing(t *testing.T) {
	tc := makeConfig("", nil)

	if got := resolveNpmPackage(tc, "@anthropic-ai/sdk"); got != "@npm//:anthropic-ai_sdk" {
		t.Errorf("resolveNpmPackage = %q, want the hub label", got)
	}
}

// End to end through the resolver: the unknown name contributes no dep and the
// installed one in the same target still does.
func TestResolveImports_UnknownBareNameContributesNoDep(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, pnpmLockfileName), unknownNameLock)
	c := &config.Config{RepoRoot: root, Exts: make(map[string]interface{})}
	configureTsConfig(c, "", nil)
	ix := buildIndex(t, c)

	r := rule.NewRule("ts_compile", "classification")
	resolveImports(c, ix, r, []string{"@anthropic-ai/sdk", "zod"}, label.New("", "tools/pr/classification", "classification"))

	want := []string{"@npm//:zod"}
	if got := r.AttrStrings("deps"); !reflect.DeepEqual(got, want) {
		t.Errorf("deps = %v, want %v", got, want)
	}
}

// The refusal set is read for the opposite answer to the inventory's, so it
// takes every name either section spells, whichever way the mapping is written,
// plus the links and aliases no package key accounts for.
func TestParsePnpmLockNames(t *testing.T) {
	const lock = `lockfileVersion: '9.0'

importers:

  .:
    dependencies:
      '@acme/lib':
        specifier: workspace:*
        version: link:packages/lib
      h3-v2:
        specifier: npm:h3@2.0.1
        version: h3@2.0.1

packages:

  zod@3.24.2: {}

  fsevents@2.3.3:
    resolution: {integrity: sha512-bbb}
    os: [darwin]

snapshots:

  h3@2.0.1: {}
`
	got := parsePnpmLockNames(strings.Split(lock, "\n"))
	want := map[string]bool{
		"zod":       true,
		"fsevents":  true,
		"h3":        true,
		"@acme/lib": true,
		"h3-v2":     true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parsePnpmLockNames = %v, want %v", got, want)
	}
}
