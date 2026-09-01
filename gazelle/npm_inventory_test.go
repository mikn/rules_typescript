package typescript

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/bazelbuild/bazel-gazelle/config"
	"github.com/bazelbuild/bazel-gazelle/label"
	"github.com/bazelbuild/bazel-gazelle/rule"
)

// A v9 lockfile carrying every shape the inventory has to tell apart: a direct
// dependency, a transitive one, a workspace link, an npm alias, a package built
// for one platform, a `snapshots:` entry whose `packages:` entry is missing, and
// the two sections shaped exactly like the ones we read.
const inventoryLockV9 = `lockfileVersion: '9.0'

settings:
  autoInstallPeers: true

overrides:
  ohno@1.0.0: 2.0.0

catalogs:
  default:
    never-an-importer:
      specifier: ^1.0.0
      version: 1.0.0

importers:

  .:
    dependencies:
      vite:
        specifier: 8.2.2
        version: 8.2.2(@types/node@22.20.1)
      shared:
        specifier: workspace:*
        version: link:packages/shared
      h3-v2:
        specifier: npm:h3@2.0.1
        version: h3@2.0.1
    devDependencies:
      '@types/node':
        specifier: 22.20.1
        version: 22.20.1

  packages/shared:
    dependencies:
      zod:
        specifier: ^3.0.0
        version: 3.24.2

packages:

  '@types/node@22.20.1':
    resolution: {integrity: sha512-aaa}

  vite@8.2.2:
    resolution: {integrity: sha512-bbb}

  zod@3.24.2:
    resolution: {integrity: sha512-ccc}

  h3@2.0.1:
    resolution: {integrity: sha512-ddd}

  picocolors@1.1.1:
    resolution: {integrity: sha512-eee}

  undici-types@6.19.8:
    resolution: {integrity: sha512-fff}

  fsevents@2.3.3:
    resolution: {integrity: sha512-ggg}
    engines: {node: ^8.16.0}
    os: [darwin]

snapshots:

  '@types/node@22.20.1':
    dependencies:
      undici-types: 6.19.8

  vite@8.2.2(@types/node@22.20.1):
    dependencies:
      picocolors: 1.1.1
    optionalDependencies:
      fsevents: 2.3.3

  zod@3.24.2: {}

  h3@2.0.1: {}

  picocolors@1.1.1: {}

  fsevents@2.3.3:
    optional: true

  undici-types@6.19.8: {}
`

func inventoryNames(t *testing.T, content string) []string {
	t.Helper()
	inventory, err := parsePnpmLockInventory(content)
	if err != nil {
		t.Fatalf("parsePnpmLockInventory: %v", err)
	}
	names := make([]string, 0, len(inventory))
	for name, lbl := range inventory {
		if lbl != "" {
			t.Errorf("inventory[%q] = %q, want the empty label the hub convention fills in", name, lbl)
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func TestParsePnpmLockInventory_V9(t *testing.T) {
	// picocolors and undici-types are transitive, and the hub declares a flat
	// label for them all the same. fsevents is the platform under-claim, and
	// undici-types earns its place only through the `snapshots:` walk.
	want := []string{
		"@types/node", "h3", "h3-v2", "picocolors", "shared", "undici-types",
		"vite", "zod",
	}
	if got := inventoryNames(t, inventoryLockV9); !reflect.DeepEqual(got, want) {
		t.Errorf("inventory = %v, want %v", got, want)
	}
}

// A `snapshots:` entry with no `packages:` entry has no bytes to download, so
// npm/lazy.bzl drops it from `live` and the hub never declares its label.
func TestParsePnpmLockInventory_SnapshotWithoutAPackagesEntry(t *testing.T) {
	content := inventoryLockV9 + "\n  ghost@1.0.0: {}\n"
	for _, name := range inventoryNames(t, content) {
		if name == "ghost" {
			t.Error("a snapshot with no packages: entry reached the inventory")
		}
	}
}

// A `packages:` entry with no snapshot is the mirror case, and equally not a
// hub label: `sids_by_label` iterates snapshots, not packages.
func TestParsePnpmLockInventory_PackageWithoutASnapshot(t *testing.T) {
	content := strings.Replace(
		inventoryLockV9,
		"  picocolors@1.1.1: {}\n",
		"",
		1,
	)
	for _, name := range inventoryNames(t, content) {
		if name == "picocolors" {
			t.Error("a packages: entry with no snapshot reached the inventory")
		}
	}
}

// v6 puts the dependency edges in `packages:` and has no `snapshots:` at all,
// so every package key is its own resolution. Keys carry a leading slash.
func TestParsePnpmLockInventory_V6(t *testing.T) {
	const content = `lockfileVersion: '6.0'

importers:

  .:
    dependencies:
      shared:
        specifier: workspace:*
        version: link:packages/shared
      react:
        specifier: ^18.3.1
        version: 18.3.1
    devDependencies:
      '@types/node': {specifier: 22.20.1, version: 22.20.1}
      tw-v3: {specifier: npm:tailwindcss@3.4.18, version: tailwindcss@3.4.18}

packages:

  /@types/node@22.20.1:
    resolution: {integrity: sha512-aaa}
    dev: true

  /react@18.3.1:
    resolution: {integrity: sha512-bbb}
    dependencies:
      loose-envify: 1.4.0

  /loose-envify@1.4.0:
    resolution: {integrity: sha512-ccc}

  /tailwindcss@3.4.18:
    resolution: {integrity: sha512-ddd}

  /@esbuild/linux-x64@0.21.5:
    resolution: {integrity: sha512-eee}
    cpu: [x64]
    os: [linux]
`
	want := []string{
		"@types/node", "loose-envify", "react", "shared", "tailwindcss", "tw-v3",
	}
	if got := inventoryNames(t, content); !reflect.DeepEqual(got, want) {
		t.Errorf("inventory = %v, want %v", got, want)
	}
}

// An npm alias whose target is not in the graph is a label the hub never
// declares, so the imported name stays out.
func TestParsePnpmLockInventory_AliasToAMissingPackage(t *testing.T) {
	const content = `lockfileVersion: '9.0'

importers:

  .:
    dependencies:
      h3-v2:
        specifier: npm:h3@2.0.1
        version: h3@2.0.1

packages: {}

snapshots: {}
`
	if got := inventoryNames(t, content); len(got) != 0 {
		t.Errorf("inventory = %v, want nothing", got)
	}
}

// A version the reader does not handle produces no inventory rather than a
// wrong one: the callers' heuristics are a better answer than a namespace read
// out of a schema this parser does not know.
func TestParsePnpmLockInventory_UnsupportedAndMalformed(t *testing.T) {
	for name, content := range map[string]string{
		"v5":            "lockfileVersion: 5.4\n\npackages:\n  /react@18.3.1:\n    resolution: {integrity: sha512-a}\n",
		"v10":           "lockfileVersion: '10.0'\n",
		"no version":    "importers:\n\n  .:\n    dependencies:\n      react:\n        version: 18.3.1\n",
		"not yaml":      "<<<not a lockfile>>>\n",
		"empty file":    "",
		"truncated key": "lockfileVersion: '9.0'\n\npackages:\n  '@types/no\n",
	} {
		inventory, err := parsePnpmLockInventory(content)
		switch name {
		case "truncated key":
			// A supported version with unparseable entries is still an answer:
			// the parser skips what it cannot key and claims only the rest.
			if err != nil {
				t.Errorf("%s: unexpected error %v", name, err)
			}
			if len(inventory) != 0 {
				t.Errorf("%s: inventory = %v, want nothing", name, inventory)
			}
		default:
			if err == nil {
				t.Errorf("%s: parsed without error, inventory = %v", name, inventory)
			}
			if inventory != nil {
				t.Errorf("%s: inventory = %v, want nil", name, inventory)
			}
		}
	}
}

// A workspace that declares nothing is a claim, not an absence: the empty map
// says so, and only a missing or unreadable lockfile gives back nil.
func TestLoadNpmInventory_EmptyIsNotNil(t *testing.T) {
	root := t.TempDir()
	if inventory, _, _ := loadNpmInventory(root); inventory != nil {
		t.Fatalf("no lockfile: inventory = %v, want nil", inventory)
	}

	writeFile(t, filepath.Join(root, pnpmLockfileName), "lockfileVersion: '9.0'\n\nimporters:\n\n  .: {}\n")
	inventory, _, _ := loadNpmInventory(root)
	if inventory == nil {
		t.Fatal("a lockfile declaring nothing gave back nil, which reads as no lockfile at all")
	}
	if len(inventory) != 0 {
		t.Errorf("inventory = %v, want empty", inventory)
	}
}

// ---- the inventory reaching resolution -------------------------------------

// configureInRepo walks configureTsConfig from the root down to rel, the way
// Gazelle does, and returns the config rel ends up with.
func configureInRepo(t *testing.T, repoRoot, rel string) *tsConfig {
	t.Helper()
	c := &config.Config{RepoRoot: repoRoot, Exts: make(map[string]interface{})}
	configureTsConfig(c, "", nil)
	if rel != "" {
		parts := strings.Split(rel, "/")
		for i := range parts {
			configureTsConfig(c, strings.Join(parts[:i+1], "/"), nil)
		}
	}
	return getConfig(c)
}

// The whole point of the change: a `node:` import picks up @types/node because
// the workspace's own pnpm-lock.yaml says the hub declares it. Nothing here
// hands the inventory to the config by hand, which is what made every earlier
// test of this pass on a ruleset that never read a lockfile.
func TestNpmInventory_NodeBuiltinResolvesFromTheLockfile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, pnpmLockfileName), inventoryLockV9)
	tc := configureInRepo(t, root, "src/server")

	for imp, want := range map[string]string{
		"node:fs":          "@npm//:types_node",
		"node:fs/promises": "@npm//:types_node",
		"fs":               "@npm//:types_node",
		"path":             "@npm//:types_node",
		"vite":             "@npm//:vite",
		"zod":              "@npm//:zod",
	} {
		if got := resolveNpmPackage(tc, imp); got != want {
			t.Errorf("resolveNpmPackage(%q) = %q, want %q", imp, got, want)
		}
	}
}

func TestNpmInventory_NodeBuiltinWithoutTypesNodeInTheLockfile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, pnpmLockfileName),
		"lockfileVersion: '9.0'\n\npackages:\n\n  zod@3.24.2:\n    resolution: {integrity: sha512-a}\n\nsnapshots:\n\n  zod@3.24.2: {}\n")
	tc := configureInRepo(t, root, "")

	if tc.npmPackages == nil {
		t.Fatal("no inventory was read")
	}
	for _, imp := range []string{"node:fs", "fs", "node:path"} {
		if got := resolveNpmPackage(tc, imp); got != "" {
			t.Errorf("resolveNpmPackage(%q) = %q, want no dep", imp, got)
		}
	}
}

// The dep reaches the rule, not just the resolver.
func TestNpmInventory_NodeBuiltinDepOnTheGeneratedRule(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, pnpmLockfileName), inventoryLockV9)

	c := &config.Config{RepoRoot: root, Exts: make(map[string]interface{})}
	configureTsConfig(c, "", nil)
	ix := buildIndex(t, c)
	r := rule.NewRule("ts_compile", "app")
	resolveImports(c, ix, r, []string{"node:fs", "zod"}, label.New("", "src", "app"))

	want := []string{"@npm//:types_node", "@npm//:zod"}
	if got := r.AttrStrings("deps"); !reflect.DeepEqual(got, want) {
		t.Errorf("deps = %v, want %v", got, want)
	}
}

// A lockfile entry carries no label of its own, so the hub directive still
// chooses the repository -- and an installed package still beats the built-in
// name it shadows.
func TestNpmInventory_LockfileEntryHonoursTheHubDirective(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, pnpmLockfileName),
		"lockfileVersion: '9.0'\n\npackages:\n\n  path@0.12.7:\n    resolution: {integrity: sha512-a}\n\n"+
			"  '@types/node@22.20.1':\n    resolution: {integrity: sha512-b}\n\n"+
			"snapshots:\n\n  path@0.12.7: {}\n\n  '@types/node@22.20.1': {}\n")

	c := &config.Config{RepoRoot: root, Exts: make(map[string]interface{})}
	f := rule.EmptyFile("BUILD.bazel", "")
	f.Directives = []rule.Directive{{Key: "ts_npm_hub", Value: "@npm_tools"}}
	configureTsConfig(c, "", f)
	tc := getConfig(c)

	if got := resolveNpmPackage(tc, "path"); got != "@npm_tools//:path" {
		t.Errorf("resolveNpmPackage(\"path\") = %q, want the installed package from the named hub", got)
	}
	if got := resolveNpmPackage(tc, "node:path"); got != "@npm_tools//:types_node" {
		t.Errorf("resolveNpmPackage(\"node:path\") = %q, want @types/node from the named hub", got)
	}
}

// gazelle_ts.json is deprecated but not broken: every package it names keeps
// its hand-written label, and the lockfile supplies the rest rather than being
// thrown away.
func TestNpmInventory_MappingFileOverridesPerKey(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, pnpmLockfileName), inventoryLockV9)
	writeFile(t, filepath.Join(root, "npm/mapping.json"), `{"vite": "//vendor/vite:vite"}`)
	writeFile(t, filepath.Join(root, "gazelle_ts.json"), `{"npmMappingFile": "npm/mapping.json"}`)

	tc := configureInRepo(t, root, "")
	if got := resolveNpmPackage(tc, "vite"); got != "//vendor/vite:vite" {
		t.Errorf("resolveNpmPackage(\"vite\") = %q, want the mapping file's label", got)
	}
	if got := resolveNpmPackage(tc, "zod"); got != "@npm//:zod" {
		t.Errorf("resolveNpmPackage(\"zod\") = %q, want the lockfile's answer to survive the overlay", got)
	}
	if got := resolveNpmPackage(tc, "node:fs"); got != "@npm//:types_node" {
		t.Errorf("resolveNpmPackage(\"node:fs\") = %q, want @types/node", got)
	}
}

// A subtree's gazelle_ts.json must not become the whole workspace's answer: the
// lockfile inventory is shared by pointer across every directory.
func TestNpmInventory_MappingFileDoesNotMutateTheSharedInventory(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, pnpmLockfileName), inventoryLockV9)
	writeFile(t, filepath.Join(root, "npm/mapping.json"), `{"vite": "//vendor/vite:vite"}`)
	writeFile(t, filepath.Join(root, "apps/web/gazelle_ts.json"), `{"npmMappingFile": "npm/mapping.json"}`)

	c := &config.Config{RepoRoot: root, Exts: make(map[string]interface{})}
	configureTsConfig(c, "", nil)
	rootTc := getConfig(c)
	configureTsConfig(c, "apps", nil)
	configureTsConfig(c, "apps/web", nil)

	if got := resolveNpmPackage(getConfig(c), "vite"); got != "//vendor/vite:vite" {
		t.Errorf("apps/web: resolveNpmPackage(\"vite\") = %q, want the mapping file's label", got)
	}
	if got := resolveNpmPackage(rootTc, "vite"); got != "@npm//:vite" {
		t.Errorf("root: resolveNpmPackage(\"vite\") = %q, want the lockfile's answer", got)
	}
}

// The lockfile is read once for the whole walk, so removing it mid-walk cannot
// change the answer. Re-reading a 30k-line lockfile in every directory is the
// failure this pins.
func TestNpmInventory_ReadOncePerWalk(t *testing.T) {
	root := t.TempDir()
	lockfile := filepath.Join(root, pnpmLockfileName)
	writeFile(t, lockfile, inventoryLockV9)

	c := &config.Config{RepoRoot: root, Exts: make(map[string]interface{})}
	configureTsConfig(c, "", nil)
	if err := os.Remove(lockfile); err != nil {
		t.Fatal(err)
	}
	configureTsConfig(c, "src", nil)

	if got := resolveNpmPackage(getConfig(c), "node:fs"); got != "@npm//:types_node" {
		t.Errorf("resolveNpmPackage(\"node:fs\") = %q after the lockfile went away mid-walk; "+
			"the inventory is being re-read per directory", got)
	}
}

// ---- codegen detectors, which the inventory was always meant to gate -------

// The prisma detector believed it was checking the lockfile and was reading a
// nil map, so schema.prisma alone emitted a @npm//:prisma_bin the hub may not
// declare. With a real inventory the check happens.
func TestNpmInventory_PrismaDetectorChecksTheLockfile(t *testing.T) {
	for _, tt := range []struct {
		name  string
		lock  string
		fires bool
	}{
		{
			name:  "prisma in the lockfile",
			lock:  "lockfileVersion: '9.0'\n\npackages:\n\n  prisma@6.1.0:\n    resolution: {integrity: sha512-a}\n\nsnapshots:\n\n  prisma@6.1.0: {}\n",
			fires: true,
		},
		{
			name:  "lockfile without prisma",
			lock:  "lockfileVersion: '9.0'\n\npackages:\n\n  zod@3.24.2:\n    resolution: {integrity: sha512-a}\n\nsnapshots:\n\n  zod@3.24.2: {}\n",
			fires: false,
		},
		{
			name:  "no lockfile at all",
			lock:  "",
			fires: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			if tt.lock != "" {
				writeFile(t, filepath.Join(root, pnpmLockfileName), tt.lock)
			}
			tc := configureInRepo(t, root, "db")

			got := detectPrisma(fileSet([]string{"schema.prisma"}), tc) != nil
			if got != tt.fires {
				t.Errorf("detectPrisma fired = %v, want %v", got, tt.fires)
			}
		})
	}
}
