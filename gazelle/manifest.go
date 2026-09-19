package typescript

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
)

// A package.json as the package model reads it: its name, and the names its
// dependencies and devDependencies declare.
type manifest struct {
	dir  string
	name string
	deps []string
}

// nearestManifest is the package.json at or above dir, the one tsc and node
// read for every file under dir.
func nearestManifest(repoRoot, dir string) *manifest {
	for ; ; dir = parentDir(dir) {
		if m := readManifest(repoRoot, dir); m != nil {
			return m
		}
		if dir == "" {
			return nil
		}
	}
}

func readManifest(repoRoot, dir string) *manifest {
	data, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(dir),
		"package.json"))
	if err != nil {
		return nil
	}
	var raw struct {
		Name            string            `json:"name"`
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if json.Unmarshal(data, &raw) != nil {
		return nil
	}
	deps := map[string]bool{}
	for name := range raw.Dependencies {
		deps[name] = true
	}
	for name := range raw.DevDependencies {
		deps[name] = true
	}
	return &manifest{dir: dir, name: raw.Name,
		deps: slices.Sorted(maps.Keys(deps))}
}
