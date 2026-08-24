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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mikn/rules_typescript/tests/integration/harness"
)

const (
	token       = "npmrc-integration-test-token"
	tarballPath = "@acme/greeter/-/greeter-1.0.0.tgz"
)

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
		r.entries = append(r.entries, fmt.Sprintf("%d %s %s", status, req.URL.RequestURI(), auth))
		r.mu.Unlock()
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(status)
		w.Write(body)
	}
	if auth != "Bearer "+token {
		reply(http.StatusUnauthorized, nil)
		return
	}
	target := filepath.Join(r.root, strings.TrimPrefix(req.URL.Path, "/"))
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

func writeTarball(it *harness.IT, path string, files map[string]string) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		it.Fail("cannot create %s: %v", filepath.Dir(path), err)
	}
	out, err := os.Create(path)
	if err != nil {
		it.Fail("cannot create %s: %v", path, err)
	}
	defer out.Close()
	gz := gzip.NewWriter(out)
	archive := tar.NewWriter(gz)
	for name, content := range files {
		header := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg}
		if err := archive.WriteHeader(header); err != nil {
			it.Fail("cannot add %s to %s: %v", name, path, err)
		}
		if _, err := archive.Write([]byte(content)); err != nil {
			it.Fail("cannot add %s to %s: %v", name, path, err)
		}
	}
	if err := archive.Close(); err != nil {
		it.Fail("cannot finish %s: %v", path, err)
	}
	if err := gz.Close(); err != nil {
		it.Fail("cannot finish %s: %v", path, err)
	}
}

func main() {
	harness.Run(harness.Config{
		Name:         "npmrc_registry",
		WorkspaceRel: "tests/integration/npmrc_registry/workspace",
		Renames:      map[string]string{"BUILD.bazel.tpl": "BUILD.bazel"},
	}, func(it *harness.IT) {
		root := it.Scratch("registry")

		// The nonce changes the tarball's integrity on every run, which is what
		// stops Bazel's repository cache from answering the fetch: without it a
		// second run never reaches the server.
		nonce := fmt.Sprintf("%d-%d", time.Now().Unix(), os.Getpid())
		writeTarball(it, filepath.Join(root, tarballPath), map[string]string{
			"package/package.json": fmt.Sprintf(`{
  "name": "@acme/greeter",
  "version": "1.0.0",
  "description": "throwaway registry fixture %s",
  "main": "index.js",
  "types": "index.d.ts"
}
`, nonce),
			"package/index.js":   "export function greet(who) {\n  return `hello ${who}`;\n}\n",
			"package/index.d.ts": "export declare function greet(who: string): string;\n",
		})
		tarball, err := os.ReadFile(filepath.Join(root, tarballPath))
		if err != nil {
			it.Fail("cannot read the tarball back: %v", err)
		}
		sum := sha512.Sum512(tarball)
		integrity := "sha512-" + base64.StdEncoding.EncodeToString(sum[:])
		it.Pass("built @acme/greeter tarball (%s)", integrity)

		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			it.Fail("cannot listen on 127.0.0.1: %v", err)
		}
		served := &registry{root: root}
		server := &http.Server{Handler: served}
		go server.Serve(listener)
		it.OnCleanup(func() { server.Close() })
		address := fmt.Sprintf("http://127.0.0.1:%d", listener.Addr().(*net.TCPAddr).Port)
		it.Pass("throwaway registry listening on %s", address)

		probe, err := http.Get(address + "/__probe__")
		if err != nil {
			it.Fail("unauthenticated probe failed to connect: %v", err)
		}
		probe.Body.Close()
		if probe.StatusCode != http.StatusUnauthorized {
			it.Fail("unauthenticated probe returned %d, want 401", probe.StatusCode)
		}
		it.Pass("unauthenticated request is refused (401)")

		// The default registry points at a path that serves nothing, so the only
		// way @acme/greeter can be fetched is via the scope override.
		it.Write(it.Path(".npmrc"), fmt.Sprintf(`registry=%s/__default__/
@acme:registry=%s
//127.0.0.1:%d/:_authToken=${ACME_TOKEN}
`, address, address, listener.Addr().(*net.TCPAddr).Port))

		it.Write(it.Path("pnpm-lock.yaml"), fmt.Sprintf(`lockfileVersion: '9.0'

settings:
  autoInstallPeers: true
  excludeLinksFromLockfile: false

importers:

  .:
    dependencies:
      '@acme/greeter':
        specifier: 1.0.0
        version: 1.0.0

packages:

  '@acme/greeter@1.0.0':
    resolution: {integrity: %s}

snapshots:

  '@acme/greeter@1.0.0': {}
`, integrity))
		it.Pass(".npmrc and pnpm-lock.yaml written")

		it.MustBazel("run", "//:gazelle")
		build := it.Path("src/consumer/BUILD.bazel")
		it.RequireFile(build, "Gazelle did not generate src/consumer/BUILD.bazel")
		if !it.Contains(build, "@npm//:acme_greeter") {
			it.Dump(build)
			it.Fail("src/consumer/BUILD.bazel does not reference @npm//:acme_greeter")
		}
		it.Pass("Gazelle resolved the scoped import to @npm//:acme_greeter")

		wrong, err := it.BazelLog("wrong_token.log", "build", "--repo_env=ACME_TOKEN=wrong-token", "//...")
		if err == nil {
			wrong.Dump()
			it.Fail("the build succeeded with the wrong _authToken; the token is not reaching the registry")
		}
		if !wrong.Contains("401") {
			wrong.Dump()
			it.Fail("the build failed, but not on the registry refusing the fetch")
		}
		if !served.saw("401 /" + tarballPath + " Bearer wrong-token") {
			fmt.Fprintf(os.Stderr, "--- request log ---\n%s\n", served.log())
			wrong.Dump()
			it.Fail("the registry never saw the tarball request carrying the wrong token")
		}
		it.Pass("the wrong _authToken is sent as a Bearer header and rejected")

		if err := it.Bazel("build", "--repo_env=ACME_TOKEN="+token, "//..."); err != nil {
			it.Fail("bazel build //... exited non-zero (the private-registry fetch should succeed)")
		}
		it.Pass("bazel build //... through the private registry")

		if !served.saw("200 /" + tarballPath + " Bearer " + token) {
			fmt.Fprintf(os.Stderr, "--- request log ---\n%s\n", served.log())
			it.Fail(".npmrc's _authToken did not arrive as an Authorization: Bearer header")
		}
		it.Pass(".npmrc's _authToken arrived as an Authorization: Bearer header")

		if strings.Contains(served.log(), "__default__") {
			fmt.Fprintf(os.Stderr, "--- request log ---\n%s\n", served.log())
			it.Fail("@acme:registry was ignored — the fetch went to the default registry")
		}
		it.Pass("@acme:registry overrode the default registry")

		for _, rel := range []string{"src/consumer/greeting.js", "src/consumer/greeting.d.ts"} {
			it.RequireFile(it.Bin(rel), "expected output file not found: %s", rel)
			it.Pass("output file exists: %s", rel)
		}
		dts := it.Bin("src/consumer/greeting.d.ts")
		if !it.Contains(dts, "message") {
			it.Dump(dts)
			it.Fail("src/consumer/greeting.d.ts does not declare the exported message")
		}
		it.Pass("src/consumer/greeting.d.ts declares the export typed from the registry package")
	})
}
