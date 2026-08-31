package typescript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bazelbuild/bazel-gazelle/label"
)

// The workspace a TanStack dynamic-segment route makes: a file literally named
// "@{$username}.tsx". Emitted bare into srcs, Bazel reads "@" as the start of a
// repository name and fails with `invalid repository name
// '{$username}.tsx'` -- which aborts `bazel query //...` for every package in
// the workspace, not only this one.
var atNamedSrcWorkspace = map[string]string{
	"package.json":                `{"name":"w","dependencies":{"zod":"3.24.2"}}` + "\n",
	"tsconfig.json":               `{"compilerOptions":{"strict":true}}` + "\n",
	"src/routes/index.tsx":        "export const index = 1;\n",
	"src/routes/@{$username}.tsx": "export const user = 1;\n",
	"src/routes/@logo.svg":        "<svg/>\n",
	"src/routes/@data.json":       `{"a":1}` + "\n",
	"src/routes/@sheet.css":       ".a{color:red}\n",
	"src/routes/@case.test.tsx":   "export const t = 1;\n",
}

// TestAtNamedSrcIsAValidLabel: every srcs entry Gazelle writes has to parse as
// a label naming a file in the package that declares it, and the file whose
// name forced the ":" has to still be there -- a src silently dropped is a file
// nothing compiles.
func TestAtNamedSrcIsAValidLabel(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, atNamedSrcWorkspace)
	captureLog(t, func() { convergeGazelle(t, root) })

	declared := map[string]bool{}
	for _, pkg := range convergePackages(t, root) {
		for _, r := range loadRules(t, root, pkg) {
			for _, src := range r.AttrStrings("srcs") {
				parsed, err := label.Parse(src)
				switch {
				case err != nil:
					t.Errorf("%s(%s) in %s declares srcs = %q, which is not a label: %v. Bazel "+
						"reads the head of the string rather than the file system, and one "+
						"unparseable entry aborts every query and every build over the "+
						"workspace.", r.Kind(), r.Name(), pkg, src, err)
					continue
				case !parsed.Relative:
					t.Errorf("%s(%s) in %s declares srcs = %q, which parses as %v -- a label in "+
						"another package, or another repository, rather than the file this "+
						"package holds.", r.Kind(), r.Name(), pkg, src, parsed)
					continue
				}
				full := filepath.Join(root, filepath.FromSlash(pkg), filepath.FromSlash(parsed.Name))
				if _, err := os.Stat(full); err != nil {
					t.Errorf("%s(%s) in %s declares srcs = %q, which names %q -- no file this "+
						"package holds.", r.Kind(), r.Name(), pkg, src, parsed.Name)
					continue
				}
				declared[filepath.ToSlash(filepath.Join(pkg, parsed.Name))] = true
			}
		}
	}

	for _, want := range []string{
		"src/routes/@{$username}.tsx",
		"src/routes/@logo.svg",
		"src/routes/@data.json",
		"src/routes/@sheet.css",
		"src/routes/@case.test.tsx",
	} {
		if !declared[want] {
			t.Errorf("no generated target declares %s, so nothing stages or compiles it", want)
		}
	}
}

// TestAtNamedSrcConverges: pinning the name with a ":" by hand is a workaround
// only if the next run keeps it. Gazelle re-emitting the bare form is what made
// the fix something to re-apply after every `bazel run //:gazelle`.
func TestAtNamedSrcConverges(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, atNamedSrcWorkspace)
	captureLog(t, func() { convergeGazelle(t, root) })
	before := buildFileText(t, root, "src/routes")

	logged := captureLog(t, func() { convergeGazelle(t, root) })
	if after := buildFileText(t, root, "src/routes"); after != before {
		t.Errorf("the second run rewrote src/routes/BUILD.bazel, so the label form has to be "+
			"re-applied by hand after every run:\n%s\nthe run said:\n%s",
			lineDiff(before, after), indentLog(logged))
	}
}

// TestSrcLabelFollowsTheLabelGrammar pins the rule rather than the one filename
// that found it, with label.Parse as the authority on what Bazel reads.
func TestSrcLabelFollowsTheLabelGrammar(t *testing.T) {
	for _, name := range []string{
		"index.tsx",
		"sub/dir/leaf.ts",
		"@{$username}.tsx",
		"@logo.svg",
		"@@double.ts",
		"routes/@{$id}.tsx",
		"-dash.ts",
		"%percent.ts",
		"..dots.ts",
	} {
		lbl, ok := srcLabel(name)
		if !ok {
			t.Errorf("srcLabel(%q) has no label, but the name holds no \":\" so some spelling "+
				"of it does", name)
			continue
		}
		parsed, err := label.Parse(lbl)
		if err != nil {
			t.Errorf("srcLabel(%q) = %q, which label.Parse rejects: %v", name, lbl, err)
			continue
		}
		if !parsed.Relative || parsed.Name != name {
			t.Errorf("srcLabel(%q) = %q, which parses as %v (relative=%v, name=%q) -- not the "+
				"file this package holds", name, lbl, parsed, parsed.Relative, parsed.Name)
		}
	}

	// A ":" splits package from target and a target name may not contain one,
	// so neither spelling is a label: bare, Bazel reads a package that is not
	// there; pinned, it refuses with "target names may not contain ':'".
	for _, name := range []string{"a:b.tsx", ":lead.tsx", "dir/a:b.ts"} {
		if lbl, ok := srcLabel(name); ok {
			t.Errorf("srcLabel(%q) = %q, but no label names a file whose name holds a \":\"",
				name, lbl)
		}
		if _, err := label.Parse(":" + name); err == nil {
			t.Errorf("label.Parse(%q) succeeded, so this case no longer says what it claims",
				":"+name)
		}
	}
}

// A name Bazel cannot spell is left out of the generated targets, and the run
// says which file and why. Dropping a source in silence is the same defect as
// emitting one nothing can parse.
func TestUnlabelableSrcIsReported(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{}
	for rel, body := range atNamedSrcWorkspace {
		files[rel] = body
	}
	files["src/routes/a:b.tsx"] = "export const c = 1;\n"
	writeWorkspace(t, root, files)

	logged := captureLog(t, func() { convergeGazelle(t, root) })
	if !strings.Contains(logged, `"a:b.tsx"`) {
		t.Errorf("a source Bazel has no label for was left out of every target and the run did "+
			"not name it:\n%s", indentLog(logged))
	}
	for _, r := range loadRules(t, root, "src/routes") {
		for _, src := range r.AttrStrings("srcs") {
			if strings.Contains(src, ":b.tsx") {
				t.Errorf("%s(%s) declares srcs = %q; no spelling of that name is a label",
					r.Kind(), r.Name(), src)
			}
		}
	}
}
