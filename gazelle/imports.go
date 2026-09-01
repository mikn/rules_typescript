package typescript

import (
	"os"
	"strings"
)

// ---- import extraction -----------------------------------------------------

// Import is one module specifier a source file names, with the 1-based line it
// was found on.
type Import struct {
	Specifier string
	Line      int
}

// ScanImports returns every module specifier in src, in source order.
//
// A quoted string is a specifier only when the token before it says so, which
// is what keeps `{ from: "x" }` and `declare module "x"` out of the result.
// Comments, template literals and regex literals are skipped whole.
//
// Recognised forms:
//
//	import ... from "spec"        export ... from "spec"
//	import "spec"                 import("spec")
//	require("spec")               import x = require("spec")
//
// This is the one place the ruleset decides what an import is: ts_compile's
// undeclared-import check applies the same rule, so a specifier Gazelle does
// not see is not one the build demands a dep for.
func ScanImports(src string) []Import {
	var found []Import

	line := 1
	lastWord := ""
	lastKind := kindNone
	lastPunct := byte(0)

	i, n := 0, len(src)
	for i < n {
		c := src[i]

		switch {
		case c == '\n':
			line++
			i++

		case c == ' ' || c == '\t' || c == '\r':
			i++

		case c == '/' && i+1 < n && src[i+1] == '/':
			for i < n && src[i] != '\n' {
				i++
			}

		case c == '/' && i+1 < n && src[i+1] == '*':
			i += 2
			for i < n && !(src[i] == '*' && i+1 < n && src[i+1] == '/') {
				if src[i] == '\n' {
					line++
				}
				i++
			}
			i = min(i+2, n)

		case c == '/' && regexCanStart(lastKind, lastWord, lastPunct):
			i = skipRegexLiteral(src, i)
			lastKind = kindPunct

		case c == '`':
			i, line = skipTemplateLiteral(src, i, line)
			lastKind = kindString

		case c == '"' || c == '\'':
			startLine := line
			value, next := readStringLiteral(src, i)
			i = next
			if isSpecifierPosition(lastKind, lastWord) && value != "" {
				found = append(found, Import{Specifier: value, Line: startLine})
			}
			lastKind = kindString

		case isWordChar(c):
			start := i
			for i < n && isWordChar(src[i]) {
				i++
			}
			lastWord = src[start:i]
			lastKind = kindWord

		case c == '(' && lastKind == kindWord:
			lastKind = kindCall
			i++

		default:
			lastPunct = c
			lastKind = kindPunct
			i++
		}
	}

	return found
}

// ScanAmbientModules returns the module names src declares with
// `declare module "x"`. In a script-mode declaration file such a block is the
// module: nothing installs it, nothing exports it from another file, and an
// import of it needs no dep at all.
//
// A pattern name (`declare module "*.svg"`) is left out. Those stand for a
// bundler's asset loader, and the specifiers they cover are relative paths that
// resolve to a real file with a real target.
func ScanAmbientModules(src string) []string {
	var found []string

	prevWord, lastWord := "", ""
	lastKind := kindNone
	lastPunct := byte(0)

	i, n := 0, len(src)
	for i < n {
		c := src[i]

		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			i++

		case c == '/' && i+1 < n && src[i+1] == '/':
			for i < n && src[i] != '\n' {
				i++
			}

		case c == '/' && i+1 < n && src[i+1] == '*':
			i += 2
			for i < n && !(src[i] == '*' && i+1 < n && src[i+1] == '/') {
				i++
			}
			i = min(i+2, n)

		case c == '/' && regexCanStart(lastKind, lastWord, lastPunct):
			i = skipRegexLiteral(src, i)
			lastKind = kindPunct

		case c == '`':
			i, _ = skipTemplateLiteral(src, i, 1)
			lastKind = kindString

		case c == '"' || c == '\'':
			value, next := readStringLiteral(src, i)
			i = next
			if lastKind == kindWord && lastWord == "module" && prevWord == "declare" &&
				value != "" && !strings.Contains(value, "*") {
				found = append(found, value)
			}
			lastKind = kindString

		case isWordChar(c):
			start := i
			for i < n && isWordChar(src[i]) {
				i++
			}
			prevWord, lastWord = lastWord, src[start:i]
			lastKind = kindWord

		case c == '(' && lastKind == kindWord:
			lastKind = kindCall
			i++

		default:
			lastPunct = c
			lastKind = kindPunct
			i++
		}
	}

	return found
}

// hasModuleSyntax reports whether a top-level `import` or `export` makes src a
// module rather than a script. An `import(...)` type query is neither, and a
// `declare module` block's exports are its own, not the file's.
func hasModuleSyntax(src string) bool {
	depth := 0
	lastWord := ""
	lastKind := kindNone
	lastPunct := byte(0)

	i, n := 0, len(src)
	for i < n {
		c := src[i]

		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			i++

		case c == '/' && i+1 < n && src[i+1] == '/':
			for i < n && src[i] != '\n' {
				i++
			}

		case c == '/' && i+1 < n && src[i+1] == '*':
			i += 2
			for i < n && !(src[i] == '*' && i+1 < n && src[i+1] == '/') {
				i++
			}
			i = min(i+2, n)

		case c == '/' && regexCanStart(lastKind, lastWord, lastPunct):
			i = skipRegexLiteral(src, i)
			lastKind = kindPunct

		case c == '`':
			i, _ = skipTemplateLiteral(src, i, 1)
			lastKind = kindString

		case c == '"' || c == '\'':
			_, i = readStringLiteral(src, i)
			lastKind = kindString

		case isWordChar(c):
			start := i
			for i < n && isWordChar(src[i]) {
				i++
			}
			lastWord = src[start:i]
			lastKind = kindWord
			if depth == 0 && (lastWord == "export" || (lastWord == "import" && !callFollows(src, i))) {
				return true
			}

		case c == '(' && lastKind == kindWord:
			lastKind = kindCall
			i++

		default:
			if c == '{' {
				depth++
			} else if c == '}' && depth > 0 {
				depth--
			}
			lastPunct = c
			lastKind = kindPunct
			i++
		}
	}

	return false
}

func callFollows(src string, i int) bool {
	for i < len(src) {
		switch src[i] {
		case ' ', '\t', '\r', '\n':
			i++
		case '(':
			return true
		default:
			return false
		}
	}
	return false
}

// extractImports parses a TypeScript/TSX file and returns its specifiers,
// deduplicated, in the order they first appear.
func extractImports(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return extractFromSource(string(data)), nil
}

func extractFromSource(src string) []string {
	seen := make(map[string]struct{})
	var result []string
	for _, imp := range ScanImports(src) {
		if _, ok := seen[imp.Specifier]; ok {
			continue
		}
		seen[imp.Specifier] = struct{}{}
		result = append(result, imp.Specifier)
	}
	return result
}

// ---- lexer -----------------------------------------------------------------

type tokenKind int

const (
	kindNone tokenKind = iota
	kindWord
	kindCall
	kindString
	kindPunct
)

// Words after which a `/` opens a regex literal rather than dividing.
var keywordsBeforeRegex = map[string]struct{}{
	"return": {}, "typeof": {}, "instanceof": {}, "in": {}, "of": {},
	"new": {}, "delete": {}, "void": {}, "do": {}, "else": {},
	"yield": {}, "await": {}, "case": {}, "throw": {},
}

// Punctuation that closes an expression, after which `/` divides.
const regexClosers = ")]"

func isSpecifierPosition(kind tokenKind, word string) bool {
	switch kind {
	case kindWord:
		return word == "from" || word == "import"
	case kindCall:
		return word == "import" || word == "require"
	default:
		return false
	}
}

func regexCanStart(kind tokenKind, word string, punct byte) bool {
	switch kind {
	case kindPunct:
		for i := 0; i < len(regexClosers); i++ {
			if regexClosers[i] == punct {
				return false
			}
		}
		return true
	case kindWord:
		_, ok := keywordsBeforeRegex[word]
		return ok
	default:
		return false
	}
}

func isWordChar(c byte) bool {
	return c == '_' || c == '$' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// skipRegexLiteral returns the index just past the closing `/`. src[i] is the
// opening one. A newline first means it was a division, not a literal.
func skipRegexLiteral(src string, i int) int {
	i++
	inClass := false
	for i < len(src) {
		switch c := src[i]; {
		case c == '\\':
			i += 2
			continue
		case c == '\n':
			return i
		case c == '[':
			inClass = true
		case c == ']':
			inClass = false
		case c == '/' && !inClass:
			return i + 1
		}
		i++
	}
	return i
}

func skipTemplateLiteral(src string, i, line int) (int, int) {
	i++
	for i < len(src) && src[i] != '`' {
		if src[i] == '\\' {
			i += 2
			continue
		}
		if src[i] == '\n' {
			line++
		}
		i++
	}
	return min(i+1, len(src)), line
}

// readStringLiteral returns the literal's value and the index just past it.
// src[i] is the opening quote. An unterminated literal ends at the newline,
// which is left for the caller to count.
func readStringLiteral(src string, i int) (string, int) {
	quote := src[i]
	i++
	var value []byte
	for i < len(src) {
		c := src[i]
		if c == '\\' && i+1 < len(src) {
			value = append(value, src[i+1])
			i += 2
			continue
		}
		if c == '\n' {
			return string(value), i
		}
		i++
		if c == quote {
			break
		}
		value = append(value, c)
	}
	return string(value), i
}
