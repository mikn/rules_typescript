// Command copy_to_workspace writes files a Bazel rule declared into the source
// tree. It is the whole of `bazel run //:refresh_tsconfig`: what to generate is
// decided at analysis time, and this only puts the result where an editor looks.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bazelbuild/rules_go/go/runfiles"
)

type entry struct {
	Rlocation string `json:"rlocation"`
	Dest      string `json:"dest"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "copy_to_workspace: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	workspace := os.Getenv("BUILD_WORKSPACE_DIRECTORY")
	if workspace == "" {
		return fmt.Errorf("BUILD_WORKSPACE_DIRECTORY is unset; run this through `bazel run`")
	}
	manifestPath := os.Getenv("COPY_TO_WORKSPACE_MANIFEST")
	if manifestPath == "" {
		return fmt.Errorf("COPY_TO_WORKSPACE_MANIFEST is unset; the refresh_workspace_files rule sets it")
	}

	files, err := runfiles.New()
	if err != nil {
		return err
	}
	raw, err := read(files, manifestPath)
	if err != nil {
		return err
	}
	var entries []entry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return fmt.Errorf("parsing %s: %w", manifestPath, err)
	}

	for _, e := range entries {
		content, err := read(files, e.Rlocation)
		if err != nil {
			return err
		}
		dest, err := resolve(workspace, e.Dest)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dest, content, 0o644); err != nil {
			return err
		}
		fmt.Printf("wrote %s\n", e.Dest)
	}
	return nil
}

func read(files *runfiles.Runfiles, rlocation string) ([]byte, error) {
	path, err := files.Rlocation(rlocation)
	if err != nil {
		return nil, fmt.Errorf("locating %s: %w", rlocation, err)
	}
	return os.ReadFile(path)
}

func resolve(workspace, dest string) (string, error) {
	// A dest that escapes the workspace would write wherever the rule pleased.
	path := filepath.Join(workspace, filepath.FromSlash(dest))
	rel, err := filepath.Rel(workspace, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("destination %q is outside the workspace", dest)
	}
	return path, nil
}
