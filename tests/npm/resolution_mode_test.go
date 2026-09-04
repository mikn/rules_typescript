package npm_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

// What each dependent resolves must not depend on whether the resolver follows
// symlinks. `--preserve-symlinks` (Vite's `resolve.preserveSymlinks`) changes a
// module's IDENTITY -- its cache key and __filename -- and this ruleset already
// needs it both ways: ts_test turns it on because a DOM environment realpaths
// ids straight out of the sandbox, and tests/workers turns it back off because
// the pool would otherwise hold two identities for one file.
//
// Which version a dependent gets is a different question, and the answer has to
// be the same in both modes: the store keeps a package's own pinned
// dependencies beside it, and both modes reach them because the walk over
// `node_modules` candidates traverses the link either way. A layout that only
// resolved correctly under realpath would break silently the moment either flag
// flipped -- silently because the version that answered would be a real version
// of the real package.
const probeJS = `
const { createRequire } = require('module');
const { join, dirname } = require('path');
const { existsSync, readFileSync } = require('fs');
const tree = process.argv[2];
const req = createRequire(join(tree, 'probe.cjs'));
// A package may ship nested package.json markers -- minimatch has
// dist/commonjs/package.json holding only {"type":"commonjs"} -- so the walk
// stops at the first one naming the package it is looking for.
function packageDirOf(entry, name) {
  for (let d = dirname(entry); d !== dirname(d); d = dirname(d)) {
    const pj = join(d, 'package.json');
    if (!existsSync(pj)) continue;
    if (JSON.parse(readFileSync(pj, 'utf8')).name === name) return d;
  }
  throw new Error('no package.json naming ' + name + ' above ' + entry);
}
let from = join(tree, process.argv[3]);
for (const name of process.argv.slice(4)) {
  const pd = packageDirOf(req.resolve(name, { paths: [from] }), name);
  const v = JSON.parse(readFileSync(join(pd, 'package.json'), 'utf8')).version;
  process.stdout.write(name + '@' + v + '\t' + pd.slice(tree.length + 1) + '\n');
  from = pd;
}
`

func resolveChain(t *testing.T, node, probe, tree, from string, preserveSymlinks bool, names ...string) string {
	t.Helper()
	args := []string{}
	if preserveSymlinks {
		args = append(args, "--preserve-symlinks")
	}
	args = append(args, probe, tree, from)
	args = append(args, names...)
	out, err := exec.Command(node, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("node %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestResolutionDoesNotDependOnFollowingSymlinks(t *testing.T) {
	tr := verify.New(t)
	node := tr.FoundFile("*node_resolved/node").Abs()
	tree := tr.FoundDir("*/multi_version_node_modules").Abs()

	probe := filepath.Join(t.TempDir(), "probe.cjs")
	if err := os.WriteFile(probe, []byte(probeJS), 0o600); err != nil {
		t.Fatalf("writing probe: %v", err)
	}

	// glob pins the older major of all three, and reaches each through a link
	// into the store rather than through the top-level copy.
	const from = "glob"
	names := []string{"minimatch", "brace-expansion", "balanced-match"}
	const want = "minimatch@9.0.9, brace-expansion@2.0.2, balanced-match@1.0.2"

	followed := resolveChain(t, node, probe, tree, from, false, names...)
	lexical := resolveChain(t, node, probe, tree, from, true, names...)

	if versionsOf(followed) != want {
		t.Errorf("following symlinks, %s resolves %s, want %s",
			from, versionsOf(followed), want)
	}
	if versionsOf(lexical) != versionsOf(followed) {
		t.Errorf("--preserve-symlinks changes what %s resolves.\n"+
			"  following symlinks: %s\n"+
			"  lexically:          %s\n"+
			"Only the paths may differ between the two, never the versions.",
			from, versionsOf(followed), versionsOf(lexical))
	}

	// Without this the two modes could be behaving identically and the
	// comparison above would hold for the wrong reason.
	if followed == lexical {
		t.Errorf("both modes produced identical output, so this chain never crosses "+
			"a symlink and proves nothing about either mode:\n%s", followed)
	}
}

func columnOf(out string, i int) string {
	fields := []string{}
	for _, line := range strings.Split(out, "\n") {
		parts := strings.Split(line, "\t")
		if len(parts) > i {
			fields = append(fields, parts[i])
		}
	}
	return strings.Join(fields, ", ")
}

func versionsOf(out string) string { return columnOf(out, 0) }
