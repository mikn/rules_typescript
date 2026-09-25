package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/mikn/rules_typescript/ts/tools/explainfiles"
	"github.com/mikn/rules_typescript/ts/tools/tsconfig"
)

func editorRun(root string, command []string, project, template, output, destination string, generated []string, env ...string) error {
	if output == "" || destination == "" {
		return errors.New("editor-template requires editor-out and editor-path")
	}
	var stdout bytes.Buffer
	runErr := runToolIn(root, &stdout, command, env...)
	listing, err := explainfiles.Parse(stdout.String())
	if err != nil {
		return errors.Join(runErr, err)
	}
	if runErr != nil {
		var exit *exec.ExitError
		if !errors.As(runErr, &exit) || exit.ExitCode() != 1 || len(listing.Diagnostics) == 0 {
			return fmt.Errorf("editor compiler listing: %w\n%s", runErr, stdout.String())
		}
	}
	if len(listing.Diagnostics) != 0 {
		fmt.Fprintln(os.Stderr, strings.Join(listing.Diagnostics, "\n"))
	}
	raw, err := os.ReadFile(template)
	if err != nil {
		return err
	}
	var config map[string]json.RawMessage
	if err := json.Unmarshal(raw, &config); err != nil {
		return err
	}
	var options map[string]json.RawMessage
	if err := json.Unmarshal(config["compilerOptions"], &options); err != nil {
		return err
	}
	if project != "" && options["paths"] != nil {
		chain, err := tsconfig.Resolve(project)
		if err != nil {
			return err
		}
		var paths map[string][]string
		if err := json.Unmarshal(options["paths"], &paths); err != nil {
			return err
		}
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		rootAbs := filepath.Join(cwd, root)
		for i := range listing.Edges {
			listing.Edges[i].From = throughRoot(rootAbs, cwd, listing.Edges[i].From)
			listing.Edges[i].To = throughRoot(rootAbs, cwd, listing.Edges[i].To)
		}
		projectEditorEdges(paths, chain, path.Dir(destination), generated, listing.Edges)
		options["paths"], err = json.Marshal(paths)
		if err != nil {
			return err
		}
	}
	config["compilerOptions"], err = json.Marshal(options)
	if err != nil {
		return err
	}
	return writeJSON(output, config)
}

func projectEditorEdges(paths map[string][]string, chain *tsconfig.Resolved, directory string, generated []string, edges []explainfiles.Edge) {
	declared := map[string]bool{}
	for _, file := range generated {
		declared[path.Clean(file)] = true
	}
	selections := map[string]map[explainfiles.Edge]bool{}
	for _, edge := range edges {
		if edge.Kind != explainfiles.Import || isRelative(edge.Specifier) || path.IsAbs(edge.Specifier) || underNodeModules(edge.From) {
			continue
		}
		owner, _ := editorAliasOwner(chain.Paths, edge.Specifier)
		if owner == "" {
			continue
		}
		if selections[edge.Specifier] == nil {
			selections[edge.Specifier] = map[explainfiles.Edge]bool{}
		}
		selections[edge.Specifier][edge] = true
	}
	for specifier, importers := range selections {
		selected := ""
		consistent := true
		for edge := range importers {
			file := path.Clean(edge.To)
			if selected != "" && selected != file {
				consistent = false
			}
			selected = file
		}
		// A paths entry cannot encode importer-specific destinations.
		if consistent && declared[selected] {
			paths[specifier] = []string{fileRelative(directory, path.Join("bazel-bin", binRelative(selected)))}
		}
	}
}
