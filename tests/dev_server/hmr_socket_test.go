// The dev-server suite's view of //tests/hmrsocket: the same client, with the
// failures raised as t.Fatalf so a test reads as a test.
package dev_server_test

import (
	"testing"

	"github.com/mikn/rules_typescript/tests/hmrsocket"
)

type hmrSocket = hmrsocket.Socket

// dialHMR completes the WebSocket handshake against a dev server's HMR endpoint.
// Where that endpoint is, and whether it demands a subprotocol, is per
// implementation: Vite upgrades on the base path and only for "vite-hmr".
func dialHMR(t *testing.T, addr, path, protocol string) *hmrSocket {
	t.Helper()
	sock, err := hmrsocket.Dial(addr, path, protocol)
	if err != nil {
		t.Fatalf("%v", err)
	}
	t.Cleanup(sock.Close)
	return sock
}
