// Package verify asserts over the build outputs a Bazel test finds in its runfiles.
//
// Every assertion reports through testing.T.Errorf rather than stopping, so one
// run names every broken output instead of only the first.
package verify

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/bazelbuild/rules_go/go/runfiles"
)

const contentExcerpt = 2000

// The stub bundle body; a real bundle must never still contain it.
const PlaceholderBundle = "Placeholder bundle"

type Tree struct {
	t        *testing.T
	rf       *runfiles.Runfiles
	repo     string
	root     string
	manifest map[string]string
	entries  []Entry
}

type Entry struct {
	Rel   string // slash path from the runfiles root; the first segment is a repository
	Abs   string
	IsDir bool
}

func New(t *testing.T) *Tree {
	t.Helper()
	rf, err := runfiles.New()
	if err != nil {
		t.Fatalf("runfiles: %v", err)
	}
	repo := os.Getenv("TEST_WORKSPACE")
	if repo == "" {
		repo = "_main"
	}
	tr := &Tree{t: t, rf: rf, repo: repo}
	for _, dir := range []string{os.Getenv("RUNFILES_DIR"), os.Getenv("TEST_SRCDIR")} {
		if dir == "" {
			continue
		}
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
			tr.root = dir
			break
		}
	}
	if tr.root == "" {
		tr.manifest = readManifest(t, os.Getenv("RUNFILES_MANIFEST_FILE"))
	}
	if tr.root == "" && len(tr.manifest) == 0 {
		t.Fatal("no runfiles: RUNFILES_DIR, TEST_SRCDIR and RUNFILES_MANIFEST_FILE are all unusable")
	}
	return tr
}

// Path resolves a path relative to this repository into an absolute one.
func (tr *Tree) Path(rel string) string {
	full := path.Join(tr.repo, rel)
	if p, err := tr.rf.Rlocation(full); err == nil && p != "" {
		return p
	}
	if tr.root != "" {
		return filepath.Join(tr.root, filepath.FromSlash(full))
	}
	if p, ok := tr.manifest[full]; ok {
		return p
	}
	return full
}

// File names a build output relative to this repository. Nothing is read yet, so
// a path that does not exist is reported by whichever assertion needs it.
func (tr *Tree) File(rel string) File {
	return File{t: tr.t, name: rel, abs: tr.Path(rel)}
}

// Dir names an output directory relative to this repository.
func (tr *Tree) Dir(rel string) Dir {
	return Dir{t: tr.t, name: rel, abs: tr.Path(rel)}
}

func (tr *Tree) Absent(rel string) {
	tr.t.Helper()
	if _, err := os.Lstat(tr.Path(rel)); err == nil {
		tr.t.Errorf("%s exists, and must not", rel)
	}
}

// FoundFile is the one file in the runfiles tree whose path matches pattern and
// none of excluding, in which "*" spans directory separators the way find -path
// does. Symlinks are followed, which is what makes it see anything at all: a
// runfiles entry is a symlink, so a walk that stats the link finds no regular
// files.
func (tr *Tree) FoundFile(pattern string, excluding ...string) File {
	tr.t.Helper()
	e, ok := tr.findOne(pattern, false, excluding...)
	if !ok {
		return File{t: tr.t, name: pattern}
	}
	return File{t: tr.t, name: e.Rel, abs: e.Abs}
}

// FoundDir is the FoundFile of directories.
func (tr *Tree) FoundDir(pattern string, excluding ...string) Dir {
	tr.t.Helper()
	e, ok := tr.findOne(pattern, true, excluding...)
	if !ok {
		return Dir{t: tr.t, name: pattern}
	}
	return Dir{t: tr.t, name: e.Rel, abs: e.Abs}
}

// Find lists every runfile matching pattern, directories included.
func (tr *Tree) Find(pattern string) []Entry {
	re := globRE(pattern)
	var out []Entry
	for _, e := range tr.walk() {
		if re.MatchString(e.Rel) {
			out = append(out, e)
		}
	}
	return out
}

func (tr *Tree) findOne(pattern string, wantDir bool, excluding ...string) (Entry, bool) {
	tr.t.Helper()
	skip := make([]*regexp.Regexp, len(excluding))
	for i, e := range excluding {
		skip[i] = globRE(e)
	}
	var hits []Entry
	for _, e := range tr.Find(pattern) {
		if e.IsDir != wantDir || slices.ContainsFunc(skip, func(re *regexp.Regexp) bool {
			return re.MatchString(e.Rel)
		}) {
			continue
		}
		hits = append(hits, e)
	}
	kind := "file"
	if wantDir {
		kind = "directory"
	}
	switch len(hits) {
	case 1:
		return hits[0], true
	case 0:
		tr.t.Errorf("no %s in runfiles matches %q\nruntree:\n%s", kind, pattern, tr.sample())
		return Entry{}, false
	default:
		names := make([]string, 0, len(hits))
		for _, h := range hits {
			names = append(names, h.Rel)
		}
		tr.t.Errorf("%d %ss in runfiles match %q, want exactly one:\n  %s",
			len(hits), kind, pattern, strings.Join(names, "\n  "))
		return Entry{}, false
	}
}

func (tr *Tree) sample() string {
	const limit = 40
	all := tr.walk()
	names := make([]string, 0, len(all))
	for _, e := range all {
		names = append(names, e.Rel)
	}
	sort.Strings(names)
	if len(names) > limit {
		return strings.Join(names[:limit], "\n") + fmt.Sprintf("\n... and %d more", len(names)-limit)
	}
	return strings.Join(names, "\n")
}

func (tr *Tree) walk() []Entry {
	if tr.entries != nil {
		return tr.entries
	}
	if tr.root != "" {
		tr.entries = walkFollowingSymlinks(tr.root)
	} else {
		tr.entries = manifestEntries(tr.manifest)
	}
	if tr.entries == nil {
		tr.entries = []Entry{}
	}
	return tr.entries
}

func walkFollowingSymlinks(root string) []Entry {
	var out []Entry
	visited := map[string]bool{}
	var recurse func(abs, rel string)
	recurse = func(abs, rel string) {
		real, err := filepath.EvalSymlinks(abs)
		if err != nil || visited[real] {
			return
		}
		visited[real] = true
		children, err := os.ReadDir(abs)
		if err != nil {
			return
		}
		for _, c := range children {
			childAbs := filepath.Join(abs, c.Name())
			childRel := path.Join(rel, c.Name())
			fi, err := os.Stat(childAbs)
			if err != nil {
				continue
			}
			out = append(out, Entry{Rel: childRel, Abs: childAbs, IsDir: fi.IsDir()})
			if fi.IsDir() {
				recurse(childAbs, childRel)
			}
		}
	}
	recurse(root, "")
	return out
}

func manifestEntries(manifest map[string]string) []Entry {
	dirs := map[string]bool{}
	out := make([]Entry, 0, len(manifest))
	for rel, abs := range manifest {
		out = append(out, Entry{Rel: rel, Abs: abs})
		for d := path.Dir(rel); d != "." && d != "/"; d = path.Dir(d) {
			dirs[d] = true
		}
	}
	for d := range dirs {
		out = append(out, Entry{Rel: d, IsDir: true})
	}
	return out
}

func readManifest(t *testing.T, file string) map[string]string {
	if file == "" {
		return nil
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("reading RUNFILES_MANIFEST_FILE %s: %v", file, err)
	}
	out := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		if line == "" || strings.HasPrefix(line, " ") {
			continue
		}
		rel, abs, ok := strings.Cut(line, " ")
		if ok && abs != "" {
			out[rel] = abs
		}
	}
	return out
}

func globRE(pattern string) *regexp.Regexp {
	var b strings.Builder
	b.WriteString("^")
	for _, r := range pattern {
		switch r {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	b.WriteString("$")
	return regexp.MustCompile(b.String())
}

type File struct {
	t    *testing.T
	name string
	abs  string
}

// Name is how the file was asked for, not its base name.
func (f File) Name() string { return f.name }

// Abs is the absolute path, empty when the lookup that produced this File failed.
func (f File) Abs() string { return f.abs }

func (f File) Exists() bool {
	f.t.Helper()
	if f.abs == "" {
		return false
	}
	fi, err := os.Stat(f.abs)
	if err != nil {
		f.t.Errorf("%s: not in runfiles (%s)", f.name, f.abs)
		return false
	}
	if fi.IsDir() {
		f.t.Errorf("%s: is a directory, want a file", f.name)
		return false
	}
	return true
}

// Text is the file's content, empty (with a failure recorded) when unreadable.
func (f File) Text() string {
	f.t.Helper()
	if f.abs == "" {
		return ""
	}
	raw, err := os.ReadFile(f.abs)
	if err != nil {
		f.t.Errorf("%s: %v", f.name, err)
		return ""
	}
	return string(raw)
}

// Size is the file's length in bytes, -1 when it cannot be measured.
func (f File) Size() int64 {
	f.t.Helper()
	if f.abs == "" {
		return -1
	}
	fi, err := os.Stat(f.abs)
	if err != nil {
		f.t.Errorf("%s: %v", f.name, err)
		return -1
	}
	return fi.Size()
}

// Contains asserts every want appears.
func (f File) Contains(wants ...string) {
	f.t.Helper()
	if !f.Exists() {
		return
	}
	text := f.Text()
	for _, want := range wants {
		if !strings.Contains(text, want) {
			f.t.Errorf("%s does not contain %q\ncontent:\n%s", f.name, want, excerpt(text))
		}
	}
}

// Excludes asserts no unwanted appears.
func (f File) Excludes(unwanted ...string) {
	f.t.Helper()
	if !f.Exists() {
		return
	}
	text := f.Text()
	for _, bad := range unwanted {
		if strings.Contains(text, bad) {
			f.t.Errorf("%s contains %q, and must not\ncontent:\n%s", f.name, bad, excerpt(text))
		}
	}
}

// MatchesRE asserts every expression matches.
func (f File) MatchesRE(exprs ...string) {
	f.t.Helper()
	if !f.Exists() {
		return
	}
	text := f.Text()
	for _, expr := range exprs {
		if !regexp.MustCompile(expr).MatchString(text) {
			f.t.Errorf("%s matches no /%s/\ncontent:\n%s", f.name, expr, excerpt(text))
		}
	}
}

// ExcludesRE asserts no expression matches.
func (f File) ExcludesRE(exprs ...string) {
	f.t.Helper()
	if !f.Exists() {
		return
	}
	text := f.Text()
	for _, expr := range exprs {
		if loc := regexp.MustCompile(expr).FindString(text); loc != "" {
			f.t.Errorf("%s matches /%s/ at %q, and must not\ncontent:\n%s", f.name, expr, loc, excerpt(text))
		}
	}
}

func (f File) JSON(out any) {
	f.t.Helper()
	if !f.Exists() {
		return
	}
	text := f.Text()
	if err := json.Unmarshal([]byte(text), out); err != nil {
		f.t.Errorf("%s is not valid JSON: %v\ncontent:\n%s", f.name, err, excerpt(text))
	}
}

type Dir struct {
	t    *testing.T
	name string
	abs  string
}

func (d Dir) Name() string { return d.name }

// Abs is the absolute path, empty when the lookup that produced this Dir failed.
func (d Dir) Abs() string { return d.abs }

func (d Dir) Exists() bool {
	d.t.Helper()
	if d.abs == "" {
		return false
	}
	fi, err := os.Stat(d.abs)
	if err != nil {
		d.t.Errorf("%s: not in runfiles (%s)", d.name, d.abs)
		return false
	}
	if !fi.IsDir() {
		d.t.Errorf("%s: is a file, want a directory", d.name)
		return false
	}
	return true
}

func (d Dir) File(rel string) File {
	if d.abs == "" {
		return File{t: d.t, name: path.Join(d.name, rel)}
	}
	return File{t: d.t, name: path.Join(d.name, rel), abs: filepath.Join(d.abs, filepath.FromSlash(rel))}
}

func (d Dir) Absent(rel string) {
	d.t.Helper()
	if d.abs == "" {
		return
	}
	if _, err := os.Lstat(filepath.Join(d.abs, filepath.FromSlash(rel))); err == nil {
		d.t.Errorf("%s exists, and must not", path.Join(d.name, rel))
	}
}

// Glob lists the files under the directory whose base name matches pattern,
// recursively and through symlinks. It fails when nothing matches, so an empty
// tree cannot look like a satisfied assertion.
func (d Dir) Glob(pattern string) []File {
	d.t.Helper()
	if !d.Exists() {
		return nil
	}
	re := globRE(pattern)
	var out []File
	for _, e := range walkFollowingSymlinks(d.abs) {
		if !e.IsDir && re.MatchString(path.Base(e.Rel)) {
			out = append(out, File{t: d.t, name: path.Join(d.name, e.Rel), abs: e.Abs})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	if len(out) == 0 {
		d.t.Errorf("%s holds no file matching %q", d.name, pattern)
	}
	return out
}

// AnyContains asserts at least one of the files matching pattern holds want.
func (d Dir) AnyContains(pattern, want string) {
	d.t.Helper()
	files := d.Glob(pattern)
	var seen []string
	for _, f := range files {
		if strings.Contains(f.Text(), want) {
			return
		}
		seen = append(seen, f.Name())
	}
	if len(files) > 0 {
		d.t.Errorf("no %q file in %s contains %q; looked at:\n  %s",
			pattern, d.name, want, strings.Join(seen, "\n  "))
	}
}

func excerpt(text string) string {
	if len(text) <= contentExcerpt {
		return text
	}
	return text[:contentExcerpt] + "\n... truncated"
}
