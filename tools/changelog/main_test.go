package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bazelbuild/rules_go/go/runfiles"
)

const entryWithCodeBlock = "### Added\n\n" +
	"- **`ts_binary` takes a plain JavaScript file as its `entry_point`.** The attr\n" +
	"  is polymorphic:\n\n" +
	"  ```python\n" +
	"  ts_binary(\n" +
	"      name = \"paraglide_compile\",\n" +
	"  )\n" +
	"  ```\n\n" +
	"  A new `data` attr carries the modules the entry imports into runfiles.\n"

func TestParseFragmentSplitsHeadingFromEntry(t *testing.T) {
	f, err := ParseFragment("ts-binary-js-entry.md", entryWithCodeBlock)
	if err != nil {
		t.Fatal(err)
	}
	if f.Section != "Added" {
		t.Errorf("section = %q, want Added", f.Section)
	}
	if !strings.HasPrefix(f.Body, "- **`ts_binary`") {
		t.Errorf("body should start at the entry, got:\n%s", f.Body)
	}
	if !strings.Contains(f.Body, "```python") {
		t.Errorf("body lost its code block:\n%s", f.Body)
	}
	if strings.Contains(f.Body, "### Added") {
		t.Errorf("body still carries the heading:\n%s", f.Body)
	}
}

func TestParseFragmentTakesAnEmDashSection(t *testing.T) {
	f, err := ParseFragment("break.md", "### Breaking — `ts_compile`\n\n- It changed.\n")
	if err != nil {
		t.Fatal(err)
	}
	if f.Section != "Breaking — `ts_compile`" {
		t.Errorf("section = %q", f.Section)
	}
}

// A `###` line inside a fence is content, not a second section: an entry may
// show a shell transcript or a Markdown sample.
func TestParseFragmentIgnoresAHeadingInsideAFence(t *testing.T) {
	body := "### Fixed\n\n- It is fixed:\n\n  ```md\n  ### not a section\n  ```\n"
	f, err := ParseFragment("fenced.md", body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.Body, "### not a section") {
		t.Errorf("body lost the fenced line:\n%s", f.Body)
	}
}

func TestParseFragmentRejects(t *testing.T) {
	for name, text := range map[string]string{
		"no heading":     "- An entry with no section.\n",
		"wrong level":    "## Added\n\n- An entry.\n",
		"empty heading":  "### \n\n- An entry.\n",
		"no entry":       "### Added\n\n",
		"empty file":     "",
		"second section": "### Added\n\n- One.\n\n### Fixed\n\n- Two.\n",
	} {
		if _, err := ParseFragment("bad.md", text); err == nil {
			t.Errorf("%s: expected a failure", name)
		}
	}
}

func TestReadFragmentsSkipsTheReadmeAndSortsByName(t *testing.T) {
	dir := t.TempDir()
	for name, text := range map[string]string{
		"README.md":  "Not a fragment, and it has no heading.\n",
		"b-entry.md": "### Fixed\n\n- B.\n",
		"a-entry.md": "### Fixed\n\n- A.\n",
		"notes.txt":  "### Fixed\n\n- Not markdown.\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := ReadFragments(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "a-entry.md" || got[1].Name != "b-entry.md" {
		t.Fatalf("got %+v", got)
	}
}

func TestRenderOrdersSectionsIndependentlyOfFileNames(t *testing.T) {
	fragments := []Fragment{
		{Name: "z.md", Section: "Fixed", Body: "- Fixed one."},
		{Name: "y.md", Section: "Added", Body: "- Added one."},
		{Name: "x.md", Section: "Known gaps", Body: "- A gap."},
		{Name: "w.md", Section: "Breaking — `ts_test`", Body: "- Broke ts_test."},
		{Name: "v.md", Section: "Breaking — `ts_compile`", Body: "- Broke ts_compile."},
		{Name: "u.md", Section: "Added", Body: "- Added two."},
	}
	want := `## [Unreleased]

### Breaking — ` + "`ts_compile`" + `

- Broke ts_compile.

### Breaking — ` + "`ts_test`" + `

- Broke ts_test.

### Added

- Added one.
- Added two.

### Fixed

- Fixed one.

### Known gaps

- A gap.
`
	if got := Render(fragments, ""); got != want {
		t.Errorf("Render() =\n%s\nwant\n%s", got, want)
	}
}

func TestRenderTitlesTheSectionWithAVersion(t *testing.T) {
	got := Render([]Fragment{{Section: "Added", Body: "- One."}}, "0.3.0")
	if !strings.HasPrefix(got, "## [0.3.0]\n") {
		t.Errorf("got:\n%s", got)
	}
}

const changelogFixture = `# Changelog

All notable changes to this project will be documented in this file.

## [0.2.0]

### Added

- The 0.2.0 entry.

## [0.1.0] — never released
`

func TestSpliceInsertsAboveTheNewestRelease(t *testing.T) {
	got, err := Splice(changelogFixture, "## [0.3.0]\n\n### Fixed\n\n- A fix.\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "# Changelog\n\nAll notable changes") {
		t.Errorf("preamble was rewritten:\n%s", got)
	}
	if !strings.Contains(got, "- The 0.2.0 entry.") || !strings.Contains(got, "## [0.1.0] — never released") {
		t.Errorf("a released section was lost:\n%s", got)
	}
	newest := strings.Index(got, "## [0.3.0]")
	if newest < 0 || newest > strings.Index(got, "## [0.2.0]") {
		t.Errorf("the new section is not above 0.2.0:\n%s", got)
	}
	if strings.Contains(got, "\n\n\n") {
		t.Errorf("blank lines piled up:\n%s", got)
	}
}

func TestSpliceRefusesAFileWithNoRelease(t *testing.T) {
	if _, err := Splice("# Changelog\n\nNothing yet.\n", "## [0.3.0]\n"); err == nil {
		t.Fatal("expected a failure")
	}
}

// The fragments actually checked in. A malformed one fails here, in
// `bazel test //...`, rather than at release time when someone runs --write.
func TestCheckedInFragmentsParse(t *testing.T) {
	dir := changelogDir(t)
	if _, err := os.Stat(filepath.Join(dir, "README.md")); err != nil {
		t.Fatalf("changelog.d is not readable from the test: %v", err)
	}
	fragments, err := ReadFragments(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fragments {
		if f.Section == "" || f.Body == "" {
			t.Errorf("%s parsed to an empty section or body", f.Name)
		}
	}
	t.Logf("%d fragments pending release", len(fragments))
}

// Under `go test` there is no runfiles tree, and the package directory is the
// working directory.
func changelogDir(t *testing.T) string {
	t.Helper()
	paths := strings.Fields(os.Getenv("CHANGELOG_FRAGMENTS"))
	if len(paths) == 0 {
		return filepath.Join("..", "..", "changelog.d")
	}
	rf, err := runfiles.New()
	if err != nil {
		t.Fatalf("runfiles: %v", err)
	}
	first, err := rf.Rlocation(paths[0])
	if err != nil {
		t.Fatalf("%s: %v", paths[0], err)
	}
	return filepath.Dir(first)
}
