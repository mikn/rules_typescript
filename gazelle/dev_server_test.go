package typescript

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bazelbuild/bazel-gazelle/rule"
)

// The entry name and the index.html that used to make a package an application
// are both here, so a ts_dev_server in the output is one the generator wrote.
func appPackageFiles(build string) map[string]string {
	files := map[string]string{
		"package.json":       convergePlainPkg,
		"tsconfig.json":      `{"compilerOptions":{"strict":true}}` + "\n",
		"src/app/main.tsx":   "export const main = 1;\n",
		"src/app/index.html": "<!doctype html>\n",
	}
	if build != "" {
		files["src/app/BUILD.bazel"] = build
	}
	return files
}

const handWrittenDevServerRule = `ts_dev_server(
    name = "dev",
    entry_point = ":app",
    port = 5173,
)
`

const handWrittenDevServerBuild = `load("@rules_typescript//ts:defs.bzl", "ts_dev_server")

` + handWrittenDevServerRule

func TestDevServer_NotWrittenForAnEntryName(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, appPackageFiles(""))
	captureLog(t, func() { convergeGazelle(t, root) })

	text := buildFileText(t, root, "src/app")
	if strings.Contains(text, "ts_dev_server") {
		t.Fatalf("Gazelle wrote a ts_dev_server into src/app for main.tsx and index.html:\n%s", indent(text))
	}
	if !strings.Contains(text, `name = "app"`) {
		t.Fatalf("src/app got no ts_compile:\n%s", indent(text))
	}
}

// Gazelle knows no ts_dev_server kind, so the merger has no candidate for the
// rule and FixLoads keeps a symbol it does not know: both stay as written.
func TestDevServer_HandWrittenSurvivesUntouched(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, appPackageFiles(handWrittenDevServerBuild))
	captureLog(t, func() { convergeGazelle(t, root) })
	first := buildFileText(t, root, "src/app")
	captureLog(t, func() { convergeGazelle(t, root) })
	second := buildFileText(t, root, "src/app")

	if first != second {
		t.Fatalf("src/app/BUILD.bazel changed between two runs:\n%s", lineDiff(first, second))
	}
	if !strings.Contains(second, handWrittenDevServerRule) {
		t.Fatalf("the hand-written ts_dev_server did not come through verbatim:\n%s", indent(second))
	}
	if !strings.Contains(second, `name = "app"`) {
		t.Fatalf("src/app got no ts_compile beside the hand-written rule:\n%s", indent(second))
	}

	f, err := rule.LoadFile(filepath.Join(root, "src/app/BUILD.bazel"), "src/app")
	if err != nil {
		t.Fatal(err)
	}
	var symbols []string
	for _, l := range f.Loads {
		if l.Name() == "@rules_typescript//ts:defs.bzl" {
			symbols = append(symbols, l.Symbols()...)
		}
	}
	if !slices.Contains(symbols, "ts_dev_server") {
		t.Fatalf("the load lost the ts_dev_server symbol; the file loads %v:\n%s", symbols, indent(second))
	}
	if !slices.Contains(symbols, "ts_compile") {
		t.Fatalf("the load gained no ts_compile symbol; the file loads %v:\n%s", symbols, indent(second))
	}
}
