package gazellebinary_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bazelbuild/rules_go/go/runfiles"
)

func TestTheExportedBinaryCarriesTypeScriptOnly(t *testing.T) {
	polyglot := generate(t, "GAZELLE_TS")
	for _, kind := range []string{"go_library(", "proto_library("} {
		if !strings.Contains(polyglot, kind) {
			t.Fatalf("the fixture no longer reaches %s, so the checks below prove nothing:\n%s", kind, polyglot)
		}
	}

	only := generate(t, "GAZELLE_TYPESCRIPT")
	for _, kind := range []string{"go_library(", "proto_library("} {
		if strings.Contains(only, kind) {
			t.Errorf("the exported binary wrote %s:\n%s", kind, only)
		}
	}
	if !strings.Contains(only, "ts_compile(") {
		t.Errorf("the exported binary generated no TypeScript rule:\n%s", only)
	}
}

func generate(t *testing.T, binaryEnv string) string {
	t.Helper()
	rf, err := runfiles.New()
	if err != nil {
		t.Fatalf("runfiles: %v", err)
	}
	binary, err := rf.Rlocation(os.Getenv(binaryEnv))
	if err != nil {
		t.Fatalf("%s: %v", binaryEnv, err)
	}

	dir := t.TempDir()
	for name, body := range map[string]string{
		"BUILD.bazel":   "",
		"go.mod":        "module example.com/fixture\n\ngo 1.26\n",
		"fixture.go":    "package fixture\n",
		"fixture.proto": "syntax = \"proto3\";\n\npackage fixture;\n",
		"index.ts":      "export const answer = 42;\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	cmd := exec.Command(binary, "-repo_root", dir, dir)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s: %v\n%s", binaryEnv, err, out)
	}

	generated, err := os.ReadFile(filepath.Join(dir, "BUILD.bazel"))
	if err != nil {
		t.Fatalf("read generated BUILD.bazel: %v", err)
	}
	return string(generated)
}
