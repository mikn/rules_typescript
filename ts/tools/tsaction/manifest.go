// The manifest step stages a package.json as built: every source-file target
// rewritten to the file the compile emits from it, key order kept.

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

func runManifest(args []string) error {
	flags := flag.NewFlagSet("manifest", flag.ExitOnError)
	tsx := flags.String("tsx", ".js",
		"the extension a .tsx emits: .js, or .jsx under jsx: preserve")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 2 {
		return errors.New("manifest takes SRC and OUT")
	}
	data, err := os.ReadFile(flags.Arg(0))
	if err != nil {
		return err
	}
	out, err := manifestAsBuilt(data, *tsx)
	if err != nil {
		return fmt.Errorf("%s: %w", flags.Arg(0), err)
	}
	return os.WriteFile(flags.Arg(1), append(out, '\n'), 0o644)
}

// The field a target sits under decides what the compile emits for it.
var roleOfField = map[string]string{
	"main":    "js",
	"module":  "js",
	"browser": "js",
	"exports": "js",
	"imports": "js",
	"types":   "dts",
	"typings": "dts",
}

var declarationSuffixes = []string{".d.ts", ".d.mts", ".d.cts"}

func emittedFile(target, role, tsx string) string {
	var by map[string]string
	switch role {
	case "js":
		by = map[string]string{".tsx": tsx, ".ts": ".js", ".mts": ".mjs",
			".cts": ".cjs"}
	case "dts":
		by = map[string]string{".tsx": ".d.ts", ".ts": ".d.ts",
			".mts": ".d.mts", ".cts": ".d.cts"}
	default:
		return target
	}
	for _, suffix := range declarationSuffixes {
		if strings.HasSuffix(target, suffix) {
			return target
		}
	}
	for source, emitted := range by {
		if strings.HasSuffix(target, source) {
			return strings.TrimSuffix(target, source) + emitted
		}
	}
	return target
}

func childRole(role, key string) string {
	switch {
	case role == "root":
		if r, ok := roleOfField[key]; ok {
			return r
		}
		return "keep"
	case role == "js" && key == "types":
		return "dts"
	}
	return role
}

// One open object or array of the manifest being copied.
type manifestFrame struct {
	object  bool
	role    string
	key     bool
	count   int
	sawType bool
}

// manifestAsBuilt copies the manifest token by token, a target under a field
// naming files rewritten to the emitted one; no `type` set means ESM, the emit.
func manifestAsBuilt(data []byte, tsx string) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var out bytes.Buffer
	var stack []*manifestFrame
	role := "root"
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if len(stack) == 0 {
			if d, ok := tok.(json.Delim); !ok || d != '{' {
				return nil, errors.New("package.json is not an object")
			}
		}
		if d, ok := tok.(json.Delim); ok && (d == '}' || d == ']') {
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if len(stack) == 0 && !top.sawType {
				if top.count > 0 {
					out.WriteByte(',')
				}
				out.WriteString(`"type":"module"`)
			}
			out.WriteByte(byte(d))
			if len(stack) > 0 {
				stack[len(stack)-1].key = stack[len(stack)-1].object
			}
			continue
		}
		var top *manifestFrame
		if len(stack) > 0 {
			top = stack[len(stack)-1]
		}
		if top != nil && top.key {
			key := tok.(string)
			if top.count > 0 {
				out.WriteByte(',')
			}
			top.count++
			out.WriteString(quoteJSON(key) + ":")
			role = childRole(top.role, key)
			if len(stack) == 1 && key == "type" {
				top.sawType = true
			}
			top.key = false
			continue
		}
		if top != nil && !top.object {
			if top.count > 0 {
				out.WriteByte(',')
			}
			top.count++
			role = top.role
		}
		switch v := tok.(type) {
		case json.Delim:
			out.WriteByte(byte(v))
			stack = append(stack, &manifestFrame{
				object: v == '{',
				role:   role,
				key:    v == '{',
			})
			continue
		case string:
			out.WriteString(quoteJSON(emittedFile(v, role, tsx)))
		case json.Number:
			out.WriteString(v.String())
		case bool:
			fmt.Fprint(&out, v)
		case nil:
			out.WriteString("null")
		}
		if top != nil && top.object {
			top.key = true
		}
	}
	if len(stack) != 0 {
		return nil, errors.New("package.json ends inside a value")
	}
	return out.Bytes(), nil
}

// quoteJSON is the string's JSON form with < > & unescaped, as tsc and node
// read them either way.
func quoteJSON(s string) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		panic(err)
	}
	return strings.TrimSuffix(b.String(), "\n")
}
