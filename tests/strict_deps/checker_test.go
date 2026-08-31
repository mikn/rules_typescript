// The undeclared-import check, driven over manifests no build graph would
// produce. The cases that must FAIL cannot be Bazel targets, so the checker
// script is exposed through the `strict_deps` output group and run here.
package strict_deps_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

// checker runs the check over one source file and one manifest.
type checker struct {
	t    *testing.T
	node string
	mjs  string
	dir  string
}

func newChecker(t *testing.T) *checker {
	t.Helper()
	tree := verify.New(t)
	node := tree.File("ts/toolchain/node_resolved/node")
	mjs := tree.File("tests/strict_deps/declared.strictdeps.mjs")
	if !node.Exists() || !mjs.Exists() {
		t.FailNow()
	}
	return &checker{t: t, node: node.Abs(), mjs: mjs.Abs(), dir: t.TempDir()}
}

// run writes source to pkg/<name>.ts and checks it against the manifest
// entries. Every path is relative to the working directory, which is what the
// real action sees: exec-root-relative paths, no leading slash.
func (c *checker) run(name, source string, manifest ...string) (string, bool) {
	c.t.Helper()

	rel := "pkg/" + name + ".ts"
	if err := os.MkdirAll(filepath.Join(c.dir, "pkg"), 0o700); err != nil {
		c.t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(c.dir, rel), []byte(source), 0o600); err != nil {
		c.t.Fatalf("write source: %v", err)
	}
	stamp := filepath.Join(c.dir, name+".stamp")

	args := append([]string{c.mjs, stamp, "label\t//pkg:target", "scan\t" + rel}, manifest...)
	cmd := exec.Command(c.node, args...)
	cmd.Dir = c.dir
	out, err := cmd.CombinedOutput()
	if err == nil {
		if _, statErr := os.Stat(stamp); statErr != nil {
			c.t.Errorf("%s: the check passed but wrote no stamp: %v", name, statErr)
		}
		return string(out), true
	}
	if _, ok := err.(*exec.ExitError); !ok {
		c.t.Fatalf("%s: running the checker: %v\n%s", name, err, out)
	}
	return string(out), false
}

// transitive is a manifest entry for a first-party file reachable only through
// another dep. The path is the one a relative specifier resolves to.
func transitive(path, label string) string { return "transitive\t" + path + "\t" + label }

func direct(path string) string { return "direct\t" + path }

func own(path string) string { return "own\t" + path }

func moduleTransitive(name, label string) string {
	return "module-transitive\t" + name + "\t" + label
}

func moduleDirect(name string) string { return "module-direct\t" + name }

func npmTransitive(name, label string) string {
	return "npm-transitive\t" + name + "\t" + label
}

func npmDirect(name string) string { return "npm-direct\t" + name }

// A tree artifact has no file list at analysis time, so it reaches the manifest
// as its own path and answers for everything under it.
func transitiveDir(path, label string) string { return "transitive-dir\t" + path + "\t" + label }

func directDir(path string) string { return "direct-dir\t" + path }

func TestFileInsideATransitiveDirectoryIsRejected(t *testing.T) {
	c := newChecker(t)
	out, ok := c.run(
		"tree_transitive",
		"import { m } from \"./compiled/messages/greeting.js\";\nexport const id = m;\n",
		transitiveDir("pkg/compiled", "//pkg:tree"),
	)
	if ok {
		t.Fatalf("a file inside a directory only a dep's dep provides was accepted:\n%s", out)
	}
	for _, want := range []string{"\"./compiled/messages/greeting.js\"", "add \"//pkg:tree\" to deps"} {
		if !strings.Contains(out, want) {
			t.Errorf("the message must name %s:\n%s", want, out)
		}
	}
}

func TestFileInsideADirectDirectoryIsAccepted(t *testing.T) {
	c := newChecker(t)
	if out, ok := c.run(
		"tree_direct",
		"import { m } from \"./compiled/messages/greeting.js\";\nexport const id = m;\n",
		directDir("pkg/compiled"),
		transitiveDir("pkg/compiled", "//pkg:tree"),
	); !ok {
		t.Fatalf("a declared directory was reported against its own label:\n%s", out)
	}
}

// The prefix is a path prefix, not a string one: pkg/compiled must not answer
// for pkg/compiled_extra.
func TestASiblingOfADirectoryIsNotInsideIt(t *testing.T) {
	c := newChecker(t)
	out, ok := c.run(
		"tree_sibling",
		"import { m } from \"./compiled_extra/greeting.js\";\nexport const id = m;\n",
		directDir("pkg/compiled"),
		transitive("pkg/compiled_extra/greeting.d.ts", "//pkg:other"),
	)
	if ok {
		t.Fatalf("a sibling of a declared directory was taken as inside it:\n%s", out)
	}
	if !strings.Contains(out, "add \"//pkg:other\" to deps") {
		t.Errorf("the message must name the label that really provides it:\n%s", out)
	}
}

func TestTransitiveModuleNameIsRejected(t *testing.T) {
	c := newChecker(t)
	out, ok := c.run(
		"module_name",
		"import { hidden } from \"@acme/hidden\";\nexport const id = hidden.id;\n",
		moduleDirect("@acme/leaf"),
		moduleTransitive("@acme/leaf", "//pkg:leaf"),
		moduleTransitive("@acme/hidden", "//pkg:hidden"),
	)
	if ok {
		t.Fatalf("a module only a dep's dep provides was accepted:\n%s", out)
	}
	for _, want := range []string{"pkg/module_name.ts:1", "\"@acme/hidden\"", "add \"//pkg:hidden\" to deps"} {
		if !strings.Contains(out, want) {
			t.Errorf("the message must name %s:\n%s", want, out)
		}
	}
}

// Node calls a #-prefixed specifier a package-private import; it is the whole
// name, not a URL fragment on an empty one.
func TestTransitivePackageImportsNameIsRejected(t *testing.T) {
	c := newChecker(t)
	out, ok := c.run(
		"hash_module",
		"import { m } from \"#app/messages\";\nexport const id = m;\n",
		moduleDirect("@acme/leaf"),
		moduleTransitive("#app/messages", "//pkg:messages"),
	)
	if ok {
		t.Fatalf("a # module only a dep's dep provides was accepted:\n%s", out)
	}
	for _, want := range []string{"\"#app/messages\"", "add \"//pkg:messages\" to deps"} {
		if !strings.Contains(out, want) {
			t.Errorf("the message must name %s:\n%s", want, out)
		}
	}
}

func TestDirectPackageImportsNameIsAccepted(t *testing.T) {
	c := newChecker(t)
	if out, ok := c.run(
		"hash_direct",
		"import { m } from \"#app/messages\";\nexport const id = m;\n",
		moduleDirect("#app/messages"),
		moduleTransitive("#app/messages", "//pkg:messages"),
	); !ok {
		t.Fatalf("a # module a direct dep provides was rejected:\n%s", out)
	}
}

// A # after the first character is still a fragment.
func TestAFragmentIsStrippedFromARelativeSpecifier(t *testing.T) {
	c := newChecker(t)
	out, ok := c.run(
		"fragment",
		"import { m } from \"./nested/hidden#frag\";\nexport const id = m;\n",
		transitive("pkg/nested/hidden.d.ts", "//pkg:hidden"),
	)
	if ok {
		t.Fatalf("a fragment hid the specifier it was attached to:\n%s", out)
	}
	if !strings.Contains(out, "add \"//pkg:hidden\" to deps") {
		t.Errorf("the message must name the label:\n%s", out)
	}
}

func TestDirectModuleNameIsAccepted(t *testing.T) {
	c := newChecker(t)
	if out, ok := c.run(
		"direct_module",
		"import { hidden } from \"@acme/hidden\";\nexport const id = hidden.id;\n",
		moduleDirect("@acme/hidden"),
		moduleTransitive("@acme/hidden", "//pkg:hidden"),
	); !ok {
		t.Fatalf("a module a direct dep provides was rejected:\n%s", out)
	}
}

func TestSubpathOfADirectModuleIsAccepted(t *testing.T) {
	c := newChecker(t)
	if out, ok := c.run(
		"subpath",
		"import { deep } from \"@acme/hidden/deep\";\nexport const id = deep;\n",
		moduleDirect("@acme/hidden"),
		moduleTransitive("@acme/hidden", "//pkg:hidden"),
	); !ok {
		t.Fatalf("a subpath of a direct dep's module was rejected:\n%s", out)
	}
}

func TestTransitiveRelativeFileIsRejected(t *testing.T) {
	c := newChecker(t)
	out, ok := c.run(
		"relative",
		"import { hidden } from \"./nested/hidden\";\nexport const id = hidden;\n",
		transitive("pkg/nested/hidden.d.ts", "//pkg:hidden"),
	)
	if ok {
		t.Fatalf("a relative import only a dep's dep provides was accepted:\n%s", out)
	}
	for _, want := range []string{"\"./nested/hidden\"", "add \"//pkg:hidden\" to deps"} {
		if !strings.Contains(out, want) {
			t.Errorf("the message must name %s:\n%s", want, out)
		}
	}
}

func TestRelativeFileFromADirectDepIsAccepted(t *testing.T) {
	c := newChecker(t)
	if out, ok := c.run(
		"relative_direct",
		"import { hidden } from \"./nested/hidden\";\nexport const id = hidden;\n",
		direct("pkg/nested/hidden.d.ts"),
		transitive("pkg/nested/hidden.d.ts", "//pkg:hidden"),
	); !ok {
		t.Fatalf("a relative import a direct dep provides was rejected:\n%s", out)
	}
}

func TestOwnSourceIsAccepted(t *testing.T) {
	c := newChecker(t)
	if out, ok := c.run(
		"own_source",
		"import { sibling } from \"./sibling\";\nexport const id = sibling;\n",
		own("pkg/sibling.ts"),
		transitive("pkg/sibling.d.ts", "//pkg:elsewhere"),
	); !ok {
		t.Fatalf("a target's own source was rejected:\n%s", out)
	}
}

func TestTransitiveNpmPackageIsRejected(t *testing.T) {
	c := newChecker(t)
	out, ok := c.run(
		"npm",
		"import { expect } from \"chai\";\nexport const e = expect;\n",
		npmDirect("vitest"),
		npmTransitive("chai", "@npm//:chai"),
	)
	if ok {
		t.Fatalf("an npm package only a dep's dep provides was accepted:\n%s", out)
	}
	if !strings.Contains(out, "add \"@npm//:chai\" to deps") {
		t.Errorf("the message must name the hub label:\n%s", out)
	}
}

func TestScopedNpmPackageNamesTheHubLabel(t *testing.T) {
	c := newChecker(t)
	out, ok := c.run(
		"scoped_npm",
		"import { x } from \"@acme/widget/sub\";\nexport const y = x;\n",
		npmTransitive("@acme/widget", "@npm_acme//:acme_widget"),
	)
	if ok {
		t.Fatalf("a scoped npm package only a dep's dep provides was accepted:\n%s", out)
	}
	if !strings.Contains(out, "add \"@npm_acme//:acme_widget\" to deps") {
		t.Errorf("the hub label must be the one the closure reported:\n%s", out)
	}
}

func TestNodeBuiltinsAndAliasesAreAccepted(t *testing.T) {
	c := newChecker(t)
	source := strings.Join([]string{
		"import { readFileSync } from \"node:fs\";",
		"import { join } from \"path\";",
		"import { leaf } from \"@/leaf\";",
		"export const all = [readFileSync, join, leaf];",
	}, "\n")
	if out, ok := c.run(
		"builtins",
		source+"\n",
		"alias\t@/",
		npmTransitive("fs", "@npm//:fs"),
		moduleTransitive("@/leaf", "//pkg:leaf"),
	); !ok {
		t.Fatalf("built-ins and a path alias were rejected:\n%s", out)
	}
}

// Nothing in the closure provides it, so no label can be named: TS2307 is the
// only honest diagnostic, and reporting an unattributable specifier here would
// print a label that does not exist.
func TestUnreachableModuleIsLeftToTheCompiler(t *testing.T) {
	c := newChecker(t)
	if out, ok := c.run(
		"unreachable",
		"import { nope } from \"@acme/nowhere\";\nexport const id = nope;\n",
		moduleDirect("@acme/leaf"),
		moduleTransitive("@acme/leaf", "//pkg:leaf"),
	); !ok {
		t.Fatalf("an unattributable import must not be reported here:\n%s", out)
	}
}

func TestEveryFindingIsReportedAtOnce(t *testing.T) {
	c := newChecker(t)
	source := strings.Join([]string{
		"import { a } from \"@acme/one\";",
		"import { b } from \"@acme/two\";",
		"export const both = [a, b];",
	}, "\n")
	out, ok := c.run(
		"multiple",
		source+"\n",
		moduleTransitive("@acme/one", "//pkg:one"),
		moduleTransitive("@acme/two", "//pkg:two"),
	)
	if ok {
		t.Fatalf("two undeclared imports were accepted:\n%s", out)
	}
	for _, want := range []string{
		"//pkg:target imports modules no direct dep provides:",
		"pkg/multiple.ts:1",
		"pkg/multiple.ts:2",
		"add \"//pkg:one\" to deps",
		"add \"//pkg:two\" to deps",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the message must name %s:\n%s", want, out)
		}
	}
}
