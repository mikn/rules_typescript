package typescript

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mikn/rules_typescript/ts/tools/explainfiles"
)

func TestResolutionCandidatesRebaseSharedFactsWithoutChangingAttribution(t *testing.T) {
	root := t.TempDir()
	observations := []explainfiles.ResolutionCandidate{
		{Kind: explainfiles.Import, From: filepath.Join(root, "app/consumer.ts"), Specifier: "#generated", Path: filepath.Join(root, "generated/first")},
		{Kind: explainfiles.TypeReference, From: filepath.Join(root, "app/consumer.ts"), Specifier: "../types", Path: filepath.Join(root, "types")},
		{Kind: explainfiles.Import, From: filepath.Join(root, "other/consumer.ts"), Specifier: "#generated", Path: filepath.Join(root, "../shared/second")},
	}
	got, err := relativeResolutionCandidates(root, observations)
	want := []resolutionCandidate{
		{"app/consumer.ts", "#generated", "generated/first"},
		{"other/consumer.ts", "#generated", "../shared/second"},
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("rebasing changed candidate order, attribution or type-reference filtering: %+v, %v", got, err)
	}
}

func TestResolutionTracePreservesActualCompilerListing(t *testing.T) {
	requireTsgo(t)
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"package.json":                         `{"name":"fixture","imports":{"#redirect":"dependency"}}`,
		"node_modules/dependency/package.json": `{"name":"dependency","types":"index.d.ts"}`,
		"node_modules/dependency/index.d.ts":   "export declare const dependency: string;\n",
		"app/tsconfig.json":                    `{"compilerOptions":{"module":"preserve","moduleResolution":"bundler","paths":{"#shared/*":["./shared/*"]}},"include":["shared/**/*.ts"]}`,
		"app/shared/value.ts":                  "export const value = 1;\n",
		"app/plugins/tsconfig.json":            `{"extends":"../tsconfig.json","include":["*.ts"]}`,
		"app/plugins/consumer.ts":              "import { dependency } from '#redirect';\nimport { value } from '#shared/value';\nimport { generated } from '#shared/generated/runtime';\nexport const result = [value, generated];\n",
	})
	tsgo, err := newProgramStore().binary()
	if err != nil {
		t.Fatal(err)
	}
	version, err := exec.Command(tsgo, "--version").CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("selected compiler %s: %s", tsgo, version)
	var baseline *explainfiles.Listing
	for _, traced := range []bool{false, true} {
		args := []string{"-p", "app/plugins/tsconfig.json", "--noEmit", "--listFilesOnly", "--explainFiles", "--pretty", "false", "--locale", "en"}
		if traced {
			args = append(args, "--traceResolution")
		}
		cmd := exec.Command(tsgo, args...)
		cmd.Dir = root
		started := time.Now()
		raw, err := cmd.CombinedOutput()
		elapsed := time.Since(started)
		if err != nil {
			t.Fatalf("compiler listing: %v\n%s", err, raw)
		}
		t.Logf("trace=%t elapsed=%s bytes=%d argv=%q; same cold-tree fixture, fresh process, untraced first, filesystem cache uncontrolled", traced, elapsed, len(raw), args)
		name := "plain"
		if traced {
			name = "trace"
		}
		if out := os.Getenv("TEST_UNDECLARED_OUTPUTS_DIR"); out != "" {
			if err := os.WriteFile(filepath.Join(out, "generated-tree-"+name+".log"), raw, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		listing, err := explainfiles.Parse(string(raw))
		if err != nil {
			t.Fatal(err)
		}
		if traced {
			if !strings.Contains(string(raw), "======== Resolving module 'dependency' from '") {
				t.Fatal("package imports continuation was not exercised")
			}
			candidates, err := relativeResolutionCandidates(root, listing.Candidates)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, candidate := range candidates {
				if candidate.from == "app/plugins/consumer.ts" && candidate.specifier == "#shared/generated/runtime" && candidate.path == "app/shared/generated/runtime" {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing actual cold-tree candidate: %+v", candidates)
			}
		}
		listed := &explainfiles.Listing{Files: listing.Files, Roots: listing.Roots, Edges: listing.Edges, Types: listing.Types, Implicit: listing.Implicit, Diagnostics: listing.Diagnostics}
		if !traced {
			baseline = listed
		} else if !reflect.DeepEqual(listed, baseline) {
			t.Fatalf("trace changed compiler listing: got %+v, want %+v", listed, baseline)
		}
	}
}
