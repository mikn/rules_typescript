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

func TestResolutionTraceKeepsCandidatesOutOfListedFilesAndEdges(t *testing.T) {
	root := t.TempDir()
	from := filepath.Join(root, "app/plugins/consumer.ts")
	candidate := filepath.Join(root, "app/shared/generated/runtime")
	listing := "app/plugins/consumer.ts\n   Matched by include pattern '*.ts' in 'app/plugins/tsconfig.json'\nerror TS2307: missing import\n"
	trace := "======== Resolving module '#generated/runtime' from '" + from + "'. ========\n" +
		"Loading module as file / folder, candidate module location '" + candidate + "', target file types: TypeScript, Declaration.\n" +
		"Directory '" + filepath.Dir(candidate) + "' does not exist, skipping all lookups in it.\n" +
		"======== Module name '#generated/runtime' was not resolved. ========\n" +
		"======== Resolving type reference directive 'node', containing file '" + from + "', root directory not set. ========\n" +
		"======== Type reference directive 'node' was not resolved. ========\n"
	got, candidates, err := splitResolutionTrace(root, trace+listing)
	if err != nil {
		t.Fatal(err)
	}
	if got != listing {
		t.Fatalf("listing changed: %q", got)
	}
	want := []resolutionCandidate{{"app/plugins/consumer.ts", "#generated/runtime", "app/shared/generated/runtime"}}
	if !reflect.DeepEqual(candidates, want) {
		t.Fatalf("candidates = %+v, want %+v", candidates, want)
	}
}

func TestResolutionTraceRejectsLostCandidateAttribution(t *testing.T) {
	root := t.TempDir()
	start := "======== Resolving module '#module' from '" + filepath.Join(root, "app/index.ts") + "'. ========\n"
	end := "======== Module name '#module' was not resolved. ========\n"
	for name, text := range map[string]string{
		"nested":              start + start + end + end,
		"unfinished":          start,
		"mismatched end":      start + strings.ReplaceAll(end, "#module", "#other"),
		"unframed candidate":  "Loading module as file / folder, candidate module location '/app/generated', target file types: TypeScript.\n",
		"malformed candidate": start + "Loading module as file / folder, unknown format\n" + end,
		"relative candidate":  start + "Loading module as file / folder, candidate module location 'generated', target file types: TypeScript.\n" + end,
		"unknown boundary":    "======== Unrecognised resolver boundary ========\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := splitResolutionTrace(root, text); err == nil {
				t.Fatal("malformed trace accepted")
			}
		})
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
		text := string(raw)
		if traced {
			if !strings.Contains(text, "======== Resolving module 'dependency' from '") {
				t.Fatal("package imports continuation was not exercised")
			}
			var candidates []resolutionCandidate
			text, candidates, err = splitResolutionTrace(root, text)
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
		listing, err := explainfiles.Parse(text)
		if err != nil {
			t.Fatal(err)
		}
		if !traced {
			baseline = listing
		} else if !reflect.DeepEqual(listing, baseline) {
			t.Fatalf("trace changed compiler listing: got %+v, want %+v", listing, baseline)
		}
	}
}

func TestResolutionTraceKeepsOriginalImporterAcrossPackageRedirect(t *testing.T) {
	root := t.TempDir()
	text := "======== Resolving module '#alias' from '" + filepath.Join(root, "app/source.ts") + "'. ========\n" +
		"Using 'imports' subpath '#alias' with target 'dependency'.\n" +
		"======== Resolving module 'dependency' from '" + root + "/'. ========\n" +
		"Loading module as file / folder, candidate module location '" + filepath.Join(root, "generated/runtime") + "', target file types: TypeScript.\n" +
		"======== Module name '#alias' was not resolved. ========\n"
	_, candidates, err := splitResolutionTrace(root, text)
	if err != nil {
		t.Fatal(err)
	}
	want := []resolutionCandidate{{"app/source.ts", "#alias", "generated/runtime"}}
	if !reflect.DeepEqual(candidates, want) {
		t.Fatalf("candidates = %+v, want %+v", candidates, want)
	}
}
