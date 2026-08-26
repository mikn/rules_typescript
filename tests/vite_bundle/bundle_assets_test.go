package vite_bundle_test

import (
	"path"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

const assetsBundle = "tests/vite_bundle/assets_app_bundle"

type manifestChunk struct {
	File    string   `json:"file"`
	Src     string   `json:"src"`
	IsEntry bool     `json:"isEntry"`
	CSS     []string `json:"css"`
	Assets  []string `json:"assets"`
}

var hashedAsset = regexp.MustCompile(`^big_logo-[A-Za-z0-9_-]+\.svg$`)

var hashedBinary = regexp.MustCompile(`^noise-[A-Za-z0-9_-]+\.png$`)

// An imported asset is emitted under a content hash and the JS that imported it
// refers to it by that name. The fixture SVG is deliberately larger than Vite's
// 4096-byte assetsInlineLimit: a smaller one becomes a data URI and never gets a
// filename at all.
func TestImportedAssetIsHashed(t *testing.T) {
	dir := verify.New(t).Dir(assetsBundle)

	svgs := dir.Glob("*.svg")
	if len(svgs) != 1 {
		t.Fatalf("want exactly one .svg in %s, got %d", assetsBundle, len(svgs))
	}
	name := path.Base(svgs[0].Name())
	if !hashedAsset.MatchString(name) {
		t.Errorf("%s is not a hashed filename, want big_logo-<hash>.svg", name)
	}
	dir.AnyContains("*.js", name)
}

// A binary asset has to survive the copy into bazel-bin and Vite's emit byte
// for byte. A mechanism that decodes the file as text on the way through
// produces something that still looks like a PNG in a directory listing.
func TestBinaryAssetSurvivesByteForByte(t *testing.T) {
	tree := verify.New(t)
	dir := tree.Dir(assetsBundle)

	pngs := dir.Glob("*.png")
	if len(pngs) != 1 {
		t.Fatalf("want exactly one .png in %s, got %d", assetsBundle, len(pngs))
	}
	if name := path.Base(pngs[0].Name()); !hashedBinary.MatchString(name) {
		t.Errorf("%s is not a hashed filename, want noise-<hash>.png", name)
	}

	want := tree.File("tests/vite_bundle/noise.png").Text()
	if got := pngs[0].Text(); got != want {
		t.Errorf("%s differs from the source: %d bytes emitted, %d in the source",
			pngs[0].Name(), len(got), len(want))
	}
}

// publicDir files are copied verbatim: same name, same content, no hash. That is
// the whole point of the directory -- a file named from outside the build graph
// has to keep the name it was named by.
func TestPublicDirCopiedVerbatim(t *testing.T) {
	tree := verify.New(t)
	dir := tree.Dir(assetsBundle)

	dir.File("robots.txt").Contains("User-agent: *")
	dir.File("nested/note.txt").Contains("copied verbatim")

	// And a binary one, unchanged: publicDir is where a favicon lives.
	want := tree.File("tests/vite_bundle/public/favicon.ico").Text()
	if got := dir.File("favicon.ico").Text(); got != want {
		t.Errorf("favicon.ico differs from the source: %d bytes copied, %d in the source",
			len(got), len(want))
	}

	for _, f := range dir.Glob("*.txt") {
		switch base := path.Base(f.Name()); base {
		case "robots.txt", "note.txt":
		default:
			t.Errorf("%s: a publicDir file was renamed, want the name it was given", base)
		}
	}
}

// The manifest maps the inputs to the hashed outputs. Its value is that a server
// emitting its own script tags agrees with the HTML Vite emitted, so that is
// what is asserted -- not the manifest on its own.
func TestManifestAgreesWithHTML(t *testing.T) {
	dir := verify.New(t).Dir(assetsBundle)

	manifest := map[string]manifestChunk{}
	dir.File("manifest.json").JSON(&manifest)

	var entry manifestChunk
	for _, chunk := range manifest {
		if chunk.IsEntry {
			entry = chunk
		}
	}
	if entry.File == "" {
		t.Fatalf("manifest.json declares no entry chunk: %v", manifest)
	}

	html := dir.File("index.html")
	html.Contains("/" + entry.File)
	if len(entry.CSS) == 0 {
		t.Error("manifest entry chunk lists no CSS, and the entry imports a stylesheet")
	}
	for _, css := range entry.CSS {
		html.Contains("/" + css)
	}
	for _, asset := range entry.Assets {
		dir.File(asset).Exists()
	}
}

// A manifest is looked up by input path, so an unusable key makes the whole file
// unusable however correct its values are. Vite keys by module id relative to
// its root, which for a bazel-out input escapes the sandbox and names the build
// configuration -- so the wrapper rewrites the keys, and this is what notices if
// it stops.
func TestManifestKeysAreWorkspaceRelative(t *testing.T) {
	dir := verify.New(t).Dir(assetsBundle)

	manifest := map[string]manifestChunk{}
	dir.File("manifest.json").JSON(&manifest)
	if len(manifest) == 0 {
		t.Fatal("manifest.json is empty")
	}

	for key, chunk := range manifest {
		for what, path := range map[string]string{"key": key, "src": chunk.Src} {
			if path == "" {
				continue
			}
			if strings.HasPrefix(path, "/") {
				t.Errorf("manifest %s %q is absolute", what, path)
			}
			if strings.Contains(path, "..") {
				t.Errorf("manifest %s %q escapes its root", what, path)
			}
			if strings.Contains(path, "bazel-out") || strings.Contains(path, "execroot") {
				t.Errorf("manifest %s %q names the output tree, so it is not configuration-stable", what, path)
			}
		}
	}
}

// The .d.ts css_module generates is a promise about the keys of the object Vite
// hands the importer. postcss-modules is what actually produces that object, so
// the fixture bundle dumps its export map through css.modules.getJSON and the
// two key sets are compared -- rather than the .d.ts being trusted on its own.
func TestCssModuleKeysMatchDeclaration(t *testing.T) {
	tree := verify.New(t)

	exportsByFile := map[string]map[string]string{}
	tree.Dir(assetsBundle).File("css-module-exports.json").JSON(&exportsByFile)
	real, ok := exportsByFile["panel.module.css"]
	if !ok {
		t.Fatalf("the bundle recorded no exports for panel.module.css: %v", exportsByFile)
	}

	// css_module declares class names, and postcss-modules also scopes the
	// @keyframes name in the same file. Listing it is what keeps the rest of
	// this an exact comparison.
	omitted := map[string]bool{"panel-fade": true}

	var want []string
	for name := range real {
		if !omitted[name] {
			want = append(want, name)
		}
	}
	got := declaredKeys(tree.File("tests/vite_bundle/panel.module.css.d.ts").Text())
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("panel.module.css.d.ts declares %v, postcss-modules exports %v", got, want)
	}
}

var declaration = regexp.MustCompile(`readonly (?:"([^"]+)"|([A-Za-z0-9_$]+)): string`)

func declaredKeys(dts string) []string {
	var out []string
	for _, m := range declaration.FindAllStringSubmatch(dts, -1) {
		out = append(out, m[1]+m[2])
	}
	sort.Strings(out)
	return out
}
