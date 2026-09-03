package typescript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bazelbuild/bazel-gazelle/config"
	"github.com/bazelbuild/bazel-gazelle/rule"
)

// ---- helper: build a config.Config with directives applied -----------------

// makeConfig creates a fresh config.Config and runs configureTsConfig with
// the provided directives in a fake BUILD file.
func makeConfig(rel string, directives []rule.Directive) *tsConfig {
	c := &config.Config{
		RepoRoot: "/tmp/fake-repo",
		Exts:     make(map[string]interface{}),
	}
	var f *rule.File
	if len(directives) > 0 {
		f = rule.EmptyFile("BUILD.bazel", rel)
		f.Directives = directives
	}
	configureTsConfig(c, rel, f)
	return getConfig(c)
}

// makeChildConfig simulates a parent directory config followed by a child
// directory config. The parent directives are applied at rel="", the child
// at the provided childRel.
func makeChildConfig(parentDirectives []rule.Directive, childRel string, childDirectives []rule.Directive) *tsConfig {
	c := &config.Config{
		RepoRoot: "/tmp/fake-repo",
		Exts:     make(map[string]interface{}),
	}

	// Apply parent config at root.
	var parentFile *rule.File
	if len(parentDirectives) > 0 {
		parentFile = rule.EmptyFile("BUILD.bazel", "")
		parentFile.Directives = parentDirectives
	}
	configureTsConfig(c, "", parentFile)

	// Apply child config.
	var childFile *rule.File
	if len(childDirectives) > 0 {
		childFile = rule.EmptyFile("BUILD.bazel", childRel)
		childFile.Directives = childDirectives
	}
	configureTsConfig(c, childRel, childFile)
	return getConfig(c)
}

// directive is a convenience constructor.
func directive(key, value string) rule.Directive {
	return rule.Directive{Key: key, Value: value}
}

// ---- ts_package_boundary directive tests -----------------------------------

func TestDirective_PackageBoundary_DefaultIsEveryDir(t *testing.T) {
	tc := makeConfig("", nil)
	if tc.packageBoundaryMode != boundaryEveryDir {
		t.Errorf("default packageBoundaryMode: got %q, want %q", tc.packageBoundaryMode, boundaryEveryDir)
	}
}

func TestDirective_PackageBoundary_EveryDir(t *testing.T) {
	tc := makeConfig("", []rule.Directive{
		directive(directivePackageBoundary, "every-dir"),
	})
	if tc.packageBoundaryMode != boundaryEveryDir {
		t.Errorf("ts_package_boundary every-dir: got %q, want %q", tc.packageBoundaryMode, boundaryEveryDir)
	}
}

func TestDirective_PackageBoundary_TsConfig(t *testing.T) {
	tc := makeConfig("", []rule.Directive{
		directive(directivePackageBoundary, "tsconfig"),
	})
	if tc.packageBoundaryMode != boundaryTsConfig {
		t.Errorf("ts_package_boundary tsconfig: got %q, want %q", tc.packageBoundaryMode, boundaryTsConfig)
	}
}

func TestDirective_PackageBoundary_ModeInheritedByChild(t *testing.T) {
	tc := makeChildConfig(
		[]rule.Directive{directive(directivePackageBoundary, "tsconfig")},
		"src/lib",
		nil,
	)
	if tc.packageBoundaryMode != boundaryTsConfig {
		t.Errorf("child should inherit tsconfig mode, got %q", tc.packageBoundaryMode)
	}
}

func TestDirective_PackageBoundary_ChildCanOverrideToEveryDir(t *testing.T) {
	tc := makeChildConfig(
		[]rule.Directive{directive(directivePackageBoundary, "tsconfig")},
		"src/lib",
		[]rule.Directive{directive(directivePackageBoundary, "every-dir")},
	)
	if tc.packageBoundaryMode != boundaryEveryDir {
		t.Errorf("child override to every-dir: got %q, want %q", tc.packageBoundaryMode, boundaryEveryDir)
	}
}

func TestDirective_PackageBoundary_EveryDirDoesNotSetPackageBoundaryFlag(t *testing.T) {
	// In every-dir mode the flag has nothing to mark; setting it would change
	// what a subtree that switches to tsconfig mode below it claims.
	tc := makeConfig("", []rule.Directive{
		directive(directivePackageBoundary, "every-dir"),
	})
	if tc.packageBoundary {
		t.Error("ts_package_boundary every-dir must not set packageBoundary = true")
	}
}

func TestDirective_PackageBoundary_TrueValueSetsFlag(t *testing.T) {
	tc := makeConfig("", []rule.Directive{
		directive(directivePackageBoundary, "true"),
	})
	if !tc.packageBoundary {
		t.Error("ts_package_boundary true should set packageBoundary = true")
	}
	if tc.packageBoundaryMode != boundaryEveryDir {
		t.Errorf("\"true\" named a mode: got %q, want the inherited %q",
			tc.packageBoundaryMode, boundaryEveryDir)
	}
}

func TestDirective_PackageBoundary_KnownValues(t *testing.T) {
	for _, tt := range []struct {
		value    string
		mode     string
		marksDir bool
	}{
		{"", boundaryEveryDir, false},
		{"every-dir", boundaryEveryDir, false},
		{"  tsconfig  ", boundaryTsConfig, false},
		{"true", "", true},
	} {
		mode, marksDir, err := boundaryFromDirective(tt.value)
		if err != nil {
			t.Fatalf("ts_package_boundary %q: %v", tt.value, err)
		}
		if mode != tt.mode || marksDir != tt.marksDir {
			t.Errorf("ts_package_boundary %q = (%q, %v), want (%q, %v)",
				tt.value, mode, marksDir, tt.mode, tt.marksDir)
		}
	}
}

// A value the ruleset does not know is an error, not the inherited mode: a
// directive that quietly does nothing leaves a tree compiling to something
// other than what its author wrote. The refusal reaching the run is
// //tests/gazelle_binary's, which can observe the exit status.
func TestDirective_PackageBoundary_UnknownValueIsAnError(t *testing.T) {
	for _, tt := range []struct {
		value string
		says  []string
	}{
		{"index-only", []string{"index-only", "was removed", boundaryEveryDir, boundaryTsConfig}},
		{"every_dir", []string{"unknown", "every_dir", boundaryEveryDir, boundaryTsConfig}},
		{"tsconfig.json", []string{"unknown", "tsconfig.json"}},
	} {
		t.Run(tt.value, func(t *testing.T) {
			_, _, err := boundaryFromDirective(tt.value)
			if err == nil {
				t.Fatalf("ts_package_boundary %q was accepted", tt.value)
			}
			for _, want := range tt.says {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the error does not mention %q: %v", want, err)
				}
			}
		})
	}
}

// ---- ts_declarations directive tests ---------------------------------------

func TestDirective_Declarations_DefaultIsTsgo(t *testing.T) {
	tc := makeConfig("", nil)
	if tc.declarations != "tsgo" {
		t.Errorf("declarations should default to \"tsgo\", got %q", tc.declarations)
	}
}

func TestDirective_Declarations_Oxc(t *testing.T) {
	tc := makeConfig("", []rule.Directive{
		directive(directiveDeclarations, "oxc"),
	})
	if tc.declarations != "oxc" {
		t.Errorf("ts_declarations oxc should set declarations = \"oxc\", got %q", tc.declarations)
	}
}

func TestDirective_Declarations_TsgoExplicit(t *testing.T) {
	tc := makeConfig("", []rule.Directive{
		directive(directiveDeclarations, "tsgo"),
	})
	if tc.declarations != "tsgo" {
		t.Errorf("ts_declarations tsgo should set declarations = \"tsgo\", got %q", tc.declarations)
	}
}

// An unrecognised value must not silently pick an emitter: a typo that flipped
// a tree to oxc would demand explicit types on every export with no diagnostic
// pointing at the directive.
func TestDirective_Declarations_InvalidValueKeepsPrevious(t *testing.T) {
	tc := makeConfig("", []rule.Directive{
		directive(directiveDeclarations, "swc"),
	})
	if tc.declarations != "tsgo" {
		t.Errorf("invalid ts_declarations value should keep the previous emitter, got %q", tc.declarations)
	}
}

func TestDirective_Declarations_InheritedByChild(t *testing.T) {
	tc := makeChildConfig(
		[]rule.Directive{directive(directiveDeclarations, "oxc")},
		"src/lib",
		nil,
	)
	if tc.declarations != "oxc" {
		t.Errorf("child should inherit declarations = \"oxc\" from parent, got %q", tc.declarations)
	}
}

// ---- ts_path_alias directive tests -----------------------------------------

func TestDirective_PathAlias_SingleAlias(t *testing.T) {
	tc := makeConfig("", []rule.Directive{
		directive(directivePathAlias, "@/ src/"),
	})
	if tc.pathAliases == nil {
		t.Fatal("pathAliases should not be nil")
	}
	if got := tc.pathAliases["@/"]; got != "src/" {
		t.Errorf("pathAliases[\"@/\"]: got %q, want %q", got, "src/")
	}
}

func TestDirective_PathAlias_MultipleAliases(t *testing.T) {
	tc := makeConfig("", []rule.Directive{
		directive(directivePathAlias, "@/ src/"),
		directive(directivePathAlias, "@components/ src/components/"),
	})
	if len(tc.pathAliases) != 2 {
		t.Fatalf("expected 2 path aliases, got %d: %v", len(tc.pathAliases), tc.pathAliases)
	}
	if got := tc.pathAliases["@/"]; got != "src/" {
		t.Errorf("pathAliases[\"@/\"]: got %q, want %q", got, "src/")
	}
	if got := tc.pathAliases["@components/"]; got != "src/components/" {
		t.Errorf("pathAliases[\"@components/\"]: got %q, want %q", got, "src/components/")
	}
}

func TestDirective_PathAlias_MergesWithInheritedAliases(t *testing.T) {
	// Parent sets one alias; child adds a new one.  Both should be present in
	// the child because directives merge with (not replace) inherited aliases.
	tc := makeChildConfig(
		[]rule.Directive{directive(directivePathAlias, "@/ src/")},
		"sub",
		[]rule.Directive{directive(directivePathAlias, "@utils/ utils/")},
	)
	// Child should have BOTH the parent's alias AND its own new alias.
	if got := tc.pathAliases["@/"]; got != "src/" {
		t.Errorf("parent alias should be preserved in child: pathAliases[\"@/\"]: got %q, want %q", got, "src/")
	}
	if got := tc.pathAliases["@utils/"]; got != "utils/" {
		t.Errorf("child alias not present: pathAliases[\"@utils/\"]: got %q, want %q", got, "utils/")
	}
}

func TestDirective_PathAlias_ChildCanOverrideParentKey(t *testing.T) {
	// When child sets the same alias key as the parent, the child's value wins.
	tc := makeChildConfig(
		[]rule.Directive{directive(directivePathAlias, "@/ src/")},
		"sub",
		[]rule.Directive{directive(directivePathAlias, "@/ override/")},
	)
	if got := tc.pathAliases["@/"]; got != "override/" {
		t.Errorf("child should override same key: pathAliases[\"@/\"]: got %q, want %q", got, "override/")
	}
}

// ---- ts_runtime_dep directive tests ----------------------------------------

func TestDirective_RuntimeDep_Single(t *testing.T) {
	tc := makeConfig("", []rule.Directive{
		directive(directiveRuntimeDep, "@npm//:happy-dom"),
	})
	if len(tc.runtimeDepsTest) != 1 || tc.runtimeDepsTest[0] != "@npm//:happy-dom" {
		t.Errorf("runtimeDepsTest: got %v, want [@npm//:happy-dom]", tc.runtimeDepsTest)
	}
}

func TestDirective_RuntimeDep_Multiple(t *testing.T) {
	tc := makeConfig("", []rule.Directive{
		directive(directiveRuntimeDep, "@npm//:happy-dom"),
		directive(directiveRuntimeDep, "@npm//:vitest_coverage_v8"),
	})
	if len(tc.runtimeDepsTest) != 2 {
		t.Fatalf("runtimeDepsTest: got %d items, want 2: %v", len(tc.runtimeDepsTest), tc.runtimeDepsTest)
	}
}

func TestDirective_RuntimeDep_AppendedToParent(t *testing.T) {
	tc := makeChildConfig(
		[]rule.Directive{directive(directiveRuntimeDep, "@npm//:happy-dom")},
		"src",
		[]rule.Directive{directive(directiveRuntimeDep, "@npm//:vitest_coverage_v8")},
	)
	// Child appends to parent's list.
	if len(tc.runtimeDepsTest) != 2 {
		t.Fatalf("runtimeDepsTest should have 2 items (parent + child), got %d: %v", len(tc.runtimeDepsTest), tc.runtimeDepsTest)
	}
}

// ---- ts_ambient_types directive tests --------------------------------------

func TestDirective_AmbientTypes_Single(t *testing.T) {
	tc := makeConfig("", []rule.Directive{
		directive(directiveAmbientTypes, "@npm//:types_node"),
	})
	if len(tc.ambientTypes) != 1 || tc.ambientTypes[0] != "@npm//:types_node" {
		t.Errorf("ambientTypes: got %v, want [@npm//:types_node]", tc.ambientTypes)
	}
}

// The whole point is declaring it once at the root, so a child directory must
// inherit it without the parent's slice being mutated by the child's append.
func TestDirective_AmbientTypes_InheritsWithoutAliasing(t *testing.T) {
	parent := makeConfig("", []rule.Directive{
		directive(directiveAmbientTypes, "@npm//:types_node"),
	})
	child := parent.clone()
	child.ambientTypes = append(child.ambientTypes, "@npm//:types_react")

	if len(parent.ambientTypes) != 1 {
		t.Errorf("the child's append mutated the parent: %v", parent.ambientTypes)
	}
	if len(child.ambientTypes) != 2 || child.ambientTypes[0] != "@npm//:types_node" {
		t.Errorf("child ambientTypes: got %v, want the parent entry plus its own", child.ambientTypes)
	}
}

// ---- ts_exclude directive tests --------------------------------------------

func TestDirective_Exclude_Single(t *testing.T) {
	tc := makeConfig("", []rule.Directive{
		directive(directiveExclude, "*.generated.ts"),
	})
	if len(tc.excludePatterns) != 1 || tc.excludePatterns[0].written != "*.generated.ts" {
		t.Errorf("excludePatterns: got %v, want [*.generated.ts]", tc.excludePatterns)
	}
}

func TestDirective_Exclude_AppendedToParent(t *testing.T) {
	tc := makeChildConfig(
		[]rule.Directive{directive(directiveExclude, "*.generated.ts")},
		"src",
		[]rule.Directive{directive(directiveExclude, "*.auto.ts")},
	)
	if len(tc.excludePatterns) != 2 {
		t.Fatalf("excludePatterns should have 2 items, got %d: %v", len(tc.excludePatterns), tc.excludePatterns)
	}
}

// ---- clone isolation tests -------------------------------------------------

// Verify that modifying a child's pathAliases does not mutate the parent's map.
func TestConfig_Clone_MapIsolation_PathAliases(t *testing.T) {
	parent := &tsConfig{
		packageBoundaryMode: boundaryEveryDir,
		declarations:        "tsgo",
		pathAliases:         map[string]string{"@/": "src/"},
	}
	child := parent.clone()
	child.pathAliases["@extra/"] = "extra/"

	if len(parent.pathAliases) != 1 {
		t.Errorf("parent pathAliases mutated: got %v, want {\"@/\": \"src/\"}", parent.pathAliases)
	}
	if _, ok := parent.pathAliases["@extra/"]; ok {
		t.Error("parent pathAliases should not have @extra/ key added by child")
	}
}

// Verify that appending to a child's runtimeDepsTest does not mutate the
// parent's slice backing array.
func TestConfig_Clone_SliceIsolation_RuntimeDeps(t *testing.T) {
	parent := &tsConfig{
		packageBoundaryMode: boundaryEveryDir,
		declarations:        "tsgo",
		runtimeDepsTest:     []string{"@npm//:a"},
	}
	child := parent.clone()
	child.runtimeDepsTest = append(child.runtimeDepsTest, "@npm//:b")

	if len(parent.runtimeDepsTest) != 1 {
		t.Errorf("parent runtimeDepsTest mutated: got %v, want [@npm//:a]", parent.runtimeDepsTest)
	}
}

// Verify that appending to a child's excludePatterns does not mutate the
// parent's slice backing array.
func TestConfig_Clone_SliceIsolation_ExcludePatterns(t *testing.T) {
	parent := &tsConfig{
		packageBoundaryMode: boundaryEveryDir,
		declarations:        "tsgo",
		excludePatterns:     []excludeRule{{written: "*.gen.ts"}},
	}
	child := parent.clone()
	child.excludePatterns = append(child.excludePatterns, excludeRule{written: "*.auto.ts"})

	if len(parent.excludePatterns) != 1 {
		t.Errorf("parent excludePatterns mutated: got %v, want [*.gen.ts]", parent.excludePatterns)
	}
}

// ---- tsconfig.json paths ---------------------------------------------------

// TestLoadTsConfigPaths_CollidingPatternsPickTheSameEntryEveryTime covers two
// paths patterns that normalise to one alias key. Whichever entry wins, it has
// to be the same entry on every run: the alias map is written into generated
// path_aliases attributes and drives dep resolution.
func TestLoadTsConfigPaths_CollidingPatternsPickTheSameEntryEveryTime(t *testing.T) {
	dir := t.TempDir()
	tsConfigPath := filepath.Join(dir, "tsconfig.json")
	contents := `{
  "compilerOptions": {
    "paths": {
      "@x/*": ["wildcard/*"],
      "@x/": ["exact/dir/"],
      "@y/*": ["y/*"]
    }
  }
}`
	if err := os.WriteFile(tsConfigPath, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}

	const runs = 200
	seen := make(map[string]int)
	for i := 0; i < runs; i++ {
		aliases := loadTsConfigPaths(tsConfigPath, "")
		if got := aliases["@y/"]; got != "y/" {
			t.Fatalf("run %d: non-colliding entry changed: aliases[\"@y/\"] = %q, want %q", i, got, "y/")
		}
		seen[aliases["@x/"]]++
	}
	if len(seen) != 1 {
		t.Fatalf("colliding alias key resolved %d different ways across %d runs: %v", len(seen), runs, seen)
	}
	if _, ok := seen["wildcard/"]; !ok {
		t.Fatalf("colliding alias key resolved to %v, want the last entry in sorted pattern order (%q)", seen, "wildcard/")
	}
}
