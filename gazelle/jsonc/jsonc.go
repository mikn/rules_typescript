// Package jsonc reads the JSON-with-comments dialect TypeScript accepts. Both
// readers of a JSON file in this ruleset go through it -- Gazelle directly,
// json_library through //gazelle/jsonc/strip -- so one file cannot mean two
// things depending on which side read it.
package jsonc

import "encoding/json"

// Unmarshal decodes JSON with comments and trailing commas, the dialect
// tsconfig.json is officially written in and TypeScript itself accepts.
func Unmarshal(data []byte, v any) error {
	return json.Unmarshal(Strip(data), v)
}

// Strip removes "//" and "/* */" comments and trailing commas,
// leaving the contents of string literals untouched.
func Strip(data []byte) []byte {
	out := make([]byte, 0, len(data))
	inString := false

	for i := 0; i < len(data); i++ {
		c := data[i]

		if inString {
			out = append(out, c)
			switch c {
			case '\\':
				if i+1 < len(data) {
					i++
					out = append(out, data[i])
				}
			case '"':
				inString = false
			}
			continue
		}

		switch {
		case c == '"':
			inString = true
			out = append(out, c)

		case c == '/' && i+1 < len(data) && data[i+1] == '/':
			for i < len(data) && data[i] != '\n' {
				i++
			}
			if i < len(data) {
				out = append(out, '\n')
			}

		case c == '/' && i+1 < len(data) && data[i+1] == '*':
			i += 2
			for i+1 < len(data) && !(data[i] == '*' && data[i+1] == '/') {
				i++
			}
			i++

		case c == '}' || c == ']':
			out = append(dropTrailingComma(out), c)

		default:
			out = append(out, c)
		}
	}

	return out
}

// dropTrailingComma removes a comma at the end of out, ignoring whitespace.
func dropTrailingComma(out []byte) []byte {
	end := len(out)
	for end > 0 {
		switch out[end-1] {
		case ' ', '\t', '\r', '\n':
			end--
		case ',':
			return out[:end-1]
		default:
			return out
		}
	}
	return out
}
