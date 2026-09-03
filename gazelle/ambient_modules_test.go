package typescript

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/bazelbuild/bazel-gazelle/config"
	"github.com/bazelbuild/bazel-gazelle/label"
	"github.com/bazelbuild/bazel-gazelle/rule"
)

// A `declare module "x"` block in a script-mode .d.ts IS the module: nothing
// installs it and nothing else exports it, so the specifier that names it needs
// no dep. The scanner is the one in ScanImports, which already refuses to read
// the declaration's own string as an import.
func TestScanAmbientModules(t *testing.T) {
	for _, tt := range []struct {
		name   string
		source string
		want   []string
	}{
		{
			name:   "block form",
			source: "declare module \"mobile\" {\n  export type AuthResult = { ok: boolean };\n}\n",
			want:   []string{"mobile"},
		},
		{
			name:   "shorthand form",
			source: "declare module \"acme-untyped\";\n",
			want:   []string{"acme-untyped"},
		},
		{
			name:   "single quotes and several blocks",
			source: "declare module 'a' {}\ndeclare module \"b\" {}\n",
			want:   []string{"a", "b"},
		},
		{
			// A wildcard stands for a bundler's asset loader. The specifiers it
			// covers are relative paths with real targets, so matching it would
			// drop the dep on the file itself.
			name:   "pattern name",
			source: "declare module \"*.svg\" {\n  const url: string;\n  export default url;\n}\n",
		},
		{
			name:   "not a declaration",
			source: "import { x } from \"mobile\";\nexport const y = x;\n",
		},
		{
			name:   "decoys in comments, strings and templates",
			source: "// declare module \"in-line-comment\"\n/* declare module \"in-block\" */\nconst s = `declare module \"in-template\"`;\nconst t = \"declare module\";\nexport const u = [s, t];\n",
		},
		{
			name:   "module keyword without declare",
			source: "module \"bare-module\" {}\n",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := ScanAmbientModules(tt.source); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ScanAmbientModules = %v, want %v", got, tt.want)
			}
		})
	}
}

// ambientRepo is the monorepo shape: //web:web compiles both the declaration
// file and the sources importing the module it declares.
func ambientRepo(t *testing.T) (*config.Config, *rule.Rule, label.Label) {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "web/shared/types/mobile.d.ts"),
		"declare module \"mobile\" {\n  export type AuthResult = { ok: boolean };\n}\n")
	writeFile(t, filepath.Join(root, "web/modules/auth/native.ts"),
		"import type { AuthResult } from \"mobile\";\nexport type R = AuthResult;\n")

	c := &config.Config{RepoRoot: root, Exts: make(map[string]interface{})}
	c.Exts[languageName] = makeConfig("", nil)

	r := rule.NewRule("ts_compile", "web")
	r.SetAttr("srcs", []string{"modules/auth/native.ts", "shared/types/mobile.d.ts"})
	return c, r, label.New("", "web", "web")
}

// The defect: `mobile` names no workspace member, no path alias and no
// installed package, so the bare-specifier ladder ended at the hub convention
// and wrote `@npm//:mobile` -- a target no hub declares, which Bazel answers
// with `no such target` during analysis and fails every target in the build.
// The declaration file in the target's own srcs is the answer: no dep at all.
func TestResolveImports_AmbientlyDeclaredModuleNeedsNoDep(t *testing.T) {
	c, r, from := ambientRepo(t)
	ix := buildIndex(t, c)

	for _, imp := range []string{"mobile", "mobile/bridge"} {
		target := rule.NewRule("ts_compile", "web")
		target.SetAttr("srcs", r.AttrStrings("srcs"))
		resolveImports(c, ix, target, []string{imp}, from)
		if got := target.AttrStrings("deps"); len(got) != 0 {
			t.Errorf("%s: deps = %v, want none", imp, got)
		}
	}
}

// Only the target that carries the declaration is exempt. A sibling package
// importing the same name has nothing declaring it and still asks the hub.
func TestResolveImports_AmbientDeclarationIsPerTarget(t *testing.T) {
	c, _, _ := ambientRepo(t)
	ix := buildIndex(t, c)

	other := rule.NewRule("ts_compile", "api")
	other.SetAttr("srcs", []string{"index.ts"})
	resolveImports(c, ix, other, []string{"mobile"}, label.New("", "api", "api"))

	want := []string{"@npm//:mobile"}
	if got := other.AttrStrings("deps"); !reflect.DeepEqual(got, want) {
		t.Errorf("deps = %v, want %v", got, want)
	}
}

// An installed package keeps its dep even when a declaration file names it:
// the lockfile says a hub target exists, and the runtime needs it.
func TestResolveImports_AmbientDeclarationDoesNotHideAnInstalledPackage(t *testing.T) {
	c, r, from := ambientRepo(t)
	tc := getConfig(c)
	tc.npmPackages = map[string]string{"mobile": ""}
	ix := buildIndex(t, c)

	resolveImports(c, ix, r, []string{"mobile"}, from)

	want := []string{"@npm//:mobile"}
	if got := r.AttrStrings("deps"); !reflect.DeepEqual(got, want) {
		t.Errorf("deps = %v, want %v", got, want)
	}
}

// A declaration's extension is not what makes it ambient: TypeScript exempts
// declaration files from the module-ness .mts forces on a source, so a `declare
// module` block in a .d.mts is read the way one in a .d.ts is.
func TestResolveImports_AmbientlyDeclaredModuleInADMts(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "web/shared/types/mobile.d.mts"),
		"declare module \"mobile\" {\n  export type AuthResult = { ok: boolean };\n}\n")
	writeFile(t, filepath.Join(root, "web/modules/auth/native.ts"),
		"import type { AuthResult } from \"mobile\";\nexport type R = AuthResult;\n")
	c := &config.Config{RepoRoot: root, Exts: make(map[string]interface{})}
	c.Exts[languageName] = makeConfig("", nil)
	ix := buildIndex(t, c)

	r := rule.NewRule("ts_compile", "web")
	r.SetAttr("srcs", []string{"modules/auth/native.ts", "shared/types/mobile.d.mts"})
	resolveImports(c, ix, r, []string{"mobile"}, label.New("", "web", "web"))
	if got := r.AttrStrings("deps"); len(got) != 0 {
		t.Errorf("deps = %v, want none: the target's own .d.mts declares the module", got)
	}
}
