package typescript

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bazelbuild/bazel-gazelle/rule"
)

func appPackageFiles(build string) map[string]string {
	files := map[string]string{
		"package.json": convergePlainPkg,
		"src/app/tsconfig.json": `{"compilerOptions":{"jsx":"react-jsx"},` +
			`"include":["*.tsx"]}` + "\n",
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
    server = "@rules_typescript//vite:dev_server",
)
`

const handWrittenDevServerBuild = `load("@rules_typescript//ts:defs.bzl", "ts_dev_server")

` + handWrittenDevServerRule

func TestDevServer_WrittenForAnEntryName(t *testing.T) {
	requireTsgo(t)
	root := t.TempDir()
	writeWorkspace(t, root, appPackageFiles(""))
	captureLog(t, func() { convergeGazelle(t, root) })

	text := buildFileText(t, root, "src/app")
	if !strings.Contains(text, "ts_dev_server(") || !strings.Contains(text, `entry_point = ":app"`) {
		t.Fatalf("Gazelle wrote no dev server for the app:\n%s", indent(text))
	}
	if strings.Contains(text, "server =") {
		t.Fatalf("the generated target overrides the default server:\n%s", indent(text))
	}
	captureLog(t, func() { convergeGazelle(t, root) })
	if second := buildFileText(t, root, "src/app"); text != second {
		t.Fatalf("the generated dev server changed on a second run:\n%s", lineDiff(text, second))
	}
	if !strings.Contains(text, `name = "app"`) {
		t.Fatalf("src/app got no ts_compile:\n%s", indent(text))
	}
}

func TestDevServer_KeepsServerAndPort(t *testing.T) {
	requireTsgo(t)
	root := t.TempDir()
	writeWorkspace(t, root, appPackageFiles(handWrittenDevServerBuild))
	captureLog(t, func() { convergeGazelle(t, root) })
	first := buildFileText(t, root, "src/app")
	captureLog(t, func() { convergeGazelle(t, root) })
	second := buildFileText(t, root, "src/app")

	if first != second {
		t.Fatalf("src/app/BUILD.bazel changed between two runs:\n%s", lineDiff(first, second))
	}
	if !strings.Contains(second, `server = "@rules_typescript//vite:dev_server"`) || !strings.Contains(second, "port = 5173") {
		t.Fatalf("the hand-written server or port changed:\n%s", indent(second))
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

func TestAppPackageSignals(t *testing.T) {
	for _, tc := range []struct {
		name    string
		sources []string
		html    bool
		want    bool
	}{
		{"entry", []string{"src/main.ts"}, false, true},
		{"react", []string{"src/App.tsx"}, false, true},
		{"html", []string{"src/index.ts"}, true, true},
		{"library", []string{"src/index.ts"}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.html {
				writeWorkspace(t, dir, map[string]string{"index.html": "<!doctype html>"})
			}
			if got := appPackage(dir, tc.sources); got != tc.want {
				t.Fatalf("appPackage = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDevServer_WithdrawnWithApplication(t *testing.T) {
	requireTsgo(t)
	for _, removed := range []string{"entry", "tsconfig.json", "main.tsx"} {
		t.Run(removed, func(t *testing.T) {
			root := t.TempDir()
			writeWorkspace(t, root, appPackageFiles(""))
			captureLog(t, func() { convergeGazelle(t, root) })
			if removed == "entry" {
				if err := os.Rename(filepath.Join(root, "src/app/main.tsx"), filepath.Join(root, "src/app/library.tsx")); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(filepath.Join(root, "src/app/index.html")); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Remove(filepath.Join(root, "src/app", removed)); err != nil {
				t.Fatal(err)
			}
			captureLog(t, func() { convergeGazelle(t, root) })
			text := buildFileText(t, root, "src/app")
			if strings.Contains(text, "ts_dev_server(") {
				t.Fatalf("removing %s left a dev server:\n%s", removed, text)
			}
			if removed == "entry" && !strings.Contains(text, "ts_compile(") {
				t.Fatalf("removing the entry removed the library target:\n%s", text)
			}
		})
	}
}

func TestDevServer_NameCollisionKeepsExistingRule(t *testing.T) {
	requireTsgo(t)
	root := t.TempDir()
	writeWorkspace(t, root, appPackageFiles(`filegroup(name = "dev", srcs = ["index.html"])`))
	captureLog(t, func() { convergeGazelle(t, root) })
	text := buildFileText(t, root, "src/app")
	if strings.Contains(text, "ts_dev_server(") || !strings.Contains(text, "filegroup(") {
		t.Fatalf("the dev target replaced an existing rule:\n%s", text)
	}
}
