package typescript

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func writeTsConfig(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// A workspace member whose tsconfig only extends a shared base still has to
// contribute the base's aliases, and the base wrote them relative to itself.
func TestLoadTsConfigPaths_ExtendsBase(t *testing.T) {
	repo := t.TempDir()
	writeTsConfig(t, filepath.Join(repo, "packages/tsconfig-base/tsconfig.json"), `{
  "compilerOptions": {
    "paths": {"@/*": ["src/*"]}
  }
}`)
	leaf := filepath.Join(repo, "apps/web/tsconfig.json")
	writeTsConfig(t, leaf, `{"extends": "../../packages/tsconfig-base/tsconfig.json"}`)

	got := loadTsConfigPaths(leaf, "apps/web")
	want := map[string]string{"@/": "packages/tsconfig-base/src/"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("loadTsConfigPaths: got %v, want %v", got, want)
	}
}

// tsc replaces an inherited compilerOptions key wholesale, so a leaf that
// redeclares paths drops the base's other entries rather than merging them.
func TestLoadTsConfigPaths_ExtendsLeafReplacesWholeKey(t *testing.T) {
	repo := t.TempDir()
	writeTsConfig(t, filepath.Join(repo, "packages/tsconfig-base/tsconfig.json"), `{
  "compilerOptions": {
    "paths": {"@/*": ["src/*"], "@lib/*": ["lib/*"]}
  }
}`)
	leaf := filepath.Join(repo, "apps/web/tsconfig.json")
	writeTsConfig(t, leaf, `{
  "extends": "../../packages/tsconfig-base/tsconfig.json",
  "compilerOptions": {
    "paths": {"@/*": ["app/*"]}
  }
}`)

	got := loadTsConfigPaths(leaf, "apps/web")
	want := map[string]string{"@/": "apps/web/app/"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("loadTsConfigPaths: got %v, want %v", got, want)
	}
}

func TestLoadTsConfigPaths_ExtendsArrayLastWins(t *testing.T) {
	repo := t.TempDir()
	writeTsConfig(t, filepath.Join(repo, "apps/web/a.json"), `{
  "compilerOptions": {
    "paths": {"@/*": ["a/*"], "@only-a/*": ["only/*"]}
  }
}`)
	writeTsConfig(t, filepath.Join(repo, "apps/web/b.json"), `{
  "compilerOptions": {
    "paths": {"@/*": ["b/*"]}
  }
}`)
	leaf := filepath.Join(repo, "apps/web/tsconfig.json")
	writeTsConfig(t, leaf, `{"extends": ["./a", "./b.json"]}`)

	got := loadTsConfigPaths(leaf, "apps/web")
	want := map[string]string{"@/": "apps/web/b/"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("loadTsConfigPaths: got %v, want %v", got, want)
	}
}

// baseUrl and paths can come from different files in the chain: baseUrl is
// relative to the config that wrote it, and paths are relative to baseUrl.
func TestLoadTsConfigPaths_ExtendsInheritedBaseURL(t *testing.T) {
	repo := t.TempDir()
	writeTsConfig(t, filepath.Join(repo, "packages/tsconfig-base/tsconfig.json"), `{
  "compilerOptions": {"baseUrl": "src"}
}`)
	leaf := filepath.Join(repo, "apps/web/tsconfig.json")
	writeTsConfig(t, leaf, `{
  "extends": "../../packages/tsconfig-base/tsconfig.json",
  "compilerOptions": {
    "paths": {"@/*": ["./*"]}
  }
}`)

	got := loadTsConfigPaths(leaf, "apps/web")
	want := map[string]string{"@/": "packages/tsconfig-base/src/"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("loadTsConfigPaths: got %v, want %v", got, want)
	}
}

// A bare or scoped specifier resolves through node_modules, which a Bazel
// checkout does not have; the rest of the config still has to load.
func TestLoadTsConfigPaths_ExtendsBareSpecifierSkipped(t *testing.T) {
	repo := t.TempDir()
	leaf := filepath.Join(repo, "tsconfig.json")
	writeTsConfig(t, leaf, `{
  "extends": "@tsconfig/node20/tsconfig.json",
  "compilerOptions": {
    "paths": {"@/*": ["src/*"]}
  }
}`)

	got := loadTsConfigPaths(leaf, "")
	want := map[string]string{"@/": "src/"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("loadTsConfigPaths: got %v, want %v", got, want)
	}
}

func TestLoadTsConfigPaths_ExtendsCycleTerminates(t *testing.T) {
	repo := t.TempDir()
	leaf := filepath.Join(repo, "tsconfig.json")
	writeTsConfig(t, leaf, `{"extends": "./ring.json"}`)
	writeTsConfig(t, filepath.Join(repo, "ring.json"), `{
  "extends": "./tsconfig.json",
  "compilerOptions": {
    "paths": {"@/*": ["src/*"]}
  }
}`)

	done := make(chan map[string]string, 1)
	go func() { done <- loadTsConfigPaths(leaf, "") }()
	select {
	case got := <-done:
		want := map[string]string{"@/": "src/"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("loadTsConfigPaths: got %v, want %v", got, want)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("loadTsConfigPaths did not terminate on an extends cycle")
	}
}

// A base outside the repository has no label to name it, and a "../" prefix in
// path_aliases is worse than no alias at all.
func TestLoadTsConfigPaths_ExtendsOutsideTheRepoIsDropped(t *testing.T) {
	root := t.TempDir()
	writeTsConfig(t, filepath.Join(root, "shared/tsconfig.json"), `{
  "compilerOptions": {
    "paths": {"@/*": ["src/*"]}
  }
}`)
	leaf := filepath.Join(root, "repo/apps/web/tsconfig.json")
	writeTsConfig(t, leaf, `{"extends": "../../../shared/tsconfig.json"}`)

	if got := loadTsConfigPaths(leaf, "apps/web"); got != nil {
		t.Errorf("loadTsConfigPaths: got %v, want no aliases", got)
	}
}

// Two branches of an extends array reaching the same base is not a cycle: the
// base is read again, because where it lands in the merge order is what
// decides whether it beats a config declared between the two branches.
func TestLoadTsConfigPaths_ExtendsDiamondReReadsTheSharedBase(t *testing.T) {
	repo := t.TempDir()
	writeTsConfig(t, filepath.Join(repo, "apps/web/shared.json"), `{
  "compilerOptions": {"paths": {"@/*": ["shared/*"]}}
}`)
	writeTsConfig(t, filepath.Join(repo, "apps/web/a.json"), `{"extends": "./shared.json"}`)
	writeTsConfig(t, filepath.Join(repo, "apps/web/b.json"), `{"extends": "./shared.json"}`)
	writeTsConfig(t, filepath.Join(repo, "apps/web/middle.json"), `{
  "compilerOptions": {"paths": {"@/*": ["middle/*"]}}
}`)
	leaf := filepath.Join(repo, "apps/web/tsconfig.json")
	writeTsConfig(t, leaf, `{"extends": ["./a.json", "./middle.json", "./b.json"]}`)

	got := loadTsConfigPaths(leaf, "apps/web")
	want := map[string]string{"@/": "apps/web/shared/"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("loadTsConfigPaths: got %v, want %v", got, want)
	}
}

// ---- the extends chain in ts_config.deps -----------------------------------

// tsConfigDeps is the deps of the ts_config target in pkg, and fails when the
// rule is not there at all -- an empty deps and a missing rule are different
// answers to every test below.
func tsConfigDeps(t *testing.T, root, pkg string) []string {
	t.Helper()
	for _, r := range loadRules(t, root, pkg) {
		if r.Kind() == "ts_config" && r.Name() == tsConfigTargetName {
			return r.AttrStrings("deps")
		}
	}
	t.Fatalf("generation wrote no ts_config(%s) in %s", tsConfigTargetName, pkg)
	return nil
}

func assertNoDanglingLabels(t *testing.T, root string) {
	t.Helper()
	if dangling := danglingLabels(t, root); len(dangling) > 0 {
		t.Errorf("%d label(s) no target satisfies:\n      %s",
			len(dangling), strings.Join(dangling, "\n      "))
	}
}

// The measured defect: a per-directory tsconfig extending the one above it. The
// parent file is not an input to any action the nested targets run, so tsgo
// reports TS5083 on a path it can see in the config and not in the sandbox.
func TestTsConfigExtendsChain_RelativeParentBecomesADep(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"package.json":                      `{"name":"w"}` + "\n",
		"workers/proxy/tsconfig.json":       `{"compilerOptions":{"lib":["es2022"]}}` + "\n",
		"workers/proxy/src/index.ts":        "export const worker = 1;\n",
		"workers/proxy/test/tsconfig.json":  `{"extends":"../tsconfig.json"}` + "\n",
		"workers/proxy/test/worker.test.ts": "export const t = 1;\n",
	})
	captureLog(t, func() { convergeGazelle(t, root) })

	want := []string{"//workers/proxy:" + tsConfigTargetName}
	// A second pass reads a BUILD file that already carries the value, which is
	// the run a non-mergeable attribute has to come out of unchanged.
	for pass := 1; pass <= 2; pass++ {
		got := tsConfigDeps(t, root, "workers/proxy/test")
		if !reflect.DeepEqual(got, want) {
			t.Errorf("pass %d: ts_config(%s).deps in workers/proxy/test = %v, want %v",
				pass, tsConfigTargetName, got, want)
		}
		captureLog(t, func() { convergeGazelle(t, root) })
	}
	assertNoDanglingLabels(t, root)
}

// The shape the monorepo is actually in: the BUILD file and its ts_config are
// already there, written by a run that generated no deps. `deps` is not
// mergeable, which the merger reads as "write it when the attribute is absent",
// so the run that reads the extends is the one that repairs the file.
func TestTsConfigExtendsChain_FillsInADepsLessRuleAlreadyInTheFile(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"package.json":                      `{"name":"w"}` + "\n",
		"workers/proxy/tsconfig.json":       `{"compilerOptions":{"lib":["es2022"]}}` + "\n",
		"workers/proxy/src/index.ts":        "export const worker = 1;\n",
		"workers/proxy/test/tsconfig.json":  `{"extends":"../tsconfig.json"}` + "\n",
		"workers/proxy/test/worker.test.ts": "export const t = 1;\n",
		"workers/proxy/test/BUILD.bazel": `load("@rules_typescript//ts:defs.bzl", "ts_config")

ts_config(
    name = "tsconfig",
    src = "tsconfig.json",
    visibility = ["//visibility:public"],
)
`,
	})
	captureLog(t, func() { convergeGazelle(t, root) })

	got := tsConfigDeps(t, root, "workers/proxy/test")
	want := []string{"//workers/proxy:" + tsConfigTargetName}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ts_config(%s).deps in workers/proxy/test = %v, want %v",
			tsConfigTargetName, got, want)
	}
	assertNoDanglingLabels(t, root)
}

func TestTsConfigExtendsChain_ClimbsMoreThanOneLevel(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"package.json":                     `{"name":"w"}` + "\n",
		"apps/web/tsconfig.json":           `{"compilerOptions":{"lib":["es2022"]}}` + "\n",
		"apps/web/src/index.ts":            "export const web = 1;\n",
		"apps/web/test/unit/tsconfig.json": `{"extends":"../../tsconfig.json"}` + "\n",
		"apps/web/test/unit/a.test.ts":     "export const t = 1;\n",
	})
	captureLog(t, func() { convergeGazelle(t, root) })

	got := tsConfigDeps(t, root, "apps/web/test/unit")
	want := []string{"//apps/web:" + tsConfigTargetName}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ts_config(%s).deps in apps/web/test/unit = %v, want %v",
			tsConfigTargetName, got, want)
	}
	assertNoDanglingLabels(t, root)
}

// A relative extends is a claim about the tree, and a claim can be wrong. The
// label would name a target in a package that holds no tsconfig.json at all,
// and one dangling label fails analysis for the whole workspace.
func TestTsConfigExtendsChain_MissingBaseMintsNoLabel(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"package.json":          `{"name":"w"}` + "\n",
		"pkg/src/index.ts":      "export const pkg = 1;\n",
		"pkg/lib/tsconfig.json": `{"extends":"../tsconfig.json"}` + "\n",
		"pkg/lib/helper.ts":     "export const helper = 1;\n",
	})
	captureLog(t, func() { convergeGazelle(t, root) })

	if got := tsConfigDeps(t, root, "pkg/lib"); len(got) != 0 {
		t.Errorf("ts_config(%s).deps in pkg/lib = %v, want none: pkg holds no tsconfig.json",
			tsConfigTargetName, got)
	}
	assertNoDanglingLabels(t, root)
}

// A base above the workspace root is a real file that stats perfectly well and
// has no label at all.
func TestTsConfigExtendsChain_BaseOutsideTheRepoMintsNoLabel(t *testing.T) {
	outer := t.TempDir()
	root := filepath.Join(outer, "repo")
	writeTsConfig(t, filepath.Join(outer, "shared/tsconfig.json"),
		`{"compilerOptions":{"lib":["es2022"]}}`+"\n")
	writeWorkspace(t, root, map[string]string{
		"package.json":           `{"name":"w"}` + "\n",
		"apps/web/tsconfig.json": `{"extends":"../../../shared/tsconfig.json"}` + "\n",
		"apps/web/index.ts":      "export const web = 1;\n",
	})
	captureLog(t, func() { convergeGazelle(t, root) })

	if got := tsConfigDeps(t, root, "apps/web"); len(got) != 0 {
		t.Errorf("ts_config(%s).deps in apps/web = %v, want none: the base is outside the repo",
			tsConfigTargetName, got)
	}
	assertNoDanglingLabels(t, root)
}

// os.Stat says yes to a directory, and Gazelle writes no ts_config for one, so
// a label computed from the extends alone would name nothing.
func TestTsConfigExtendsChain_BaseIsADirectoryMintsNoLabel(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"package.json":          `{"name":"w"}` + "\n",
		"pkg/src/index.ts":      "export const pkg = 1;\n",
		"pkg/lib/tsconfig.json": `{"extends":"../tsconfig.json"}` + "\n",
		"pkg/lib/helper.ts":     "export const helper = 1;\n",
	})
	if err := os.MkdirAll(filepath.Join(root, "pkg/tsconfig.json"), 0o750); err != nil {
		t.Fatal(err)
	}
	captureLog(t, func() { convergeGazelle(t, root) })

	if got := tsConfigDeps(t, root, "pkg/lib"); len(got) != 0 {
		t.Errorf("ts_config(%s).deps in pkg/lib = %v, want none: pkg/tsconfig.json is a directory",
			tsConfigTargetName, got)
	}
	assertNoDanglingLabels(t, root)
}

// An array states a merge order, not which of its entries Bazel should stage,
// so Gazelle writes nothing and what the author writes is what survives.
func TestTsConfigExtendsChain_ArrayIsLeftToTheAuthor(t *testing.T) {
	files := map[string]string{
		"package.json":                     `{"name":"w"}` + "\n",
		"workers/proxy/tsconfig.json":      `{"compilerOptions":{"lib":["es2022"]}}` + "\n",
		"workers/proxy/src/index.ts":       "export const worker = 1;\n",
		"workers/proxy/test/local.json":    `{"compilerOptions":{"noEmit":true}}` + "\n",
		"workers/proxy/test/tsconfig.json": `{"extends":["../tsconfig.json","./local.json"]}` + "\n",
		"workers/proxy/test/a.test.ts":     "export const t = 1;\n",
	}

	t.Run("gazelle writes no deps", func(t *testing.T) {
		root := t.TempDir()
		writeWorkspace(t, root, files)
		captureLog(t, func() { convergeGazelle(t, root) })

		if got := tsConfigDeps(t, root, "workers/proxy/test"); len(got) != 0 {
			t.Errorf("ts_config(%s).deps in workers/proxy/test = %v, want none: the extends is an array",
				tsConfigTargetName, got)
		}
		assertNoDanglingLabels(t, root)
	})

	t.Run("the author's deps survive", func(t *testing.T) {
		root := t.TempDir()
		writeWorkspace(t, root, files)
		writeWorkspace(t, root, map[string]string{
			"workers/proxy/test/BUILD.bazel": authoredTsConfig(`"//workers/proxy:tsconfig", "local.json"`),
		})
		captureLog(t, func() { convergeGazelle(t, root) })

		got := tsConfigDeps(t, root, "workers/proxy/test")
		want := []string{"local.json", "//workers/proxy:" + tsConfigTargetName}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("ts_config(%s).deps in workers/proxy/test = %v, want the author's %v",
				tsConfigTargetName, got, want)
		}
		assertNoDanglingLabels(t, root)
	})
}

// An absolute specifier resolves on exactly the machine that wrote it, and a
// path that only one checkout has is not a chain Gazelle should bake into a
// BUILD file the whole repository reads.
func TestTsConfigExtendsChain_AbsoluteSpecifierIsLeftToTheAuthor(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"package.json":          `{"name":"w"}` + "\n",
		"pkg/tsconfig.json":     `{"compilerOptions":{"lib":["es2022"]}}` + "\n",
		"pkg/src/index.ts":      "export const pkg = 1;\n",
		"pkg/lib/tsconfig.json": `{"extends":"` + filepath.ToSlash(filepath.Join(root, "pkg/tsconfig.json")) + `"}` + "\n",
		"pkg/lib/helper.ts":     "export const helper = 1;\n",
	})
	captureLog(t, func() { convergeGazelle(t, root) })

	if got := tsConfigDeps(t, root, "pkg/lib"); len(got) != 0 {
		t.Errorf("ts_config(%s).deps in pkg/lib = %v, want none: the extends is an absolute path",
			tsConfigTargetName, got)
	}
	assertNoDanglingLabels(t, root)
}

// A package-form specifier resolves through node_modules, which a Bazel
// checkout does not have and no label names.
func TestTsConfigExtendsChain_PackageFormIsLeftToTheAuthor(t *testing.T) {
	files := map[string]string{
		"package.json":           `{"name":"w"}` + "\n",
		"apps/web/base.json":     `{"compilerOptions":{"lib":["es2022"]}}` + "\n",
		"apps/web/tsconfig.json": `{"extends":"@tsconfig/node20/tsconfig.json"}` + "\n",
		"apps/web/index.ts":      "export const web = 1;\n",
	}

	t.Run("gazelle writes no deps", func(t *testing.T) {
		root := t.TempDir()
		writeWorkspace(t, root, files)
		captureLog(t, func() { convergeGazelle(t, root) })

		if got := tsConfigDeps(t, root, "apps/web"); len(got) != 0 {
			t.Errorf("ts_config(%s).deps in apps/web = %v, want none: the extends is package-form",
				tsConfigTargetName, got)
		}
		assertNoDanglingLabels(t, root)
	})

	t.Run("the author's deps survive", func(t *testing.T) {
		root := t.TempDir()
		writeWorkspace(t, root, files)
		writeWorkspace(t, root, map[string]string{
			"apps/web/BUILD.bazel": authoredTsConfig(`"base.json"`),
		})
		captureLog(t, func() { convergeGazelle(t, root) })

		got := tsConfigDeps(t, root, "apps/web")
		want := []string{"base.json"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("ts_config(%s).deps in apps/web = %v, want the author's %v",
				tsConfigTargetName, got, want)
		}
		assertNoDanglingLabels(t, root)
	})
}

func authoredTsConfig(deps string) string {
	return `load("@rules_typescript//ts:defs.bzl", "ts_config")

ts_config(
    name = "tsconfig",
    src = "tsconfig.json",
    deps = [` + deps + `],
)
`
}
