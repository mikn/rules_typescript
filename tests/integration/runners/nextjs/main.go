package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mikn/rules_typescript/tests/integration/harness"
)

const (
	greetingSrc = "src/lib/greeting.ts"
	goodSig     = "export function greet(name: string): string {"
	badSig      = "export function greet(name: string): number {"
	typeError   = "Type error: Type 'string' is not assignable to type 'number'."

	configSrcsAttr = "    config_srcs = [\"next.shared.mjs\"],\n"
)

var greetingLocation = regexp.MustCompile(`^\./src/lib/greeting\.ts:[0-9]+:[0-9]+$`)

// Next prints the offending file and position, then the diagnostic on a line
// below; matching them together pins the error to the file that was edited.
func reportsTypeErrorInGreeting(log *harness.Log) bool {
	lines := log.Lines()
	for i, line := range lines {
		if !greetingLocation.MatchString(strings.TrimRight(line, "\r")) {
			continue
		}
		for _, following := range lines[i+1 : min(i+3, len(lines))] {
			if strings.Contains(following, typeError) {
				return true
			}
		}
	}
	return false
}

func onlyMatch(it *harness.IT, pattern, what string) string {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		it.Fail("bad glob %s: %v", pattern, err)
	}
	if len(matches) != 1 {
		it.Fail("expected exactly one %s at %s, found %d", what, pattern, len(matches))
	}
	return matches[0]
}

// ── A server left running while assertions are made against it ──────────────

// server is a nested `bazel run` that stays up. Its output goes to a file under
// the scratch dir so a failing assertion has the same paper trail as a failing
// build.
type server struct {
	path string
	cmd  *exec.Cmd
	log  *os.File
}

// serve starts `bazel run <target> -- --port <port>` and returns while it runs.
//
// Its own process group, killed with SIGKILL rather than SIGTERM: the launcher
// ignores SIGTERM on purpose, because that is what ibazel sends after every
// rebuild.
func serve(it *harness.IT, logName, target string, port int) *server {
	path := it.Scratch(logName)
	log, err := os.Create(path)
	if err != nil {
		it.Fail("cannot create %s: %v", path, err)
	}
	args := []string{
		"--output_base=" + it.OutputBase, "run", target, "--", "--port", strconv.Itoa(port),
	}
	fmt.Printf("INFO: bazel %s &\n", strings.Join(args, " "))
	cmd := exec.Command(os.Getenv("BIT_BAZEL_BINARY"), args...)
	cmd.Dir = it.WorkspaceDir
	cmd.Stdout = log
	cmd.Stderr = log
	// TEST_TMPDIR would put the nested Bazel inside the outer execroot, which it
	// refuses with "repo contents cache is inside main repo".
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "TEST_TMPDIR=") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		log.Close()
		it.Fail("cannot start bazel %s: %v", strings.Join(args, " "), err)
	}
	s := &server{path: path, cmd: cmd, log: log}
	it.OnCleanup(s.stop)
	return s
}

func (s *server) stop() {
	if s.cmd.Process != nil {
		syscall.Kill(-s.cmd.Process.Pid, syscall.SIGKILL)
		s.cmd.Wait()
	}
	s.log.Close()
}

func (s *server) text() string {
	text, err := os.ReadFile(s.path)
	if err != nil {
		return ""
	}
	return string(text)
}

// ── HTTP, for the assertions a file cannot answer ────────────────────────────

type reply struct {
	status int
	body   string
	header http.Header
}

// freePort takes a kernel-assigned one: on a fixed port two concurrent runs
// answer each other's requests, and the failure looks like a wrong response
// rather than a collision.
func freePort(it *harness.IT) int {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		it.Fail("reserving a port: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func request(url string) (reply, error) {
	client := &http.Client{Timeout: 60 * time.Second}
	response, err := client.Get(url)
	if err != nil {
		return reply{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return reply{status: response.StatusCode}, err
	}
	return reply{status: response.StatusCode, body: string(body), header: response.Header}, nil
}

// await polls until the server answers, and dumps its log when it never does.
func await(it *harness.IT, srv *server, url string) {
	deadline := time.Now().Add(4 * time.Minute)
	var last error
	for time.Now().Before(deadline) {
		got, err := request(url)
		if err == nil && got.status == 200 {
			return
		}
		last = err
		time.Sleep(500 * time.Millisecond)
	}
	fmt.Fprintln(os.Stderr, srv.text())
	it.Fail("GET %s never answered 200 (last error: %v)", url, last)
}

func get(it *harness.IT, srv *server, base, path string) reply {
	got, err := request(base + path)
	if err != nil {
		fmt.Fprintln(os.Stderr, srv.text())
		it.Fail("GET %s: %v", base+path, err)
	}
	if got.status != 200 {
		fmt.Fprintln(os.Stderr, srv.text())
		it.Fail("GET %s answered %d, want 200\nbody:\n%s", base+path, got.status, got.body)
	}
	return got
}

func (r reply) requireContains(it *harness.IT, url string, wants ...string) {
	for _, want := range wants {
		if !strings.Contains(r.body, want) {
			it.Fail("GET %s did not render %q\nbody:\n%s", url, want, r.body)
		}
	}
}

// assertServesTheApp makes the assertions only a running server can answer: a
// route the framework marked dynamic, a value the request itself supplied, the
// middleware's header, and both API-route flavours.
func assertServesTheApp(it *harness.IT, srv *server, base, host string) {
	home := get(it, srv, base, "/")
	home.requireContains(it, base+"/", "Hello, Bazel!", "SHARED_VERSION_MARKER")

	// The host comes off the request, so finding it in the HTML says the page
	// was rendered for THIS request rather than read off disk.
	dynamic := get(it, srv, base, "/dynamic")
	dynamic.requireContains(it, base+"/dynamic", "DYNAMIC_MARKER", host)
	if got := dynamic.header.Get("x-fixture-middleware"); got != "MIDDLEWARE_MARKER" {
		it.Fail("GET %s/dynamic carries x-fixture-middleware %q; the middleware did not run", base, got)
	}

	// Two requests, two bodies. A prerendered file cannot do this, which is what
	// separates "server-rendered on demand" from "whatever was in .next".
	again := get(it, srv, base, "/dynamic")
	if again.body == dynamic.body {
		it.Fail("two requests to %s/dynamic returned identical HTML, so the route is not "+
			"rendered per request\nbody:\n%s", base, dynamic.body)
	}

	ssr := get(it, srv, base, "/ssr")
	ssr.requireContains(it, base+"/ssr", "SSR_MARKER", host)

	get(it, srv, base, "/legacy").requireContains(it, base+"/legacy", "LEGACY_MARKER")
	get(it, srv, base, "/api/hello").requireContains(it, base+"/api/hello", `{"hello":"route-handler"}`)
	get(it, srv, base, "/api/ping").requireContains(it, base+"/api/ping", `{"pong":true}`)
}

// awaitBody polls until the response contains want. `next dev` compiles a route
// on demand, so the edit lands on the next request after the one that triggers
// the recompile.
func awaitBody(url, want string) bool {
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		if got, err := request(url); err == nil && strings.Contains(got.body, want) {
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}

// imageURL is the optimizer URL next/image put in the HTML, unescaped. It is a
// srcset entry, so it ends at the space before the density descriptor.
func imageURL(it *harness.IT, html string) string {
	match := regexp.MustCompile(`/_next/image\?[^" ]+`).FindString(html)
	if match == "" {
		it.Fail("no /_next/image URL in the served HTML:\n%s", html)
	}
	return strings.ReplaceAll(match, "&amp;", "&")
}

func main() {
	harness.Run(harness.Config{
		Name:         "nextjs",
		WorkspaceRel: "tests/integration/nextjs",
		Lockfile:     "examples/nextjs-app/pnpm-lock.yaml",
	}, func(it *harness.IT) {
		// The action carries block-network, so a green build is also the
		// statement that nothing this app needs comes off the network.
		it.MustBazel("build", "//:app")
		it.Pass("bazel build //:app")

		nextOut := it.Bin("app_next_out")
		it.RequireDir(nextOut, "next_build output directory not found: app_next_out/")
		it.Pass("app_next_out/ directory exists")

		for _, subdir := range []string{"server", "static"} {
			it.RequireDir(it.Bin("app_next_out", subdir), ".next/%s/ not found in output", subdir)
			it.Pass(".next/%s/ exists", subdir)
		}

		it.RequireFile(it.Bin("app_next_out", "BUILD_ID"), ".next/BUILD_ID not found")
		it.Pass(".next/BUILD_ID exists")

		// .next/cache/ is non-hermetic and would pollute the remote cache.
		it.RequireNoDir(it.Bin("app_next_out", "cache"), ".next/cache/ must be excluded from output (non-hermetic)")
		it.Pass(".next/cache/ correctly excluded from output")

		it.RequireNoDir(it.Bin("app_next_out", "_staging"), "_staging/ must be cleaned up from output")
		it.Pass("_staging/ correctly absent from output")

		// `trace` records the absolute staging path of every build span and
		// diagnostics/ the wall clock, so both differ between two identical
		// builds -- and nothing serves from either.
		it.RequireNoFile(it.Bin("app_next_out", "trace"), ".next/trace must be excluded from output (absolute paths, timestamps)")
		it.RequireNoDir(it.Bin("app_next_out", "diagnostics"), ".next/diagnostics/ must be excluded from output (build timings)")
		it.Pass("trace and diagnostics/ correctly excluded from output")

		it.RequireNoFile(it.Bin("app_next_out", "_next_build.log"), "the wrapper's build log leaked into the output")
		it.Pass("no build log left in the output")

		// ── App Router ────────────────────────────────────────────────────────
		for _, route := range []string{"app/page.js", "app/about/page.js", "app/client/page.js", "app/dynamic/page.js"} {
			it.RequireFile(it.Bin("app_next_out", "server", route), "compiled route missing: .next/server/%s", route)
			it.Pass(".next/server/%s exists", route)
		}

		it.RequireFile(it.Bin("app_next_out", "server/app/api/hello/route.js"),
			"App Router route handler missing: .next/server/app/api/hello/route.js")
		it.Pass("App Router route handler compiled (.next/server/app/api/hello/route.js)")

		// Route kind, not just route presence: asserting only that /about is
		// listed would also pass on a build that prerendered everything.
		prerender := it.Bin("app_next_out", "prerender-manifest.json")
		it.RequireContains(prerender, `"/about"`, "static route /about is not in prerender-manifest.json")
		it.RequireNotContains(prerender, "/dynamic", "the force-dynamic route was prerendered — /dynamic is in prerender-manifest.json")
		it.Pass("prerender-manifest.json lists /about and not the force-dynamic /dynamic")

		// ── Pages Router ──────────────────────────────────────────────────────
		it.RequireFile(it.Bin("app_next_out", "server/pages/legacy.html"),
			"Pages Router page missing: .next/server/pages/legacy.html")
		it.RequireFile(it.Bin("app_next_out", "server/pages/ssr.js"),
			"getServerSideProps page missing: .next/server/pages/ssr.js")
		it.RequireFile(it.Bin("app_next_out", "server/pages/api/ping.js"),
			"Pages API route missing: .next/server/pages/api/ping.js")
		it.Pass("Pages Router compiled: legacy.html, ssr.js and api/ping.js")

		it.RequireContains(it.Bin("app_next_out", "server/pages-manifest.json"), `"/ssr"`,
			"/ssr is not in pages-manifest.json")
		it.Pass("pages-manifest.json routes the Pages Router pages")

		// ── Server Components ─────────────────────────────────────────────────
		it.RequireFile(it.Bin("app_next_out", "server/app/client/page_client-reference-manifest.js"),
			"no client-reference manifest for the \"use client\" route")
		it.RequireContains(it.Bin("app_next_out", "server/server-reference-manifest.json"), "action-browser",
			"no \"use server\" action registered under layer action-browser")
		it.Pass("\"use client\" boundary and \"use server\" action both registered")

		// ── Middleware ────────────────────────────────────────────────────────
		it.RequireFile(it.Bin("app_next_out", "server/src/middleware.js"), "middleware not compiled: .next/server/src/middleware.js")
		it.RequireFile(it.Bin("app_next_out", "server/edge-runtime-webpack.js"), "middleware edge runtime missing")
		it.RequireContains(it.Bin("app_next_out", "server/middleware-manifest.json"), `"originalSource": "/dynamic"`,
			"middleware-manifest.json does not carry the declared matcher")
		it.Pass("middleware compiled with its matcher in middleware-manifest.json")

		// ── CSS and next/image ────────────────────────────────────────────────
		css := onlyMatch(it, it.Bin("app_next_out", "static/css/*.css"), "stylesheet")
		it.RequireContains(css, ".fixture-css-marker{color:#123456}",
			"the imported stylesheet's declaration is not in %s", css)
		it.Pass("imported CSS compiled into static/css/")

		onlyMatch(it, it.Bin("app_next_out", "static/media/logo.*.png"), "static image")
		it.RequireContains(it.Bin("app_next_out", "server/app/index.html"), "_next%2Fstatic%2Fmedia%2Flogo",
			"the prerendered page does not point at the optimizer URL for the imported image")
		it.Pass("statically imported image emitted to static/media/ and served through /_next/image")

		// ── staging_srcs, both forms ──────────────────────────────────────────
		// `next build` folds greet("Bazel") into the prerendered route, so this
		// exact string is only there if the staged greeting.ts was compiled.
		it.RequireContains(it.Bin("app_next_out", "server/app/page.js"), "Hello, Bazel!",
			"'Hello, Bazel!' not in .next/server/app/page.js — staging_srcs was not compiled into the route")
		it.Pass("greet() output compiled into .next/server/app/page.js (staging_srcs source form works)")

		// The other form: staging_srcs pointing at a ts_compile target stages
		// that target's compiled output, and Next.js resolves the import to it.
		manifest := it.Bin("app_next_manifest.txt")
		it.RequireMatches(manifest, `(?m)^shared/version\.js\tbazel-out/`,
			"the ts_compile target's compiled .js is not staged (looked in %s)", manifest)
		it.RequireMatches(manifest, `(?m)^shared/version\.d\.ts\t`,
			"the ts_compile target's .d.ts is not staged")
		it.RequireContains(it.Bin("app_next_out", "server/app/index.html"), "SHARED_VERSION_MARKER",
			"the ts_compile output was staged but never bundled into the page")
		it.Pass("staging_srcs on a ts_compile target stages .js + .d.ts and Next.js bundles the .js")

		// ── config_srcs ───────────────────────────────────────────────────────
		// poweredByHeader is only false if next.config.mjs loaded, which needs
		// the sibling module it imports staged beside it.
		it.RequireContains(it.Bin("app_next_out", "required-server-files.json"), `"poweredByHeader": false`,
			"next.config.mjs did not take effect — poweredByHeader is not false")
		it.Pass("next.config.mjs and the module it imports were both read")

		// ── The network boundary ──────────────────────────────────────────────
		blocked := it.BazelStdout("aquery", `mnemonic("NextBuild", //:app)`, "--output=text")
		if !strings.Contains(blocked, "ExecutionInfo: {block-network: ''}") {
			it.Fail("//:app's NextBuild action does not carry block-network:\n%s", blocked)
		}
		it.Pass("//:app's NextBuild action runs with the network blocked")

		networked := it.BazelStdout("aquery", `mnemonic("NextBuild", //:font_app_networked)`, "--output=text")
		if !strings.Contains(networked, "ExecutionInfo: {requires-network: ''}") {
			it.Fail("allow_network = True did not turn into requires-network:\n%s", networked)
		}
		it.Pass("allow_network = True asks for the network instead of blocking it")

		// next/font/google downloads its payloads while compiling. The build has
		// to fail, and it has to say why: an ENETUNREACH out of a webpack loader
		// is not something a user can act on.
		fontLog, err := it.BazelLog("font.log", "build", "//:font_app")
		switch {
		// block-network is declared on the action -- asserted above -- but only a
		// sandbox with a network namespace of its own enforces it, and a nested
		// Bazel does not always get one: a runner that denies unprivileged user
		// namespaces leaves the inner build on processwrapper-sandbox, which
		// honours the requirement by ignoring it. The declaration still holds
		// there; it is the effect that cannot be observed, so name which of the
		// two this run actually measured rather than blaming the rule.
		case err == nil && fontLog.Matches(`NextBuild //:font_app[^\n]*(processwrapper-sandbox|standalone|local)`):
			it.Pass("next/font/google: this sandbox does not enforce block-network, so the failure " +
				"it should produce is unobservable here -- the declaration is asserted above")
		case err == nil:
			fontLog.DumpTail(60)
			it.Fail("//:font_app built a next/font/google import with the network blocked")
		default:
			for _, expected := range []string{
				"Failed to fetch `Inter` from Google Fonts",
				"next_build: `next build` failed while reaching for the network.",
				"allow_network = True",
			} {
				if !fontLog.Contains(expected) {
					fontLog.DumpTail(60)
					it.Fail("the next/font failure does not mention %q", expected)
				}
			}
			it.Pass("next/font/google fails under the blocked network, naming the cause and the opt-out")
		}

		// ── Negative: config_srcs is what resolves the config's sibling ───────
		buildFile := it.Path("BUILD.bazel")
		buildText := it.Read(buildFile)
		if !strings.Contains(buildText, configSrcsAttr) {
			it.Fail("BUILD.bazel does not contain %q, so removing it would prove nothing", configSrcsAttr)
		}
		it.Write(buildFile, strings.ReplaceAll(buildText, configSrcsAttr, ""))
		configLog, err := it.BazelLog("config_srcs.log", "build", "//:app")
		if err == nil {
			configLog.DumpTail(40)
			it.Fail("//:app built without config_srcs — the config's sibling import resolved from somewhere undeclared")
		}
		if !configLog.Contains("ERR_MODULE_NOT_FOUND") || !configLog.Contains("next.shared.mjs") {
			configLog.DumpTail(40)
			it.Fail("dropping config_srcs failed the build for some reason other than the missing sibling module")
		}
		it.Pass("without config_srcs the config's sibling import is ERR_MODULE_NOT_FOUND")
		it.Write(buildFile, buildText)

		// ── Negative: next build is the type checker ──────────────────────────
		// This workspace has no ts_compile target over the app sources: the only
		// type-checking is the one `next build` runs itself, with the staged
		// tsconfig.json.
		it.Replace(it.Path(greetingSrc), goodSig, badSig)

		typeLog, err := it.BazelLog("type_error.log", "build", "//:app")
		if err == nil {
			typeLog.DumpTail(80)
			it.Fail("//:app built with a type error in a staged source; next build is not type-checking")
		}
		it.Pass("//:app failed to build once a staged source had a type error")

		// Exiting non-zero is not the assertion: a missing toolchain, a bad label
		// or a fetch failure does that too.
		if !typeLog.Contains("INFO: Analyzed target //:app") {
			typeLog.DumpTail(80)
			it.Fail("//:app never got past analysis — the failure is not next build's type check")
		}
		it.Pass("the failure happened during execution, not loading or analysis")

		if !typeLog.Contains("NextBuild //:app failed") {
			typeLog.DumpTail(80)
			it.Fail("no failing NextBuild action for //:app — something else broke the build")
		}
		if !typeLog.Contains("Failed to compile.") {
			typeLog.DumpTail(80)
			it.Fail("next build did not report a compile failure")
		}
		it.Pass("//:app's NextBuild action is what failed")

		if !reportsTypeErrorInGreeting(typeLog) {
			typeLog.DumpTail(80)
			it.Fail("no 'string is not assignable to number' type error reported in ./src/lib/greeting.ts")
		}
		it.Pass("type error reported in src/lib/greeting.ts with the expected message")

		for _, pattern := range []string{
			"ERROR: Analysis of target",
			"no such target",
			"no such package",
			"error loading package",
			"no matching toolchains",
			"Error in fail",
			"Error downloading",
			"Failed to fetch `",
			"No space left on device",
			"command not found",
		} {
			if typeLog.Contains(pattern) {
				typeLog.DumpTail(80)
				it.Fail("build log contains '%s' — the build broke for an unrelated reason", pattern)
			}
		}
		it.Pass("no toolchain, loading, analysis or fetch error in the failing build")

		it.Replace(it.Path(greetingSrc), badSig, goodSig)
		restored, err := it.BazelLog("restored.log", "build", "//:app")
		if err != nil {
			restored.DumpTail(80)
			it.Fail("//:app does not build after reverting the type error — the earlier failure was not caused by the edit")
		}
		it.Pass("//:app builds again once the type error is reverted")

		// ── next_serve: the build, served ─────────────────────────────────────
		// Every assertion above is about files. These are about responses, which
		// is the only way to tell a route the framework renders per request from
		// a file it prerendered -- and prerender-manifest.json above already
		// pinned which of the two Next.js decided each route is.
		servePort := freePort(it)
		serveBase := fmt.Sprintf("http://127.0.0.1:%d", servePort)
		serveHost := fmt.Sprintf("127.0.0.1:%d", servePort)
		built := serve(it, "serve.log", "//:serve", servePort)
		await(it, built, serveBase+"/about")
		it.Pass("bazel run //:serve answers on a kernel-assigned port")

		assertServesTheApp(it, built, serveBase, serveHost)
		it.Pass("the built app server-renders: /dynamic differs per request and echoes the " +
			"request host, /ssr runs getServerSideProps, both API routes answer, middleware sets its header")

		// The image optimizer writes its cache under .next at request time, so a
		// 200 here is also the reason next_serve copies the build output instead
		// of serving the read-only Bazel tree.
		optimized := imageURL(it, get(it, built, serveBase, "/").body)
		if got := get(it, built, serveBase, optimized); !strings.HasPrefix(got.header.Get("content-type"), "image/") {
			it.Fail("GET %s%s answered content-type %q, want an image", serveBase, optimized, got.header.Get("content-type"))
		}
		it.Pass("next/image optimizes the statically imported asset at request time")

		// public/ is never copied into .next, so serving it at its own URL is
		// what pins next_serve's srcs: without the staging there is no file here.
		if got := get(it, built, serveBase, "/logo.png"); !strings.HasPrefix(got.header.Get("content-type"), "image/") {
			it.Fail("GET %s/logo.png answered content-type %q; public/ was not staged beside the build",
				serveBase, got.header.Get("content-type"))
		}
		it.Pass("public/ is served from the staged project root")

		built.stop()

		// ── next_dev_server: the source tree, served ──────────────────────────
		devPort := freePort(it)
		devBase := fmt.Sprintf("http://127.0.0.1:%d", devPort)
		devHost := fmt.Sprintf("127.0.0.1:%d", devPort)
		dev := serve(it, "dev.log", "//:dev", devPort)
		await(it, dev, devBase+"/about")
		it.Pass("bazel run //:dev answers on a kernel-assigned port")

		assertServesTheApp(it, dev, devBase, devHost)
		it.Pass("next dev serves the same app from source: no next_build output is involved")

		// What a dev server is for: an edit reaches the response without Bazel.
		// Nothing is rebuilt between the write and the request.
		it.Replace(it.Path(greetingSrc), "Hello, ", "Reloaded, ")
		if !awaitBody(devBase+"/", "Reloaded, Bazel!") {
			fmt.Fprintln(os.Stderr, dev.text())
			it.Fail("GET %s/ never picked up the edit to %s; the dev server is not serving source",
				devBase, greetingSrc)
		}
		it.Pass("an edit to a staged source reaches the dev server's response with no rebuild")

		dev.stop()
	})
}
