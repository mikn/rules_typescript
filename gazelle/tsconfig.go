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
