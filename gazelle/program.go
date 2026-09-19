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
	"slices"
	"strings"

	"github.com/bazelbuild/rules_go/go/runfiles"

	"github.com/mikn/rules_typescript/ts/tools/explainfiles"
	"github.com/mikn/rules_typescript/ts/tools/tsconfig"
)

// Linked in from gazelle/BUILD.bazel's x_defs; empty under a plain go build.
var tsgoRlocationpath string

// One tsconfig.json as tsgo lists it from the repository root, first-party
// paths relative to it and the compiler's libs under ../ or bundled:///.
type program struct {
	explainfiles.Listing
	dir     string
	refused string
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
	// Per walked directory, the package.json that makes it a foreign project.
	foreign     map[string]string
	foreignSaid map[string]bool
	// The vitest configs the generated tests name, listed together at the
	// first ask into vitestEdges, the listing's edges by importing file.
	vitestConfigs   map[string]bool
	vitestEdges     map[string][]explainfiles.Edge
	wranglerConfigs map[string]string
	installChecked  bool
	noLockSaid      bool
}

func newProgramStore() *programStore {
	return &programStore{
		programs:        map[string]*program{},
		packages:        map[string]map[string]bool{},
		visited:         map[string][]string{},
		files:           map[string][]string{},
		walked:          map[string]bool{},
		bases:           map[string][]string{},
		extended:        map[string]bool{},
		foreign:         map[string]string{},
		foreignSaid:     map[string]bool{},
		vitestConfigs:   map[string]bool{},
		wranglerConfigs: map[string]string{},
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
	if s.foreign[rel] != "" {
		return
	}
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
	if m := tc.foreignManifest; m != "" {
		refused := foreignReason(m)
		store.record(&program{dir: rel, refused: refused})
		store.say("%s: not listed: %s", cfg, refused)
		store.sayForeign(m)
		return
	}
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
	p, err := listProgram(repoRoot, rel, tsgo)
	if err != nil {
		log.Fatalf("typescript: %v", err)
	}
	store.record(p)
	if p.refused != "" {
		store.say("%s: not listed: %s", cfg, p.refused)
		return
	}
	// tsgo's diagnostics stay on the program and print under -ts_verbose only: a
	// types entry naming a generated file draws one on every run over a clean checkout.
	for _, d := range p.Diagnostics {
		store.say("%s: %s", cfg, d)
	}
	if store.packages[rel] == nil {
		store.say("%s: not a package: its listing names no first-party file", cfg)
		return
	}
	store.say("%s: %d files listed, %d roots, %d edges, %d type entries",
		cfg, len(p.Files), len(p.Roots), len(p.Edges), len(p.Types))
}

// The visited .ts/.tsx/.mts/.cts files no listing names, once every directory
// has generated: one line per directory and a total.
func (s *programStore) reportUnlisted() {
	if !s.verbose || s.skipped {
		return
	}
	listed := map[string]bool{}
	for _, p := range s.programs {
		for _, f := range p.Files {
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

func foreignReason(manifest string) string {
	return manifest + " is no importer in " + pnpmLockfileName
}

// Once per manifest, whatever the run finds under it.
func (s *programStore) sayForeign(manifest string) {
	if s.foreignSaid[manifest] {
		return
	}
	s.foreignSaid[manifest] = true
	log.Printf("typescript: %s, so pnpm installs nothing for it; nothing is "+
		"written under %s", foreignReason(manifest),
		orRepoRoot(parentDir(manifest)))
}

func listProgram(repoRoot, rel, tsgo string) (*program, error) {
	cfg := path.Join(rel, "tsconfig.json")
	// --pretty false: a FORCE_COLOR in the environment would otherwise colour the diagnostics.
	args := []string{"-p", cfg, "--noEmit", "--listFilesOnly", "--explainFiles",
		"--pretty", "false"}
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

// configEdges is what cfg and every first-party module it reaches import:
// the runner imports the config, so the closure's imports are the test's.
func (s *programStore) configEdges(repoRoot, cfg string) []explainfiles.Edge {
	var out []explainfiles.Edge
	for _, f := range s.configClosure(repoRoot, cfg) {
		out = append(out, s.vitestEdges[f]...)
	}
	return out
}

// configSrcs is the first-party modules cfg reaches, sorted.
func (s *programStore) configSrcs(repoRoot, cfg string) []string {
	closure := s.configClosure(repoRoot, cfg)
	if len(closure) < 2 {
		return nil
	}
	return slices.Sorted(slices.Values(closure[1:]))
}

// configClosure is cfg, then every first-party file reached from it over the
// listing's edges: one run over every registered config, at the first ask.
func (s *programStore) configClosure(repoRoot, cfg string) []string {
	if s.vitestEdges == nil {
		s.vitestEdges = map[string][]explainfiles.Edge{}
		if configs := slices.Sorted(maps.Keys(s.vitestConfigs)); len(configs) > 0 {
			tsgo, err := s.binary()
			if err != nil {
				log.Fatalf("typescript: vitest configs: %v", err)
			}
			p, err := listVitestConfigs(repoRoot, tsgo, configs)
			if err != nil {
				log.Fatalf("typescript: %v", err)
			}
			for _, e := range p.Edges {
				s.vitestEdges[e.From] = append(s.vitestEdges[e.From], e)
			}
		}
	}
	if !s.vitestConfigs[cfg] {
		return nil
	}
	seen := map[string]bool{cfg: true}
	closure := []string{cfg}
	for i := 0; i < len(closure); i++ {
		for _, e := range s.vitestEdges[closure[i]] {
			if firstParty(e.To) && !seen[e.To] {
				seen[e.To] = true
				closure = append(closure, e.To)
			}
		}
	}
	return closure
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

func (p *program) typeEdges() []explainfiles.Edge {
	cfg := tsconfigIn(p.dir)
	var out []explainfiles.Edge
	for _, entries := range [][]explainfiles.TypeEntry{p.Types, p.Implicit} {
		for _, te := range entries {
			out = append(out, explainfiles.Edge{Kind: explainfiles.TypeReference,
				From: cfg, To: te.File, Specifier: te.Entry})
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
	l, err := explainfiles.Parse(stdout.String())
	if err != nil {
		return nil, fmt.Errorf("%s: %w", subject, err)
	}
	p := &program{Listing: *l}
	switch {
	case runErr == nil:
		return p, nil
	case len(p.Diagnostics) == 0:
		return nil, fmt.Errorf("%s: tsgo %s failed (%v) with no diagnostic:\n%s%s",
			subject, strings.Join(args, " "), runErr, stdout.String(),
			stderr.String())
	case len(p.Files) > 0, noInputs(p):
		return p, nil
	}
	p.refused = fmt.Sprintf("tsgo %v: %s", runErr,
		strings.Join(p.Diagnostics, "\n"))
	return p, nil
}

func noInputs(p *program) bool {
	for _, d := range p.Diagnostics {
		if !strings.Contains(d, "error TS18003:") {
			return false
		}
	}
	return len(p.Diagnostics) > 0
}
