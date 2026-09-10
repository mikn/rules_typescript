package typescript

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
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
	requireTsgo(t)
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
	// A second pass reads a BUILD file that already carries the value and
	// recomputes it, which is the run that has to land on the same label.
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
// already there, written by a run that generated no deps, so the run that reads
// the extends is the one that repairs the file.
func TestTsConfigExtendsChain_FillsInADepsLessRuleAlreadyInTheFile(t *testing.T) {
	requireTsgo(t)
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
	requireTsgo(t)
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
	requireTsgo(t)
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
	requireTsgo(t)
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

// Every base of an extends array that is a tsconfig.json in the repository is
// a dep; one that is not has no ts_config to name and is the author's.
func TestTsConfigExtendsChain_ArrayBasesAreEachADep(t *testing.T) {
	requireTsgo(t)
	files := map[string]string{
		"package.json":                     `{"name":"w"}` + "\n",
		"workers/proxy/tsconfig.json":      `{"compilerOptions":{"lib":["es2022"]}}` + "\n",
		"workers/proxy/src/index.ts":       "export const worker = 1;\n",
		"workers/proxy/test/local.json":    `{"compilerOptions":{"noEmit":true}}` + "\n",
		"workers/proxy/test/tsconfig.json": `{"extends":["../tsconfig.json","./local.json"]}` + "\n",
		"workers/proxy/test/a.test.ts":     "export const t = 1;\n",
	}

	t.Run("the tsconfig.json base is a dep, the other said", func(t *testing.T) {
		root := t.TempDir()
		writeWorkspace(t, root, files)
		logged := captureLog(t, func() { convergeGazelle(t, root) })

		got := tsConfigDeps(t, root, "workers/proxy/test")
		want := []string{"//workers/proxy:" + tsConfigTargetName}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("ts_config(%s).deps in workers/proxy/test = %v, want %v",
				tsConfigTargetName, got, want)
		}
		if !strings.Contains(logged, "local.json") {
			t.Errorf("the base with no ts_config was not named:\n%s", logged)
		}
		assertNoDanglingLabels(t, root)
	})

	t.Run("the author's kept dep joins Gazelle's", func(t *testing.T) {
		root := t.TempDir()
		writeWorkspace(t, root, files)
		writeWorkspace(t, root, map[string]string{
			"workers/proxy/test/BUILD.bazel": authoredTsConfig(`
        # keep
        "local.json",
    `),
		})
		captureLog(t, func() { convergeGazelle(t, root) })

		got := tsConfigDeps(t, root, "workers/proxy/test")
		want := []string{"local.json", "//workers/proxy:" + tsConfigTargetName}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("ts_config(%s).deps in workers/proxy/test = %v, want %v",
				tsConfigTargetName, got, want)
		}
		assertNoDanglingLabels(t, root)
	})
}

// An absolute specifier inside the repository is a chain file like any other:
// tsc reads it, so the ts_config stages it.
func TestTsConfigExtendsChain_AbsoluteSpecifierInTheRepoIsADep(t *testing.T) {
	requireTsgo(t)
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"package.json":          `{"name":"w"}` + "\n",
		"pkg/tsconfig.json":     `{"compilerOptions":{"lib":["es2022"]}}` + "\n",
		"pkg/src/index.ts":      "export const pkg = 1;\n",
		"pkg/lib/tsconfig.json": `{"extends":"` + filepath.ToSlash(filepath.Join(root, "pkg/tsconfig.json")) + `"}` + "\n",
		"pkg/lib/helper.ts":     "export const helper = 1;\n",
	})
	captureLog(t, func() { convergeGazelle(t, root) })

	got := tsConfigDeps(t, root, "pkg/lib")
	want := []string{"//pkg:" + tsConfigTargetName}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ts_config(%s).deps in pkg/lib = %v, want %v",
			tsConfigTargetName, got, want)
	}
	assertNoDanglingLabels(t, root)
}

// A package-form specifier resolves through node_modules, which a Bazel
// checkout does not have and no label names.
func TestTsConfigExtendsChain_PackageFormIsLeftToTheAuthor(t *testing.T) {
	requireTsgo(t)
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

	t.Run("the author's kept deps survive", func(t *testing.T) {
		root := t.TempDir()
		writeWorkspace(t, root, files)
		writeWorkspace(t, root, map[string]string{
			"apps/web/BUILD.bazel": keptTsConfig(`"base.json"`),
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

// The defect: a relative extends is a claim about the tree, and the tree
// changes. Gazelle wrote the label, so the run after the base is gone is the
// run that has to clear it -- one label no target satisfies fails analysis for
// the whole workspace, however few targets named it.
func TestTsConfigExtendsChain_RemovedBaseDropsTheDep(t *testing.T) {
	requireTsgo(t)
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"package.json":                      `{"name":"w"}` + "\n",
		"workers/proxy/tsconfig.json":       `{"compilerOptions":{"lib":["es2022"]}}` + "\n",
		"workers/proxy/src/index.ts":        "export const worker = 1;\n",
		"workers/proxy/test/tsconfig.json":  `{"extends":"../tsconfig.json"}` + "\n",
		"workers/proxy/test/worker.test.ts": "export const t = 1;\n",
	})
	captureLog(t, func() { convergeGazelle(t, root) })

	if err := os.Remove(filepath.Join(root, "workers/proxy/tsconfig.json")); err != nil {
		t.Fatal(err)
	}
	writeWorkspace(t, root, map[string]string{
		"workers/proxy/test/tsconfig.json": `{"compilerOptions":{"lib":["es2022"]}}` + "\n",
	})

	for pass := 1; pass <= 2; pass++ {
		captureLog(t, func() { convergeGazelle(t, root) })
		if got := tsConfigDeps(t, root, "workers/proxy/test"); len(got) != 0 {
			t.Errorf("pass %d: ts_config(%s).deps in workers/proxy/test = %v, want none: "+
				"workers/proxy holds no tsconfig.json any more", pass, tsConfigTargetName, got)
		}
	}
	assertNoDanglingLabels(t, root)
}

// The base moves rather than going away: the label has to follow it. A merger
// that only appended would leave both, and the one Gazelle no longer computes
// is the dangling half.
func TestTsConfigExtendsChain_MovedBaseRepointsTheDep(t *testing.T) {
	requireTsgo(t)
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"package.json":                      `{"name":"w"}` + "\n",
		"workers/proxy/tsconfig.json":       `{"compilerOptions":{"lib":["es2022"]}}` + "\n",
		"workers/proxy/src/index.ts":        "export const worker = 1;\n",
		"workers/proxy/test/tsconfig.json":  `{"extends":"../tsconfig.json"}` + "\n",
		"workers/proxy/test/worker.test.ts": "export const t = 1;\n",
	})
	captureLog(t, func() { convergeGazelle(t, root) })

	if err := os.Remove(filepath.Join(root, "workers/proxy/tsconfig.json")); err != nil {
		t.Fatal(err)
	}
	writeWorkspace(t, root, map[string]string{
		"workers/tsconfig.json":            `{"compilerOptions":{"lib":["es2022"]}}` + "\n",
		"workers/proxy/test/tsconfig.json": `{"extends":"../../tsconfig.json"}` + "\n",
	})
	captureLog(t, func() { convergeGazelle(t, root) })

	got := tsConfigDeps(t, root, "workers/proxy/test")
	want := []string{"//workers:" + tsConfigTargetName}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ts_config(%s).deps in workers/proxy/test = %v, want %v",
			tsConfigTargetName, got, want)
	}
	assertNoDanglingLabels(t, root)
}

// The break. `deps` is Gazelle's now, so a hand-written value with no "# keep"
// is replaced -- and a declared build input disappearing has to be said out
// loud, which reportManagedAttrDrops does for every mergeable attribute.
func TestTsConfigExtendsChain_HandWrittenDepWithNoKeepIsReplaced(t *testing.T) {
	requireTsgo(t)
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"package.json":                      `{"name":"w"}` + "\n",
		"workers/proxy/tsconfig.json":       `{"compilerOptions":{"lib":["es2022"]}}` + "\n",
		"workers/proxy/src/index.ts":        "export const worker = 1;\n",
		"workers/proxy/test/local.json":     `{"compilerOptions":{"noEmit":true}}` + "\n",
		"workers/proxy/test/tsconfig.json":  `{"extends":"../tsconfig.json"}` + "\n",
		"workers/proxy/test/worker.test.ts": "export const t = 1;\n",
		"workers/proxy/test/BUILD.bazel":    authoredTsConfig(`"local.json"`),
	})
	logged := captureLog(t, func() { convergeGazelle(t, root) })

	got := tsConfigDeps(t, root, "workers/proxy/test")
	want := []string{"//workers/proxy:" + tsConfigTargetName}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ts_config(%s).deps in workers/proxy/test = %v, want %v",
			tsConfigTargetName, got, want)
	}
	if !strings.Contains(logged, `"local.json"`) {
		t.Errorf("the run replaced the hand-written deps and said nothing about "+
			"\"local.json\"; log was %q", logged)
	}
	assertNoDanglingLabels(t, root)
}

// The other half of the break: "# keep" on the element is the edit that holds a
// hand-written entry, and Gazelle's own label joins it rather than replacing it.
func TestTsConfigExtendsChain_KeptHandWrittenDepSurvives(t *testing.T) {
	requireTsgo(t)
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"package.json":                      `{"name":"w"}` + "\n",
		"workers/proxy/tsconfig.json":       `{"compilerOptions":{"lib":["es2022"]}}` + "\n",
		"workers/proxy/src/index.ts":        "export const worker = 1;\n",
		"workers/proxy/test/local.json":     `{"compilerOptions":{"noEmit":true}}` + "\n",
		"workers/proxy/test/tsconfig.json":  `{"extends":"../tsconfig.json"}` + "\n",
		"workers/proxy/test/worker.test.ts": "export const t = 1;\n",
		"workers/proxy/test/BUILD.bazel": authoredTsConfig(`
        # keep
        "local.json",
    `),
	})
	want := []string{"local.json", "//workers/proxy:" + tsConfigTargetName}
	// Twice: a run that keeps the value but loses the "# keep" holding it has
	// only moved the deletion one run out.
	for pass := 1; pass <= 2; pass++ {
		captureLog(t, func() { convergeGazelle(t, root) })
		got := tsConfigDeps(t, root, "workers/proxy/test")
		if !reflect.DeepEqual(got, want) {
			t.Errorf("pass %d: ts_config(%s).deps in workers/proxy/test = %v, want %v",
				pass, tsConfigTargetName, got, want)
		}
	}
	assertNoDanglingLabels(t, root)
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

func keptTsConfig(deps string) string {
	return `load("@rules_typescript//ts:defs.bzl", "ts_config")

ts_config(
    name = "tsconfig",
    src = "tsconfig.json",
    # keep
    deps = [` + deps + `],
)
`
}

// tsConfigAttr is a string attribute of the ts_config target in pkg, "" when
// unset; the rule is required to be there, as for tsConfigDeps.
func tsConfigAttr(t *testing.T, root, pkg, attr string) string {
	t.Helper()
	for _, r := range loadRules(t, root, pkg) {
		if r.Kind() == "ts_config" && r.Name() == tsConfigTargetName {
			return r.AttrString(attr)
		}
	}
	t.Fatalf("generation wrote no ts_config(%s) in %s", tsConfigTargetName, pkg)
	return ""
}

// jsx: preserve names a .tsx's emit, and the rule reads it off the ts_config:
// the chain's effective value is written, inherited through extends, no other.
func TestTsConfigJsx_PreserveIsDeclaredFromTheChain(t *testing.T) {
	requireTsgo(t)
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"package.json":             `{"name":"w"}` + "\n",
		"solid/tsconfig.json":      `{"compilerOptions":{"jsx":"preserve"}}` + "\n",
		"solid/view.tsx":           "export const v = 1;\n",
		"solid/test/tsconfig.json": `{"extends":"../tsconfig.json"}` + "\n",
		"solid/test/view.test.tsx": "export const t = 1;\n",
		"react/tsconfig.json":      `{"compilerOptions":{"jsx":"react-jsx"}}` + "\n",
		"react/view.tsx":           "export const r = 1;\n",
	})
	captureLog(t, func() { convergeGazelle(t, root) })

	want := map[string]string{
		"solid": "preserve", "solid/test": "preserve", "react": "",
	}
	for pass := 1; pass <= 2; pass++ {
		for pkg, jsx := range want {
			if got := tsConfigAttr(t, root, pkg, "jsx"); got != jsx {
				t.Errorf("pass %d: ts_config(%s).jsx in %s = %q, want %q",
					pass, tsConfigTargetName, pkg, got, jsx)
			}
		}
		captureLog(t, func() { convergeGazelle(t, root) })
	}
	assertNoDanglingLabels(t, root)
}

// The chain's module, when tsgo emits it, names a program's ES twins: written
// lowercased as tsgo prints it, inherited through extends; an ES kind: nothing.
func TestTsConfigModule_TsgoEmittedIsDeclaredFromTheChain(t *testing.T) {
	requireTsgo(t)
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"package.json":           `{"name":"w"}` + "\n",
		"cjs/tsconfig.json":      `{"compilerOptions":{"module":"CommonJS"}}` + "\n",
		"cjs/lib.ts":             "export const c = 1;\n",
		"cjs/test/tsconfig.json": `{"extends":"../tsconfig.json"}` + "\n",
		"cjs/test/lib.test.ts":   "export const t = 1;\n",
		"node/tsconfig.json":     `{"compilerOptions":{"module":"NodeNext"}}` + "\n",
		"node/lib.ts":            "export const n = 1;\n",
		"esm/tsconfig.json":      `{"compilerOptions":{"module":"ESNext"}}` + "\n",
		"esm/lib.ts":             "export const e = 1;\n",
	})
	captureLog(t, func() { convergeGazelle(t, root) })

	want := map[string]string{
		"cjs": "commonjs", "cjs/test": "commonjs", "node": "nodenext", "esm": "",
	}
	for pass := 1; pass <= 2; pass++ {
		for pkg, module := range want {
			if got := tsConfigAttr(t, root, pkg, "module"); got != module {
				t.Errorf("pass %d: ts_config(%s).module in %s = %q, want %q",
					pass, tsConfigTargetName, pkg, got, module)
			}
		}
		captureLog(t, func() { convergeGazelle(t, root) })
	}
	assertNoDanglingLabels(t, root)
}
