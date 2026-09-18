package typescript

import (
	"path/filepath"
	"strings"
	"testing"
)

// One file in the config's package, whatever the quotes or the name; none,
// two, an escape or a missing file is nothing, said once.
func TestWranglerConfigOf(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		"w/wrangler.jsonc":       "{}\n",
		"w/wrangler.test.jsonc":  "{}\n",
		"w/config/wrangler.toml": "\n",
		"w/sub/tsconfig.json":    includeTs,
		"w/sub/wrangler.json":    "{}\n",
		"w/double.mts":           "'./wrangler.jsonc'; `wrangler.toml`\n",
		"w/single.mts":           `configPath: "./wrangler.jsonc"` + "\n",
		"w/twice.mts":            `"./wrangler.jsonc"; "wrangler.jsonc"` + "\n",
		"w/named.mts":            `configPath: './wrangler.test.jsonc'` + "\n",
		"w/below.mts":            "`config/wrangler.toml`\n",
		"w/deeper.mts":           `"sub/wrangler.json"` + "\n",
		"w/above.mts":            `"../wrangler.jsonc"` + "\n",
		"w/absent.mts":           `"./wrangler.staging.jsonc"` + "\n",
		"w/none.mts":             "export default {}\n",
		"w/comment.mts":          "// the wrangler.jsonc beside this file\n",
		"wrangler.jsonc":         "{}\n",
		"root.mts":               `"wrangler.jsonc"` + "\n",
	})
	s := newProgramStore()
	s.packages["w"] = map[string]bool{"w/a.ts": true}
	s.packages["w/sub"] = map[string]bool{"w/sub/b.ts": true}
	for _, c := range []struct{ cfg, want, said string }{
		{"w/single.mts", "w/wrangler.jsonc", ""},
		{"w/twice.mts", "w/wrangler.jsonc", ""},
		{"w/named.mts", "w/wrangler.test.jsonc", ""},
		{"w/below.mts", "w/config/wrangler.toml", ""},
		{"root.mts", "wrangler.jsonc", ""},
		{"w/none.mts", "", ""},
		{"w/comment.mts", "", ""},
		{"w/double.mts", "", "names 2 wrangler configs"},
		{"w/above.mts", "", "../wrangler.jsonc, which is not in w"},
		{"w/deeper.mts", "", "sub/wrangler.json, which is not in w"},
		{"w/absent.mts", "", "w/wrangler.staging.jsonc, which is not there"},
	} {
		var got string
		logged := captureLog(t, func() {
			got = s.wranglerConfigOf(root, c.cfg)
			s.wranglerConfigOf(root, c.cfg)
		})
		if got != c.want {
			t.Errorf("%s: %q, want %q", c.cfg, got, c.want)
		}
		if c.said == "" && logged != "" {
			t.Errorf("%s: said %q, want nothing", c.cfg, logged)
		}
		if c.said != "" && (!strings.Contains(logged, c.said) ||
			strings.Count(logged, "\n") != 1) {
			t.Errorf("%s: log, want one line with %q:\n%s", c.cfg, c.said, logged)
		}
	}
	if got := s.wranglerConfigOf(root, filepath.Join("w", "gone.mts")); got != "" {
		t.Errorf("an unreadable config names %q", got)
	}
}
