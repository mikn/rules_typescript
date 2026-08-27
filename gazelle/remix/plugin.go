// Package remix implements the Gazelle plugin for Remix v2 file conventions.
//
// It post-processes the base TypeScript extension's GenerateResult, the way the
// tanstack plugin does, and closes two holes the base extension leaves:
//
//   - Route targets carry no route metadata. The plugin annotates each one with
//     the route id, the URL it answers on and its parent, so a generated BUILD
//     file says what the route tree is.
//   - Conventions that cannot be honoured were honoured silently or not at all.
//     app/routes.ts (future.v3_routeConfig) turns file conventions off
//     entirely, .md/.mdx route modules have no compilation path in this
//     ruleset, and appDirectory / ignoredRouteFiles / routes move the route set
//     from inside a Vite config Gazelle cannot execute. Each gets a named
//     refusal rather than a wrong route set or a silent no-op.
//
// Staging a folder route is no longer here: the framework bundle walks the
// stage directories itself, so app/routes/<name>/ gets its filegroup and its
// staging_srcs label from the base extension like any other directory.
package remix

import (
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/bazelbuild/bazel-gazelle/language"
	"github.com/bazelbuild/bazel-gazelle/rule"
	bzl "github.com/bazelbuild/buildtools/build"
)

// appDirName is the Remix application directory, the root of every route id.
const appDirName = "app"

// routeConfigBasename is the file that switches Remix from file conventions to
// a route config module, under future.v3_routeConfig.
const routeConfigBasename = "routes"

// AdjustGenerateResult post-processes the base extension's result for a Remix
// workspace. Directories outside app/ and roots without an app/ directory are
// returned unchanged.
func AdjustGenerateResult(args language.GenerateArgs, result language.GenerateResult) language.GenerateResult {
	if args.Rel == "" {
		reportUnreadOptions(args.Config.RepoRoot)
	}

	appDir := filepath.Join(args.Config.RepoRoot, appDirName)
	if info, err := os.Stat(appDir); err != nil || !info.IsDir() {
		return result
	}

	if args.Rel == "" {
		routes, diags := Manifest(appDir)
		reportRefusals(appDir, routes)
		reportDiagnostics(diags)
		return result
	}

	if args.Rel != appDirName && !strings.HasPrefix(args.Rel, appDirName+"/") {
		return result
	}

	routes, _ := Manifest(appDir)
	return annotateRouteTargets(args, result, routes)
}

// ---- annotation ------------------------------------------------------------

// annotateRouteTargets comments each ts_compile with the routes its sources
// define, and rewrites that block on the rule in the BUILD file too: the merger
// never touches comments, so it would otherwise freeze at what run 1 saw.
func annotateRouteTargets(
	args language.GenerateArgs,
	result language.GenerateResult,
	routes []Route,
) language.GenerateResult {
	appRel := strings.TrimPrefix(strings.TrimPrefix(args.Rel, appDirName), "/")
	byFile := make(map[string]Route, len(routes))
	for _, r := range routes {
		byFile[r.File] = r
	}
	for _, r := range result.Gen {
		if r.Kind() != "ts_compile" {
			continue
		}
		var comments []string
		for _, src := range r.AttrStrings("srcs") {
			route, ok := byFile[path.Join(appRel, src)]
			if !ok {
				continue
			}
			comments = append(comments, routeComment(route))
		}
		sort.Strings(comments)
		for _, comment := range comments {
			r.AddComment(comment)
		}
		rewriteRouteAnnotation(args.File, r.Name(), comments)
	}
	return result
}

// routeAnnotationPrefix is what marks a comment line as this plugin's, so
// rewriting the block leaves a hand-written comment above the rule alone.
const routeAnnotationPrefix = "# Remix route "

// rewriteRouteAnnotation replaces the annotation lines above the named
// ts_compile, keeping every other comment. The rule API has no comment removal,
// so the block is written onto the parsed call expression.
func rewriteRouteAnnotation(f *rule.File, name string, want []string) {
	if f == nil {
		return
	}
	for _, existing := range f.Rules {
		if existing.Kind() != "ts_compile" || existing.Name() != name || existing.ShouldKeep() {
			continue
		}
		call := routeCallExpr(f, name)
		if call == nil {
			return
		}
		var kept []bzl.Comment
		for _, comment := range call.Comment().Before {
			if !strings.HasPrefix(comment.Token, routeAnnotationPrefix) {
				kept = append(kept, comment)
			}
		}
		for _, line := range want {
			kept = append(kept, bzl.Comment{Token: line})
		}
		call.Comment().Before = kept
		return
	}
}

// routeCallExpr finds the parsed call for a ts_compile by name. rule.Rule keeps
// its own expression private, so the file's statements are where it is reachable.
func routeCallExpr(f *rule.File, name string) *bzl.CallExpr {
	for _, stmt := range f.File.Stmt {
		call, ok := stmt.(*bzl.CallExpr)
		if !ok {
			continue
		}
		if ident, ok := call.X.(*bzl.Ident); !ok || ident.Name != "ts_compile" {
			continue
		}
		for _, arg := range call.List {
			assign, ok := arg.(*bzl.AssignExpr)
			if !ok {
				continue
			}
			lhs, ok := assign.LHS.(*bzl.Ident)
			if !ok || lhs.Name != "name" {
				continue
			}
			if str, ok := assign.RHS.(*bzl.StringExpr); ok && str.Value == name {
				return call
			}
		}
	}
	return nil
}

func routeComment(r Route) string {
	if r.Index {
		return fmt.Sprintf("# Remix route %s → %s (index, parent %s)", r.ID, r.URL(), r.ParentID)
	}
	if r.FullPath == "" {
		return fmt.Sprintf("# Remix route %s → pathless layout (parent %s)", r.ID, r.ParentID)
	}
	return fmt.Sprintf("# Remix route %s → %s (parent %s)", r.ID, r.URL(), r.ParentID)
}

// ---- refusals --------------------------------------------------------------

// reportRefusals names the conventions this plugin does not apply, rather than
// applying something else quietly.
func reportRefusals(appDir string, routes []Route) {
	if config := findConfig(appDir, routeConfigBasename); config != "" {
		log.Printf("typescript: Remix: app/%s is present, so future.v3_routeConfig defines the routes "+
			"and file conventions are off. That module is TypeScript Gazelle cannot execute, so no "+
			"route metadata was applied and no folder route was staged from conventions. "+
			"List the route directories in staging_srcs by hand.", config)
	}

	var unsupported []string
	for _, r := range routes {
		ext := path.Ext(r.File)
		if ext == ".md" || ext == ".mdx" {
			unsupported = append(unsupported, r.File)
		}
	}
	if len(unsupported) > 0 {
		sort.Strings(unsupported)
		log.Printf("typescript: Remix: %s are route modules to Remix, but this ruleset has no mdx "+
			"compilation path, so they get no ts_compile target. They are still routes: their URLs "+
			"and their effect on the route tree are real, and only their compilation is missing.",
			strings.Join(unsupported, ", "))
	}
}

// unreadOptions are the Remix plugin options that move the route set somewhere
// this plugin does not look. appDirectory relocates the whole tree, and the
// other two add and remove routes inside it.
var unreadOptions = map[string]*regexp.Regexp{
	"appDirectory":      regexp.MustCompile(`["']?appDirectory["']?\s*:`),
	"ignoredRouteFiles": regexp.MustCompile(`["']?ignoredRouteFiles["']?\s*:`),
	// The v2 routes option is a callback taking defineRoutes. Matching the bare
	// key instead would claim a resolve.alias entry named "routes" moves the
	// route set, and a refusal that is wrong is worse than the one it replaces.
	"routes": regexp.MustCompile(`["']?routes["']?\s*(:\s*(async\b|function\b|\(|defineRoutes\b)|\()`),
}

var viteConfigRE = regexp.MustCompile(`(?i)vite[-.\w]*\.config\.[cm]?[jt]s$`)

// reportUnreadOptions names the plugin options a root Vite config sets that
// Gazelle cannot honour, because honouring them means executing the config.
// appDirectory is the one that has to be said out loud: it moves app/ somewhere
// else, and everything below keys off app/ existing, so the whole plugin turns
// into a no-op that annotates nothing and stages nothing.
//
// Only a config that imports @remix-run/dev is read: "routes" is too plain a
// key to attribute to Remix anywhere else.
func reportUnreadOptions(repoRoot string) {
	entries, err := os.ReadDir(repoRoot)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !viteConfigRE.MatchString(entry.Name()) {
			continue
		}
		body, err := os.ReadFile(filepath.Join(repoRoot, entry.Name()))
		if err != nil || !strings.Contains(string(body), "@remix-run/dev") {
			continue
		}
		var found []string
		for option, pattern := range unreadOptions {
			if pattern.Match(body) {
				found = append(found, option)
			}
		}
		if len(found) == 0 {
			continue
		}
		sort.Strings(found)
		log.Printf("typescript: Remix: %s sets %s, which Gazelle does not read -- it derives the "+
			"route set from app/routes by convention rather than by running the config. The route "+
			"annotations and the staged folder routes describe the conventional route set, not the "+
			"one Remix will build. Check them by hand, or move the routes back onto conventions.",
			entry.Name(), strings.Join(found, ", "))
	}
}

func reportDiagnostics(diags []string) {
	for _, diag := range diags {
		log.Printf("typescript: Remix: %s", diag)
	}
}
