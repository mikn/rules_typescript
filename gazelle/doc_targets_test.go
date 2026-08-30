package typescript

import (
	"fmt"
	"path"
	"sort"
	"strings"
	"testing"

	"github.com/bazelbuild/bazel-gazelle/rule"
)

// The shape a component library reaches on its own: switch's doc file imports
// label to demonstrate it, and label's doc file imports switch for the same
// reason. Neither component depends on the other -- only their docs do -- so a
// cycle here is one the directory-granular package boundary invented.
func docComponentLibrary() map[string]string {
	return map[string]string{
		"package.json":  `{"name":"design-system","dependencies":{}}` + "\n",
		"tsconfig.json": `{"compilerOptions":{"strict":true}}` + "\n",

		"components/inputs/label/index.ts":  "export * from \"./label\";\n",
		"components/inputs/label/label.tsx": "export const Label = () => null;\n",
		"components/inputs/label/label.doc.tsx": "import { Label } from \"./label\";\n" +
			"import { Switch } from \"../switch\";\n" +
			"export const LabelDoc = () => [Label(), Switch()];\n",

		"components/inputs/switch/index.ts":   "export * from \"./switch\";\n",
		"components/inputs/switch/switch.tsx": "export const Switch = () => null;\n",
		"components/inputs/switch/switch.doc.tsx": "import { Switch } from \"./switch\";\n" +
			"import { Label } from \"../label\";\n" +
			"export const SwitchDoc = () => [Switch(), Label()];\n",
	}
}

func TestDocFilesDoNotCycleBetweenComponents(t *testing.T) {
	repoRoot := t.TempDir()
	writeWorkspace(t, repoRoot, docComponentLibrary())
	captureLog(t, func() { convergeGazelle(t, repoRoot) })

	if cycle := targetCycle(t, repoRoot); cycle != "" {
		t.Errorf("the generated targets contain a dependency cycle, which fails analysis for "+
			"the whole workspace:\n      %s", cycle)
	}

	compiled := compiledSources(t, repoRoot)
	for _, doc := range []string{
		"components/inputs/label/label.doc.tsx",
		"components/inputs/switch/switch.doc.tsx",
	} {
		claims := compiled[doc]
		if len(claims) == 0 {
			t.Errorf("%s is in no target's srcs, so nothing type-checks it", doc)
			continue
		}
		for _, c := range claims {
			if c.kind != "ts_compile" {
				t.Errorf("%s is compiled by %s, a %s; a doc file is not executable",
					doc, c.label, c.kind)
			}
		}
	}
}

// Outside every-dir mode the files of a plain subdirectory roll up into the
// nearest package, so the roll-up walk -- not the directory listing -- is what
// finds them. A doc file it reaches has to land in the doc target all the same:
// in the package target it is the cycle this split exists to prevent, and in no
// target at all nothing type-checks it. Both modes that roll up have to agree.
func TestDocFilesRolledUpFromASubdirectory(t *testing.T) {
	cases := []struct {
		mode  string
		files map[string]string
	}{
		{
			mode: "index-only",
			files: map[string]string{
				"pkg/index.ts": "export { Checkbox } from \"./widget/checkbox\";\n",
			},
		},
		{
			mode: "tsconfig",
			files: map[string]string{
				"pkg/tsconfig.json": `{"compilerOptions":{"strict":true}}` + "\n",
				"pkg/entry.ts":      "export { Checkbox } from \"./widget/checkbox\";\n",
			},
		},
	}
	for _, c := range cases {
		t.Run(c.mode, func(t *testing.T) {
			files := map[string]string{
				"pkg/widget/checkbox.tsx":         "export const Checkbox = () => null;\n",
				"pkg/widget/checkbox.doc.tsx":     "export * from \"./checkbox\";\n",
				"pkg/widget/checkbox.stories.tsx": "export * from \"./checkbox\";\n",
				"pkg/widget/checkbox.css":         ".checkbox { color: red }\n",
			}
			for name, content := range c.files {
				files[name] = content
			}
			res := generateTree(t, files,
				[]rule.Directive{directive(directivePackageBoundary, c.mode)}, "pkg")

			claims := map[string][]string{}
			for _, r := range res.Gen {
				for _, s := range r.AttrStrings("srcs") {
					claims[s] = append(claims[s], r.Name())
				}
			}
			for _, doc := range []string{
				"widget/checkbox.doc.tsx",
				"widget/checkbox.stories.tsx",
			} {
				if got := claims[doc]; len(got) != 1 || got[0] != "pkg_doc" {
					t.Errorf("%s is claimed by %v, want [pkg_doc]", doc, got)
				}
			}
			if got := claims["widget/checkbox.tsx"]; len(got) != 1 || got[0] != "pkg" {
				t.Errorf("widget/checkbox.tsx is claimed by %v, want [pkg]: the split moves "+
					"the doc files out of the package target and nothing else", got)
			}
			if len(claims["widget/checkbox.css"]) != 1 {
				t.Errorf("widget/checkbox.css is claimed by %v, want exactly one target: the "+
					"doc split must leave the rest of the roll-up alone",
					claims["widget/checkbox.css"])
			}
		})
	}
}

// The first shape holding both classifications at once: a rolled-up subtree with
// an ambient declaration beside a story file. The two groups pull in opposite
// directions -- nothing imports a declaration, so it reaches the test only
// through srcs membership in both targets, while a story has to leave the
// package target, which is where demonstrating a sibling becomes a cycle.
func TestRolledUpAmbientDeclarationAndStorySplitApart(t *testing.T) {
	res := generateTree(t, map[string]string{
		"pkg/package.json": `{"name":"pkg"}`,
		"pkg/tsconfig.json": `{
  "compilerOptions": { "strict": true },
  "include": ["src/**/*.ts", "src/**/*.tsx"]
}`,
		"pkg/src/globals.d.ts":      "declare type ZzTheme = { color: string };\n",
		"pkg/src/badge.tsx":         "export const Badge = (t: ZzTheme) => t.color;\n",
		"pkg/src/badge.stories.tsx": "import { Badge } from \"./badge\";\nexport const Primary = Badge;\n",
		"pkg/src/badge.test.tsx":    "import { Badge } from \"./badge\";\nexport const t = Badge;\n",
	}, []rule.Directive{directive(directivePackageBoundary, "tsconfig")}, "pkg")

	srcs := map[string][]string{}
	for _, r := range res.Gen {
		srcs[r.Name()] = r.AttrStrings("srcs")
	}
	for _, name := range []string{"pkg", "pkg_test", "pkg_doc"} {
		if _, ok := srcs[name]; !ok {
			t.Fatalf("no %s target was generated, got %v", name, generatedNames(t, res))
		}
	}

	// A .d.ts is passed through rather than compiled, so it declares no outputs
	// and every program that needs the globals can name it -- which the doc
	// target does need: a story reaching a global has no import to carry it.
	const decl = "src/globals.d.ts"
	for _, name := range []string{"pkg", "pkg_test", "pkg_doc"} {
		if n := count(srcs[name], decl); n != 1 {
			t.Errorf("%s srcs %v: %s listed %d times, want exactly 1", name, srcs[name], decl, n)
		}
	}

	const story = "src/badge.stories.tsx"
	if n := count(srcs["pkg_doc"], story); n != 1 {
		t.Errorf("pkg_doc srcs %v: %s listed %d times, want exactly 1", srcs["pkg_doc"], story, n)
	}
	for _, name := range []string{"pkg", "pkg_test"} {
		if count(srcs[name], story) != 0 {
			t.Errorf("%s srcs %v claim %s, which the doc target exists to hold",
				name, srcs[name], story)
		}
	}
}

func count(haystack []string, needle string) int {
	n := 0
	for _, s := range haystack {
		if s == needle {
			n++
		}
	}
	return n
}

// ---- the generated graph ----------------------------------------------------

type srcClaim struct {
	label string
	kind  string
}

// compiledSources maps every workspace-relative source a type-checking target
// declares to the targets declaring it.
func compiledSources(t *testing.T, repoRoot string) map[string][]srcClaim {
	t.Helper()
	out := map[string][]srcClaim{}
	for _, pkg := range convergePackages(t, repoRoot) {
		for _, r := range loadRules(t, repoRoot, pkg) {
			if r.Kind() != "ts_compile" && r.Kind() != "ts_test" {
				continue
			}
			for _, src := range r.AttrStrings("srcs") {
				rel := path.Join(pkg, src)
				out[rel] = append(out[rel], srcClaim{canonicalLabel(pkg, ":"+r.Name()), r.Kind()})
			}
		}
	}
	return out
}

// targetCycle returns a printable cycle through the generated deps, or "" when
// the graph is acyclic.
func targetCycle(t *testing.T, repoRoot string) string {
	t.Helper()
	graph := map[string][]string{}
	var nodes []string
	for _, pkg := range convergePackages(t, repoRoot) {
		for _, r := range loadRules(t, repoRoot, pkg) {
			from := canonicalLabel(pkg, ":"+r.Name())
			nodes = append(nodes, from)
			for _, v := range attrValues(r, "deps") {
				if isWorkspaceLabel(v) {
					graph[from] = append(graph[from], canonicalLabel(pkg, v))
				}
			}
		}
	}
	sort.Strings(nodes)

	const (
		onStack = 1
		done    = 2
	)
	state := map[string]int{}
	var stack, cycle []string

	var visit func(string)
	visit = func(n string) {
		if cycle != nil || state[n] == done {
			return
		}
		if state[n] == onStack {
			for i, s := range stack {
				if s == n {
					cycle = append(append([]string{}, stack[i:]...), n)
					return
				}
			}
			return
		}
		state[n] = onStack
		stack = append(stack, n)
		for _, next := range graph[n] {
			visit(next)
		}
		stack = stack[:len(stack)-1]
		state[n] = done
	}
	for _, n := range nodes {
		visit(n)
	}
	return strings.Join(cycle, " -> ")
}

func canonicalLabel(pkg, lbl string) string {
	targetPkg, name := splitLabel(absLabel(pkg, lbl))
	return fmt.Sprintf("//%s:%s", targetPkg, name)
}

// A test that composes a story runs the story's npm imports. ts_test builds its
// runtime node_modules from its own @npm// deps, so a package's npm imports are
// collected into them -- and the doc files are still that package.
func TestDocNpmImportsReachTheTestTarget(t *testing.T) {
	repoRoot := t.TempDir()
	writeWorkspace(t, repoRoot, map[string]string{
		"package.json":  `{"name":"design-system","dependencies":{"storybook":"9.0.0"}}` + "\n",
		"tsconfig.json": `{"compilerOptions":{"strict":true}}` + "\n",

		"button/button.tsx": "export const Button = () => null;\n",
		"button/button.stories.tsx": "import \"storybook\";\n" +
			"import { Button } from \"./button\";\nexport const Primary = Button;\n",
		"button/button.test.tsx": "import { Primary } from \"./button.stories\";\n" +
			"export const t = Primary;\n",
	})
	captureLog(t, func() { convergeGazelle(t, repoRoot) })

	const want = "@npm//:storybook"
	for _, r := range loadRules(t, repoRoot, "button") {
		if r.Kind() != "ts_test" {
			continue
		}
		if deps := attrValues(r, "deps"); !contains(deps, want) {
			t.Errorf("%s declares deps %v, without %s: the story it composes cannot "+
				"resolve that package in the generated node_modules tree", r.Name(), deps, want)
		}
		return
	}
	t.Error("no ts_test target was generated, so nothing pins the runtime tree")
}

// A generator can declare a story file, and the file stays checked in until the
// day it does not. Both roles at once is the pair Bazel rejects outright, so the
// claim that keeps a declared out of ts_compile.srcs has to reach the doc target
// too -- on the directory's own files and on the ones a boundary rolls up.
func TestCodegenOutIsNotAlsoADocSrc(t *testing.T) {
	for _, tc := range []struct {
		name     string
		build    string
		files    map[string]string
		declared string
		handIn   string
	}{
		{
			name:  "own directory",
			build: "# gazelle:ts_codegen stories_gen //tools:storygen widget.stories.tsx srcs:widget.json --out {out}\n",
			files: map[string]string{
				"widget.tsx":         "export const Widget = () => null;\n",
				"widget.json":        "{}\n",
				"widget.stories.tsx": "export const Generated = 1;\n",
				"card.stories.tsx":   "export const Card = 1;\n",
			},
			declared: "widget.stories.tsx",
			handIn:   "card.stories.tsx",
		},
		{
			name: "rolled up below the boundary",
			build: "# gazelle:ts_package_boundary index-only\n" +
				"# gazelle:ts_codegen stories_gen //tools:storygen sub/widget.stories.tsx srcs:tokens.json --out {out}\n",
			files: map[string]string{
				"index.ts":               "export const a = 1;\n",
				"tokens.json":            "{}\n",
				"sub/widget.stories.tsx": "export const Generated = 1;\n",
				"sub/card.stories.tsx":   "export const Card = 1;\n",
			},
			declared: "sub/widget.stories.tsx",
			handIn:   "sub/card.stories.tsx",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := runGenerateWithBuild(t, "api", tc.build, tc.files)

			gen := generatedRule(res, "stories_gen")
			if gen == nil {
				t.Fatalf("no ts_codegen for the directive; generated %v", generatedNames(t, res))
			}
			if outs := gen.AttrStrings("outs"); !contains(outs, tc.declared) {
				t.Errorf("ts_codegen outs = %v, without %s", outs, tc.declared)
			}

			doc := generatedRule(res, "api_doc")
			if doc == nil {
				t.Fatalf("no ts_compile named api_doc; generated %v", generatedNames(t, res))
			}
			srcs := doc.AttrStrings("srcs")
			if contains(srcs, tc.declared) {
				t.Errorf("api_doc srcs = %v: %s is a source of the package and an out of "+
					"ts_codegen stories_gen, which Bazel rejects", srcs, tc.declared)
			}
			if !contains(srcs, tc.handIn) {
				t.Errorf("api_doc srcs = %v, without the hand-written %s", srcs, tc.handIn)
			}
			for _, r := range res.Gen {
				if r.Kind() != "ts_codegen" && contains(r.AttrStrings("srcs"), tc.declared) {
					t.Errorf("%s %s compiles %s, which ts_codegen stories_gen declares as an out",
						r.Kind(), r.Name(), tc.declared)
				}
			}
		})
	}
}

// ---- the compilerOptions baseline ------------------------------------------

// A story is TypeScript in its package, so it needs the package's own lib,
// types and strictness the way its sources do. The ruleset's baseline supplies
// five options and nothing else, and it sits *under* the user's file: a doc
// target that named no tsconfig would not merely miss `lib` and `types`, it
// would compile under a `strict` the package may have turned off -- stricter
// than the code it demonstrates, and failing where its own siblings pass.
//
// The label is the one the package's other targets name, resolved once, so a
// directory the baseline is refused for refuses it for the doc target too
// rather than leaving it naming a label the package itself declined.
func TestTsConfigBaselineReachesTheDocTarget(t *testing.T) {
	t.Run("beside the package targets", func(t *testing.T) {
		root := t.TempDir()
		writeWorkspace(t, root, map[string]string{
			"package.json":                     `{"name":"w"}` + "\n",
			"pnpm-workspace.yaml":              "packages:\n  - packages/*\n",
			"packages/core/package.json":       `{"name":"@w/core"}` + "\n",
			"packages/core/tsconfig.json":      `{"compilerOptions":{"lib":["es2022"],"jsx":"react-jsx"}}` + "\n",
			"packages/core/src/index.ts":       "export * from \"./badge\";\n",
			"packages/core/src/badge.tsx":      "export const Badge = () => null;\n",
			"packages/core/src/badge.test.tsx": "import { Badge } from \"./badge\";\nexport const t = Badge;\n",
			"packages/core/src/badge.stories.tsx": "import { Badge } from \"./badge\";\n" +
				"export const Primary = Badge;\n",
		})
		captureLog(t, func() { convergeGazelle(t, root) })

		want := "//packages/core:" + tsConfigTargetName
		for _, target := range []struct{ kind, name string }{
			{"ts_compile", "src"},
			{"ts_test", "src_test"},
			{"ts_compile", "src_doc"},
		} {
			got, ok := attrOf(t, root, "packages/core/src", target.kind, target.name, "tsconfig")
			if !ok {
				t.Fatalf("generation wrote no %s(%s) in packages/core/src", target.kind, target.name)
			}
			if got != want {
				t.Errorf("%s(%s).tsconfig = %q, want %q -- the story compiles under the "+
					"ruleset's baseline while its own package's sources compile under %s",
					target.kind, target.name, got, want, want)
			}
		}
		if dangling := danglingLabels(t, root); len(dangling) > 0 {
			t.Errorf("%d label(s) no target satisfies:\n      %s",
				len(dangling), strings.Join(dangling, "\n      "))
		}
	})

	// The doc target alone in its directory: no ts_compile, no ts_test and no
	// framework entry, so nothing else in the package asks for the baseline and
	// the doc target is the only reason to resolve it at all.
	t.Run("a directory holding only stories", func(t *testing.T) {
		root := t.TempDir()
		writeWorkspace(t, root, map[string]string{
			"package.json":            `{"name":"w"}` + "\n",
			"tsconfig.json":           `{"compilerOptions":{"lib":["es2022"]}}` + "\n",
			"lib/greeting.ts":         "export const greeting = 1;\n",
			"gallery/tour.stories.ts": "export * from \"../lib/greeting\";\n",
		})
		captureLog(t, func() { convergeGazelle(t, root) })

		if _, ok := attrOf(t, root, "gallery", "ts_compile", "gallery", "srcs"); ok {
			t.Fatal("gallery holds only a story, so there should be no package target there")
		}
		want := "//:" + tsConfigTargetName
		got, ok := attrOf(t, root, "gallery", "ts_compile", "gallery_doc", "tsconfig")
		if !ok {
			t.Fatalf("generation wrote no ts_compile(gallery_doc) in gallery, got:\n%s",
				buildFileText(t, root, "gallery"))
		}
		if got != want {
			t.Errorf("ts_compile(gallery_doc).tsconfig = %q, want %q -- a doc target is a "+
				"reason to resolve the baseline, so the guard has to count doc files", got, want)
		}
		if dangling := danglingLabels(t, root); len(dangling) > 0 {
			t.Errorf("%d label(s) no target satisfies:\n      %s",
				len(dangling), strings.Join(dangling, "\n      "))
		}
	})

	// The refusal cases are the package's, not the target's: a BUILD file
	// written into packages/core to hold the ts_config would take
	// packages/core/other out of the target above it, so nobody here names the
	// file -- and the doc target has to be one of the nobodies. Resolving the
	// label a second time for the doc target on its own is what this catches.
	t.Run("refused for the package refuses for the doc target", func(t *testing.T) {
		root := t.TempDir()
		writeWorkspace(t, root, map[string]string{
			"BUILD.bazel":                        "# gazelle:ts_package_boundary index-only\n",
			"package.json":                       `{"name":"w"}` + "\n",
			"packages/core/tsconfig.json":        `{"compilerOptions":{"lib":["es2022"]}}` + "\n",
			"packages/core/src/index.ts":         "export * from \"./badge\";\n",
			"packages/core/src/badge.ts":         "export const Badge = 1;\n",
			"packages/core/src/badge.stories.ts": "export * from \"./badge\";\n",
			"packages/core/other/util.ts":        "export const util = 1;\n",
		})
		logged := captureLog(t, func() { convergeGazelle(t, root) })

		if hasBuildFile(root, "packages/core") {
			t.Error("generation made packages/core a package in a roll-up mode, which drops " +
				"every source beneath it from the target above")
		}
		for _, name := range []string{"src", "src_doc"} {
			if got, _ := attrOf(t, root, "packages/core/src", "ts_compile", name, "tsconfig"); got != "" {
				t.Errorf("ts_compile(%s).tsconfig = %q, want no attribute: nothing writes "+
					"that package", name, got)
			}
		}
		if _, ok := attrOf(t, root, "packages/core/src", "ts_compile", "src_doc", "srcs"); !ok {
			t.Fatalf("generation wrote no ts_compile(src_doc), so the story is compiled by "+
				"nothing:\n%s", buildFileText(t, root, "packages/core/src"))
		}
		if !strings.Contains(logged, "packages/core") {
			t.Errorf("the refusal was silent; log said:\n%s", logged)
		}
		if dangling := danglingLabels(t, root); len(dangling) > 0 {
			t.Errorf("%d label(s) no target satisfies:\n      %s",
				len(dangling), strings.Join(dangling, "\n      "))
		}
	})
}
