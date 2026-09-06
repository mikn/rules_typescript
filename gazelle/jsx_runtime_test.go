package typescript

import (
	"reflect"
	"strings"
	"testing"

	"github.com/bazelbuild/bazel-gazelle/rule"
)

// react and preact both installed, so which one a target gets is the derivation's.
const jsxRuntimeLock = `lockfileVersion: '9.0'

importers:

  .:
    dependencies:
      preact:
        specifier: 10.27.1
        version: 10.27.1
      react:
        specifier: 19.2.0
        version: 19.2.0

packages:

  preact@10.27.1:
    resolution: {integrity: sha512-aaa}

  react@19.2.0:
    resolution: {integrity: sha512-bbb}

snapshots:

  preact@10.27.1: {}

  react@19.2.0: {}
`

const importFreeIcon = "export const icon: string = <div id=\"icon\" />;\n"

func TestJsxRuntime_ImportFreeTsxTakesTheTsconfigsImportSource(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"pnpm-lock.yaml": jsxRuntimeLock,
		"tsconfig.json":  `{"compilerOptions":{"jsx":"react-jsx","jsxImportSource":"preact"}}` + "\n",
		"src/Icon.tsx":   importFreeIcon,
	})
	captureLog(t, func() { convergeGazelle(t, root) })

	got, want := depsOf(t, root, "src", "ts_compile", "src"), []string{"@npm//:preact"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("//src deps = %v, want %v: Icon.tsx imports nothing and the tsconfig names preact as the JSX runtime", got, want)
	}
}

// ts_compile compiles under its jsx_mode, react-jsx by default, whatever jsx the
// tsconfig sets; react-jsx's runtime is react's.
func TestJsxRuntime_NoImportSourceTakesReact(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"jsx without a source": {"tsconfig.json": `{"compilerOptions":{"jsx":"react-jsx"}}` + "\n"},
		"jsx preserve":         {"tsconfig.json": `{"compilerOptions":{"jsx":"preserve"}}` + "\n"},
		"no tsconfig at all":   {},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			files["pnpm-lock.yaml"] = jsxRuntimeLock
			files["src/Icon.tsx"] = importFreeIcon
			writeWorkspace(t, root, files)
			captureLog(t, func() { convergeGazelle(t, root) })

			got, want := depsOf(t, root, "src", "ts_compile", "src"), []string{"@npm//:react"}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("//src deps = %v, want %v", got, want)
			}
		})
	}
}

func TestJsxRuntime_ImportSourceComesDownTheExtendsChain(t *testing.T) {
	for name, c := range map[string]struct{ leaf, want string }{
		"inherited": {
			leaf: `{"extends":"./tsconfig.base.json"}` + "\n",
			want: "@npm//:preact",
		},
		"the leaf wins": {
			leaf: `{"extends":"./tsconfig.base.json","compilerOptions":{"jsxImportSource":"react"}}` + "\n",
			want: "@npm//:react",
		},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeWorkspace(t, root, map[string]string{
				"pnpm-lock.yaml":     jsxRuntimeLock,
				"tsconfig.base.json": `{"compilerOptions":{"jsx":"react-jsx","jsxImportSource":"preact"}}` + "\n",
				"tsconfig.json":      c.leaf,
				"src/Icon.tsx":       importFreeIcon,
			})
			captureLog(t, func() { convergeGazelle(t, root) })

			got, want := depsOf(t, root, "src", "ts_compile", "src"), []string{c.want}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("//src deps = %v, want %v", got, want)
			}
		})
	}
}

func TestJsxRuntime_NameTheLockfileLacksIsNoDep(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"BUILD.bazel":    "# gazelle:ts_warn_unresolved true\n",
		"pnpm-lock.yaml": jsxRuntimeLock,
		"tsconfig.json":  `{"compilerOptions":{"jsx":"react-jsx","jsxImportSource":"solid-js"}}` + "\n",
		"src/Icon.tsx":   importFreeIcon,
	})
	logged := captureLog(t, func() { convergeGazelle(t, root) })

	if build := generated(t, root, "src", "BUILD.bazel"); strings.Contains(build, "deps") {
		t.Errorf("a dep was written for a runtime the lockfile does not answer:\n%s", build)
	}
	if !strings.Contains(logged, `unresolved JSX runtime "solid-js/jsx-runtime" in //src:src`) {
		t.Errorf("the run did not say which runtime it wrote no dep for:\n%s", logged)
	}
}

// The srcs list is judged, not the file: a .tsx with no tag gets the dep too,
// though tsc makes the import only for a file with one.
func TestJsxRuntime_TagFreeTsxCountsAsWell(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"pnpm-lock.yaml": jsxRuntimeLock,
		"src/props.tsx":  "export type Props = { id: string };\n",
	})
	captureLog(t, func() { convergeGazelle(t, root) })

	got, want := depsOf(t, root, "src", "ts_compile", "src"), []string{"@npm//:react"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("//src deps = %v, want %v: the dep follows the .tsx extension, not a tag", got, want)
	}
}

func TestJsxRuntime_TypeScriptOnlySourcesGetNothing(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"pnpm-lock.yaml": jsxRuntimeLock,
		"tsconfig.json":  `{"compilerOptions":{"jsx":"react-jsx","jsxImportSource":"preact"}}` + "\n",
		"src/index.ts":   "export const n = 1;\n",
	})
	captureLog(t, func() { convergeGazelle(t, root) })

	if build := generated(t, root, "src", "BUILD.bazel"); strings.Contains(build, "deps") {
		t.Errorf("a JSX runtime dep was written for a target with no JSX source:\n%s", build)
	}
}

func TestJsxRuntime_ReachesTheTestTarget(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"pnpm-lock.yaml":    jsxRuntimeLock,
		"src/Icon.tsx":      importFreeIcon,
		"src/Icon.test.tsx": "export const rendered: string = <div />;\n",
	})
	captureLog(t, func() { convergeGazelle(t, root) })

	for _, target := range []struct{ kind, name string }{{"ts_compile", "src"}, {"ts_test", "src_test"}} {
		if got := depsOf(t, root, "src", target.kind, target.name); !contains(got, "@npm//:react") {
			t.Errorf("//src:%s deps = %v: a .tsx source and no runtime dep", target.name, got)
		}
	}
}

// The roundtrip fixture's shape, and tests/compiler_options/jsx's: the runtime
// is a first-party module_name target, which the index answers before the hub.
func TestJsxRuntime_FirstPartyModuleNameAnswersIt(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"pnpm-lock.yaml":             jsxRuntimeLock,
		"jsx/tsconfig.json":          `{"compilerOptions":{"jsx":"react-jsx","jsxImportSource":"@acme/jsx"}}` + "\n",
		"jsx/runtime/jsx-runtime.ts": "export function jsx(type: string): string {\n\treturn type;\n}\n",
		"jsx/runtime/BUILD.bazel":    "load(\"@rules_typescript//ts:defs.bzl\", \"ts_compile\")\n\nts_compile(\n    name = \"runtime\",\n    srcs = [\"jsx-runtime.ts\"],\n    module_name = \"@acme/jsx\",\n)\n",
		"jsx/view/Icon.tsx":          importFreeIcon,
	})
	captureLog(t, func() { convergeGazelle(t, root) })

	got, want := depsOf(t, root, "jsx/view", "ts_compile", "view"), []string{"//jsx/runtime"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("//jsx/view deps = %v, want %v", got, want)
	}
}

// tests/smoke's shape: a jsx-shim.d.ts beside the .tsx declares the module, and
// no lockfile installs react.
func TestJsxRuntime_AmbientDeclarationAnswersIt(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"src/Icon.tsx": importFreeIcon,
		"src/jsx-shim.d.ts": "declare namespace JSX {\n  type Element = string;\n  interface IntrinsicElements {\n    [name: string]: unknown;\n  }\n}\n\n" +
			"declare module \"react/jsx-runtime\" {\n  export function jsx(type: string, props: unknown): JSX.Element;\n}\n",
	})
	captureLog(t, func() { convergeGazelle(t, root) })

	if build := generated(t, root, "src", "BUILD.bazel"); strings.Contains(build, "deps") {
		t.Errorf("a dep was written for a runtime the target's own declarations answer:\n%s", build)
	}
}

func TestJsxRuntimeSpecifier(t *testing.T) {
	for name, c := range map[string]struct {
		srcs   []string
		source string
		want   string
	}{
		"ts only":                   {srcs: []string{"index.ts", "env.d.ts"}, want: ""},
		"tsx":                       {srcs: []string{"Icon.tsx"}, want: "react/jsx-runtime"},
		"a label src is not judged": {srcs: []string{":gen"}, want: ""},
		"the tsconfig's source":     {srcs: []string{"index.ts", "Icon.tsx"}, source: "preact", want: "preact/jsx-runtime"},
	} {
		tc := makeConfig("", nil)
		tc.tsconfigJsxImportSource = c.source
		r := rule.NewRule("ts_compile", "x")
		r.SetAttr("srcs", c.srcs)
		if got := jsxRuntimeSpecifier(tc, r); got != c.want {
			t.Errorf("%s: jsxRuntimeSpecifier = %q, want %q", name, got, c.want)
		}
	}
}
