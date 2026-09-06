package typescript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A cross-package cycle is invisible from any one GenerateRules call, so these
// tests drive the whole run -- generate, index, resolve, finish -- the way
// convergeGazelle does, and read what the finished run said.

// cycleReport is the clause this reporter is recognised by, so the
// same-directory controls below cannot pass on another message.
const cycleReport = "a dependency cycle Bazel rejects"

// mutualImports is the two-directory cycle most of these fixtures are built on.
func mutualImports() map[string]string {
	return map[string]string{
		"BUILD.bazel": "",
		"a/thing.ts":  "import { b } from \"../b/thing\";\nexport const a = b;\n",
		"b/thing.ts":  "import { a } from \"../a/thing\";\nexport const b = 1;\nexport { a };\n",
	}
}

// ringOfThree is the same shape one package longer, where no two packages
// import each other directly.
func ringOfThree() map[string]string {
	return map[string]string{
		"BUILD.bazel": "",
		"a/thing.ts":  "import { b } from \"../b/thing\";\nexport const a = b;\n",
		"b/thing.ts":  "import { c } from \"../c/thing\";\nexport const b = c;\n",
		"c/thing.ts":  "import { a } from \"../a/thing\";\nexport const c = a;\n",
	}
}

func withFiles(base map[string]string, extra map[string]string) map[string]string {
	for name, content := range extra {
		base[name] = content
	}
	return base
}

func readBuildFile(t *testing.T, repoRoot, pkg string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(pkg), "BUILD.bazel"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestImportCycle_SiblingDirectoriesAreReported is a PIN: under the every-dir
// boundary each directory is a package, so two siblings importing each other
// are two Bazel targets importing each other. Bazel prints a loop of target
// labels when it loads them; this says which packages closed it, while the
// BUILD files are still being written.
func TestImportCycle_SiblingDirectoriesAreReported(t *testing.T) {
	logged := captureLog(t, func() {
		convergeTree(t, mutualImports())
	})

	for _, want := range []string{cycleReport, "//a:a", "//b:b"} {
		if !strings.Contains(logged, want) {
			t.Errorf("the report does not name %q; log was %q", want, logged)
		}
	}
}

// TestImportCycle_ThreeDirectoriesAreOneReport is a PIN: one strongly connected
// component is one problem, and the count the message opens with is the number
// of packages in it. A message per edge, per direction, or per directory
// visited turns a cycle a repository has lived with into noise on every run,
// and noise is worse than silence.
func TestImportCycle_ThreeDirectoriesAreOneReport(t *testing.T) {
	logged := captureLog(t, func() {
		convergeTree(t, ringOfThree())
	})

	if n := strings.Count(logged, cycleReport); n != 1 {
		t.Fatalf("a three-package cycle was reported %d times, want 1; log was %q", n, logged)
	}
	if !strings.Contains(logged, "3 packages") {
		t.Errorf("the report does not count the packages it goes on to name; log was %q",
			logged)
	}
	for _, want := range []string{"//a:a", "//b:b", "//c:c"} {
		if !strings.Contains(logged, want) {
			t.Errorf("the report does not name %q; log was %q", want, logged)
		}
	}
}

// typeRingWithASuppressedValueImport is the shape a per-edge remedy cannot
// survive. //a, //b and //c import each other with `import type` alone, and
// //a also imports //c for a value -- a label the held deps on //a keeps out
// of every BUILD file. So the three erased edges are the whole of what Bazel
// rejects, while a runtime dependency does run from //a to //c.
func typeRingWithASuppressedValueImport() map[string]string {
	return map[string]string{
		"BUILD.bazel": "",
		"a/thing.ts": "import type { B } from \"../b/thing\";\n" +
			"import { c } from \"../c/thing\";\n" +
			"export type A = { b: B };\nexport const a = c;\n",
		"b/thing.ts": "import type { C } from \"../c/thing\";\nexport type B = { c: C };\n",
		"c/thing.ts": "import type { A } from \"../a/thing\";\n" +
			"export type C = { a?: A };\nexport const c = 1;\n",
		"a/BUILD.bazel": `load("@rules_typescript//ts:defs.bzl", "ts_compile")

ts_compile(
    name = "a",
    srcs = ["thing.ts"],
    visibility = ["//visibility:public"],
    # keep
    deps = ["//b"],
)
`,
	}
}

// TestImportCycle_ReportClaimsNothingAboutTheImportsOrTheRemedy is a PIN on the
// whole of what the message is allowed to say. Every edge of the reported cycle
// here is erased at emit, and a value import still runs between two of the
// packages it names -- so "no runtime dependency runs between them" is false of
// this tree, and so is any remedy that follows from it. The report names the
// cycle and stops.
func TestImportCycle_ReportClaimsNothingAboutTheImportsOrTheRemedy(t *testing.T) {
	var repoRoot string
	logged := captureLog(t, func() {
		repoRoot = convergeTree(t, typeRingWithASuppressedValueImport())
	})

	emitted := readBuildFile(t, repoRoot, "a")
	if !strings.Contains(emitted, `"//b"`) || strings.Contains(emitted, `"//c"`) {
		t.Fatalf("the held deps no longer names //b alone, so the value import //a -> //c "+
			"is no longer one the BUILD files leave out; a/BUILD.bazel was:\n%s", emitted)
	}
	if !strings.Contains(logged, cycleReport) {
		t.Fatalf("the cycle the emitted deps do close went unreported; log was %q", logged)
	}
	for _, forbidden := range []string{"no runtime dependency", "hoist", "invert"} {
		if strings.Contains(logged, forbidden) {
			t.Errorf("the report says %q of a cycle whose every reported edge is erased "+
				"while a value import runs from //a:a to //c:c; log was %q", forbidden, logged)
		}
	}
	for _, forbidden := range []string{" -> ", ` imports "`} {
		if strings.Contains(logged, forbidden) {
			t.Errorf("the report lists the imports behind its edges (%q); the reduced report "+
				"names the cycle and nothing that could be false about it; log was %q",
				forbidden, logged)
		}
	}
}

// keptRuleNamingB hands the whole rule back to its author, source list
// included -- which is the usual reason to keep a rule -- and names the dep
// that closes the cycle.
const keptRuleNamingB = `load("@rules_typescript//ts:defs.bzl", "ts_compile")

# keep
ts_compile(
    name = "a",
    srcs = ["thing.ts"],
    visibility = ["//visibility:public"],
    deps = ["//b"],
)
`

// TestImportCycle_ASourceTheKeptRuleLeavesOutIsNoEdge is a PIN on the half of
// the edge rule that reads srcs. //a:a is held with a source list its author
// wrote, and the only file importing //b is one that list leaves out -- so the
// target Bazel loads compiles no import of //b at all. Reading the generated
// rule's srcs instead reports a cycle on the strength of a file //a:a never
// sees, and sends the reader to delete an import that changes nothing.
func TestImportCycle_ASourceTheKeptRuleLeavesOutIsNoEdge(t *testing.T) {
	var repoRoot string
	logged := captureLog(t, func() {
		repoRoot = convergeTree(t, map[string]string{
			"BUILD.bazel":   "",
			"a/BUILD.bazel": keptRuleNamingB,
			"a/thing.ts":    "export const a = 1;\n",
			"a/other.ts":    "import { b } from \"../b/thing\";\nexport const other = b;\n",
			"b/thing.ts":    "import { a } from \"../a/thing\";\nexport const b = a;\n",
		})
	})

	emitted := readBuildFile(t, repoRoot, "a")
	if !strings.Contains(emitted, `srcs = ["thing.ts"]`) || strings.Contains(emitted, "other.ts") {
		t.Fatalf("the kept rule no longer holds a source list the generated one would widen, "+
			"so this pin says nothing; a/BUILD.bazel was:\n%s", emitted)
	}
	if !strings.Contains(emitted, `"//b"`) {
		t.Fatalf("the kept rule no longer carries the //b dep, so there is no candidate edge "+
			"left to confirm; a/BUILD.bazel was:\n%s", emitted)
	}
	if strings.Contains(logged, cycleReport) {
		t.Errorf("reported a cycle whose //a:a -> //b:b edge only a file outside //a:a's own "+
			"srcs writes; log was %q", logged)
	}
}

// TestImportCycle_AKeptTargetIsNotCalledGenerated is a PIN on the headline.
// //a:a is written by hand and held with "# keep": Gazelle generates a
// candidate for that directory and the merger throws it away, so the target in
// the cycle is not one Gazelle generated.
func TestImportCycle_AKeptTargetIsNotCalledGenerated(t *testing.T) {
	var repoRoot string
	logged := captureLog(t, func() {
		repoRoot = convergeTree(t, withFiles(mutualImports(), map[string]string{
			"a/BUILD.bazel": keptRuleNamingB,
		}))
	})

	if emitted := readBuildFile(t, repoRoot, "a"); !strings.Contains(emitted, `"//b"`) {
		t.Fatalf("the kept rule lost its dep, so this fixture emits no cycle; "+
			"a/BUILD.bazel was:\n%s", emitted)
	}
	if !strings.Contains(logged, cycleReport) {
		t.Fatalf("the cycle the kept rule closes went unreported; log was %q", logged)
	}
	if strings.Contains(logged, "generated") {
		t.Errorf("the report calls the cycle's targets generated, of one its author wrote "+
			"and held with \"# keep\"; log was %q", logged)
	}
}

// TestImportCycle_AcyclicTreeIsSilent is a CONTROL: it passes both before and
// after the detector exists. A one-way import between packages is the ordinary
// shape of every repository, and a report on it would be a false positive on
// every run.
func TestImportCycle_AcyclicTreeIsSilent(t *testing.T) {
	logged := captureLog(t, func() {
		convergeTree(t, map[string]string{
			"BUILD.bazel": "",
			"a/thing.ts":  "import { b } from \"../b/thing\";\nexport const a = b;\n",
			"b/thing.ts":  "export const b = 1;\n",
			"c/thing.ts":  "import { b } from \"../b/thing\";\nexport const c = b;\n",
		})
	})

	if strings.Contains(logged, cycleReport) {
		t.Errorf("reported a cycle in an acyclic tree; log was %q", logged)
	}
}

// TestImportCycle_DocSplitInOneDirectoryIsNotReported is a CONTROL and a
// documented gap: it passes both before and after the detector exists. A doc
// file is its own target in the same directory, so a doc file and the library
// importing each other is a cycle Bazel rejects that no reporter sees. The
// fixture asserts the emitted cycle so the gap cannot be mistaken for absence.
func TestImportCycle_DocSplitInOneDirectoryIsNotReported(t *testing.T) {
	var repoRoot string
	logged := captureLog(t, func() {
		repoRoot = convergeTree(t, map[string]string{
			"BUILD.bazel":     "",
			"a/thing.ts":      "import { d } from \"./thing.doc\";\nexport const a = d;\n",
			"a/thing.doc.tsx": "import { a } from \"./thing\";\nexport const d = a;\n",
		})
	})

	if !contains(depsOf(t, repoRoot, "a", "ts_compile", "a"), ":a_doc") ||
		!contains(depsOf(t, repoRoot, "a", "ts_compile", "a_doc"), ":a") {
		t.Fatalf("the doc split no longer emits a cycle, so this control says nothing; "+
			"a/BUILD.bazel was:\n%s", readBuildFile(t, repoRoot, "a"))
	}
	if strings.Contains(logged, cycleReport) {
		t.Errorf("the same-directory doc-split cycle is reported now -- the documented "+
			"coverage says it is not; log was %q", logged)
	}
}

// TestImportCycle_TestSplitInOneDirectoryIsNotReported is the same CONTROL for
// the other same-directory split: a test file is its own ts_test target, and a
// library importing its own test file closes a cycle between the two.
func TestImportCycle_TestSplitInOneDirectoryIsNotReported(t *testing.T) {
	var repoRoot string
	logged := captureLog(t, func() {
		repoRoot = convergeTree(t, map[string]string{
			"BUILD.bazel":     "",
			"a/thing.ts":      "import { helper } from \"./thing.test\";\nexport const a = helper;\n",
			"a/thing.test.ts": "import { a } from \"./thing\";\nexport const helper = a;\n",
		})
	})

	emitted := readBuildFile(t, repoRoot, "a")
	if !strings.Contains(emitted, `deps = [":a_test"]`) || !strings.Contains(emitted, `deps = [":a"]`) {
		t.Fatalf("the test split no longer emits a cycle, so this control says nothing; "+
			"a/BUILD.bazel was:\n%s", emitted)
	}
	if strings.Contains(logged, cycleReport) {
		t.Errorf("the same-directory test-split cycle is reported now -- the documented "+
			"coverage says it is not; log was %q", logged)
	}
}

// keptDepsBuild hands the deps attribute back to the author, which is what a
// repository does to work around a resolver gap.
const keptDepsBuild = `load("@rules_typescript//ts:defs.bzl", "ts_compile")

ts_compile(
    name = "a",
    srcs = ["thing.ts"],
    visibility = ["//visibility:public"],
    # keep
    deps = [],
)
`

// keptRuleBuild hands the whole rule back with no deps at all, the coarsest of
// the three "# keep" granularities.
const keptRuleBuild = `load("@rules_typescript//ts:defs.bzl", "ts_compile")

# keep
ts_compile(
    name = "a",
    srcs = ["thing.ts"],
    visibility = ["//visibility:public"],
)
`

// unmergeableDepsBuild states deps as a select over a key that is not a
// platform. `extractPlatformStringsExprs` cannot classify it, so
// `mergeAttrValues` errors, and `rule.MergeRules` logs "could not merge
// expression" and LEAVES THE ATTRIBUTE ALONE -- the same outcome as a `# keep`,
// reached without one.
const unmergeableDepsBuild = `load("@rules_typescript//ts:defs.bzl", "ts_compile")

ts_compile(
    name = "a",
    srcs = ["thing.ts"],
    visibility = ["//visibility:public"],
    deps = select({
        "//:prod": [],
        "//conditions:default": [],
    }),
)
`

// TestImportCycle_UnmergeableDepsIsNotReported is a PIN. A `# keep` is not the
// only thing that stops the merger writing what Gazelle computed: an expression
// it cannot reconcile does too, and the emitted files are then just as acyclic.
// The extension's own reportUnmergeableExpr fires on this very attribute in the
// same run, so the fact is already in hand when the report is written.
func TestImportCycle_UnmergeableDepsIsNotReported(t *testing.T) {
	var repoRoot string
	logged := captureLog(t, func() {
		repoRoot = convergeTree(t, withFiles(mutualImports(), map[string]string{
			"a/BUILD.bazel": unmergeableDepsBuild,
		}))
	})

	if emitted := readBuildFile(t, repoRoot, "a"); strings.Contains(emitted, "//b") {
		t.Fatalf("the resolved label reached the unmergeable attribute, so this fixture no "+
			"longer emits an acyclic pair of BUILD files; a/BUILD.bazel was:\n%s", emitted)
	}
	if strings.Contains(logged, cycleReport) {
		t.Errorf("reported a cycle Bazel accepts: the deps that would close it sit in an "+
			"expression the merger leaves alone; log was %q", logged)
	}
}

// TestImportCycle_KeptDepsIsNotReported is a PIN: "# keep" above deps makes the
// merger discard the labels the resolver computed, so the emitted BUILD files
// are acyclic and Bazel accepts them. Reporting one is a false positive, and it
// lands on the repository that already had a resolver problem -- handing deps
// back is what you do about one.
func TestImportCycle_KeptDepsIsNotReported(t *testing.T) {
	var repoRoot string
	logged := captureLog(t, func() {
		repoRoot = convergeTree(t, withFiles(mutualImports(), map[string]string{
			"a/BUILD.bazel": keptDepsBuild,
		}))
	})

	if emitted := readBuildFile(t, repoRoot, "a"); strings.Contains(emitted, "//b") {
		t.Fatalf("the resolved label reached the kept attribute, so this fixture no longer "+
			"emits an acyclic pair of BUILD files; a/BUILD.bazel was:\n%s", emitted)
	}
	if strings.Contains(logged, cycleReport) {
		t.Errorf("reported a cycle Bazel accepts: the deps that would close it are kept and "+
			"never reach a BUILD file; log was %q", logged)
	}
}

// TestImportCycle_KeptRuleIsNotReported is a PIN for the same defect at the
// coarsest granularity: rule.MergeRules returns on a kept rule before it looks
// at an attribute, so nothing the resolver computed for it is emitted.
func TestImportCycle_KeptRuleIsNotReported(t *testing.T) {
	var repoRoot string
	logged := captureLog(t, func() {
		repoRoot = convergeTree(t, withFiles(mutualImports(), map[string]string{
			"a/BUILD.bazel": keptRuleBuild,
		}))
	})

	if emitted := readBuildFile(t, repoRoot, "a"); strings.Contains(emitted, "//b") {
		t.Fatalf("the resolved label reached the kept rule, so this fixture no longer emits "+
			"an acyclic pair of BUILD files; a/BUILD.bazel was:\n%s", emitted)
	}
	if strings.Contains(logged, cycleReport) {
		t.Errorf("reported a cycle Bazel accepts: the rule that would close it is kept and "+
			"takes none of the resolved deps; log was %q", logged)
	}
}

// TestImportCycle_KeptDepsThatNameTheImportDoNotSuppress is a CONTROL: it
// passes both before and after the mixed-shape fix. The held list names the
// same label //b's own source imports, so the edge is both an import and a dep
// Bazel loads and nothing about it is suppressed. Dropping a kept target's
// edges outright would have silenced this one.
func TestImportCycle_KeptDepsThatNameTheImportDoNotSuppress(t *testing.T) {
	var repoRoot string
	logged := captureLog(t, func() {
		repoRoot = convergeTree(t, withFiles(mutualImports(), map[string]string{
			"b/BUILD.bazel": `load("@rules_typescript//ts:defs.bzl", "ts_compile")

ts_compile(
    name = "b",
    srcs = ["thing.ts"],
    visibility = ["//visibility:public"],
    # keep
    deps = ["//a"],
)
`,
		}))
	})

	if emitted := readBuildFile(t, repoRoot, "b"); !strings.Contains(emitted, `"//a"`) {
		t.Fatalf("the kept deps lost its label, so this fixture no longer emits a cycle; "+
			"b/BUILD.bazel was:\n%s", emitted)
	}
	if !strings.Contains(logged, cycleReport) {
		t.Errorf("a cycle the emitted BUILD files do close went unreported; log was %q", logged)
	}
}

// TestImportCycle_HandWrittenCycleWithNoImportBehindItIsSilent is a PIN: an
// edge is an import, so a cycle no import crosses has no edges and is not a
// cycle this reports. The labels that close it are already written where the
// reader is looking, and Bazel's loop of labels names the same BUILD files.
func TestImportCycle_HandWrittenCycleWithNoImportBehindItIsSilent(t *testing.T) {
	logged := captureLog(t, func() {
		convergeTree(t, map[string]string{
			"BUILD.bazel": "",
			"a/BUILD.bazel": `load("@rules_typescript//ts:defs.bzl", "ts_compile")

ts_compile(
    name = "a",
    srcs = ["thing.ts"],
    visibility = ["//visibility:public"],
    # keep
    deps = ["//b"],
)
`,
			"b/BUILD.bazel": `load("@rules_typescript//ts:defs.bzl", "ts_compile")

ts_compile(
    name = "b",
    srcs = ["thing.ts"],
    visibility = ["//visibility:public"],
    # keep
    deps = ["//a"],
)
`,
			"a/thing.ts": "export const a = 1;\n",
			"b/thing.ts": "export const b = 2;\n",
		})
	})

	if strings.Contains(logged, cycleReport) {
		t.Errorf("reported a cycle with no import behind any of its edges, so the report "+
			"describes imports that do not exist; log was %q", logged)
	}
}

// TestImportCycle_ValueLevelKeepStillReportsTheCycle is a CONTROL on the
// narrowest of the three "# keep" granularities: one held dep value is not a
// held attribute. rule.MergeRules skips only a kept rule and a kept assignment;
// a kept value is preserved and the resolved labels still merge in beside it,
// so they do reach the BUILD file and the cycle is Bazel's.
func TestImportCycle_ValueLevelKeepStillReportsTheCycle(t *testing.T) {
	var repoRoot string
	logged := captureLog(t, func() {
		repoRoot = convergeTree(t, withFiles(mutualImports(), map[string]string{
			"vendor/thing.ts": "export const v = 1;\n",
			"a/BUILD.bazel": `load("@rules_typescript//ts:defs.bzl", "ts_compile")

ts_compile(
    name = "a",
    srcs = ["thing.ts"],
    visibility = ["//visibility:public"],
    deps = [
        "//vendor",  # keep
    ],
)
`,
		}))
	})

	if emitted := readBuildFile(t, repoRoot, "a"); !strings.Contains(emitted, `"//b"`) {
		t.Fatalf("a kept dep value held the resolved label out after all, so this fixture no "+
			"longer emits a cycle; a/BUILD.bazel was:\n%s", emitted)
	}
	if !strings.Contains(logged, cycleReport) {
		t.Errorf("a kept dep value suppressed the whole report, but only a kept attribute or "+
			"a kept rule discards what the resolver computed; log was %q", logged)
	}
}

// keptDepsOnB hands b's deps to the author naming //a, the label b's own
// sources do not import.
const keptDepsOnB = `load("@rules_typescript//ts:defs.bzl", "ts_compile")

ts_compile(
    name = "b",
    srcs = ["thing.ts"],
    visibility = ["//visibility:public"],
    # keep
    deps = ["//a"],
)
`

// TestImportCycle_MixedHandWrittenAndImportedEdgeIsSilent is a PIN on the
// mixed shape: //a imports //b, and //b depends back on //a only because a
// hand holds that label in its deps. Bazel does reject the pair, but the
// packages do not import each other and the closing label is written plainly
// in a BUILD file, which is where Bazel's own loop of labels sends the reader.
func TestImportCycle_MixedHandWrittenAndImportedEdgeIsSilent(t *testing.T) {
	var repoRoot string
	logged := captureLog(t, func() {
		repoRoot = convergeTree(t, map[string]string{
			"BUILD.bazel":   "",
			"a/thing.ts":    "import { b } from \"../b/thing\";\nexport const a = b;\n",
			"b/thing.ts":    "export const b = 1;\n",
			"b/BUILD.bazel": keptDepsOnB,
		})
	})

	if emitted := readBuildFile(t, repoRoot, "b"); !strings.Contains(emitted, `"//a"`) {
		t.Fatalf("the kept deps lost its label, so this fixture no longer emits the mixed "+
			"shape; b/BUILD.bazel was:\n%s", emitted)
	}
	if strings.Contains(logged, cycleReport) {
		t.Errorf("reported a cycle half-closed by a hand-written dep, so the report says "+
			"//b imports //a when its sources import nothing; log was %q", logged)
	}
}

// TestImportCycle_TestTargetDepFromOutsideItsSrcsIsSilent is a PIN and a
// documented gap. A ts_test target resolves deps from its package's production
// and doc sources too, because it builds its own node_modules tree -- so it
// carries labels no file in its own srcs imports. A cycle through such a label
// has an edge no source of the target it leaves writes.
func TestImportCycle_TestTargetDepFromOutsideItsSrcsIsSilent(t *testing.T) {
	var repoRoot string
	logged := captureLog(t, func() {
		repoRoot = convergeTree(t, map[string]string{
			"BUILD.bazel":     "",
			"a/thing.ts":      "import { b } from \"../b/thing\";\nexport const a = b;\n",
			"a/thing.test.ts": "export const helper = 1;\n",
			"b/thing.ts":      "import { helper } from \"../a/thing.test\";\nexport const b = helper;\n",
		})
	})

	if emitted := readBuildFile(t, repoRoot, "a"); !strings.Contains(emitted, `"//b"`) {
		t.Fatalf("//a:a_test no longer takes a dep resolved from outside its srcs, so this "+
			"fixture emits no cycle; a/BUILD.bazel was:\n%s", emitted)
	}
	if emitted := readBuildFile(t, repoRoot, "b"); !strings.Contains(emitted, `"//a:a_test"`) {
		t.Fatalf("//b:b no longer depends on the test target, so this fixture emits no "+
			"cycle; b/BUILD.bazel was:\n%s", emitted)
	}
	if strings.Contains(logged, cycleReport) {
		t.Errorf("reported a cycle one edge of which no file in its own target imports; "+
			"log was %q", logged)
	}
}

// TestImportCycle_KeptDepsNarrowTheCycleTheyLeave is a CONTROL: it passes both
// before and after the mixed-shape fix. A held deps list drops one label of
// three, and what is left is a smaller cycle among the same targets -- so
// suppression is decided per edge, not per target, and the report names the
// two packages Bazel will reject rather than the three that import each other.
func TestImportCycle_KeptDepsNarrowTheCycleTheyLeave(t *testing.T) {
	var repoRoot string
	logged := captureLog(t, func() {
		repoRoot = convergeTree(t, withFiles(ringOfThree(), map[string]string{
			"a/thing.ts": "import { b } from \"../b/thing\";\n" +
				"import { c } from \"../c/thing\";\nexport const a = b + c;\n",
			"b/thing.ts": "import { c } from \"../c/thing\";\nexport const b = c;\n",
			"a/BUILD.bazel": `load("@rules_typescript//ts:defs.bzl", "ts_compile")

ts_compile(
    name = "a",
    srcs = ["thing.ts"],
    visibility = ["//visibility:public"],
    # keep
    deps = ["//c"],
)
`,
		}))
	})

	emitted := readBuildFile(t, repoRoot, "a")
	if !strings.Contains(emitted, `"//c"`) || strings.Contains(emitted, `"//b"`) {
		t.Fatalf("the held deps no longer names //c alone, so this fixture no longer "+
			"narrows the cycle; a/BUILD.bazel was:\n%s", emitted)
	}
	if n := strings.Count(logged, cycleReport); n != 1 {
		t.Fatalf("the narrowed cycle was reported %d times, want 1; log was %q", n, logged)
	}
	for _, want := range []string{"//a:a", "//c:c"} {
		if !strings.Contains(logged, want) {
			t.Errorf("the report does not name %q, which the emitted deps do put in a "+
				"cycle; log was %q", want, logged)
		}
	}
	if strings.Contains(logged, "//b:b") {
		t.Errorf("the report names //b:b, which the held deps left out of every cycle "+
			"the BUILD files close; log was %q", logged)
	}
}

// TestImportCycle_AnImportTheHeldDepsLeaveOutStillLeavesACycle is a CONTROL: it
// passes both before and after the report is reduced. //a imports //b and //c
// but holds deps naming //b alone, so //a -> //c is an import Bazel never sees
// -- and the ring the remaining three edges make is still there. Dropping an
// edge must narrow the report, not silence it.
func TestImportCycle_AnImportTheHeldDepsLeaveOutStillLeavesACycle(t *testing.T) {
	var repoRoot string
	logged := captureLog(t, func() {
		repoRoot = convergeTree(t, withFiles(ringOfThree(), map[string]string{
			"a/thing.ts": "import { b } from \"../b/thing\";\n" +
				"import { c } from \"../c/thing\";\nexport const a = b + c;\n",
			"b/thing.ts": "import { c } from \"../c/thing\";\nexport const b = c;\n",
			"a/BUILD.bazel": `load("@rules_typescript//ts:defs.bzl", "ts_compile")

ts_compile(
    name = "a",
    srcs = ["thing.ts"],
    visibility = ["//visibility:public"],
    # keep
    deps = ["//b"],
)
`,
		}))
	})

	emitted := readBuildFile(t, repoRoot, "a")
	if !strings.Contains(emitted, `"//b"`) || strings.Contains(emitted, `"//c"`) {
		t.Fatalf("the held deps no longer names //b alone, so //a -> //c is no longer an "+
			"import the emitted files leave out; a/BUILD.bazel was:\n%s", emitted)
	}
	if n := strings.Count(logged, cycleReport); n != 1 {
		t.Fatalf("the cycle the emitted deps do close was reported %d times, want 1; "+
			"log was %q", n, logged)
	}
	for _, want := range []string{"3 packages", "//a:a", "//b:b", "//c:c"} {
		if !strings.Contains(logged, want) {
			t.Errorf("the report does not name %q; log was %q", want, logged)
		}
	}
}
