package tsgo

import (
	"debug/buildinfo"
	"os"
	"strings"
	"testing"

	"github.com/bazelbuild/rules_go/go/runfiles"
)

// Linked in from BUILD.bazel's x_defs, the pin @tsgo_source//:defs.bzl holds.
var (
	pinnedModule  string
	pinnedVersion string
)

func TestLockfileCompilerIsThePinnedCommit(t *testing.T) {
	path, err := runfiles.Rlocation(os.Getenv("TSGO_LOCKFILE_BINARY"))
	if err != nil {
		t.Fatal(err)
	}
	info, err := buildinfo.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	if info.Main.Path != pinnedModule {
		t.Errorf("the lockfile's compiler is module %s; "+
			"ts/private/tsgo_source/go.mod pins %s",
			info.Main.Path, pinnedModule)
	}
	revision := ""
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" {
			revision = setting.Value
		}
	}
	pinned := pinnedVersion[strings.LastIndex(pinnedVersion, "-")+1:]
	if !strings.HasPrefix(revision, pinned) {
		t.Errorf("the lockfile's compiler was built at %s; "+
			"ts/private/tsgo_source/go.mod pins %s %s: move the pin with "+
			"`go get %s@%s && go mod tidy` in that directory",
			revision, pinnedModule, pinnedVersion, pinnedModule, revision)
	}
}
