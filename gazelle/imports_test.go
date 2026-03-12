package typescript

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// ---- extractImports tests --------------------------------------------------

func TestExtractImports_StaticImports(t *testing.T) {
	src := `
import { foo } from "./foo";
import type { Bar } from "./bar";
import * as ns from "./ns";
import defaultExport from "./module";
import defaultExport2, { namedExport } from "./combined";
import "./side-effect";
`
	got := extractFromSource(stripComments(src))
	want := []string{"./bar", "./combined", "./foo", "./module", "./ns", "./side-effect"}
	assertStringSliceEqual(t, "static imports", got, want)
}

func TestExtractImports_DynamicImports(t *testing.T) {
	src := `
const page = import("./page");
const lazy = import('./lazy-component');
const req = require("./required");
`
	got := extractFromSource(stripComments(src))
	want := []string{"./lazy-component", "./page", "./required"}
	assertStringSliceEqual(t, "dynamic imports", got, want)
}

func TestExtractImports_TemplateStringDynamicImport_IsSkipped(t *testing.T) {
	// Template literal dynamic imports like import(`./pages/${name}`) must be
	// silently skipped, not produce an error or a spurious match.
	src := "const m = import(`./pages/${name}`);\n"
	got := extractFromSource(stripComments(src))
	if len(got) != 0 {
		t.Errorf("expected no imports from template literal dynamic import, got: %v", got)
	}
}

func TestExtractImports_ReexportBareStar(t *testing.T) {
	// export * from "./utils" — the bare-star re-export form previously missed.
	src := `
export * from "./utils";
export * from "./helpers";
`
	got := extractFromSource(stripComments(src))
	want := []string{"./helpers", "./utils"}
	assertStringSliceEqual(t, "bare-star re-exports", got, want)
}

func TestExtractImports_ReexportNamespaceStar(t *testing.T) {
	// export * as ns from "./ns"
	src := `export * as ns from "./ns";`
	got := extractFromSource(stripComments(src))
	want := []string{"./ns"}
	assertStringSliceEqual(t, "namespace re-export", got, want)
}

func TestExtractImports_ReexportNamedExports(t *testing.T) {
	// export { foo, bar } from "./bar"
	src := `
export { foo, bar } from "./bar";
export type { MyType } from "./types";
`
	got := extractFromSource(stripComments(src))
	want := []string{"./bar", "./types"}
	assertStringSliceEqual(t, "named re-exports", got, want)
}

func TestExtractImports_BarrelFile(t *testing.T) {
	// Barrel files (index.ts) typically re-export everything from sub-modules.
	src := `
export * from "./utils";
export * from "./helpers";
export { Button } from "./components/Button";
export type { ButtonProps } from "./components/Button";
`
	got := extractFromSource(stripComments(src))
	want := []string{"./components/Button", "./helpers", "./utils"}
	assertStringSliceEqual(t, "barrel file imports", got, want)
}

func TestExtractImports_DeduplicatesSpecifiers(t *testing.T) {
	// The same specifier appearing multiple times should appear once in output.
	src := `
import { foo } from "./utils";
export { bar } from "./utils";
`
	got := extractFromSource(stripComments(src))
	want := []string{"./utils"}
	assertStringSliceEqual(t, "deduplicated imports", got, want)
}

func TestExtractImports_NpmPackages(t *testing.T) {
	src := `
import React from "react";
import { useState } from "react";
import type { FC } from "react";
import { something } from "@tanstack/router";
`
	got := extractFromSource(stripComments(src))
	want := []string{"@tanstack/router", "react"}
	assertStringSliceEqual(t, "npm imports", got, want)
}

func TestExtractImports_CommentsAreStripped(t *testing.T) {
	src := `
// import { commented } from "./should-not-appear";
/* import { blockCommented } from "./should-not-appear-either"; */
import { real } from "./real";
`
	got := extractFromSource(stripComments(src))
	want := []string{"./real"}
	assertStringSliceEqual(t, "comment stripping", got, want)
}

func TestExtractImports_FromFile(t *testing.T) {
	// Write a temp file and verify the file-based extraction path.
	dir := t.TempDir()
	f := filepath.Join(dir, "test.ts")
	content := `
import { foo } from "./foo";
export * from "./bar";
const lazy = import("./lazy");
`
	if err := os.WriteFile(f, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := extractImports(f)
	if err != nil {
		t.Fatalf("extractImports error: %v", err)
	}
	want := []string{"./bar", "./foo", "./lazy"}
	assertStringSliceEqual(t, "file extraction", got, want)
}

// ---- generate.go classification tests -------------------------------------

func TestIsGeneratedFile_BuiltinPatterns(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"routeTree.gen.ts", true},
		{"foo.gen.tsx", true},
		{"schema.generated.ts", true},
		{"types.generated.tsx", true},
		{"api.auto.ts", true},
		{"helpers.auto.tsx", true},
		// Non-generated files:
		{"index.ts", false},
		{"utils.ts", false},
		{"button.tsx", false},
		{"component.test.ts", false},
		// Tricky: "generator.ts" — "generator" does not end with ".gen"
		{"generator.ts", false},
	}
	for _, tc := range cases {
		got := isGeneratedFile(tc.name)
		if got != tc.want {
			t.Errorf("isGeneratedFile(%q): got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestIsConfiguredExclude(t *testing.T) {
	patterns := []string{"*.generated.ts", "*.auto.ts", "schema-*.ts"}
	cases := []struct {
		name string
		want bool
	}{
		{"foo.generated.ts", true},
		{"bar.auto.ts", true},
		{"schema-v2.ts", true},
		{"foo.ts", false},
		{"auto.ts", false}, // doesn't end with .auto.ts
	}
	for _, tc := range cases {
		got := isConfiguredExclude(tc.name, patterns)
		if got != tc.want {
			t.Errorf("isConfiguredExclude(%q): got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestIsExcludedDir_BuiltinDirs(t *testing.T) {
	cases := []struct {
		dir  string
		want bool
	}{
		{".next", true},
		{".nuxt", true},
		{".svelte-kit", true},
		{"dist", true},
		{"build", true},
		{"node_modules", true},
		// Not excluded:
		{"src", false},
		{"lib", false},
		{"components", false},
		{"app", false},
	}
	for _, tc := range cases {
		got := isExcludedDir(tc.dir, nil)
		if got != tc.want {
			t.Errorf("isExcludedDir(%q, nil): got %v, want %v", tc.dir, got, tc.want)
		}
	}
}

func TestIsExcludedDir_ConfiguredDirs(t *testing.T) {
	additional := []string{"coverage", "storybook-static"}
	if !isExcludedDir("coverage", additional) {
		t.Error("expected coverage to be excluded with additional dirs")
	}
	if !isExcludedDir("storybook-static", additional) {
		t.Error("expected storybook-static to be excluded with additional dirs")
	}
	if isExcludedDir("src", additional) {
		t.Error("expected src to NOT be excluded")
	}
}

// ---- resolve.go helpers tests ----------------------------------------------

func TestIsNodeBuiltin(t *testing.T) {
	cases := []struct {
		imp  string
		want bool
	}{
		{"node:fs", true},
		{"node:path", true},
		{"node:crypto", true},
		{"fs", true},
		{"path", true},
		{"os", true},
		{"crypto", true},
		{"events", true},
		{"stream", true},
		// Not built-ins:
		{"react", false},
		{"./local", false},
		{"@types/node", false},
		{"filesystem", false}, // not a built-in (starts with fs but isn't "fs")
	}
	for _, tc := range cases {
		got := isNodeBuiltin(tc.imp)
		if got != tc.want {
			t.Errorf("isNodeBuiltin(%q): got %v, want %v", tc.imp, got, tc.want)
		}
	}
}

// ---- CSS import tests -------------------------------------------------------

func TestExtractImports_CSSImports(t *testing.T) {
	// CSS side-effect imports should be extracted just like TypeScript imports.
	// Gazelle needs them to generate css_library deps.
	src := `
import "./button.css";
import styles from "./theme.css";
import { foo } from "./utils";
`
	got := extractFromSource(stripComments(src))
	want := []string{"./button.css", "./theme.css", "./utils"}
	assertStringSliceEqual(t, "CSS imports", got, want)
}

func TestIsCSSFile(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"button.css", true},
		{"theme.css", true},
		{"Button.module.css", true}, // module.css is still a .css file
		{"styles.CSS", false},       // case-sensitive
		{"button.ts", false},
		{"index.tsx", false},
		{"cssHelper.ts", false},
	}
	for _, tc := range cases {
		got := isCSSFile(tc.name)
		if got != tc.want {
			t.Errorf("isCSSFile(%q): got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestIsCSSModuleFile(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"Button.module.css", true},
		{"theme.module.css", true},
		{"button.css", false},  // plain CSS, not a module
		{"button.ts", false},
		{"Button.MODULE.css", false}, // case-sensitive
	}
	for _, tc := range cases {
		got := isCSSModuleFile(tc.name)
		if got != tc.want {
			t.Errorf("isCSSModuleFile(%q): got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestIsAssetFile(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"logo.svg", true},
		{"hero.png", true},
		{"photo.jpg", true},
		{"photo.jpeg", true},
		{"animation.gif", true},
		{"image.webp", true},
		{"font.woff", true},
		{"font.woff2", true},
		{"font.ttf", true},
		{"font.eot", true},
		// JSON files are handled by json_library, NOT asset_library.
		{"data.json", false},
		{"config.json", false},
		// Case-insensitive:
		{"logo.SVG", true},
		{"photo.PNG", true},
		// Not assets:
		{"styles.css", false},
		{"Button.module.css", false},
		{"component.ts", false},
		{"index.tsx", false},
		{"package.json.lock", false}, // .lock extension, not .json
	}
	for _, tc := range cases {
		got := isAssetFile(tc.name)
		if got != tc.want {
			t.Errorf("isAssetFile(%q): got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestIsJSONFile(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"config.json", true},
		{"data.json", true},
		{"schema.json", true},
		// Case-insensitive extension:
		{"DATA.JSON", true},
		// Not JSON:
		{"logo.svg", false},
		{"hero.png", false},
		{"styles.css", false},
		{"component.ts", false},
		// Well-known config JSON files are excluded from generation (tested
		// separately in generateRules), but isJSONFile itself returns true for them.
		{"package.json", true},
	}
	for _, tc := range cases {
		got := isJSONFile(tc.name)
		if got != tc.want {
			t.Errorf("isJSONFile(%q): got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestExtractImports_CSSModuleImports(t *testing.T) {
	// CSS Module imports use a default import syntax.
	// Gazelle must extract them for dep resolution against css_module targets.
	src := `
import styles from "./Button.module.css";
import themeStyles from "./theme.module.css";
import "./side-effect.css";
import { foo } from "./utils";
`
	got := extractFromSource(stripComments(src))
	want := []string{"./Button.module.css", "./side-effect.css", "./theme.module.css", "./utils"}
	assertStringSliceEqual(t, "CSS module imports", got, want)
}

func TestExtractImports_AssetImports(t *testing.T) {
	// Asset imports (SVGs, images, fonts) should be extracted for resolution.
	src := `
import logo from "./logo.svg";
import heroImage from "./hero.png";
import { foo } from "./utils";
`
	got := extractFromSource(stripComments(src))
	want := []string{"./hero.png", "./logo.svg", "./utils"}
	assertStringSliceEqual(t, "asset imports", got, want)
}

// ---- helpers ---------------------------------------------------------------

// assertStringSliceEqual checks that got and want contain the same strings in
// sorted order. It normalises by sorting both slices before comparing.
func assertStringSliceEqual(t *testing.T, label string, got, want []string) {
	t.Helper()
	sort.Strings(got)
	sort.Strings(want)
	if len(got) != len(want) {
		t.Errorf("%s: len mismatch\n  got  (%d): %v\n  want (%d): %v", label, len(got), got, len(want), want)
		return
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("%s: element %d mismatch\n  got  %q\n  want %q", label, i, got[i], want[i])
		}
	}
}
