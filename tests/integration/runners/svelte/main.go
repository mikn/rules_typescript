package main

import (
	"encoding/json"
	"os"
	"regexp"

	"github.com/mikn/rules_typescript/tests/integration/harness"
)

const (
	svelteDep  = `deps = ["@npm//:svelte"]`
	noSvelte   = `deps = ["@npm//:esm-env"]`
	plainArrow = `const shout = (text: string): string => text.toUpperCase();`
	tsEnum     = `enum Volume { Loud }` + "\n" + `  const shout = (text: string): string => text.toUpperCase() + Volume.Loud;`
)

var scopeClass = regexp.MustCompile(`svelte-[0-9a-z]+`)

// scope returns the class Svelte derived from the component's style block. It
// appears in both outputs, so reading it from each is how a test sees that one
// action wrote them: two actions would eventually hash different inputs.
func scope(it *harness.IT, path string) string {
	found := scopeClass.FindString(it.Read(path))
	if found == "" {
		it.Fail("%s has no svelte-<hash> scope class", path)
	}
	return found
}

func main() {
	harness.Run(harness.Config{
		Name:         "svelte",
		WorkspaceRel: "tests/integration/svelte/workspace",
		Renames:      map[string]string{"BUILD.bazel.tpl": "BUILD.bazel"},
	}, func(it *harness.IT) {
		it.MustBazel("build", "//:components", "//:components_ssr")
		it.Pass("bazel build //:components //:components_ssr")

		cardJS := it.Bin("src", "Card.svelte.js")
		cardMap := it.Bin("src", "Card.svelte.js.map")
		cardCSS := it.Bin("src", "Card.svelte.css")
		for _, path := range []string{cardJS, cardMap, cardCSS} {
			it.RequireFile(path, "%s missing — svelte_library declared it but wrote nothing", path)
		}
		it.Pass("svelte_library wrote .js, .js.map and .css for Card.svelte")

		it.RequireContains(cardJS, "acme-card-marker",
			"%s does not contain the marker from the component's markup", cardJS)
		it.RequireContains(cardJS, "svelte/internal/client",
			"%s does not import svelte/internal/client, so it is not client output", cardJS)
		it.RequireNotContains(cardJS, "svelte/internal/server",
			"%s imports the server runtime under the default generate setting", cardJS)
		it.Pass("Card.svelte compiled to client-dialect JavaScript")

		// The compiler strips types itself; anything left means it passed the
		// TypeScript through instead, which no bundler downstream would accept.
		for _, leftover := range []string{"interface Props", ": string", "count?"} {
			it.RequireNotContains(cardJS, leftover,
				"%s still contains %q from the <script lang=\"ts\"> block", cardJS, leftover)
		}
		it.Pass("the <script lang=\"ts\"> block came out as JavaScript")

		jsScope, cssScope := scope(it, cardJS), scope(it, cardCSS)
		if jsScope != cssScope {
			it.Fail("scope class differs between outputs: %s in the JS, %s in the CSS", jsScope, cssScope)
		}
		it.RequireContains(cardCSS, "rebeccapurple",
			"%s does not contain the component's declaration", cardCSS)
		it.Pass("JS and CSS agree on the scope class %s", jsScope)

		it.RequireContains(cardJS, "//# sourceMappingURL=Card.svelte.js.map",
			"%s has no sourceMappingURL comment, so its map is unreachable", cardJS)
		var sourceMap struct {
			Version int      `json:"version"`
			Sources []string `json:"sources"`
		}
		if err := json.Unmarshal([]byte(it.Read(cardMap)), &sourceMap); err != nil {
			it.Fail("%s is not JSON: %v", cardMap, err)
		}
		if sourceMap.Version != 3 || len(sourceMap.Sources) == 0 {
			it.Fail("%s is not a usable source map: version %d, %d sources",
				cardMap, sourceMap.Version, len(sourceMap.Sources))
		}
		it.Pass("the source map is version 3 and names %v", sourceMap.Sources)

		// A component with no <style> still has to produce the .css Bazel
		// declared for it, because Starlark cannot see the style block.
		plainCSS := it.Bin("src", "nested", "Plain.svelte.css")
		it.RequireFile(plainCSS, "%s missing for a component with no <style> block", plainCSS)
		if info, err := os.Stat(plainCSS); err != nil || info.Size() != 0 {
			it.Fail("%s is not empty for a component with no <style> block", plainCSS)
		}
		it.RequireContains(it.Bin("src", "nested", "Plain.svelte.js"), "acme-plain-marker",
			"the nested component's output does not come from its own source")
		it.Pass("a styleless component in a subdirectory compiled beside its source")

		badgeJS := it.Bin("src", "Badge.svelte.js")
		it.RequireContains(badgeJS, "svelte/internal/server",
			"%s does not import the server runtime, so generate = \"server\" did nothing", badgeJS)
		it.RequireNotContains(badgeJS, "svelte/internal/client",
			"%s imports the client runtime under generate = \"server\"", badgeJS)
		it.Pass("generate = \"server\" produced SSR output")

		// TypeScript the compiler cannot strip has to fail the build. Silently
		// emitting the source would put `enum` in a browser bundle.
		card := it.Path("src", "Card.svelte")
		it.Replace(card, plainArrow, tsEnum)
		enumLog, err := it.BazelLog("ts_enum.log", "build", "//:components")
		if err == nil {
			enumLog.DumpTail(30)
			it.Fail("//:components built with an enum in <script lang=\"ts\">")
		}
		if !enumLog.Contains("typescript_invalid_feature") {
			enumLog.DumpTail(30)
			it.Fail("//:components failed for some reason other than the unsupported enum")
		}
		it.Pass("an enum in <script lang=\"ts\"> fails the build by name")

		it.Replace(card, tsEnum, plainArrow)
		it.MustBazel("build", "//:components")
		it.Pass("//:components builds again once the enum is gone")

		// The compiler is not vendored, so a node_modules tree without svelte
		// has to say which dep is missing rather than fail inside Node.
		build := it.Path("BUILD.bazel")
		it.Replace(build, svelteDep, noSvelte)
		missing, err := it.BazelLog("missing_svelte.log", "build", "//:components")
		if err == nil {
			missing.DumpTail(30)
			it.Fail("//:components built from a node_modules tree with no svelte in it")
		}
		if !missing.Contains("cannot load 'svelte/compiler'") ||
			!missing.Contains(`"@npm//:svelte"`) {
			missing.DumpTail(30)
			it.Fail("the missing-svelte failure does not name the dep to add")
		}
		it.Pass("a node_modules tree without svelte fails naming the dep to add")

		it.Replace(build, noSvelte, svelteDep)
		it.MustBazel("build", "//...")
		it.Pass("bazel build //... is green again")
	})
}
