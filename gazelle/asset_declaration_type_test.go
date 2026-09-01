package typescript

// # gazelle:ts_asset_declaration_type, end to end through a converge run: the
// directive has to reach the asset_library targets a repo already has, not only
// the ones a run writes for the first time. An existing target's srcs are
// claimed, so the generator emits nothing for it at all.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const svgrDirective = `# gazelle:ts_asset_declaration_type .svg ` +
	`import("react").FC<import("react").SVGProps<SVGSVGElement>>` + "\n"

const svgrEntry = `".svg": "import(\"react\").FC<import(\"react\").SVGProps<SVGSVGElement>>"`

func assetWorkspace(t *testing.T, extra map[string]string) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"package.json":  `{"name":"w","dependencies":{}}` + "\n",
		"tsconfig.json": `{"compilerOptions":{"strict":true}}` + "\n",
		"src/index.ts":  "import logo from \"./logo.svg\";\nexport const a = logo;\n",
		"src/logo.svg":  "<svg/>\n",
		"src/note.md":   "hi\n",
	}
	for rel, body := range extra {
		files[rel] = body
	}
	writeWorkspace(t, root, files)
	return root
}

func buildFile(t *testing.T, root, pkg string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(pkg), "BUILD.bazel"))
	if err != nil {
		t.Fatalf("reading %s/BUILD.bazel: %v", pkg, err)
	}
	return string(body)
}

func rewriteBuildFile(t *testing.T, root, pkg, old, new string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(pkg), "BUILD.bazel")
	body := buildFile(t, root, pkg)
	if !strings.Contains(body, old) {
		t.Fatalf("%s/BUILD.bazel does not contain %q:\n%s", pkg, old, body)
	}
	if err := os.WriteFile(full, []byte(strings.Replace(body, old, new, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

const generatedSvgTarget = "    srcs = [\"logo.svg\"],"

func requireContains(t *testing.T, body, want, why string) {
	t.Helper()
	if !strings.Contains(body, want) {
		t.Fatalf("%s\nwant %q in:\n%s", why, want, body)
	}
}

func requireOmits(t *testing.T, body, unwanted, why string) {
	t.Helper()
	if strings.Contains(body, unwanted) {
		t.Fatalf("%s\ndid not want %q in:\n%s", why, unwanted, body)
	}
}

// The plain case: a target the run writes for the first time carries the type.
func TestAssetDeclarationType_WritesTheTargetsItGenerates(t *testing.T) {
	root := assetWorkspace(t, map[string]string{"BUILD.bazel": svgrDirective})
	convergeGazelle(t, root)

	src := buildFile(t, root, "src")
	requireContains(t, src, svgrEntry, "the directive did not reach the generated asset_library")
	requireOmits(t, src[strings.Index(src, `name = "note_md"`):], "declaration_type",
		"the .md target took an entry no directive named for it")
}

// A directive value is split on spaces before anything else, which is what made
// a ts_codegen glob unwritable in #83. Here only the first space separates.
func TestAssetDeclarationType_ExpressionKeepsItsSpaces(t *testing.T) {
	root := assetWorkspace(t, map[string]string{
		"BUILD.bazel": "# gazelle:ts_asset_declaration_type .md { default: string; toc: string[] }\n",
	})
	convergeGazelle(t, root)

	requireContains(t, buildFile(t, root, "src"),
		`".md": "{ default: string; toc: string[] }"`,
		"the type expression was split on its spaces")
}

func TestAssetDeclarationType_InheritsIntoSubdirectories(t *testing.T) {
	root := assetWorkspace(t, map[string]string{
		"BUILD.bazel":       svgrDirective,
		"src/deep/x.svg":    "<svg/>\n",
		"src/deep/index.ts": "import x from \"./x.svg\";\nexport const b = x;\n",
	})
	convergeGazelle(t, root)

	requireContains(t, buildFile(t, root, "src/deep"), svgrEntry,
		"a subdirectory two levels below the directive did not inherit it")
}

func TestAssetDeclarationType_ASubdirectoryOverridesIt(t *testing.T) {
	root := assetWorkspace(t, map[string]string{
		"BUILD.bazel":          svgrDirective,
		"src/deep/BUILD.bazel": "# gazelle:ts_asset_declaration_type .svg OwnType\n",
		"src/deep/x.svg":       "<svg/>\n",
		"src/deep/index.ts":    "import x from \"./x.svg\";\nexport const b = x;\n",
	})
	convergeGazelle(t, root)

	deep := buildFile(t, root, "src/deep")
	requireContains(t, deep, `".svg": "OwnType"`, "the nested directive did not win")
	requireOmits(t, deep, "import(", "the inherited expression survived the override")
}

// The extension on its own: the subtree declares nothing for it, so the entry
// goes -- including one an earlier run wrote from the inherited directive.
func TestAssetDeclarationType_ASubdirectoryClearsIt(t *testing.T) {
	root := assetWorkspace(t, map[string]string{
		"BUILD.bazel":      svgrDirective,
		"src/raw/y.svg":    "<svg/>\n",
		"src/raw/index.ts": "import y from \"./y.svg\";\nexport const c = y;\n",
	})
	convergeGazelle(t, root)
	requireContains(t, buildFile(t, root, "src/raw"), svgrEntry,
		"the inherited directive did not reach src/raw on the first run")

	writeWorkspace(t, root, map[string]string{
		"src/raw/BUILD.bazel": "# gazelle:ts_asset_declaration_type .svg\n" +
			buildFile(t, root, "src/raw"),
	})
	convergeGazelle(t, root)

	raw := buildFile(t, root, "src/raw")
	requireOmits(t, raw, "declaration_type = ",
		"the bare directive left the entry the inherited one had written")
	requireContains(t, raw, `name = "y_svg"`, "the target itself was lost with the entry")
}

// The measured gap this directive exists to close: an asset_library the BUILD
// file already holds has its srcs claimed, so the generator writes nothing for
// it, and #92's attribute had to be hand-edited on every one of them.
func TestAssetDeclarationType_ReachesTargetsAlreadyWritten(t *testing.T) {
	root := assetWorkspace(t, nil)
	convergeGazelle(t, root)
	requireOmits(t, buildFile(t, root, "src"), "declaration_type",
		"the first run wrote an entry with no directive in the tree")

	writeWorkspace(t, root, map[string]string{"BUILD.bazel": svgrDirective})
	convergeGazelle(t, root)
	requireContains(t, buildFile(t, root, "src"), svgrEntry,
		"the directive did not reach the asset_library the previous run wrote")

	second := buildFile(t, root, "src")
	convergeGazelle(t, root)
	if third := buildFile(t, root, "src"); third != second {
		t.Fatalf("a further run rewrote the file:\n--- second ---\n%s--- third ---\n%s", second, third)
	}
}

// The directive replaces a hand-written value it disagrees with, the way every
// attribute Gazelle owns is replaced.
func TestAssetDeclarationType_ReplacesAHandWrittenValue(t *testing.T) {
	root := assetWorkspace(t, nil)
	convergeGazelle(t, root)
	rewriteBuildFile(t, root, "src", generatedSvgTarget,
		generatedSvgTarget+"\n    declaration_type = {\".svg\": \"HandWritten\"},")

	writeWorkspace(t, root, map[string]string{"BUILD.bazel": svgrDirective})
	convergeGazelle(t, root)

	src := buildFile(t, root, "src")
	requireContains(t, src, svgrEntry, "the directive did not replace the hand-written value")
	requireOmits(t, src, "HandWritten", "the hand-written value survived the directive")
}

func TestAssetDeclarationType_KeepHoldsTheEntry(t *testing.T) {
	root := assetWorkspace(t, nil)
	convergeGazelle(t, root)
	rewriteBuildFile(t, root, "src", generatedSvgTarget,
		generatedSvgTarget+"\n    declaration_type = {\n        # keep\n        \".svg\": \"HandWritten\",\n    },")

	writeWorkspace(t, root, map[string]string{"BUILD.bazel": svgrDirective})
	convergeGazelle(t, root)

	src := buildFile(t, root, "src")
	requireContains(t, src, `".svg": "HandWritten"`, "\"# keep\" on the entry did not hold it")
	requireOmits(t, src, "import(", "the directive wrote a second entry beside the kept one")
}

func TestAssetDeclarationType_KeepHoldsTheWholeAttribute(t *testing.T) {
	root := assetWorkspace(t, nil)
	convergeGazelle(t, root)
	rewriteBuildFile(t, root, "src", generatedSvgTarget,
		generatedSvgTarget+"\n    # keep\n    declaration_type = {\".svg\": \"HandWritten\"},")

	writeWorkspace(t, root, map[string]string{"BUILD.bazel": svgrDirective})
	convergeGazelle(t, root)

	requireContains(t, buildFile(t, root, "src"), `declaration_type = {".svg": "HandWritten"}`,
		"\"# keep\" above the attribute did not hold it")
}

// An extension no directive names is not Gazelle's, whatever else the dict says.
func TestAssetDeclarationType_LeavesAnUnnamedExtensionAlone(t *testing.T) {
	root := assetWorkspace(t, map[string]string{
		"src/hero.png": "\x89PNG\n",
	})
	convergeGazelle(t, root)
	rewriteBuildFile(t, root, "src", "    srcs = [\"hero.png\"],",
		"    srcs = [\"hero.png\"],\n    declaration_type = {\".png\": \"HandWritten\"},")

	writeWorkspace(t, root, map[string]string{"BUILD.bazel": svgrDirective})
	convergeGazelle(t, root)

	src := buildFile(t, root, "src")
	requireContains(t, src, `".png": "HandWritten"`, "the .png entry no directive named was rewritten")
	requireContains(t, src, svgrEntry, "the .svg entry the directive names was not written")
}

func TestAssetDeclarationType_NoDirectiveTouchesNothing(t *testing.T) {
	root := assetWorkspace(t, nil)
	convergeGazelle(t, root)
	rewriteBuildFile(t, root, "src", generatedSvgTarget,
		generatedSvgTarget+"\n    declaration_type = {\".svg\": \"HandWritten\"},")
	before := buildFile(t, root, "src")

	convergeGazelle(t, root)

	if after := buildFile(t, root, "src"); after != before {
		t.Fatalf("a run with no directive rewrote the file:\n--- before ---\n%s--- after ---\n%s", before, after)
	}
}

func TestAssetDeclarationType_RefusesAnExtensionAssetLibraryDoesNotTake(t *testing.T) {
	root := assetWorkspace(t, map[string]string{
		"BUILD.bazel": "# gazelle:ts_asset_declaration_type svg Foo\n",
	})
	logged := captureLog(t, func() { convergeGazelle(t, root) })

	requireOmits(t, buildFile(t, root, "src"), "declaration_type",
		"an extension asset_library does not take was written anyway")
	for _, want := range []string{`extension "svg"`, "write the leading dot", ".jsonc"} {
		if !strings.Contains(logged, want) {
			t.Fatalf("the refusal does not say %q:\n%s", want, logged)
		}
	}
}

// declaration_type is keyed by extension because one target can hold several,
// which a hand-written asset_library does. The merged dict is ordered by key,
// so the entry the directive owns and the entry a "# keep" holds do not swap
// places from one run to the next.
func TestAssetDeclarationType_MergesIntoAMultiExtensionTarget(t *testing.T) {
	root := assetWorkspace(t, map[string]string{
		"BUILD.bazel":  svgrDirective,
		"src/hero.png": "\x89PNG\n",
		"src/BUILD.bazel": `load("@rules_typescript//ts:defs.bzl", "asset_library")

asset_library(
    name = "icons",
    srcs = [
        "hero.png",
        "logo.svg",
    ],
    declaration_type = {
        # keep
        ".png": "HandWritten",
    },
    visibility = ["//visibility:public"],
)
`,
	})
	convergeGazelle(t, root)

	src := buildFile(t, root, "src")
	requireContains(t, src, svgrEntry, "the directive did not reach the hand-written target")
	requireContains(t, src, `".png": "HandWritten"`, "\"# keep\" did not hold the .png entry")
	if png, svg := strings.Index(src, `".png"`), strings.Index(src, `".svg"`); png > svg {
		t.Fatalf("the merged dict is not ordered by key:\n%s", src)
	}

	second := buildFile(t, root, "src")
	convergeGazelle(t, root)
	if third := buildFile(t, root, "src"); third != second {
		t.Fatalf("a further run rewrote the file:\n--- second ---\n%s--- third ---\n%s", second, third)
	}
}
