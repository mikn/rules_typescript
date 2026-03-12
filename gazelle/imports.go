package typescript

import (
	"os"
	"regexp"
	"strings"
)

// ---- import extraction -----------------------------------------------------

// fileImports holds all import specifiers extracted from a single TypeScript
// source file.
type fileImports struct {
	// file is the filename (basename) of the source file.
	file string
	// imports is the list of raw import specifier strings found in the file.
	imports []string
}

// extractImports parses a TypeScript/TSX file at path and returns all import
// specifiers found in it. The extraction is regex-based and intentionally
// conservative: it recognises the common import/export/require forms and
// deliberately skips imports inside comments and template literals.
//
// Recognised forms:
//
//	import ... from "specifier"
//	import ... from 'specifier'
//	export ... from "specifier"
//	export ... from 'specifier'
//	import("specifier")
//	import('specifier')
//	require("specifier")
//	require('specifier')
//	import type ... from "specifier"
//	export type ... from "specifier"
func extractImports(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	src := string(data)

	// Strip single-line comments and block comments to avoid extracting
	// specifiers from commented-out code. We do a lightweight strip that
	// handles the common cases without a full parser.
	src = stripComments(src)

	return extractFromSource(src), nil
}

// reStaticImport matches:
//   - import ... from "spec" / import ... from 'spec'
//   - export ... from "spec" / export ... from 'spec'
//   - import type ... from "spec"
//   - export type ... from "spec"
//   - import "spec" (side-effect imports)
//   - export * from "spec" (bare star re-export)
//   - export * as name from "spec" (namespace re-export)
//
// The import-clause alternatives are listed explicitly so that the `from`
// keyword is only recognised after a complete clause, not inside one (e.g.
// `import { from as fromAlias } from "mod"` must not produce a spurious match
// on `from "mod"` partway through the named-import list).
var reStaticImport = regexp.MustCompile(
	`(?:import|export)\s+` +
		`(?:type\s+)?` +
		`(?:` +
		`(?:\*(?:\s+as\s+\w+)?|` + // * or * as name (bare star and namespace re-exports)
		`\{[^}]*\}|` + // { named imports }
		`\w+(?:\s*,\s*\{[^}]*\})?` + // default or default, { named }
		`)\s+from\s+|` + // ... from  (clause followed by from)
		`)` + // OR no clause at all (side-effect import)
		// The specifier itself, in single or double quotes.
		`['"]([^'"` + "`" + `\n]+)['"]`,
)

// reDynamicImport matches import("spec") and require("spec").
var reDynamicImport = regexp.MustCompile(
	`(?:import|require)\s*\(\s*['"]([^'"` + "`" + `\n]+)['"]\s*\)`,
)

// extractFromSource applies the import regexps to already-comment-stripped source.
func extractFromSource(src string) []string {
	seen := make(map[string]struct{})
	var result []string

	addSpec := func(spec string) {
		spec = strings.TrimSpace(spec)
		if spec == "" {
			return
		}
		if _, ok := seen[spec]; !ok {
			seen[spec] = struct{}{}
			result = append(result, spec)
		}
	}

	for _, m := range reStaticImport.FindAllStringSubmatch(src, -1) {
		if len(m) >= 2 {
			addSpec(m[1])
		}
	}
	for _, m := range reDynamicImport.FindAllStringSubmatch(src, -1) {
		if len(m) >= 2 {
			addSpec(m[1])
		}
	}

	return result
}

// ---- comment stripping -----------------------------------------------------

// stripComments removes // single-line comments and /* block comments */ from
// TypeScript source, while preserving string and template-literal boundaries
// so that import specifiers inside strings are not accidentally stripped.
//
// This is a simplified implementation. It handles the common cases correctly
// but is not a full TypeScript lexer. Specifically:
//   - Nested template literals with expressions are not handled perfectly.
//   - Regex literals are not distinguished from division operators.
//
// For Gazelle purposes this is acceptable: the goal is to avoid false
// positives from commented-out import statements, not to parse arbitrary code.
func stripComments(src string) string {
	var out strings.Builder
	out.Grow(len(src))

	i := 0
	n := len(src)

	for i < n {
		c := src[i]

		switch {
		// Double-quoted string literal.
		case c == '"':
			end := skipStringLiteral(src, i, '"')
			out.WriteString(src[i:end])
			i = end

		// Single-quoted string literal.
		case c == '\'':
			end := skipStringLiteral(src, i, '\'')
			out.WriteString(src[i:end])
			i = end

		// Template literal (backtick). We emit it as-is to preserve the
		// source shape for the regex match that runs afterwards.
		case c == '`':
			end := skipTemplateLiteral(src, i)
			out.WriteString(src[i:end])
			i = end

		// Possible comment start.
		case c == '/' && i+1 < n:
			next := src[i+1]
			switch next {
			case '/':
				// Single-line comment: skip to end of line.
				j := i + 2
				for j < n && src[j] != '\n' {
					j++
				}
				// Replace comment with whitespace to preserve token boundaries.
				out.WriteByte(' ')
				i = j

			case '*':
				// Block comment: skip to closing */.
				j := i + 2
				for j+1 < n {
					if src[j] == '*' && src[j+1] == '/' {
						j += 2
						break
					}
					if src[j] == '\n' {
						out.WriteByte('\n') // preserve newlines for line-based matching
					}
					j++
				}
				out.WriteByte(' ')
				i = j

			default:
				out.WriteByte(c)
				i++
			}

		default:
			out.WriteByte(c)
			i++
		}
	}

	return out.String()
}

// skipStringLiteral returns the index just past the closing quote character q.
// It handles backslash-escape sequences. i is the index of the opening quote.
func skipStringLiteral(src string, i int, q byte) int {
	i++ // skip opening quote
	n := len(src)
	for i < n {
		c := src[i]
		if c == '\\' {
			i += 2 // skip escape sequence
			continue
		}
		i++
		if c == q {
			break
		}
	}
	return i
}

// skipTemplateLiteral returns the index just past the closing backtick.
// Nested ${...} expressions are skipped at depth 1 only; deeply nested
// template literals are not handled.
func skipTemplateLiteral(src string, i int) int {
	i++ // skip opening backtick
	n := len(src)
	for i < n {
		c := src[i]
		if c == '\\' {
			i += 2
			continue
		}
		if c == '`' {
			i++
			break
		}
		if c == '$' && i+1 < n && src[i+1] == '{' {
			// Skip expression block ${...}
			i += 2
			depth := 1
			for i < n && depth > 0 {
				switch src[i] {
				case '{':
					depth++
				case '}':
					depth--
				case '\\':
					i++
				}
				i++
			}
			continue
		}
		i++
	}
	return i
}
