// A src the chain's exclude names and no import brings in is outside the
// program tsgo checked; the step reports it instead of leaving it unchecked.

package main

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/mikn/rules_typescript/ts/tools/tsconfig"
)

type excludedSrc struct{ file, entry string }

// excludedSrcs pairs each own src the program lacks with the chain's first
// exclude entry naming it; a .json src is never a root and is left out.
func excludedSrcs(own map[string]bool, program []string,
	chain *tsconfig.Resolved, execroot string) []excludedSrc {
	if chain == nil || chain.Exclude == nil {
		return nil
	}
	listed := make(map[string]bool, len(program))
	for _, f := range program {
		listed[path.Clean(f)] = true
	}
	dir := absolute(execroot, chain.ExcludeDir)
	patterns := make([]*regexp.Regexp, len(*chain.Exclude))
	for i, entry := range *chain.Exclude {
		patterns[i] = excludePattern(dir, entry)
	}
	files := make([]string, 0, len(own))
	for f := range own {
		files = append(files, f)
	}
	sort.Strings(files)
	var out []excludedSrc
	for _, f := range files {
		if path.Ext(f) == ".json" || listed[path.Clean(f)] {
			continue
		}
		abs := absolute(execroot, f)
		for i, p := range patterns {
			if p.MatchString(abs) {
				out = append(out, excludedSrc{f, (*chain.Exclude)[i]})
				break
			}
		}
	}
	return out
}

func absolute(execroot, p string) string {
	if path.IsAbs(p) {
		return p
	}
	return path.Join(execroot, p)
}

// excludePattern is tsc's regular expression for an exclude entry written in
// dir: `*` and `?` within a component, `**` any depth, a bare name its tree.
func excludePattern(dir, entry string) *regexp.Regexp {
	components := segments(absolute(dir, entry))
	last := ""
	if n := len(components); n > 0 {
		last = components[n-1]
	}
	if !strings.ContainsAny(last, ".*?") {
		components = append(components, "**", "*")
	}
	var b strings.Builder
	b.WriteString("^(")
	for _, c := range components {
		if c == "**" {
			b.WriteString("(/.+?)?")
			continue
		}
		b.WriteByte('/')
		for _, r := range c {
			switch r {
			case '*':
				b.WriteString("[^/]*")
			case '?':
				b.WriteString("[^/]")
			default:
				b.WriteString(regexp.QuoteMeta(string(r)))
			}
		}
	}
	b.WriteString(")($|/)")
	return regexp.MustCompile(b.String())
}

// reportExcluded is the failing action's message: each src the program lacks
// with the exclude entry naming it, and the two edits.
func (o *ownership) reportExcluded(project string, hits []excludedSrc) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: srcs the program never read, each named by an "+
		"exclude entry of %s's chain:\n", o.label, project)
	for _, h := range hits {
		fmt.Fprintf(&b, "  %s\texcluded by %q\n", h.file, h.entry)
	}
	b.WriteString("The program is the srcs: drop the file from srcs, or " +
		"the entry from exclude.\nGazelle writes srcs from the program's " +
		"listing and never one of these.")
	return b.String()
}
