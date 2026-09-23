package typescript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmission_NodeBoundaryEmitsNewDependenciesInOneRun(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "new", true: "existing"}[existing], func(t *testing.T) {
			requireTsgo(t)
			root := t.TempDir()
			writeWorkspace(t, root, map[string]string{
				"package.json": convergePlainPkg,
				"BUILD.bazel": `load("@rules_typescript//ts:defs.bzl", "ts_binary")
ts_binary(name = "run", entry_point = "//app")
`,
				"app/tsconfig.json":    `{"compilerOptions":{"module":"esnext","moduleResolution":"bundler"},"include":["*.ts"]}`,
				"app/index.ts":         `import {value} from "../lib/index"; export const result = value;`,
				"lib/tsconfig.json":    `{"compilerOptions":{"module":"esnext","moduleResolution":"bundler"},"include":["*.ts"]}`,
				"lib/index.ts":         `export const value = 1;`,
				"native/tsconfig.json": `{"include":["*.ts"]}`,
				"native/index.ts":      `export const untouched = 1;`,
			})
			if existing {
				for _, pkg := range []string{"app", "lib"} {
					writeWorkspace(t, root, map[string]string{pkg + "/BUILD.bazel": "load(\"@rules_typescript//ts:defs.bzl\", \"ts_compile\")\nts_compile(name = \"" + pkg + "\", srcs = [\"index.ts\"])\n"})
				}
			}
			captureLog(t, func() { convergeGazelle(t, root) })
			first := map[string]string{}
			for _, pkg := range []string{"app", "lib", "native"} {
				first[pkg] = buildFileText(t, root, pkg)
				want := pkg != "native"
				if strings.Contains(first[pkg], "emit = True") != want {
					t.Fatalf("%s emission boundary wrong:\n%s", pkg, first[pkg])
				}
			}
			logs := captureLog(t, func() { convergeGazelle(t, root) })
			if strings.Contains(logs, "declares emit as an expression") {
				t.Fatalf("generated boolean reported as unmergeable: %s", logs)
			}
			for pkg, body := range first {
				if next := buildFileText(t, root, pkg); next != body {
					t.Fatalf("%s needs second run:\n%s", pkg, lineDiff(body, next))
				}
			}

			writeWorkspace(t, root, map[string]string{"BUILD.bazel": ""})
			captureLog(t, func() { convergeGazelle(t, root) })
			for _, pkg := range []string{"app", "lib"} {
				if text := buildFileText(t, root, pkg); strings.Contains(text, "emit = True") {
					t.Fatalf("%s retained emission after its consumer was removed:\n%s", pkg, text)
				}
			}
		})
	}
}

func TestEmission_GeneratedWorkspaceLinkReachesSourceMember(t *testing.T) {
	requireTsgo(t)
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"package.json":               convergePlainPkg,
		"pnpm-lock.yaml":             convergeMemberLock,
		"pnpm-workspace.yaml":        "packages:\n  - packages/*\n",
		"node_modules/.modules.yaml": "hoistPattern:\n  - '*'\n",
		"BUILD.bazel": `load("@rules_typescript//ts:defs.bzl", "ts_binary")
ts_binary(name="run",entry_point="//packages/app")`,
		"packages/core/package.json":  `{"name":"@w/core","version":"1.0.0","exports":"./index.ts"}`,
		"packages/core/tsconfig.json": `{"include":["*.ts"]}`,
		"packages/core/index.ts":      `export const value = 1;`,
		"packages/app/tsconfig.json":  `{"compilerOptions":{"module":"esnext","moduleResolution":"bundler"},"include":["*.ts"]}`,
		"packages/app/index.ts":       `import { value } from "@w/core"; export const result = value;`,
	})
	if err := os.MkdirAll(filepath.Join(root, "node_modules/@w"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "packages/core"), filepath.Join(root, "node_modules/@w/core")); err != nil {
		t.Fatal(err)
	}
	captureLog(t, func() { convergeGazelle(t, root) })
	for _, pkg := range []string{"packages/app", "packages/core"} {
		text := buildFileText(t, root, pkg)
		if !strings.Contains(text, "emit = True") {
			t.Fatalf("workspace boundary %s not emitted:\n%s", pkg, text)
		}
	}
}

func TestEmission_NodeRunnerAliasOptsInWithoutASecondRun(t *testing.T) {
	requireTsgo(t)
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"package.json":  convergePlainPkg,
		"tsconfig.json": `{"include":["*.ts"]}`,
		"check.test.ts": `export const value = 1;`,
		"BUILD.bazel": `load("@rules_typescript//ts:defs.bzl", "ts_test")
alias(name="node_runner",actual="@rules_typescript//ts/runners:node_test")
ts_test(name="root_test",srcs=["check.test.ts"],runner=":node_runner")`,
	})
	captureLog(t, func() { convergeGazelle(t, root) })
	text := buildFileText(t, root, "")
	if !strings.Contains(text, "emit = True") {
		t.Fatalf("Node alias left sources untransformed:\n%s", text)
	}
}

func TestEmission_ManifestOutputsFollowNestedProgramOwnership(t *testing.T) {
	requireTsgo(t)
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"package.json":                  `{"main":"./src/index.js","exports":{"./types":"./types/index.d.ts","./features/*":"./features/*.js","./js":{"types":"./js/index.d.ts"},"./esm":{"types":"./esm/index.d.mts"}}}`,
		"src/tsconfig.json":             `{"include":["*.ts"]}`,
		"src/index.ts":                  `export const value = 1;`,
		"types/tsconfig.json":           `{"include":["*.ts"]}`,
		"types/index.ts":                `export type Value = number;`,
		"features/tsconfig.json":        `{"include":["*.ts"]}`,
		"js/tsconfig.json":              `{"compilerOptions":{"allowJs":true},"include":["*.js"]}`,
		"js/index.js":                   `export const value = 1;`,
		"esm/tsconfig.json":             `{"compilerOptions":{"allowJs":true},"include":["*.mjs"]}`,
		"esm/index.mjs":                 `export const value = 1;`,
		"features/nested/tsconfig.json": `{"include":["*.ts"]}`,
		"features/nested/index.ts":      `export const nested = 1;`,
		"features/feature.ts":           `export const feature = 1;`,
		"native/tsconfig.json":          `{"include":["*.ts"]}`,
		"native/index.ts":               `export const untouched = 1;`,
	})
	captureLog(t, func() { convergeGazelle(t, root) })
	for _, pkg := range []string{"src", "types", "features", "features/nested", "js", "esm", "native"} {
		body := buildFileText(t, root, pkg)
		if strings.Contains(body, "emit = True") != (pkg != "native") {
			t.Fatalf("%s manifest output ownership wrong:\n%s", pkg, body)
		}
	}
}

func TestEmission_ExportPatternsPreserveNestedAndRepeatedSubpaths(t *testing.T) {
	for _, tc := range []struct {
		pattern, file string
		want          bool
	}{
		{"features/*.js", "features/nested/index.js", true},
		{"features/*/copy/*.js", "features/nested/index/copy/nested/index.js", true},
		{"features/*/copy/*.js", "features/a/copy/b.js", false},
		{"features/*.js", "x.js", false},
	} {
		if got := matchesExportOutput(tc.pattern, tc.file); got != tc.want {
			t.Fatalf("%s matches %s = %v, want %v", tc.pattern, tc.file, got, tc.want)
		}
	}
}

func TestEmission_WorkersPoolDoesNotForceSourceDependenciesToEmit(t *testing.T) {
	requireTsgo(t)
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"package.json":         convergePlainPkg,
		"worker/tsconfig.json": `{"compilerOptions":{"module":"esnext","moduleResolution":"bundler"},"include":["*.ts"]}`,
		"worker/check.test.ts": `import {value} from "../lib/index"; export const result = value;`,
		"worker/wrangler.json": `{"main":"../lib/index.ts"}`,
		"worker/BUILD.bazel": `load("@rules_typescript//ts:defs.bzl", "ts_test")
ts_test(
    name = "worker_test",
    srcs = ["check.test.ts"],
    wrangler_config = "wrangler.json", # keep
)
`,
		"lib/tsconfig.json": `{"include":["*.ts"]}`,
		"lib/index.ts":      `export const value = 1;`,
	})
	captureLog(t, func() { convergeGazelle(t, root) })
	for _, pkg := range []string{"worker", "lib"} {
		body := buildFileText(t, root, pkg)
		if strings.Contains(body, "emit = True") {
			t.Fatalf("%s Workers pool forced source emission:\n%s", pkg, body)
		}
	}
}
