package typescript

import (
	"log"
	"strings"

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

// Whether the chain's effective jsx is preserve, the one value that names a
// .tsx's emit; tsc reads the value case-insensitively.
func programPreservesJsx(tsConfigPath string) bool {
	resolved, err := tsconfig.Resolve(tsConfigPath)
	return err == nil && strings.EqualFold(resolved.Jsx, "preserve")
}

// The chain's effective module when tsgo emits it, lowercased as tsgo prints
// it: the ts_config value that names a program's ES twins; "" for oxc's kinds.
func programModule(tsConfigPath string) string {
	resolved, err := tsconfig.Resolve(tsConfigPath)
	if err != nil || tsconfig.OxcEmits(resolved.Module) {
		return ""
	}
	return strings.ToLower(resolved.Module)
}
