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
	if opts["checkJs"] != true {
		t.Errorf("checkJs = %v, want the inherited compiler setting", opts["checkJs"])
	}
	if len(config.Extends) != 0 {
		t.Errorf("extends = %v, want a resolved program without discovery cycles", config.Extends)
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

func TestWrittenConfigPreservesResolvedBaselineAndUserOptions(t *testing.T) {
	config := readConfig(t, "over_tsconfig")
	if len(config.Extends) != 0 {
		t.Fatalf("extends = %v, want resolved compiler options", config.Extends)
	}
	for key, want := range map[string]any{
		"strict": true, "module": "preserve", "target": "es2022", "jsx": "react-jsx",
		"skipLibCheck": true, "esModuleInterop": true, "noUnusedLocals": true,
	} {
		if value := config.CompilerOptions[key]; value != want {
			t.Errorf("compilerOptions.%s = %v, want inherited %v", key, value, want)
		}
	}
}
