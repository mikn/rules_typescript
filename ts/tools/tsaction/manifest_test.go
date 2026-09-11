package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every field a resolver reads names the emitted file; the rest of the
// manifest, its key order included, is what the member wrote.
func TestManifestAsBuilt(t *testing.T) {
	for _, c := range []struct{ shape, manifest, tsx, want string }{
		{
			"exports subpaths, the @lovable/canvas-sdk shape: .ts targets " +
				"become .js, a css target and a null stay, and fields no " +
				"resolver reads are untouched",
			`{
			  "name": "@lovable/canvas-sdk",
			  "private": true,
			  "type": "module",
			  "exports": {
			    "./wire": "./src/wire/index.ts",
			    "./client/styles.css": "./src/client/styles.css",
			    "./internal/*": null
			  },
			  "files": ["src/*"],
			  "scripts": {"typecheck": "tsc --noEmit"}
			}`,
			".js",
			`{"name":"@lovable/canvas-sdk","private":true,"type":"module",` +
				`"exports":{"./wire":"./src/wire/index.js",` +
				`"./client/styles.css":"./src/client/styles.css",` +
				`"./internal/*":null},"files":["src/*"],` +
				`"scripts":{"typecheck":"tsc --noEmit"}}`,
		},
		{
			"conditions in the order the map writes them: types names the " +
				".d.ts, default the .js",
			`{"name":"pulse","exports":{".":{"types":"./entry.ts",` +
				`"default":"./dist/entry.js"}}}`,
			".js",
			`{"name":"pulse","exports":{".":{"types":"./entry.d.ts",` +
				`"default":"./dist/entry.js"}},"type":"module"}`,
		},
		{
			"the same conditions written the other way round keep that order",
			`{"name":"pulse","exports":{".":{"default":"./entry.ts",` +
				`"types":"./entry.ts"}}}`,
			".js",
			`{"name":"pulse","exports":{".":{"default":"./entry.js",` +
				`"types":"./entry.d.ts"}},"type":"module"}`,
		},
		{
			"a fallback array and a wildcard target",
			`{"name":"m","exports":{".":["./first.ts","./second.js"],` +
				`"./tokens/*":"./styles/tokens/*.ts"}}`,
			".js",
			`{"name":"m","exports":{".":["./first.js","./second.js"],` +
				`"./tokens/*":"./styles/tokens/*.js"},"type":"module"}`,
		},
		{
			"exports as a bare string",
			`{"name":"ws-linked","exports":"./index.ts"}`,
			".js",
			`{"name":"ws-linked","exports":"./index.js","type":"module"}`,
		},
		{
			"main, module and browser name the .js; types and typings the .d.ts",
			`{"name":"m","main":"./src/index.ts","module":"src/index.ts",` +
				`"browser":"./src/browser.tsx","types":"./src/index.ts",` +
				`"typings":"src/legacy.tsx"}`,
			".js",
			`{"name":"m","main":"./src/index.js","module":"src/index.js",` +
				`"browser":"./src/browser.js","types":"./src/index.d.ts",` +
				`"typings":"src/legacy.d.ts","type":"module"}`,
		},
		{
			"imports, the package's own #-specifiers",
			`{"name":"m","imports":{"#internal/*":"./src/internal/*.ts",` +
				`"#dep":"zod"}}`,
			".js",
			`{"name":"m","imports":{"#internal/*":"./src/internal/*.js",` +
				`"#dep":"zod"},"type":"module"}`,
		},
		{
			".mts and .cts keep their module format in both roles",
			`{"name":"m","main":"./a.cts","module":"./b.mts","types":"./a.cts",` +
				`"exports":{".":{"types":"./b.mts","import":"./b.mts"}}}`,
			".js",
			`{"name":"m","main":"./a.cjs","module":"./b.mjs","types":"./a.d.cts",` +
				`"exports":{".":{"types":"./b.d.mts","import":"./b.mjs"}},` +
				`"type":"module"}`,
		},
		{
			"a declaration target is already the emitted file",
			`{"name":"m","types":"./dist/index.d.ts","exports":{".":{"types":` +
				`"./dist/index.d.mts","default":"./dist/index.d.cts"}}}`,
			".js",
			`{"name":"m","types":"./dist/index.d.ts","exports":{".":{"types":` +
				`"./dist/index.d.mts","default":"./dist/index.d.cts"}},` +
				`"type":"module"}`,
		},
		{
			"type is kept when the member sets it",
			`{"name":"m","type":"commonjs","main":"./index.ts"}`,
			".js",
			`{"name":"m","type":"commonjs","main":"./index.js"}`,
		},
		{
			"scalars survive the round trip",
			`{"name":"m","version":"0.0.0","sideEffects":false,` +
				`"engines":{"node":22}}`,
			".js",
			`{"name":"m","version":"0.0.0","sideEffects":false,` +
				`"engines":{"node":22},"type":"module"}`,
		},
		{
			"under jsx: preserve a .tsx target names the .jsx, a .ts the .js " +
				"and types the .d.ts, in every role and through a wildcard",
			`{"name":"preserve-view","browser":"./browser.tsx","exports":{".":` +
				`"./view.tsx","./icons/*":"./icons/components/*.tsx",` +
				`"./util":{"types":"./util.tsx","default":"./util.ts"}}}`,
			".jsx",
			`{"name":"preserve-view","browser":"./browser.jsx","exports":{".":` +
				`"./view.jsx","./icons/*":"./icons/components/*.jsx",` +
				`"./util":{"types":"./util.d.ts","default":"./util.js"}},` +
				`"type":"module"}`,
		},
		{
			"a nested scope manifest: type kept, nothing else to rewrite",
			`{"type": "module"}`,
			".js",
			`{"type":"module"}`,
		},
	} {
		got, err := manifestAsBuilt([]byte(c.manifest), c.tsx)
		if err != nil {
			t.Errorf("%s: %v", c.shape, err)
			continue
		}
		if string(got) != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.shape, got, c.want)
		}
	}
}

func TestManifestAsBuilt_RefusesANonObject(t *testing.T) {
	for _, text := range []string{`"main"`, `["./a.ts"]`, `{"name": "m"`} {
		if _, err := manifestAsBuilt([]byte(text), ".js"); err == nil {
			t.Errorf("manifestAsBuilt(%s) = nil error, want one", text)
		}
	}
}

// The step reads SRC and writes OUT with a trailing newline; -tsx is the
// declared emit of a .tsx.
func TestManifestStep_WritesTheFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "package.json")
	out := filepath.Join(dir, "out", "package.json")
	writeFile(t, src,
		"{\n  \"name\": \"view\",\n  \"exports\": \"./view.tsx\"\n}\n")
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := runManifest([]string{"-tsx=.jsx", src, out}); err != nil {
		t.Fatalf("runManifest: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"name":"view","exports":"./view.jsx","type":"module"}` + "\n"
	if string(data) != want {
		t.Errorf("wrote %q, want %q", data, want)
	}
	if err := runManifest([]string{src}); err == nil ||
		!strings.Contains(err.Error(), "SRC and OUT") {
		t.Errorf("runManifest with one argument = %v, want the usage", err)
	}
}
