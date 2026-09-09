// lcov_merger is ts_test's _lcov_merger: the .dat files under COVERAGE_DIR
// merged into the report, kept to what the coverage manifest selected.
package main

import (
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func main() { os.Exit(run(os.Args[1:], os.Stderr)) }

type patterns []string

func (p *patterns) String() string     { return strings.Join(*p, ",") }
func (p *patterns) Set(v string) error { *p = append(*p, v); return nil }

func run(args []string, stderr io.Writer) int {
	flags := flag.NewFlagSet("lcov_merger", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dir := flags.String("coverage_dir", "", "the run's .dat files")
	out := flags.String("output_file", "", "where the report goes")
	manifest := flags.String("source_file_manifest", "",
		"the files --instrumentation_filter selected, one per line")
	var drop patterns
	flags.Var(&drop, "filter_sources", "regex over SF: paths to drop")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *dir == "" || *out == "" || *manifest == "" || flags.NArg() > 0 {
		fmt.Fprintln(stderr, "lcov_merger: --coverage_dir, --output_file and "+
			"--source_file_manifest are required")
		return 2
	}
	report, err := Merge(*dir, *manifest, drop)
	if err == nil {
		err = os.WriteFile(*out, report, 0o644)
	}
	if err != nil {
		fmt.Fprintln(stderr, "lcov_merger:", err)
		return 1
	}
	return 0
}

// Merge is the report: every record in the .dat files under dir whose file the
// manifest selects and no pattern in drop matches whole.
func Merge(dir, manifest string, drop []string) ([]byte, error) {
	selected, err := readManifest(manifest)
	if err != nil {
		return nil, err
	}
	dropping := make([]*regexp.Regexp, 0, len(drop))
	for _, p := range drop {
		re, err := regexp.Compile("^(?:" + p + ")$")
		if err != nil {
			return nil, err
		}
		dropping = append(dropping, re)
	}
	var kept []string
	walk := func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".dat") {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, record := range splitRecords(string(data)) {
			file := sourceFile(record)
			if selected[Key(file)] && !anyMatch(dropping, file) {
				kept = append(kept, record)
			}
		}
		return nil
	}
	if err := filepath.WalkDir(dir, walk); err != nil {
		return nil, err
	}
	if len(kept) == 0 {
		return nil, nil
	}
	return []byte(strings.Join(kept, "\n") + "\n"), nil
}

// splitRecords cuts an lcov file at its end_of_record lines; lcov's TN: line
// precedes the SF: it belongs to, so it travels with the record that follows.
func splitRecords(text string) []string {
	var records, record []string
	for _, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		record = append(record, line)
		if line == "end_of_record" {
			records = append(records, strings.Join(record, "\n"))
			record = nil
		}
	}
	if len(record) > 0 {
		records = append(records, strings.Join(record, "\n"))
	}
	return records
}

func sourceFile(record string) string {
	for _, line := range strings.Split(record, "\n") {
		if rest, ok := strings.CutPrefix(line, "SF:"); ok {
			return rest
		}
	}
	return ""
}

// readManifest is the set Bazel selected, by Key: the manifest names the .ts
// a target declared, the report the .js compiled from it.
func readManifest(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	selected := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		if key := Key(line); key != "" {
			selected[key] = true
		}
	}
	return selected, nil
}

// Key names a file across its two spellings, the source's workspace path and
// the output's exec path under bazel-out, with the extension dropped.
func Key(p string) string {
	p = filepath.ToSlash(strings.TrimSpace(p))
	if p == "" {
		return ""
	}
	if rest, ok := strings.CutPrefix(p, "bazel-out/"); ok {
		if parts := strings.SplitN(rest, "/", 3); len(parts) == 3 {
			p = parts[2]
		}
	}
	return strings.TrimSuffix(p, filepath.Ext(p))
}

func anyMatch(res []*regexp.Regexp, s string) bool {
	for _, re := range res {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}
