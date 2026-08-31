// Command strip rewrites a JSON-with-comments file as plain JSON, so that a
// build action can read it with a parser that only speaks strict JSON.
package main

import (
	"fmt"
	"os"

	"github.com/mikn/rules_typescript/gazelle/jsonc"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: strip <in.json> <out.json>")
		os.Exit(2)
	}
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "strip: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(os.Args[2], jsonc.Strip(data), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "strip: %v\n", err)
		os.Exit(1)
	}
}
