// The edit-to-HMR benchmark: how long a developer waits between saving a file
// and the browser being told to change.
//
// The design goal on record for `ts_dev_server` is under 500ms from save to
// browser update, and until this existed nothing measured it, so a change that
// doubled it would have been invisible. Timing the transform alone would not
// have caught it either: the loop a developer feels is a file write, the
// server's watcher noticing, the transform, and a frame arriving on the HMR
// socket a browser is holding open. So this runs the launcher exactly as
// `bazel run` does against a throwaway workspace, seats a module in the server's
// graph by requesting it, holds a real WebSocket open as an HMR client, and
// edits the file on disk.
//
// Two numbers per edit, and the second is the headline:
//
//	notify — the write to the HMR frame reaching the client. The server has
//	         decided what changed, and has said so.
//	served — the write to the changed module coming back over HTTP with the new
//	         bytes, fetched after the frame the way a client fetches it. This is
//	         the one that contains the transform.
//
// What this cannot measure is the browser's share: re-executing the module and
// re-rendering happens in a page, and there is no page here. So the numbers are
// the server side of the 500ms budget, not the whole of it — which is also why
// the ceiling below is not tight against them.
//
// One assertion, on the median served time, and it is the whole 500ms budget:
// the server side of this loop measures single-digit milliseconds, so a run that
// spends the entire user-visible budget before the browser has been handed
// anything is broken however fast the machine is. Tighter would be a flakiness
// machine — these numbers move by a factor of two on a loaded machine, and a
// test that fails because CI was busy teaches everyone to ignore it. At forty
// times the observed median the only things that trip it are the ones worth
// waking up for: HMR falling back to a rebuild, a watcher gone to polling, the
// transform moving off the warm path. Everything measured is logged on every run
// either way, so `bazel test --test_output=all --test_arg=-test.v` is the
// report, and a longer run is `--test_env=HMR_ITERATIONS=100`.
//
// Which server is under test comes from the env of the go_test target: DEV_TARGET
// names the ts_dev_server, DEV_IMPL and HMR_WS_PATH/HMR_WS_PROTOCOL say where
// that implementation's HMR socket is.
package dev_server_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mikn/rules_typescript/tests/verify"
)

// The design goal itself. See the package comment for why the whole budget is
// the right ceiling for the part of the loop this measures.
const hmrCeiling = 500 * time.Millisecond

// Long enough that a slow machine is not a failure, short enough that "HMR does
// not work at all" is reported rather than timing the whole test out.
const hmrFrameTimeout = 30 * time.Second

// The module the benchmark saves, as a workspace-relative path and as the URL it
// is served at.
const hotFile = "hot.ts"

// Chokidar suppresses a second change to the same path within 50ms of the one it
// emitted -- dropped, not deferred -- so a benchmark that saves as fast as it can
// measures one edit and then hangs. Nobody types that fast; the samples are
// spaced instead, outside the timed window.
const editSpacing = 300 * time.Millisecond

func TestHMRLatency(t *testing.T) {
	tree := verify.New(t)
	target := env(t, "DEV_TARGET")
	impl := env(t, "DEV_IMPL")
	wsPath := env(t, "HMR_WS_PATH")
	wsProtocol := os.Getenv("HMR_WS_PROTOCOL")
	iterations := iterationCount(t)

	launcher := tree.File("tests/dev_server/" + target + "_launcher")
	if !launcher.Exists() {
		t.FailNow()
	}

	tmp := t.TempDir()
	ws := filepath.Join(tmp, "ws")
	mkdir(t, filepath.Join(ws, "bazel-bin"))

	// hot.ts is the file the benchmark saves, and it declares itself an HMR
	// boundary so a server that can send a scoped update does. app.ts imports it
	// so that a server which only tracks changes to modules it has served has
	// both of them: requesting the importer is not enough, since neither server
	// transforms a module before the client asks for it.
	write(t, filepath.Join(ws, hotFile), hotModule(0))
	write(t, filepath.Join(ws, "app.ts"),
		"import { revision } from \"./"+hotFile+"\";\nexport { revision };\n")

	var extraArgs []string
	if impl == "vite" {
		extraArgs = append(extraArgs, "--strictPort")
	}
	srv := start(t, launcher.Abs(), ws, tmp, extraArgs...)
	base := srv.awaitHTTP(t, "/app.ts")
	for _, path := range []string{"/app.ts", "/" + hotFile} {
		if r := get(t, base, path); r.status != 200 {
			t.Fatalf("GET %s returned %d, want 200; the module is not in the server's "+
				"graph and no edit to it will produce an update\n%s", path, r.status, srv.log(t))
		}
	}

	sock := dialHMR(t, strings.TrimPrefix(base, "http://"), wsPath, wsProtocol)
	t.Logf("%s (%s) is serving on %s, HMR socket at %s", target, impl, base, wsPath)

	// The first edit pays for whatever the server does once: a watcher settling,
	// a transform pipeline warming up. It is reported rather than averaged in,
	// because it is not what the next hour of editing costs.
	cold := hmrEdit(t, srv, sock, base, ws, 1)
	t.Logf("cold first edit: notify %s, served %s (%s)", cold.notify, cold.served, cold.kind)

	var notify, served []time.Duration
	kinds := map[string]int{}
	for i := 0; i < iterations; i++ {
		e := hmrEdit(t, srv, sock, base, ws, i+2)
		notify = append(notify, e.notify)
		served = append(served, e.served)
		kinds[e.kind]++
	}

	t.Logf("edit → HMR frame  (n=%d): %s", iterations, distribution(notify))
	t.Logf("edit → new bytes  (n=%d): %s", iterations, distribution(served))
	t.Logf("what the server sent: %v", kinds)

	if median := percentile(served, 50); median > hmrCeiling {
		t.Errorf("the median save-to-served time is %s, and the whole design budget from "+
			"save to browser update is %s. The browser has not been handed anything yet, so "+
			"this is not machine noise: HMR is no longer on the warm path.\n%s",
			median, hmrCeiling, srv.log(t))
	}
}

// hotModule is the file under edit. The marker is a string rather than the
// number itself so that finding it in the response cannot depend on how the
// transform spaces an expression out.
func hotModule(revision int) string {
	return fmt.Sprintf(
		"export const revision = \"HMR_REV_%d\";\nif (import.meta.hot) import.meta.hot.accept();\n",
		revision)
}

type hmrEditResult struct {
	notify time.Duration
	served time.Duration
	// kind is the HMR message the server chose. Which one is the server's call
	// and not this test's: a module that is an HMR boundary gets a scoped update,
	// and one that is not gets a full reload, which is equally what the developer
	// sees. Recorded so that a run says which happened.
	kind string
}

func hmrEdit(t *testing.T, srv *server, sock *hmrSocket, base, ws string, revision int) hmrEditResult {
	t.Helper()
	marker := fmt.Sprintf("HMR_REV_%d", revision)

	time.Sleep(editSpacing)
	sock.drain()
	saved := time.Now()
	write(t, filepath.Join(ws, hotFile), hotModule(revision))

	kind, frame := awaitHMRUpdate(t, srv, sock)
	notify := time.Since(saved)

	// The frame only says a module changed; the bytes come from the fetch a
	// client makes next, and that fetch is where the transform happens.
	deadline := saved.Add(hmrFrameTimeout)
	for !strings.Contains(bodyOf(base, "/"+hotFile), marker) {
		if time.Now().After(deadline) {
			t.Fatalf("the server announced %s (%s) but never served %s\n%s",
				kind, frame, marker, srv.log(t))
		}
	}
	return hmrEditResult{notify: notify, served: time.Since(saved), kind: kind}
}

// awaitHMRUpdate returns the first frame that tells the client the page has to
// change, skipping the ones that do not: a connection greeting, a heartbeat, an
// implementation's own custom event.
func awaitHMRUpdate(t *testing.T, srv *server, sock *hmrSocket) (string, string) {
	t.Helper()
	deadline := time.Now().Add(hmrFrameTimeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			t.Fatalf("no HMR update within %s of the save; the edit never reached a client\n%s",
				hmrFrameTimeout, srv.log(t))
		}
		frame, err := sock.next(remaining)
		if err != nil {
			t.Fatalf("waiting for an HMR update: %v\n%s", err, srv.log(t))
		}
		var msg struct {
			Type string `json:"type"`
		}
		if json.Unmarshal([]byte(frame), &msg) != nil {
			continue
		}
		switch msg.Type {
		case "update", "js-update", "css-update", "full-reload", "patch":
			return msg.Type, frame
		case "error":
			t.Fatalf("the dev server answered the save with a transform error: %s\n%s",
				frame, srv.log(t))
		}
	}
}

func iterationCount(t *testing.T) int {
	t.Helper()
	raw := os.Getenv("HMR_ITERATIONS")
	if raw == "" {
		return 12
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		t.Fatalf("HMR_ITERATIONS=%q is not a positive number of samples", raw)
	}
	return n
}

func distribution(samples []time.Duration) string {
	return fmt.Sprintf("min %s, median %s, p90 %s, max %s",
		percentile(samples, 0), percentile(samples, 50),
		percentile(samples, 90), percentile(samples, 100))
}

// percentile is nearest-rank on a copy, which is the honest summary of a dozen
// samples: interpolating between two of them invents a measurement.
func percentile(samples []time.Duration, p int) time.Duration {
	sorted := slices.Clone(samples)
	slices.Sort(sorted)
	i := len(sorted) * p / 100
	return sorted[min(i, len(sorted)-1)]
}
