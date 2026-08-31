package main

import "testing"

// wrangler compiles TypeScript itself, so a worker's config names the .ts entry
// -- that is what `wrangler dev` needs. Bazel stages what it compiled, so the
// only file beside the staged config is the .js, and wrangler stops at
// "The entry-point file at src/index.ts was not found."
func TestRetargetMain(t *testing.T) {
	staged := map[string]bool{"src/index.js": true, "src/worker.js": true}
	exists := func(rel string) bool { return staged[rel] }

	for _, tt := range []struct {
		name, in, want string
	}{
		{
			name: "a .ts main names the .js Bazel staged",
			in:   "{\n  // the entry\n  \"main\": \"src/index.ts\",\n  \"compatibility_date\": \"2026-01-01\"\n}",
			want: "{\n  // the entry\n  \"main\": \"src/index.js\",\n  \"compatibility_date\": \"2026-01-01\"\n}",
		},
		{
			name: "a .tsx main too",
			in:   `{"main": "src/worker.tsx"}`,
			want: `{"main": "src/worker.js"}`,
		},
		{
			name: "a main already naming .js is left alone",
			in:   `{"main": "src/index.js"}`,
			want: `{"main": "src/index.js"}`,
		},
		{
			name: "a .ts main with no compiled sibling is left alone, so wrangler reports it",
			in:   `{"main": "src/missing.ts"}`,
			want: `{"main": "src/missing.ts"}`,
		},
		{
			name: "a leading ./ survives the rewrite",
			in:   `{"main": "./src/index.ts"}`,
			want: `{"main": "./src/index.js"}`,
		},
		{
			name: "no main at all",
			in:   `{"name": "w"}`,
			want: `{"name": "w"}`,
		},
		{
			name: "a string that merely contains main is not the key",
			in:   `{"vars": {"DOMAIN": "main.example.com"}}`,
			want: `{"vars": {"DOMAIN": "main.example.com"}}`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := retargetMain(tt.in, exists); got != tt.want {
				t.Errorf("retargetMain:\n got %q\nwant %q", got, tt.want)
			}
		})
	}
}
