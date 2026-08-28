package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/mikn/rules_typescript/tests/integration/harness"
)

type tsconfig struct {
	CompilerOptions struct {
		Paths map[string][]string `json:"paths"`
	} `json:"compilerOptions"`
}

func pathsEntry(it *harness.IT, paths map[string][]string, key string) []string {
	entries, ok := paths[key]
	if !ok || len(entries) == 0 {
		keys := []string{}
		for name := range paths {
			keys = append(keys, name)
		}
		sort.Strings(keys)
		fmt.Fprintf(os.Stderr, "available paths keys: %v\n", keys)
		it.Fail("%q not found in compilerOptions.paths (or empty)", key)
	}
	return entries
}

func main() {
	harness.Run(harness.Config{
		Name:         "lsp",
		WorkspaceRel: "tests/integration/lsp",
		Lockfile:     "tests/npm/pnpm-lock.yaml",
	}, func(it *harness.IT) {
		it.MustBazel("run", "//:gazelle")
		it.Pass("bazel run //:gazelle")

		for _, dir := range []string{"src/models", "src/components"} {
			it.RequireFile(it.Path(dir, "BUILD.bazel"), "Gazelle did not generate %s/BUILD.bazel", dir)
			it.Pass("%s/BUILD.bazel generated", dir)
		}

		// refresh_tsconfig reads the @npm BUILD.bazel out of the output base, and
		// //... is what forces the repo rule to write it. @npm//... would drag in
		// the workspace alias for a packages/shared this workspace does not have.
		it.MustBazel("build", "//...")
		it.Pass("bazel build //...")

		it.MustBazel("run", "//:refresh_tsconfig")
		it.Pass("bazel run //:refresh_tsconfig")

		generated := it.Path("tsconfig.json")
		it.RequireFile(generated, "refresh_tsconfig did not generate tsconfig.json")
		it.Pass("tsconfig.json generated")

		parsed := tsconfig{}
		if err := json.Unmarshal([]byte(it.Read(generated)), &parsed); err != nil {
			it.Fail("tsconfig.json is not valid JSON: %v", err)
		}
		paths := parsed.CompilerOptions.Paths

		zod := pathsEntry(it, paths, "zod")[0]
		fmt.Printf("INFO: zod path entry = %q\n", zod)
		if !strings.HasSuffix(zod, ".d.ts") {
			it.Fail("'zod' path does not end in .d.ts: %q", zod)
		}
		resolved := zod
		if !filepath.IsAbs(resolved) {
			resolved = filepath.Join(filepath.Dir(generated), resolved)
		}
		it.RequireFile(resolved, "'zod' path does not exist on disk: %q", resolved)
		it.Pass("tsconfig.json has 'zod' in paths pointing at a real .d.ts file")

		alias := pathsEntry(it, paths, "@/*")
		fmt.Printf("INFO: @/* path entries = %q\n", alias)
		found := false
		for _, entry := range alias {
			if strings.Contains(entry, "src/*") {
				found = true
			}
		}
		if !found {
			it.Fail("'@/*' paths do not contain 'src/*': %q", alias)
		}
		it.Pass("tsconfig.json has '@/*' in paths mapping to src/*")

		// ts_pnpm's contract is a pnpm that runs from the workspace root with no
		// pnpm on the host. `--version` needs neither network nor package.json.
		out := it.BazelStdout("run", "//:pnpm", "--", "--version")
		version := ""
		for _, line := range strings.Split(out, "\n") {
			if trimmed := strings.TrimSpace(line); trimmed != "" {
				version = trimmed
			}
		}
		if !regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+`).MatchString(version) {
			it.Fail("bazel run //:pnpm -- --version printed %q, want a version number", version)
		}
		it.Pass("bazel run //:pnpm -- --version reported %s", version)

		// ts_add_package wraps the same binary with `add ... --lockfile-only`;
		// running it for real would need the registry.
		if err := it.Bazel("build", "//:add_package"); err != nil {
			it.Fail("//:add_package does not build")
		}
		it.Pass("//:add_package builds")
	})
}
