package analysis_test

import (
	"strings"
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

const pkg = "tests/compiler_options/analysis/"

type actionConfig struct {
	Extends         []string       `json:"extends"`
	CompilerOptions map[string]any `json:"compilerOptions"`
	Include         []string       `json:"include"`
	Files           []string       `json:"files"`
}

func readConfig(t *testing.T, target string) actionConfig {
	var config actionConfig
	verify.New(t).File(pkg + target + ".tsconfig.json").JSON(&config)
	return config
}

// The keys Bazel owns are the written config's; every option the target chose
// is the tsconfig's, reached through `extends`.
func TestWrittenConfigForASubtreeWithJavaScript(t *testing.T) {
	config := readConfig(t, "everything")
	opts := config.CompilerOptions

	if opts["allowJs"] != true {
		t.Errorf("allowJs = %v, want true: a JavaScript src is in the program",
			opts["allowJs"])
	}
	if got, ok := opts["outDir"]; ok {
		t.Errorf("outDir = %v, want unset: the declare's --outDir carries it", got)
	}
	rootDir, _ := opts["rootDir"].(string)
	if !strings.HasPrefix(rootDir, "../") || strings.HasSuffix(rootDir, "/bin") {
		t.Errorf("rootDir = %q, want the exec root", rootDir)
	}
	if _, ok := opts["checkJs"]; ok {
		t.Errorf("checkJs = %v in the written config; the tsconfig's options stay in the chain", opts["checkJs"])
	}
	if len(config.Extends) != 2 || !strings.HasSuffix(config.Extends[1], "/tsconfig.checkjs.json") {
		t.Errorf("extends = %v, want the baseline then the target's tsconfig", config.Extends)
	}

	// The tsconfig names no include, so the roots are tsc's default pattern
	// over its directory, which every src of the target is under.
	pattern := "/tests/compiler_options/analysis/**/*"
	if len(config.Include) != 1 || !strings.HasPrefix(config.Include[0], "../") ||
		!strings.HasSuffix(config.Include[0], pattern) {
		t.Errorf("include = %v, want the package's **/* alone", config.Include)
	}
	if len(config.Files) != 0 {
		t.Errorf("files = %v, want []: the pattern names every src", config.Files)
	}
}

// The written config's rootDir is the exec root, under which every input sits,
// and it carries no outDir: the declare's --outDir and --rootDir are its own.
func TestWrittenConfigForASourceFromTheExecRoot(t *testing.T) {
	opts := readConfig(t, "from_exec_root").CompilerOptions
	rootDir, _ := opts["rootDir"].(string)
	if !strings.HasPrefix(rootDir, "../") || strings.HasSuffix(rootDir, "/bin") {
		t.Errorf("rootDir = %q, want it to climb out to the exec root", rootDir)
	}
	if got, ok := opts["outDir"]; ok {
		t.Errorf("outDir = %v, want unset", got)
	}
}

// The baseline is the file the config extends FIRST: a later entry overrides an
// earlier one, so the baseline reaches only keys the user's chain never sets.
func TestWrittenConfigExtendsTheBaselineThenTheTsconfig(t *testing.T) {
	config := readConfig(t, "over_tsconfig")
	if len(config.Extends) != 2 {
		t.Fatalf("extends = %v, want the baseline and the user's file", config.Extends)
	}
	if !strings.HasSuffix(config.Extends[0], ".tsconfig_baseline.json") {
		t.Errorf("extends[0] = %q, want the baseline first", config.Extends[0])
	}
	if !strings.HasSuffix(config.Extends[1], "/silent.tsconfig.json") {
		t.Errorf("extends[1] = %q, want the user's file last", config.Extends[1])
	}
	for _, key := range []string{"strict", "module", "target", "jsx", "skipLibCheck", "esModuleInterop", "moduleResolution"} {
		if value, ok := config.CompilerOptions[key]; ok {
			t.Errorf("compilerOptions.%s = %v in the written file, want it left to the chain", key, value)
		}
	}
}
