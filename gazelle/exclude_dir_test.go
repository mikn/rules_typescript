package typescript

import (
	"strings"
	"testing"
)

// A directory Gazelle should not enter has no build file to carry ts_ignore,
// and creating one to say "ignore me" is backwards. ts_exclude_dir is declared
// in an ancestor and names the basename.
func TestExcludeDir_NamedBasenameGetsNoTargets(t *testing.T) {
	files := map[string]string{
		"BUILD.bazel":          "# gazelle:ts_exclude_dir coverage\n",
		"web/app.ts":           "export const a = 1;\n",
		"web/coverage/lcov.ts": "export const c = 1;\n",
	}

	if srcs := compiledSrcs(t, generateUnder(t, files, "web/coverage")); len(srcs) != 0 {
		t.Errorf("web/coverage still got a ts_compile with srcs %v; the directive did not keep Gazelle out", srcs)
	}
	if srcs := compiledSrcs(t, generateUnder(t, files, "web")); !hasSrc(srcs, "app.ts") {
		t.Errorf("web lost its own sources to a directive naming a subdirectory: %v", srcs)
	}
}

// The point of the whole change: the effective set is the ancestor's plus this
// directory's, so it does not depend on which directory asks.
func TestExcludeDir_MergesWithAnAncestorsSet(t *testing.T) {
	files := map[string]string{
		"BUILD.bazel":                         "# gazelle:ts_exclude_dir coverage\n",
		"apps/BUILD.bazel":                    "# gazelle:ts_exclude_dir storybook-static\n",
		"apps/web/app.ts":                     "export const a = 1;\n",
		"apps/web/coverage/lcov.ts":           "export const c = 1;\n",
		"apps/web/storybook-static/iframe.ts": "export const i = 1;\n",
	}

	if srcs := compiledSrcs(t, generateUnder(t, files, "apps/web/coverage")); len(srcs) != 0 {
		t.Errorf("apps/web/coverage got srcs %v; the root's basename was replaced rather than merged", srcs)
	}
	if srcs := compiledSrcs(t, generateUnder(t, files, "apps/web/storybook-static")); len(srcs) != 0 {
		t.Errorf("apps/web/storybook-static got srcs %v; the nearer directive did not apply", srcs)
	}
	if srcs := compiledSrcs(t, generateUnder(t, files, "apps/web")); !hasSrc(srcs, "app.ts") {
		t.Errorf("apps/web lost its own sources: %v", srcs)
	}
}

// A repeated directive is how a directory names more than one basename.
func TestExcludeDir_RepeatsInOneBuildFile(t *testing.T) {
	files := map[string]string{
		"BUILD.bazel":                    "# gazelle:ts_exclude_dir coverage\n# gazelle:ts_exclude_dir storybook-static\n",
		"web/coverage/lcov.ts":           "export const c = 1;\n",
		"web/storybook-static/iframe.ts": "export const i = 1;\n",
	}

	for _, pkg := range []string{"web/coverage", "web/storybook-static"} {
		if srcs := compiledSrcs(t, generateUnder(t, files, pkg)); len(srcs) != 0 {
			t.Errorf("%s got srcs %v; only one of the two directives took effect", pkg, srcs)
		}
	}
}

// One basename is all the traversal ever compares against, so a value carrying
// a path, a glob, or a second name excludes nothing. The list shape is the one
// worth pinning: ts_js_srcs and ts_asset_declaration_type take several values,
// so an author has every reason to expect this one does too.
func TestExcludeDir_AValueThatCannotMatchIsRefusedOutLoud(t *testing.T) {
	for _, value := range []string{"web/coverage", "cover*", "coverage storybook-static", ""} {
		files := map[string]string{
			"BUILD.bazel":          "# gazelle:ts_exclude_dir " + value + "\n",
			"web/coverage/lcov.ts": "export const c = 1;\n",
		}

		var srcs []string
		logged := captureLog(t, func() {
			srcs = compiledSrcs(t, generateUnder(t, files, "web/coverage"))
		})
		if !strings.Contains(logged, "# gazelle:ts_exclude_dir") {
			t.Errorf("ts_exclude_dir %q excluded nothing and said nothing:\n%s", value, logged)
		}
		if !hasSrc(srcs, "lcov.ts") {
			t.Errorf("ts_exclude_dir %q was refused and still excluded web/coverage: %v", value, srcs)
		}
	}
}
