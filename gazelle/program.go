package typescript

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"maps"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/bazelbuild/rules_go/go/runfiles"

	"github.com/mikn/rules_typescript/ts/tools/tsconfig"
)

// Linked in from gazelle/BUILD.bazel's x_defs; empty under a plain go build.
var tsgoRlocationpath string

// One tsconfig.json as tsgo lists it from the repository root: first-party
// paths print relative to the root, node_modules and the toolchain's libs under ../.
type program struct {
	dir         string
	files       []string
	roots       []string
	edges       []edge
	types       []typeEntry
	implicit    []typeEntry
	diagnostics []string
	unresolved  []string
	refused     string
}

type edgeKind int

const (
	edgeImport        edgeKind = iota
	edgeReference              // /// <reference path>
	edgeTypeReference          // /// <reference types>
)

type edge struct {
	kind      edgeKind
	from      string
	to        string
	specifier string
}

// A compilerOptions.types entry as written, and the file it resolved to.
type typeEntry struct {
	entry string
	file  string
}

// One run's listings, one store every directory's config shares: the packages
// and their first-party files, each walked directory's files, the chains.
type programStore struct {
	tsgoFlag string
	verbose  bool
	tsgo     string
	skipped  bool
	programs map[string]*program
	packages map[string]map[string]bool
	visited  map[string][]string
	files    map[string][]string
	walked   map[string]bool
	bases    map[string][]string
	extended map[string]bool
	// The vitest configs the generated tests name, listed together at the
	// first ask; vitestEdges is nil until then.
	vitestConfigs  map[string]bool
	vitestEdges    map[string][]edge
	installChecked bool
	noLockSaid     bool
}

func newProgramStore() *programStore {
	return &programStore{
		programs:      map[string]*program{},
		packages:      map[string]map[string]bool{},
		visited:       map[string][]string{},
		files:         map[string][]string{},
		walked:        map[string]bool{},
		bases:         map[string][]string{},
		extended:      map[string]bool{},
		vitestConfigs: map[string]bool{},
	}
}

func (s *programStore) say(format string, a ...any) {
	if s.verbose {
		log.Printf("typescript: "+format, a...)
	}
}

var errNoTsgo = errors.New("no tsgo binary: run the gazelle_typescript binary, pass -ts_tsgo=<path>, or set TSGO")

func (s *programStore) binary() (string, error) {
	if s.tsgo != "" {
		return s.tsgo, nil
	}
	var found, how string
	switch {
	case s.tsgoFlag != "":
		found, how = s.tsgoFlag, "-ts_tsgo"
	case os.Getenv("TSGO") != "":
		found, how = os.Getenv("TSGO"), "TSGO"
	case tsgoRlocationpath != "":
		p, err := runfiles.Rlocation(tsgoRlocationpath)
		if err != nil {
			return "", fmt.Errorf("the toolchain's tsgo is not in the runfiles (%v); pass -ts_tsgo=<path>", err)
		}
		found, how = p, "runfiles"
	default:
		return "", errNoTsgo
	}
	abs, err := filepath.Abs(found)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(abs); err != nil {
		return "", fmt.Errorf("tsgo from %s: %w", how, err)
	}
	s.tsgo = abs
	s.say("listing programs with %s (%s)", abs, how)
	return abs, nil
}

var tsSourceExtensions = []string{".ts", ".tsx", ".mts", ".cts"}

func (s *programStore) visit(rel string, files []string) {
	s.walked[rel] = true
	s.files[rel] = files
	for _, f := range files {
		if slices.Contains(tsSourceExtensions, path.Ext(f)) {
			s.visited[rel] = append(s.visited[rel], path.Join(rel, f))
		}
	}
}

// Listed from Configure, before any directory generates: every package and
// every base an extends names is known when a rule asks about an ancestor.
func listTsConfigProgram(repoRoot, rel string, tc *tsConfig) {
	store := tc.programs
	cfg := tsconfigIn(rel)
	store.readBases(repoRoot, rel)
	inputs, ok := programNamesInputs(filepath.Join(repoRoot, cfg))
	var refused string
	switch {
	case !ok:
		refused = "the file could not be read"
	case !inputs && rel == "":
		refused = "neither include nor files in its extends chain, so tsgo would enumerate the whole repository"
	}
	if refused != "" {
		store.record(&program{dir: rel, refused: refused})
		store.say("%s: not listed: %s", cfg, refused)
		return
	}
	tsgo, err := store.binary()
	if errors.Is(err, errNoTsgo) {
		// Nothing reads the listing yet, so a run without the binary goes on.
		if !store.skipped {
			store.skipped = true
			log.Printf("typescript: programs are not listed: %v", err)
		}
		return
	}
	if err != nil {
		log.Fatalf("typescript: %s: %v", cfg, err)
	}
	isle := islandManifest(repoRoot, rel, tc.lock)
	p, err := listProgram(repoRoot, rel, tsgo, isle != nil)
	if err != nil {
		log.Fatalf("typescript: %v", err)
	}
	store.record(p)
	if p.refused != "" {
		store.say("%s: not listed: %s", cfg, p.refused)
		return
	}
	if isle != nil && len(p.unresolved) > 0 {
		log.Printf("typescript: %s: %s is no importer in %s, so pnpm installs "+
			"nothing for it; tsgo could not resolve: %s", cfg,
			path.Join(isle.dir, "package.json"), pnpmLockfileName,
			strings.Join(p.unresolved, ", "))
	}
	// tsgo's diagnostics stay on the program and print under -ts_verbose only: a
	// types entry naming a generated file draws one on every run over a clean checkout.
	for _, d := range p.diagnostics {
		store.say("%s: %s", cfg, d)
	}
	if store.packages[rel] == nil {
		store.say("%s: not a package: its listing names no first-party file", cfg)
		return
	}
	store.say("%s: %d files listed, %d roots, %d edges, %d type entries",
		cfg, len(p.files), len(p.roots), len(p.edges), len(p.types))
}

// The visited .ts/.tsx/.mts/.cts files no listing names, once every directory
// has generated: one line per directory and a total.
func (s *programStore) reportUnlisted() {
	if !s.verbose || s.skipped {
		return
	}
	listed := map[string]bool{}
	for _, p := range s.programs {
		for _, f := range p.files {
			listed[f] = true
		}
	}
	total, dirs := 0, 0
	for _, rel := range slices.Sorted(maps.Keys(s.visited)) {
		n := 0
		for _, f := range s.visited[rel] {
			if !listed[f] {
				n++
			}
		}
		if n == 0 {
			continue
		}
		total += n
		dirs++
		log.Printf("typescript: %s: %d file%s in no program", path.Join(".", rel), n, plural(n, "s"))
	}
	log.Printf("typescript: %d .ts/.tsx/.mts/.cts file%s in no program across %d director%s",
		total, plural(total, "s"), dirs, plural(dirs, "ies", "y"))
}

func plural(n int, many string, one ...string) string {
	if n == 1 {
		return strings.Join(one, "")
	}
	return many
}

// readBases records the tsconfig.json files rel's own extends names, the
// ts_config deps; a base of another name has no ts_config and is said.
func (s *programStore) readBases(repoRoot, rel string) {
	own := filepath.Join(repoRoot, filepath.FromSlash(tsconfigIn(rel)))
	f, err := tsconfig.Read(own)
	if err != nil {
		return
	}
	dir := filepath.Join(repoRoot, filepath.FromSlash(rel))
	for _, spec := range f.Extends {
		basePath, ok := tsconfig.ResolveExtends(dir, spec)
		if !ok {
			continue
		}
		if st, err := os.Stat(basePath); err != nil || st.IsDir() {
			continue
		}
		baseRel, err := filepath.Rel(repoRoot, basePath)
		if err != nil || strings.HasPrefix(baseRel, "..") {
			continue
		}
		baseRel = filepath.ToSlash(baseRel)
		if path.Base(baseRel) != "tsconfig.json" {
			log.Printf("typescript: %s extends %q, a file Gazelle writes no "+
				"ts_config for (only a tsconfig.json is); declare that dep by "+
				"hand under # keep", tsconfigIn(rel), spec)
			continue
		}
		baseDir := parentDir(baseRel)
		if !slices.Contains(s.bases[rel], baseDir) {
			s.bases[rel] = append(s.bases[rel], baseDir)
		}
		s.extended[baseDir] = true
	}
}

// An island: a package whose nearest package.json is no lockfile importer, so
// pnpm installed nothing for it; a bare import there resolves by accident.
func islandManifest(repoRoot, rel string, lock *npmLock) *manifest {
	if lock == nil {
		return nil
	}
	m := nearestManifest(repoRoot, rel)
	if m != nil && lock.importers[m.dir] == nil {
		return m
	}
	return nil
}

func listProgram(repoRoot, rel, tsgo string, trace bool) (*program, error) {
	cfg := path.Join(rel, "tsconfig.json")
	// --pretty false: a FORCE_COLOR in the environment would otherwise colour the diagnostics.
	args := []string{"-p", cfg, "--noEmit", "--listFilesOnly", "--explainFiles",
		"--pretty", "false"}
	if trace {
		args = append(args, "--traceResolution")
	}
	p, err := runListing(repoRoot, tsgo, cfg, args)
	if err != nil {
		return nil, err
	}
	p.dir = rel
	return p, nil
}

// A vitest config's listing: no tsconfig.json is its program (--ignoreConfig,
// else TS5112), a .mjs is a root (--allowJs), and the runner's resolution.
var vitestConfigFlags = []string{"--noEmit", "--listFilesOnly",
	"--explainFiles", "--ignoreConfig", "--allowJs", "--module", "esnext",
	"--moduleResolution", "bundler", "--skipLibCheck", "--pretty", "false"}

func listVitestConfigs(repoRoot, tsgo string, configs []string,
) (*program, error) {
	args := append(slices.Clone(vitestConfigFlags), configs...)
	return runListing(repoRoot, tsgo, "vitest configs", args)
}

func (s *programStore) vitestConfig(cfg string) { s.vitestConfigs[cfg] = true }

// configEdges is what cfg imports, from one run over every registered config at
// the first ask: the runner imports the config, so its imports are the test's.
func (s *programStore) configEdges(repoRoot, cfg string) []edge {
	if s.vitestEdges == nil {
		s.vitestEdges = map[string][]edge{}
		if configs := slices.Sorted(maps.Keys(s.vitestConfigs)); len(configs) > 0 {
			tsgo, err := s.binary()
			if err != nil {
				log.Fatalf("typescript: vitest configs: %v", err)
			}
			p, err := listVitestConfigs(repoRoot, tsgo, configs)
			if err != nil {
				log.Fatalf("typescript: %v", err)
			}
			for _, e := range p.edges {
				if s.vitestConfigs[e.from] {
					s.vitestEdges[e.from] = append(s.vitestEdges[e.from], e)
				}
			}
		}
	}
	return s.vitestEdges[cfg]
}

// installMissing says why the listing cannot be trusted: a root lockfile with
// no node_modules from pnpm; tsgo prints no line for an unresolved import.
func installMissing(repoRoot string) string {
	if _, err := os.Stat(filepath.Join(repoRoot, pnpmLockfileName)); err != nil {
		return ""
	}
	marker := filepath.Join(repoRoot, "node_modules", ".modules.yaml")
	if _, err := os.Stat(marker); err == nil {
		return ""
	}
	return pnpmLockfileName + " is at the root and node_modules/.modules.yaml " +
		"is not: tsgo lists no edge into a package that is not installed, so " +
		"run pnpm install and rerun"
}

func (s *programStore) requireInstall(repoRoot string) {
	if s.installChecked {
		return
	}
	s.installChecked = true
	if why := installMissing(repoRoot); why != "" {
		log.Fatalf("typescript: %s", why)
	}
}

func (p *program) typeEdges() []edge {
	cfg := tsconfigIn(p.dir)
	var out []edge
	for _, entries := range [][]typeEntry{p.types, p.implicit} {
		for _, te := range entries {
			out = append(out, edge{kind: edgeTypeReference, from: cfg,
				to: te.file, specifier: te.entry})
		}
	}
	return out
}

// tsgo exits non-zero on any diagnostic and still lists what it could, so the
// listing is kept whatever the exit; TS18003 alone is a program with no inputs.
func runListing(repoRoot, tsgo, subject string, args []string,
) (*program, error) {
	cmd := exec.Command(tsgo, args...)
	cmd.Dir = repoRoot
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Run()
	p, err := parseListing(stdout.String())
	if err != nil {
		return nil, fmt.Errorf("%s: %w", subject, err)
	}
	switch {
	case runErr == nil:
		return p, nil
	case len(p.diagnostics) == 0:
		return nil, fmt.Errorf("%s: tsgo %s failed (%v) with no diagnostic:\n%s%s",
			subject, strings.Join(args, " "), runErr, stdout.String(),
			stderr.String())
	case len(p.files) > 0, noInputs(p):
		return p, nil
	}
	p.refused = fmt.Sprintf("tsgo %v: %s", runErr, strings.Join(p.diagnostics, "\n"))
	return p, nil
}

func noInputs(p *program) bool {
	for _, d := range p.diagnostics {
		if !strings.Contains(d, "error TS18003:") {
			return false
		}
	}
	return len(p.diagnostics) > 0
}

// A file prints on its own line, each reason for it indented three spaces; a
// diagnostic's continuation lines are indented two.
var diagnosticLine = regexp.MustCompile(`^(?:\S.*\(\d+,\d+\): )?error TS\d+: `)

// A --traceResolution block runs from its Resolving line to the line saying how
// it went, all before the listing; its prose sits at column 0 like a path.
const traceOpens = "======== Resolving "

var notResolved = regexp.MustCompile(
	`^======== (?:Module name|Type reference directive) '(.*)' was not ` +
		`resolved\. ========$`)

func parseListing(text string) (*program, error) {
	p := &program{}
	var file string
	inDiagnostic, inTrace := false, false
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case line == "":
		case inTrace:
			if !strings.HasPrefix(line, "========") {
				continue
			}
			inTrace = false
			m := notResolved.FindStringSubmatch(line)
			if m != nil && !slices.Contains(p.unresolved, m[1]) {
				p.unresolved = append(p.unresolved, m[1])
			}
		case strings.HasPrefix(line, traceOpens):
			inTrace = true
		case inDiagnostic && strings.HasPrefix(line, "  "):
			p.diagnostics[len(p.diagnostics)-1] += "\n" + line
		case strings.HasPrefix(line, "   "):
			if file == "" {
				return nil, fmt.Errorf("a reason before any file line: %q", line)
			}
			if err := readReason(p, file, line[3:]); err != nil {
				return nil, err
			}
		case diagnosticLine.MatchString(line):
			p.diagnostics = append(p.diagnostics, line)
			inDiagnostic = true
		default:
			file = line
			p.files = append(p.files, line)
			inDiagnostic = false
		}
	}
	return p, nil
}

func readReason(p *program, file, reason string) error {
	for _, f := range reasonForms {
		if m := f.re.FindStringSubmatch(reason); m != nil {
			f.read(p, file, m)
			return nil
		}
	}
	return fmt.Errorf("%s: unrecognised --explainFiles reason %q; the grammar is tsgo's, pinned in gazelle/program.go", file, reason)
}

// tsgo's --explainFiles templates for a program without project references,
// from its string table, each with what it says of its file.
type reasonForm struct {
	re   *regexp.Regexp
	read func(p *program, file string, m []string)
}

// A quoted value runs to the quote before its form's next literal token, so a
// quote inside a path parses; a family's longer forms come first for the same reason.
const (
	quoted    = `'(.*?)'`
	specifier = `(".*?"|'.*?')`
	packageID = ` with packageId '.*?'`
)

func form(pattern string, read func(p *program, file string, m []string)) reasonForm {
	return reasonForm{regexp.MustCompile("^" + pattern + "$"), read}
}

func asRoot(p *program, file string, _ []string) { p.roots = append(p.roots, file) }

func asNothing(*program, string, []string) {}

func asEdge(kind edgeKind) func(p *program, file string, m []string) {
	return func(p *program, file string, m []string) {
		spec := m[1]
		if kind == edgeImport {
			spec = spec[1 : len(spec)-1]
		}
		p.edges = append(p.edges, edge{kind: kind, from: m[2], to: file, specifier: spec})
	}
}

func asTypeEntry(p *program, file string, m []string) {
	p.types = append(p.types, typeEntry{entry: m[1], file: file})
}

func asImplicit(p *program, file string, m []string) {
	p.implicit = append(p.implicit, typeEntry{entry: m[1], file: file})
}

var reasonForms = []reasonForm{
	form(`Imported via `+specifier+` from file `+quoted+packageID+` to import 'jsx' and 'jsxs' factory functions`, asEdge(edgeImport)),
	form(`Imported via `+specifier+` from file `+quoted+packageID+` to import 'importHelpers' as specified in compilerOptions`, asEdge(edgeImport)),
	form(`Imported via `+specifier+` from file `+quoted+packageID, asEdge(edgeImport)),
	form(`Imported via `+specifier+` from file `+quoted+` to import 'jsx' and 'jsxs' factory functions`, asEdge(edgeImport)),
	form(`Imported via `+specifier+` from file `+quoted+` to import 'importHelpers' as specified in compilerOptions`, asEdge(edgeImport)),
	form(`Imported via `+specifier+` from file `+quoted, asEdge(edgeImport)),
	form(`Referenced via `+quoted+` from file `+quoted, asEdge(edgeReference)),
	form(`Type library referenced via `+quoted+` from file `+quoted+packageID, asEdge(edgeTypeReference)),
	form(`Type library referenced via `+quoted+` from file `+quoted, asEdge(edgeTypeReference)),
	form(`Entry point of type library `+quoted+` specified in compilerOptions`+packageID, asTypeEntry),
	form(`Entry point of type library `+quoted+` specified in compilerOptions`, asTypeEntry),
	form(`Entry point for implicit type library `+quoted+packageID, asImplicit),
	form(`Entry point for implicit type library `+quoted, asImplicit),
	form(`Matched by include pattern `+quoted+` in `+quoted, asRoot),
	form(`Matched by default include pattern `+quoted, asRoot),
	form(`Part of 'files' list in tsconfig.json`, asRoot),
	form(`Root file specified for compilation`, asRoot),
	form(`Library referenced via `+quoted+` from file `+quoted, asNothing),
	form(`Library `+quoted+` specified in compilerOptions`, asNothing),
	form(`Default library for target `+quoted, asNothing),
	form(`Default library`, asNothing),
	form(`File is ECMAScript module because `+quoted+` has field "type" with value "module"`, asNothing),
	form(`File is CommonJS module because `+quoted+` has field "type" whose value is not "module"`, asNothing),
	form(`File is CommonJS module because `+quoted+` does not have field "type"`, asNothing),
	form(`File is CommonJS module because 'package.json' was not found`, asNothing),
	form(`File redirects to file `+quoted, asNothing),
}
