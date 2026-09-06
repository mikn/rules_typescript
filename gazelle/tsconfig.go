package typescript

// Whether the tsconfig or a config in its extends chain sets include or files;
// without either, tsc enumerates the directory tree. ok is false when unreadable.
func programNamesInputs(tsConfigPath string) (inputs, ok bool) {
	resolved := resolveTsConfigChain(tsConfigPath, map[string]bool{})
	if resolved == nil {
		return false, false
	}
	return resolved.inputs, true
}

// loadTsConfigJsxImportSource returns compilerOptions.jsxImportSource as the extends
// chain leaves it; "" when no config in the chain names one or the file is unreadable.
func loadTsConfigJsxImportSource(tsConfigPath string) string {
	resolved := resolveTsConfigChain(tsConfigPath, map[string]bool{})
	if resolved == nil {
		return ""
	}
	return resolved.jsxImportSource
}
