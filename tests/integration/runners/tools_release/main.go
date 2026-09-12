package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/mikn/rules_typescript/tests/integration/harness"
	"github.com/mikn/rules_typescript/tools/toolpack"
)

const releasePrefix = "github.com/mikn/rules_typescript/releases/download/"

var (
	lockVersion   = regexp.MustCompile(`(?m)^TOOLS_VERSION = "([0-9]+)"$`)
	lockIntegrity = regexp.MustCompile(`(?m)^    "([a-z0-9_]+)": "([^"]*)",$`)
)

func hostPlatform(it *harness.IT) string {
	arch := map[string]string{"amd64": "amd64", "arm64": "arm64"}[runtime.GOARCH]
	if arch == "" || (runtime.GOOS != "linux" && runtime.GOOS != "darwin") {
		it.Fail("the tools release covers no %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	return runtime.GOOS + "_" + arch
}

func readLock(it *harness.IT) (version string, table map[string]string) {
	text := it.Read(filepath.Join(it.RulesTSRoot, "ts/private/tools_lock.bzl"))
	m := lockVersion.FindStringSubmatch(text)
	if m == nil {
		it.Fail("ts/private/tools_lock.bzl has no TOOLS_VERSION line")
	}
	table = map[string]string{}
	for _, row := range lockIntegrity.FindAllStringSubmatch(text, -1) {
		table[row[1]] = row[2]
	}
	return m[1], table
}

// A port nothing listens on: the release URL rewritten there is refused at
// once, so a fetch that reaches the network fails without touching it.
func closedPort(it *harness.IT) int {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		it.Fail("cannot listen on 127.0.0.1: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	return port
}

func main() {
	harness.Run(harness.Config{
		Name:         "tools_release",
		WorkspaceRel: "tests/integration/tools_release/workspace",
		Renames:      map[string]string{"BUILD.bazel.tpl": "BUILD.bazel"},
	}, func(it *harness.IT) {
		version, table := readLock(it)
		platform := hostPlatform(it)
		paths := map[string]string{}
		for _, name := range toolpack.Binaries {
			dir := map[string]string{
				"tsaction":          "ts/tools/tsaction",
				"ts_launcher":       "tools/launcher",
				"lcov_merger":       "tools/lcov_merger",
				"copy_to_workspace": "tools/copy_to_workspace",
			}[name]
			rel := fmt.Sprintf("_main/%s/%s_/%s", dir, name, name)
			paths[name] = it.Runfile(rel)
		}
		dist := it.Scratch("dist")
		if err := os.MkdirAll(dist, 0o755); err != nil {
			it.Fail("cannot create %s: %v", dist, err)
		}
		basename := toolpack.Prefix(version, platform) + ".tar.gz"
		asset := filepath.Join(dist, basename)
		sri, err := toolpack.Pack(asset, version, platform, paths)
		if err != nil {
			it.Fail("packing the asset: %v", err)
		}
		if sri != table[platform] {
			it.Fail("the tree's tools pack to %s for %s; ts/private/tools_lock.bzl "+
				"has %q. A tool change bumps TOOLS_VERSION and fills the table "+
				"from tools/ci/check_tools_lock.sh", sri, platform, table[platform])
		}
		it.Pass("the four binaries pack to the table's %s asset: %s", platform, sri)

		cfg := it.Scratch("downloader.cfg")
		it.Write(cfg, fmt.Sprintf("rewrite %s(.*) 127.0.0.1:%d/$1\n",
			releasePrefix, closedPort(it)))
		flags := []string{
			"--downloader_config=" + cfg,
			"--experimental_repository_downloader_retries=0",
			"--repo_contents_cache=",
		}

		wrong := it.Scratch("dist-wrong")
		if err := os.MkdirAll(wrong, 0o755); err != nil {
			it.Fail("cannot create %s: %v", wrong, err)
		}
		bytes, err := os.ReadFile(asset)
		if err != nil {
			it.Fail("cannot read the asset back: %v", err)
		}
		wrongAsset := filepath.Join(wrong, basename)
		if err := os.WriteFile(wrongAsset, append(bytes, '\n'), 0o644); err != nil {
			it.Fail("cannot write the wrong asset: %v", err)
		}
		args := append([]string{"build"}, flags...)
		log, err := it.BazelLog("wrong_bytes.log", append(args,
			"--repository_cache="+it.Scratch("repository_cache-wrong"),
			"--distdir="+wrong, "//:hello")...)
		if err == nil {
			log.Dump()
			it.Fail("the build succeeded on an asset whose bytes are not the table's")
		}
		if !log.Contains("127.0.0.1") || !log.Contains(basename) {
			log.Dump()
			it.Fail("the build failed, but not on fetching %s from the rewritten "+
				"release URL", basename)
		}
		it.Pass("an asset whose bytes disagree with the table is not used; the " +
			"release URL is fetched and refused")

		served := []string{
			"--repository_cache=" + it.Scratch("repository_cache"),
			"--distdir=" + dist,
		}
		it.MustBazel(append(append(args, served...), "//:hello")...)
		it.Pass("a ts_compile builds with tsaction from the release asset")

		repo := "rules_typescript++ts+tools_" + platform
		fetched := filepath.Join(it.OutputBase, "external", repo, "tsaction")
		it.RequireFile(fetched, "the tools repository holds no tsaction: %s", fetched)
		if it.Read(fetched) != it.Read(paths["tsaction"]) {
			it.Fail("the fetched tsaction is not the packed one: %s", fetched)
		}
		it.Pass("@tools_%s holds the asset's binaries", platform)

		run := append(append([]string{"run"}, flags...), served...)
		resolved := "@rules_typescript//ts/toolchain:tools_resolved"
		usage, _ := it.BazelLog("tools_resolved.log", append(run, resolved)...)
		if !usage.Contains("tsaction tsgo -root=DIR") {
			usage.Dump()
			it.Fail("tools_resolved did not run the release's tsaction")
		}
		it.Pass("bazel run %s runs the release's tsaction", resolved)

		out := it.BazelStdout(append(run, "//:hello_bin")...)
		if !strings.Contains(out, "hello from the tools release") {
			it.Fail("hello_bin printed %q", strings.TrimSpace(out))
		}
		launcher, err := os.Readlink(it.Bin("hello_bin_launcher"))
		if err != nil {
			it.Fail("hello_bin_launcher is not the launcher symlink: %v", err)
		}
		if !strings.Contains(launcher, "tools_"+platform) ||
			!strings.HasSuffix(launcher, "/ts_launcher") {
			it.Fail("hello_bin runs %s, not the release's launcher", launcher)
		}
		it.Pass("a ts_binary runs under the launcher from the release asset: %s",
			launcher)
	})
}
