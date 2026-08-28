package main

import "testing"

const moduleFixture = `"""docstring."""

module(
    name = "rules_typescript",
    version = "0.1.0",
    compatibility_level = 0,
)

bazel_dep(name = "bazel_skylib", version = "1.9.0")
bazel_dep(name = "platforms", version = "1.0.0")
`

func TestSetModuleVersionLeavesBazelDepsAlone(t *testing.T) {
	got, old, err := setModuleVersion(moduleFixture, "0.2.0")
	if err != nil {
		t.Fatal(err)
	}
	if old != "0.1.0" {
		t.Errorf("old version = %q, want 0.1.0", old)
	}
	for _, want := range []string{
		`    version = "0.2.0",`,
		`bazel_dep(name = "bazel_skylib", version = "1.9.0")`,
		`bazel_dep(name = "platforms", version = "1.0.0")`,
	} {
		if !contains(got, want) {
			t.Errorf("result is missing %q:\n%s", want, got)
		}
	}
}

func TestSetModuleVersionRejectsAnotherModule(t *testing.T) {
	if _, _, err := setModuleVersion("module(\n    name = \"other\",\n    version = \"1.0.0\",\n)\n", "0.2.0"); err == nil {
		t.Fatal("expected a failure on a foreign module()")
	}
}

func TestVersionFormat(t *testing.T) {
	for _, ok := range []string{"0.2.0", "1.10.3", "0.2.0-rc.1", "0.2.0-beta.1"} {
		if !semver.MatchString(ok) {
			t.Errorf("%q should be accepted", ok)
		}
	}
	for _, bad := range []string{"1.2", "v1.2.3", "1.2.3.4", "", "1.2.3-"} {
		if semver.MatchString(bad) {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// The release-PR path: the bump is already in HEAD, so there is nothing to write
// and nothing to commit. run() keys the skip on old == version.
func TestSetModuleVersionIsIdempotent(t *testing.T) {
	got, old, err := setModuleVersion(moduleFixture, "0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if old != "0.1.0" {
		t.Errorf("old version = %q, want 0.1.0", old)
	}
	if got != moduleFixture {
		t.Errorf("re-setting the same version rewrote the file:\n%s", got)
	}
}
