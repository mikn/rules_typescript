package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const (
	keptRecord      = "TN:\nSF:tests/app/kept.js\nDA:1,1\nend_of_record\n"
	generatedRecord = "TN:\nSF:tests/app/generated.js\nDA:1,1\nend_of_record\n"
	filteredRecord  = "TN:\nSF:tests/app/filtered.js\nDA:1,0\nend_of_record\n"
)

func TestMergeKeepsTheFilesTheManifestSelects(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "instrumented.txt")
	write(t, manifest,
		"tests/app/kept.ts\nbazel-out/k8-fastbuild/bin/tests/app/generated.ts\n")
	write(t, filepath.Join(dir, "cov", "vitest.dat"),
		keptRecord+generatedRecord+filteredRecord)
	got, err := Merge(filepath.Join(dir, "cov"), manifest, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := keptRecord + generatedRecord; string(got) != want {
		t.Errorf("report = %q, want %q", got, want)
	}
}

func TestMergeReadsEveryDatUnderTheDirectoryAndNothingElse(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "instrumented.txt")
	write(t, manifest, "tests/app/kept.ts\ntests/app/generated.ts\n")
	write(t, filepath.Join(dir, "cov", "vitest.dat"), keptRecord)
	write(t, filepath.Join(dir, "cov", "shard", "second.dat"), generatedRecord)
	write(t, filepath.Join(dir, "cov", "vitest", "lcov.info"), generatedRecord)
	got, err := Merge(filepath.Join(dir, "cov"), manifest, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := generatedRecord + keptRecord; string(got) != want {
		t.Errorf("report = %q, want %q", got, want)
	}
}

func TestMergeWithAnEmptyManifestKeepsNothing(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "instrumented.txt")
	write(t, manifest, "")
	write(t, filepath.Join(dir, "cov", "vitest.dat"), keptRecord)
	got, err := Merge(filepath.Join(dir, "cov"), manifest, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("report = %q, want an empty one", got)
	}
}

func TestMergeDropsTheSourcesAFilterMatchesWhole(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "instrumented.txt")
	write(t, manifest, "tests/app/kept.ts\n/usr/lib/node/fs.ts\n")
	system := "TN:\nSF:/usr/lib/node/fs.js\nDA:1,1\nend_of_record\n"
	write(t, filepath.Join(dir, "cov", "vitest.dat"), system+keptRecord)
	got, err := Merge(filepath.Join(dir, "cov"), manifest,
		[]string{"/usr/lib/.+", "tests/app"})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != keptRecord {
		t.Errorf("report = %q, want %q", got, keptRecord)
	}
}

func TestKeyMatchesASourceAgainstWhatWasCompiledFromIt(t *testing.T) {
	cases := map[string]string{
		"tests/app/a.ts": "tests/app/a",
		"bazel-out/k8-fastbuild/bin/tests/app/a.js": "tests/app/a",
		"  tests/app/a.tsx  ":                       "tests/app/a",
		"":                                          "",
	}
	for in, want := range cases {
		if got := Key(in); got != want {
			t.Errorf("Key(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRunWritesTheOutputFileWhenNothingWasCovered(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "instrumented.txt")
	write(t, manifest, "tests/app/kept.ts\n")
	if err := os.Mkdir(filepath.Join(dir, "cov"), 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "coverage.dat")
	var stderr bytes.Buffer
	code := run([]string{
		"--coverage_dir=" + filepath.Join(dir, "cov"),
		"--output_file=" + out,
		"--filter_sources=/usr/bin/.+",
		"--source_file_manifest=" + manifest,
	}, &stderr)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("Bazel reads the output file after the merger: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("report = %q, want an empty one", got)
	}
}

func TestRunRejectsAFlagItDoesNotImplement(t *testing.T) {
	var stderr bytes.Buffer
	code := run([]string{
		"--coverage_dir=x", "--output_file=y", "--source_file_manifest=z",
		"--sources_to_replace_file=w",
	}, &stderr)
	if code != 2 {
		t.Errorf("exit %d, want 2: %s", code, stderr.String())
	}
}
