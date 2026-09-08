package typescript

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The directives the package model retired: one row each in the removal
// table of docs/gazelle/directives.md, and nowhere else in the docs.
var removedDirectives = []string{
	"ts_ambient_types", "ts_asset_declaration_type", "ts_codegen",
	"ts_declarations", "ts_exclude", "ts_exclude_dir", "ts_ignore",
	"ts_js_srcs", "ts_npm_hub", "ts_npm_mapping", "ts_package_boundary",
	"ts_path_alias", "ts_runtime_dep", "ts_target_name", "ts_warn_unresolved",
}

const (
	directivesPage    = "../docs/gazelle/directives.md"
	removalTableTitle = "## Removed Directives"
)

var (
	tsDirective    = regexp.MustCompile(`gazelle:(ts_[a-z_]*)`)
	snippetInclude = regexp.MustCompile(`^-+8<-+ "(.+)"$`)
)

// docPages is every page the site builds, the files its snippet includes
// pull in (CHANGELOG.md excepted as history) and the repository's front matter.
func docPages(t *testing.T) []string {
	seen := map[string]bool{}
	var pages []string
	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			pages = append(pages, p)
		}
	}
	front := []string{"../README.md", "../AGENTS.md", "../CONTRIBUTING.md"}
	for _, p := range front {
		add(p)
	}
	visit := func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".md") {
			return nil
		}
		add(p)
		for _, line := range readLines(t, p) {
			m := snippetInclude.FindStringSubmatch(line)
			if m != nil && m[1] != "CHANGELOG.md" {
				add("../" + m[1])
			}
		}
		return nil
	}
	err := filepath.WalkDir("../docs", visit)
	if err != nil {
		t.Fatalf("walking ../docs: %v", err)
	}
	slices.Sort(pages)
	return pages
}

func readLines(t *testing.T, page string) []string {
	data, err := os.ReadFile(page)
	if err != nil {
		t.Fatalf("%s: %v", page, err)
	}
	return strings.Split(string(data), "\n")
}

// removalTable is the section from its heading to the next heading; blanked,
// the page keeps its line numbers with the table's rows emptied.
func removalTable(lines []string, blanked bool) []string {
	var out []string
	inTable := false
	for _, line := range lines {
		switch {
		case line == removalTableTitle:
			inTable = true
		case inTable && strings.HasPrefix(line, "## "):
			inTable = false
		}
		switch {
		case blanked && inTable:
			out = append(out, "")
		case blanked || inTable:
			out = append(out, line)
		}
	}
	return out
}

func TestDocs_RemovalTableNamesEachRetiredDirectiveOnce(t *testing.T) {
	table := removalTable(readLines(t, directivesPage), false)
	if len(table) == 0 {
		t.Fatalf("%s has no %q section", directivesPage, removalTableTitle)
	}
	var named []string
	for _, line := range table {
		if !strings.HasPrefix(line, "| `# gazelle:") {
			continue
		}
		for _, m := range tsDirective.FindAllStringSubmatch(line, -1) {
			named = append(named, m[1])
		}
	}
	slices.Sort(named)
	if !slices.Equal(named, removedDirectives) {
		t.Errorf("the removal table names %v\nwant %v", named, removedDirectives)
	}
}

func TestDocs_NoPageNamesATsDirectiveOutsideTheRemovalTable(t *testing.T) {
	want := len((&tsLang{}).KnownDirectives())
	got := 0
	for _, page := range docPages(t) {
		lines := readLines(t, page)
		if page == directivesPage {
			lines = removalTable(lines, true)
		}
		for i, line := range lines {
			for _, m := range tsDirective.FindAllString(line, -1) {
				got++
				t.Errorf("%s:%d names %s", page, i+1, m)
			}
		}
	}
	if got != want {
		t.Errorf("the docs name %d ts_ directives outside the removal table; "+
			"KnownDirectives() has %d", got, want)
	}
}
