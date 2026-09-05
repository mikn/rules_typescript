package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mikn/rules_typescript/tests/integration/harness"
)

// A lockfile pinning something, none of it TypeScript.
const noTypescript = `lockfileVersion: '9.0'

importers:

  .:
    dependencies:
      lodash:
        specifier: 4.18.1
        version: 4.18.1

packages:

  lodash@4.18.1:
    resolution: {integrity: sha512-TwqaxBS4nGHIRNYLe/T0sRCDfy+hcrhmxwbaE0GBhVAuWqwgg9sSL1jMZLDdSbKUoLqR0c5F1bxhoYQPGbs8qA==}

snapshots:

  lodash@4.18.1: {}
`

// TypeScript before 7: a JavaScript compiler, no platform packages to fetch.
const typescript5 = `lockfileVersion: '9.0'

importers:

  .:
    devDependencies:
      typescript:
        specifier: 5.9.2
        version: 5.9.2

packages:

  typescript@5.9.2:
    resolution: {integrity: sha512-CWBzXQrc/qOkhidw1OzBTQuYRbfyxDXJMVJ1XNwUHGROVmuaeiEm3OslpZ1RV96d7SKKjZKrSJu3+t/xlw3R9A==}
    engines: {node: '>=14.17'}
    hasBin: true

snapshots:

  typescript@5.9.2: {}
`

const (
	fixtureVersion = "Version 7.0.2-lockfile-fixture"
	token          = "tsgo-lockfile-test-token"
)

func hostNpmArch(it *harness.IT) (npmOS, npmCPU string) {
	cpu := map[string]string{"amd64": "x64", "arm64": "arm64"}[runtime.GOARCH]
	if cpu == "" || (runtime.GOOS != "linux" && runtime.GOOS != "darwin") {
		it.Fail("no tsgo toolchain is declared for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	return runtime.GOOS, cpu
}

// writeTarball writes a `package/`-prefixed tarball, the registry's layout, whose
// lib/tsc is a script that only answers --version; the nonce keeps it uncached.
func writeTarball(it *harness.IT, path, name, nonce string) string {
	out, err := os.Create(path)
	if err != nil {
		it.Fail("cannot create %s: %v", path, err)
	}
	gz := gzip.NewWriter(out)
	archive := tar.NewWriter(gz)
	files := []struct {
		name    string
		mode    int64
		content string
	}{
		{"package/package.json", 0o644, fmt.Sprintf(`{"name": %q, "version": "7.0.2", "description": %q}`+"\n", name, nonce)},
		{"package/lib/tsc", 0o755, "#!/bin/sh\necho '" + fixtureVersion + "'\n"},
	}
	for _, f := range files {
		header := &tar.Header{Name: f.name, Mode: f.mode, Size: int64(len(f.content)), Typeflag: tar.TypeReg}
		if err := archive.WriteHeader(header); err != nil {
			it.Fail("cannot add %s to %s: %v", f.name, path, err)
		}
		if _, err := archive.Write([]byte(f.content)); err != nil {
			it.Fail("cannot add %s to %s: %v", f.name, path, err)
		}
	}
	if err := archive.Close(); err != nil {
		it.Fail("cannot finish %s: %v", path, err)
	}
	if err := gz.Close(); err != nil {
		it.Fail("cannot finish %s: %v", path, err)
	}
	if err := out.Close(); err != nil {
		it.Fail("cannot finish %s: %v", path, err)
	}
	bytes, err := os.ReadFile(path)
	if err != nil {
		it.Fail("cannot read %s back: %v", path, err)
	}
	sum := sha512.Sum512(bytes)
	return "sha512-" + base64.StdEncoding.EncodeToString(sum[:])
}

// withResolution rewrites the host platform's `packages:` entry of the
// checked-in lockfile to carry the given `resolution:` mapping.
func withResolution(it *harness.IT, lockfile, pkg, resolution string) string {
	lines := strings.Split(lockfile, "\n")
	key := fmt.Sprintf("  '%s@7.0.2':", pkg)
	for i := 0; i+1 < len(lines); i++ {
		if lines[i] == key && strings.HasPrefix(lines[i+1], "    resolution:") {
			lines[i+1] = "    resolution: " + resolution
			return strings.Join(lines, "\n")
		}
	}
	it.Fail("the checked-in lockfile has no packages: entry for %s", pkg)
	return ""
}

func flipped(integrity string) string {
	digest := strings.TrimPrefix(integrity, "sha512-")
	first := "A"
	if digest[0] == 'A' {
		first = "B"
	}
	return "sha512-" + first + digest[1:]
}

func mustFailMentioning(it *harness.IT, log *harness.Log, err error, what string, wants ...string) {
	if err == nil {
		log.Dump()
		it.Fail("the build succeeded on %s", what)
	}
	for _, want := range wants {
		if !log.Contains(want) {
			log.Dump()
			it.Fail("the failure on %s does not mention %q", what, want)
		}
	}
}

// registry serves its root to requests carrying the token, 401 to the rest, and
// records every answer as "<status> <path> <Authorization>".
type registry struct {
	root    string
	mu      sync.Mutex
	entries []string
}

func (r *registry) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	auth := req.Header.Get("Authorization")
	if auth == "" {
		auth = "-"
	}
	reply := func(status int, body []byte) {
		r.mu.Lock()
		r.entries = append(r.entries, fmt.Sprintf("%d %s %s", status, req.URL.Path, auth))
		r.mu.Unlock()
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(status)
		w.Write(body)
	}
	if auth != "Bearer "+token {
		reply(http.StatusUnauthorized, nil)
		return
	}
	target := filepath.Join(r.root, filepath.FromSlash(strings.TrimPrefix(req.URL.Path, "/")))
	if !strings.HasPrefix(target, r.root+string(os.PathSeparator)) {
		reply(http.StatusNotFound, nil)
		return
	}
	body, err := os.ReadFile(target)
	if err != nil {
		reply(http.StatusNotFound, nil)
		return
	}
	reply(http.StatusOK, body)
}

func (r *registry) log() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.entries, "\n")
}

func (r *registry) saw(line string) bool {
	for _, entry := range strings.Split(r.log(), "\n") {
		if entry == line {
			return true
		}
	}
	return false
}

func main() {
	harness.Run(harness.Config{
		Name:         "tsgo_lockfile",
		WorkspaceRel: "tests/integration/tsgo_lockfile/workspace",
		Renames:      map[string]string{"BUILD.bazel.tpl": "BUILD.bazel"},
	}, func(it *harness.IT) {
		checkedIn := it.Read(it.Path("pnpm-lock.yaml"))

		it.MustBazel("build", "//:hello")
		it.Pass("a ts_compile builds with the compiler the lockfile pins")

		version := it.BazelStdout("run", "@rules_typescript//ts/toolchain:tsgo_resolved", "--", "--version")
		if !strings.Contains(version, "Version 7.0.2") {
			it.Fail("tsgo_resolved --version printed %q, not the lockfile's 7.0.2", strings.TrimSpace(version))
		}
		it.Pass("the resolved compiler is the lockfile's version: %s", strings.TrimSpace(version))

		it.Write(it.Path("pnpm-lock.yaml"), noTypescript)
		log, err := it.BazelLog("no_typescript.log", "build", "//:hello")
		mustFailMentioning(it, log, err, "a lockfile with no typescript",
			"no typescript in", "pnpm-lock.yaml", "pnpm add -D typescript@7")
		it.Pass("a lockfile with no typescript is refused, naming the fix")

		it.Write(it.Path("pnpm-lock.yaml"), typescript5)
		log, err = it.BazelLog("typescript5.log", "build", "//:hello")
		mustFailMentioning(it, log, err, "a lockfile pinning typescript 5",
			"ships no native compiler", "typescript@5.9.2")
		it.Pass("a lockfile pinning TypeScript 5 is refused, naming the version")

		npmOS, npmCPU := hostNpmArch(it)
		base := fmt.Sprintf("typescript-%s-%s", npmOS, npmCPU)
		pkg := "@typescript/" + base
		nonce := fmt.Sprintf("%d-%d", time.Now().Unix(), os.Getpid())
		tarball := it.Scratch(base + "-7.0.2.tgz")
		integrity := writeTarball(it, tarball, pkg, nonce)

		it.Write(it.Path("pnpm-lock.yaml"), withResolution(it, checkedIn, pkg,
			fmt.Sprintf("{integrity: %s, tarball: file://%s}", integrity, tarball)))
		version = it.BazelStdout("run", "@rules_typescript//ts/toolchain:tsgo_resolved", "--", "--version")
		if !strings.Contains(version, fixtureVersion) {
			it.Fail("tsgo_resolved ran %q, not the tarball the lockfile's resolution names", strings.TrimSpace(version))
		}
		it.Pass("the lockfile's resolution decides the bytes: url and integrity both")

		it.Write(it.Path("pnpm-lock.yaml"), withResolution(it, checkedIn, pkg,
			fmt.Sprintf("{integrity: %s, tarball: file://%s}", flipped(integrity), tarball)))
		log, err = it.BazelLog("flipped_integrity.log", "build", "//:hello")
		if err == nil {
			log.Dump()
			it.Fail("the build succeeded on a lockfile whose integrity does not match the tarball")
		}
		if !log.Matches(`(?i)checksum`) {
			log.Dump()
			it.Fail("the build failed, but not on the checksum mismatch")
		}
		it.Pass("a tarball whose bytes disagree with the lockfile's integrity is refused")

		tarballPath := fmt.Sprintf("/@typescript/%s/-/%s-7.0.2.tgz", base, base)
		served := &registry{root: it.Scratch("registry")}
		registryIntegrity := writeTarball(it, it.Scratch("registry", filepath.FromSlash(tarballPath)), pkg, nonce+"-registry")
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			it.Fail("cannot listen on 127.0.0.1: %v", err)
		}
		server := &http.Server{Handler: served}
		go server.Serve(listener)
		it.OnCleanup(func() { server.Close() })
		port := listener.Addr().(*net.TCPAddr).Port

		it.Write(it.Path(".npmrc"), fmt.Sprintf("@typescript:registry=http://127.0.0.1:%d\n//127.0.0.1:%d/:_authToken=${TSGO_TOKEN}\n", port, port))
		it.Replace(it.Path("MODULE.bazel"), `ts.tsgo(pnpm_lock = "//:pnpm-lock.yaml")`,
			"ts.tsgo(\n    npmrc = \"//:.npmrc\",\n    pnpm_lock = \"//:pnpm-lock.yaml\",\n)")
		it.Write(it.Path("pnpm-lock.yaml"), withResolution(it, checkedIn, pkg, fmt.Sprintf("{integrity: %s}", registryIntegrity)))

		log, err = it.BazelLog("wrong_token.log", "build", "--repo_env=TSGO_TOKEN=wrong-token", "//:hello")
		mustFailMentioning(it, log, err, "a registry refusing the token", "401")
		if !served.saw("401 " + tarballPath + " Bearer wrong-token") {
			fmt.Fprintf(os.Stderr, "--- request log ---\n%s\n", served.log())
			log.Dump()
			it.Fail("the registry never saw the compiler request carrying the wrong token")
		}
		it.Pass("the .npmrc _authToken reaches the scope's registry as a Bearer header, and a wrong one is refused")

		version = it.BazelStdout("run", "--repo_env=TSGO_TOKEN="+token, "@rules_typescript//ts/toolchain:tsgo_resolved", "--", "--version")
		if !strings.Contains(version, fixtureVersion) {
			it.Fail("tsgo_resolved ran %q, not the tarball the registry served", strings.TrimSpace(version))
		}
		if !served.saw("200 " + tarballPath + " Bearer " + token) {
			fmt.Fprintf(os.Stderr, "--- request log ---\n%s\n", served.log())
			it.Fail("the registry never served the compiler to the right token")
		}
		it.Pass("the compiler is fetched from the .npmrc's @typescript:registry with its credentials")
	})
}
