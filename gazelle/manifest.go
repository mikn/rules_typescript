package typescript

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
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

func manifestProgramRoots(repoRoot, dir string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(dir), "package.json"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	roots := map[string]bool{}
	var visit func(any) error
	visit = func(value any) error {
		switch v := value.(type) {
		case string:
			if !programCandidate(v) {
				return nil
			}
			if strings.Contains(v, "*") || path.IsAbs(v) || strings.HasPrefix(path.Clean(v), "../") {
				return fmt.Errorf("%s/package.json: source export %q needs an explicit tsconfig.json program; did you mean to declare the package's compiler inputs there?", dir, v)
			}
			file := path.Join(dir, v)
			if _, err := os.Stat(filepath.Join(repoRoot, filepath.FromSlash(file))); os.IsNotExist(err) {
				return nil
			} else if err != nil {
				return err
			}
			roots[file] = true
		case map[string]any:
			for _, key := range slices.Sorted(maps.Keys(v)) {
				if err := visit(v[key]); err != nil {
					return err
				}
			}
		case []any:
			for _, item := range v {
				if err := visit(item); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for _, field := range []string{"exports", "types", "typings", "main", "module", "browser"} {
		if err := visit(fields[field]); err != nil {
			return nil, err
		}
	}
	return slices.Sorted(maps.Keys(roots)), nil
}
