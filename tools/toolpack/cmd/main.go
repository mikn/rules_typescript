// Command toolpack writes one tools release asset from name=path arguments,
// one per toolpack.Binaries entry, and prints its SRI.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/mikn/rules_typescript/tools/toolpack"
)

func main() {
	version := flag.String("version", "", "the N of tools-v<N>")
	platform := flag.String("platform", "", "a key of //platforms:PLATFORMS")
	out := flag.String("out", "", "the asset to write")
	flag.Parse()
	if *version == "" || *platform == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "toolpack: -version, -platform and -out are required")
		os.Exit(2)
	}
	paths := map[string]string{}
	for _, arg := range flag.Args() {
		name, path, ok := strings.Cut(arg, "=")
		if !ok {
			fmt.Fprintf(os.Stderr, "toolpack: %q is not name=path\n", arg)
			os.Exit(2)
		}
		paths[name] = path
	}
	sri, err := toolpack.Pack(*out, *version, *platform, paths)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(sri)
}
