package typescript

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

type resolutionCandidate struct {
	from, specifier, path string
}

var moduleTraceStart = regexp.MustCompile(`^======== Resolving module '(.*)' from '(.*)'\. ========$`)
var moduleTraceEnd = regexp.MustCompile(`^======== Module name '(.*)' (?:was not resolved\.|was successfully resolved to '.*'\.) ========$`)
var typeTraceStart = regexp.MustCompile(`^======== Resolving type reference directive '(.*?)', .* ========$`)
var typeTraceEnd = regexp.MustCompile(`^======== Type reference directive '(.*?)' .* ========$`)
var importsTrace = regexp.MustCompile(`^Using 'imports' subpath '.*' with target '(.*)'\.$`)
var candidateTrace = regexp.MustCompile(`^Loading module as file / folder, candidate module location '(.*)', target file types: .+\.$`)

// Trace arrays are emitted serially by the compiler's filesparser, before its listing.
func splitResolutionTrace(root, output string) (string, []resolutionCandidate, error) {
	var listing strings.Builder
	var candidates []resolutionCandidate
	var kind, specifier, from, previous string
	relative := func(value string) (string, error) {
		if !filepath.IsAbs(value) {
			return "", fmt.Errorf("compiler resolution path is not absolute: %q", value)
		}
		rel, err := filepath.Rel(root, value)
		return filepath.ToSlash(rel), err
	}
	for _, raw := range strings.SplitAfter(output, "\n") {
		line := strings.TrimRight(raw, "\r\n")
		if strings.HasPrefix(line, "========") {
			switch {
			case moduleTraceStart.MatchString(line), typeTraceStart.MatchString(line):
				if kind != "" {
					start := moduleTraceStart.FindStringSubmatch(line)
					redirect := importsTrace.FindStringSubmatch(previous)
					if kind != "module" || start == nil || redirect == nil || start[1] != redirect[1] || !filepath.IsAbs(start[2]) {
						return "", nil, fmt.Errorf("unrecognised nested compiler resolution trace: %q", line)
					}
					previous = ""
					continue
				}
				if m := moduleTraceStart.FindStringSubmatch(line); m != nil {
					kind, specifier = "module", m[1]
					var err error
					from, err = relative(m[2])
					if err != nil {
						return "", nil, err
					}
				} else {
					kind, specifier = "type", typeTraceStart.FindStringSubmatch(line)[1]
				}
			default:
				pattern := moduleTraceEnd
				if kind == "type" {
					pattern = typeTraceEnd
				}
				m := pattern.FindStringSubmatch(line)
				if kind == "" || m == nil || m[1] != specifier {
					return "", nil, fmt.Errorf("unrecognised compiler resolution boundary: %q", line)
				}
				kind, specifier, from = "", "", ""
			}
			previous = ""
			continue
		}
		previous = line
		if strings.HasPrefix(line, "Loading module as file / folder") {
			m := candidateTrace.FindStringSubmatch(line)
			if kind == "" || m == nil {
				return "", nil, fmt.Errorf("unframed or malformed compiler candidate: %q", line)
			}
			if kind == "module" {
				candidate, err := relative(m[1])
				if err != nil {
					return "", nil, err
				}
				candidates = append(candidates, resolutionCandidate{from, specifier, candidate})
			}
		}
		if kind == "" {
			listing.WriteString(raw)
		}
	}
	if kind != "" {
		return "", nil, fmt.Errorf("unfinished compiler resolution trace for %q", specifier)
	}
	return listing.String(), candidates, nil
}
