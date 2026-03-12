package typescript

import (
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

func TestDirective_PackageBoundary_IndexOnly(t *testing.T) {
	tc := makeConfig("", []rule.Directive{
		directive(directivePackageBoundary, "index-only"),
	})
	if tc.packageBoundaryMode != boundaryIndexOnly {
		t.Errorf("ts_package_boundary index-only: got %q, want %q", tc.packageBoundaryMode, boundaryIndexOnly)
	}
}

func TestDirective_PackageBoundary_ModeInheritedByChild(t *testing.T) {
	tc := makeChildConfig(
		[]rule.Directive{directive(directivePackageBoundary, "index-only")},
		"src/lib",
		nil,
	)
	if tc.packageBoundaryMode != boundaryIndexOnly {
		t.Errorf("child should inherit index-only mode, got %q", tc.packageBoundaryMode)
	}
}

func TestDirective_PackageBoundary_ChildCanOverrideToEveryDir(t *testing.T) {
	tc := makeChildConfig(
		[]rule.Directive{directive(directivePackageBoundary, "index-only")},
		"src/lib",
		[]rule.Directive{directive(directivePackageBoundary, "every-dir")},
	)
	if tc.packageBoundaryMode != boundaryEveryDir {
		t.Errorf("child override to every-dir: got %q, want %q", tc.packageBoundaryMode, boundaryEveryDir)
	}
}

func TestDirective_PackageBoundary_EveryDirDoesNotSetPackageBoundaryFlag(t *testing.T) {
	// In every-dir mode the packageBoundary flag must NOT be set; setting it
	// would cause confusing side-effects when a sub-tree switches to index-only.
	tc := makeConfig("", []rule.Directive{
		directive(directivePackageBoundary, "every-dir"),
	})
	if tc.packageBoundary {
		t.Error("ts_package_boundary every-dir must not set packageBoundary = true")
	}
}

func TestDirective_PackageBoundary_TrueValueSetsFlag(t *testing.T) {
	// The special value "true" marks this directory as an explicit boundary,
	// which is useful in index-only mode without an index.ts.
	tc := makeConfig("", []rule.Directive{
		directive(directivePackageBoundary, "true"),
	})
	if !tc.packageBoundary {
		t.Error("ts_package_boundary true should set packageBoundary = true")
	}
}

// ---- ts_isolated_declarations directive tests ------------------------------

func TestDirective_IsolatedDeclarations_DefaultIsTrue(t *testing.T) {
	tc := makeConfig("", nil)
	if !tc.isolatedDeclarations {
		t.Error("isolatedDeclarations should default to true")
	}
}

func TestDirective_IsolatedDeclarations_FalseDisables(t *testing.T) {
	tc := makeConfig("", []rule.Directive{
		directive(directiveIsolatedDeclarations, "false"),
	})
	if tc.isolatedDeclarations {
		t.Error("ts_isolated_declarations false should set isolatedDeclarations = false")
	}
}

func TestDirective_IsolatedDeclarations_TrueExplicit(t *testing.T) {
	tc := makeConfig("", []rule.Directive{
		directive(directiveIsolatedDeclarations, "true"),
	})
	if !tc.isolatedDeclarations {
		t.Error("ts_isolated_declarations true should set isolatedDeclarations = true")
	}
}

func TestDirective_IsolatedDeclarations_InheritedByChild(t *testing.T) {
	tc := makeChildConfig(
		[]rule.Directive{directive(directiveIsolatedDeclarations, "false")},
		"src/lib",
		nil,
	)
	if tc.isolatedDeclarations {
		t.Error("child should inherit isolatedDeclarations = false from parent")
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

// ---- ts_exclude directive tests --------------------------------------------

func TestDirective_Exclude_Single(t *testing.T) {
	tc := makeConfig("", []rule.Directive{
		directive(directiveExclude, "*.generated.ts"),
	})
	if len(tc.excludePatterns) != 1 || tc.excludePatterns[0] != "*.generated.ts" {
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
		packageBoundaryMode:  boundaryEveryDir,
		isolatedDeclarations: true,
		pathAliases:          map[string]string{"@/": "src/"},
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
		packageBoundaryMode:  boundaryEveryDir,
		isolatedDeclarations: true,
		runtimeDepsTest:      []string{"@npm//:a"},
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
		packageBoundaryMode:  boundaryEveryDir,
		isolatedDeclarations: true,
		excludePatterns:      []string{"*.gen.ts"},
	}
	child := parent.clone()
	child.excludePatterns = append(child.excludePatterns, "*.auto.ts")

	if len(parent.excludePatterns) != 1 {
		t.Errorf("parent excludePatterns mutated: got %v, want [*.gen.ts]", parent.excludePatterns)
	}
}
