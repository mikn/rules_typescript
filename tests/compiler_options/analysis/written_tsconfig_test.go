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
		t.Errorf("allowJs = %v, want true: a JavaScript src is in `include`", opts["allowJs"])
	}
	if opts["preserveSymlinks"] != true {
		t.Errorf("preserveSymlinks = %v, want true", opts["preserveSymlinks"])
	}
	if opts["outDir"] != "." {
		t.Errorf("outDir = %v, want \".\"", opts["outDir"])
	}
	rootDir, _ := opts["rootDir"].(string)
	if !strings.HasSuffix(rootDir, "/tests/compiler_options/analysis") {
		t.Errorf("rootDir = %q, want the package, not a src's directory", rootDir)
	}
	if _, ok := opts["checkJs"]; ok {
		t.Errorf("checkJs = %v in the written config; the tsconfig's options stay in the chain", opts["checkJs"])
	}
	if len(config.Extends) != 2 || !strings.HasSuffix(config.Extends[1], "/tsconfig.checkjs.json") {
		t.Errorf("extends = %v, want the baseline then the target's tsconfig", config.Extends)
	}

	nested := 0
	for _, entry := range config.Include {
		if !strings.HasPrefix(entry, "../") {
			t.Errorf("include entry %q is not relative", entry)
		}
		if strings.HasSuffix(entry, "/nested/leaf.ts") {
			nested++
		}
	}
	if nested != 1 {
		t.Errorf("include = %v, want one entry for the nested source", config.Include)
	}
}

// The exec root is a source root. Read as a boolean it is "no root dir", and
// rootDir then points at the bin directory, under which no source sits.
func TestWrittenConfigForASourceFromTheExecRoot(t *testing.T) {
	opts := readConfig(t, "from_exec_root").CompilerOptions
	rootDir, _ := opts["rootDir"].(string)
	if !strings.HasPrefix(rootDir, "../") || strings.HasSuffix(rootDir, "/bin") {
		t.Errorf("rootDir = %q, want it to climb out to the exec root", rootDir)
	}
	if opts["outDir"] != "." {
		t.Errorf("outDir = %v, want \".\"", opts["outDir"])
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
