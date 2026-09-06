package typescript

import (
	"log"

	"github.com/mikn/rules_typescript/ts/tools/tsconfig"
)

// Whether the tsconfig or a config in its extends chain sets include or files;
// without either, tsc enumerates the directory tree. ok is false when unreadable.
func programNamesInputs(tsConfigPath string) (inputs, ok bool) {
	resolved, err := tsconfig.Resolve(tsConfigPath)
	if err != nil {
		log.Printf("typescript: %v", err)
		return false, false
	}
	return resolved.Inputs, true
}

// loadTsConfigJsxImportSource returns compilerOptions.jsxImportSource as the extends
// chain leaves it; "" when no config in the chain names one or the file is unreadable.
func loadTsConfigJsxImportSource(tsConfigPath string) string {
	resolved, err := tsconfig.Resolve(tsConfigPath)
	if err != nil {
		return ""
	}
	return resolved.JsxImportSource
}
