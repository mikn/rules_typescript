package typescript

import (
	"log"
	"maps"
	"path"
	"slices"
	"strings"
)

// A listed path is first-party when it lies inside the repository: not above
// it, not absolute and not under a node_modules directory.
func firstParty(f string) bool {
	if strings.HasPrefix(f, "../") || strings.HasPrefix(f, "/") {
		return false
	}
	return !slices.Contains(strings.Split(f, "/"), "node_modules")
}

// record keeps a listing; a program naming a first-party file is a package.
func (s *programStore) record(p *program) {
	s.programs[p.dir] = p
	files := map[string]bool{}
	for _, f := range p.files {
		if firstParty(f) {
			files[f] = true
		}
	}
	if len(files) > 0 {
		s.packages[p.dir] = files
	}
}

func (s *programStore) packageDirs() []string {
	return slices.Sorted(maps.Keys(s.packages))
}

// owner is the package that owns f: the nearest package at or above f's
// directory, when its program lists f. "" says no package does.
func (s *programStore) owner(f string) string {
	if !firstParty(f) || !s.walked[parentDir(f)] {
		return ""
	}
	for dir := parentDir(f); ; dir = parentDir(dir) {
		if files, ok := s.packages[dir]; ok {
			if files[f] {
				return dir
			}
			return ""
		}
		if dir == "" {
			return ""
		}
	}
}

func parentDir(p string) string {
	if dir := path.Dir(p); dir != "." {
		return dir
	}
	return ""
}

type fileClass int

const (
	libraryFile fileClass = iota
	testFile
	declarationFile
)

// A declaration file, .d.mts and .d.cts included: tsc reads each as a script.
func isDeclarationFile(name string) bool {
	return strings.HasSuffix(name, ".d.ts") || strings.HasSuffix(name, ".d.mts") ||
		strings.HasSuffix(name, ".d.cts")
}

// *.test.* and *.spec.*, whatever the source extension.
func isTestFile(name string) bool {
	base := strings.TrimSuffix(name, path.Ext(name))
	return strings.HasSuffix(base, ".test") || strings.HasSuffix(base, ".spec")
}

func classify(f string) fileClass {
	base := path.Base(f)
	switch {
	case isDeclarationFile(base):
		return declarationFile
	case isTestFile(base):
		return testFile
	}
	return libraryFile
}

// The files a package owns, by class and sorted.
type srcSet struct {
	library, test, declaration []string
}

func (a srcSet) equal(b srcSet) bool {
	return slices.Equal(a.library, b.library) && slices.Equal(a.test, b.test) &&
		slices.Equal(a.declaration, b.declaration)
}

// srcs is what pkg's targets compile: the files it owns, less those under a
// codegen out_dir, which are that rule's output whatever a local run left.
func (s *programStore) srcs(pkg string, tc *tsConfig) srcSet {
	var set srcSet
	for _, f := range slices.Sorted(maps.Keys(s.packages[pkg])) {
		if s.owner(f) != pkg {
			continue
		}
		if _, out := codegenOutDirOwning(parentDir(f), tc); out {
			continue
		}
		switch classify(f) {
		case declarationFile:
			set.declaration = append(set.declaration, f)
		case testFile:
			set.test = append(set.test, f)
		default:
			set.library = append(set.library, f)
		}
	}
	return set
}

// Every first-party file some program lists and no package owns, with the
// programs that reached it, once the walk is done.
func (s *programStore) reportUnowned() {
	// A run over one subtree has not listed the packages above it.
	if s.skipped || !s.walked[""] {
		return
	}
	listedBy := map[string][]string{}
	for _, dir := range s.packageDirs() {
		for f := range s.packages[dir] {
			listedBy[f] = append(listedBy[f], tsconfigIn(dir))
		}
	}
	for _, f := range slices.Sorted(maps.Keys(listedBy)) {
		if s.owner(f) == "" {
			log.Printf("typescript: %s: %s; listed by %s", f, s.whyUnowned(f),
				strings.Join(listedBy[f], ", "))
		}
	}
}

func (s *programStore) whyUnowned(f string) string {
	dir := parentDir(f)
	if !s.walked[dir] {
		return "under " + dir + ", which this run did not walk (excluded or ignored)"
	}
	for ; ; dir = parentDir(dir) {
		if _, ok := s.packages[dir]; ok {
			return "no package owns it: " + tsconfigIn(dir) +
				", the nearest, does not list it"
		}
		if dir == "" {
			return "no package owns it: no tsconfig.json above it lists a file"
		}
	}
}

func tsconfigIn(dir string) string {
	return path.Join(dir, "tsconfig.json")
}

// Under -ts_verbose, once the walk is done: the tsconfig.json met and how
// many are packages.
func (s *programStore) reportCensus() {
	if !s.verbose || s.skipped {
		return
	}
	packages, refused := 0, 0
	for _, p := range s.programs {
		switch {
		case p.refused != "":
			refused++
		case s.packages[p.dir] != nil:
			packages++
		}
	}
	log.Printf("typescript: %d tsconfig.json: %d package%s, %d refused, "+
		"%d listing no first-party file", len(s.programs), packages,
		plural(packages, "s"), refused, len(s.programs)-packages-refused)
}
