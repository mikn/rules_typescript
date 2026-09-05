package typescript

import (
	"reflect"
	"testing"
)

func TestScanTypeReferences_LeadingCommentsOnly(t *testing.T) {
	src := "\ufeff#!/usr/bin/env node\n" +
		"// a comment\n" +
		"/* a block\n   comment */\n" +
		"/// <reference types=\"vite/client\" />\n" +
		"  /// <reference types='google.maps' />\r\n" +
		"/// <reference path=\"./other.d.ts\" />\n" +
		"/// <reference lib=\"dom\" />\n" +
		"/// <reference preserve=\"true\" types=\"node\" />\n" +
		"/// <reference types=\"\" />\n" +
		"export {};\n" +
		"/// <reference types=\"after-a-statement\" />\n"

	want := []string{"vite/client", "google.maps", "node"}
	if got := ScanTypeReferences(src); !reflect.DeepEqual(got, want) {
		t.Errorf("ScanTypeReferences = %v, want %v", got, want)
	}
}

func TestScanTypeReferences_NoneWithoutALeadingDirective(t *testing.T) {
	for name, src := range map[string]string{
		"statement first":     "export const x = 1;\n/// <reference types=\"node\" />\n",
		"plain comment":       "// <reference types=\"node\" />\nexport {};\n",
		"block comment":       "/* /// <reference types=\"node\" /> */\nexport {};\n",
		"declare module only": "declare module \"x\" {}\n",
		"empty":               "",
	} {
		if got := ScanTypeReferences(src); len(got) != 0 {
			t.Errorf("%s: ScanTypeReferences = %v, want none", name, got)
		}
	}
}

func TestTypeReferenceLabel_TheLockfileDecides(t *testing.T) {
	tc := makeConfig("", nil)
	tc.npmLockNames = map[string]bool{
		"@types/node": true, "vite": true, "bun-types": true, "@cloudflare/workers-types": true,
	}
	tc.npmPackages = map[string]string{
		"@types/node": "", "vite": "", "bun-types": "", "@cloudflare/workers-types": "",
	}

	for name, want := range map[string]string{
		"node":                      "@npm//:types_node",
		"vite/client":               "@npm//:vite",
		"@cloudflare/workers-types": "@npm//:cloudflare_workers-types",
		"bun-types":                 "@npm//:bun-types",
		"google.maps":               "",
		"deno":                      "",
		"events":                    "",
		"buffer":                    "",
		"./env.d.ts":                "",
	} {
		if got := typeReferenceLabel(tc, name); got != want {
			t.Errorf("typeReferenceLabel(%q) = %q, want %q", name, got, want)
		}
	}
}

// The hub the imports resolve to is the hub the directive resolves to.
func TestTypeReferenceLabel_FollowsTheNpmHub(t *testing.T) {
	tc := makeConfig("", nil)
	tc.npmHub = "@npm_tools"
	tc.npmPackages = map[string]string{"@types/node": ""}

	if got := typeReferenceLabel(tc, "node"); got != "@npm_tools//:types_node" {
		t.Errorf("typeReferenceLabel(\"node\") = %q, want @npm_tools//:types_node", got)
	}
}

// With no inventory the mapping is the tsconfig `types` writer's, unchecked.
func TestTypeReferenceLabel_NoInventoryWritesTheTypesLabel(t *testing.T) {
	tc := makeConfig("", nil)

	for name, want := range map[string]string{
		"node":      "@npm//:types_node",
		"bun-types": "@npm//:types_bun-types",
	} {
		if got := typeReferenceLabel(tc, name); got != want {
			t.Errorf("typeReferenceLabel(%q) = %q, want %q", name, got, want)
		}
	}
}
