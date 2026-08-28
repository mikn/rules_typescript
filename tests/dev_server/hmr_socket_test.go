// A WebSocket client, because measuring the dev server's HMR loop means being
// an HMR client: the update a developer waits for is a frame on this socket,
// and nothing else in the ruleset speaks the protocol.
//
// It is read-only past the handshake. Server-to-client frames are unmasked, so
// the only frame this ever sends is a masked pong, and the handshake omits
// Origin -- which is what keeps Vite's WebSocket token out of the picture, since
// Vite only demands the token from a request that carries one.
package dev_server_test

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"strings"
	"testing"
	"time"
)

// A frame larger than this is not an HMR message; reading its length as one
// would allocate whatever a confused peer put on the wire.
const maxFramePayload = 1 << 24

type hmrSocket struct {
	conn   net.Conn
	frames chan string
	fail   chan error
}

// dialHMR completes the WebSocket handshake against a dev server's HMR endpoint.
// Where that endpoint is, and whether it demands a subprotocol, is per
// implementation: Vite upgrades on the base path and only for "vite-hmr", oj
// upgrades on /__ws for anything.
func dialHMR(t *testing.T, addr, path, protocol string) *hmrSocket {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		t.Fatalf("dialing the HMR socket at %s: %v", addr, err)
	}

	var key [16]byte
	if _, err := rand.Read(key[:]); err != nil {
		t.Fatalf("generating a Sec-WebSocket-Key: %v", err)
	}
	req := "GET " + path + " HTTP/1.1\r\n" +
		"Host: " + addr + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Version: 13\r\n" +
		"Sec-WebSocket-Key: " + base64.StdEncoding.EncodeToString(key[:]) + "\r\n"
	if protocol != "" {
		req += "Sec-WebSocket-Protocol: " + protocol + "\r\n"
	}
	if _, err := conn.Write([]byte(req + "\r\n")); err != nil {
		t.Fatalf("sending the HMR handshake: %v", err)
	}

	reader := bufio.NewReader(conn)
	status, err := textproto.NewReader(reader).ReadLine()
	if err != nil {
		t.Fatalf("reading the HMR handshake response: %v", err)
	}
	if !strings.HasPrefix(status, "HTTP/1.1 101") {
		t.Fatalf("the HMR endpoint %s%s answered %q, want a 101 upgrade", addr, path, status)
	}
	for {
		line, err := textproto.NewReader(reader).ReadLine()
		if err != nil {
			t.Fatalf("reading the HMR handshake headers: %v", err)
		}
		if line == "" {
			break
		}
	}

	s := &hmrSocket{conn: conn, frames: make(chan string, 64), fail: make(chan error, 1)}
	go s.read(reader)
	t.Cleanup(func() { _ = conn.Close() })
	return s
}

func (s *hmrSocket) next(timeout time.Duration) (string, error) {
	select {
	case frame := <-s.frames:
		return frame, nil
	case err := <-s.fail:
		s.fail <- err
		return "", err
	case <-time.After(timeout):
		return "", fmt.Errorf("no HMR frame within %s", timeout)
	}
}

// drain discards whatever the server has already said, so that the next frame is
// the answer to the edit about to be made.
func (s *hmrSocket) drain() {
	for {
		select {
		case <-s.frames:
		default:
			return
		}
	}
}

func (s *hmrSocket) read(reader *bufio.Reader) {
	var message []byte
	var opcode byte
	for {
		op, fin, payload, err := readFrame(reader)
		if err != nil {
			s.fail <- err
			return
		}
		switch op {
		case 0x9:
			if err := s.pong(payload); err != nil {
				s.fail <- err
				return
			}
			continue
		case 0xA:
			continue
		case 0x8:
			s.fail <- errors.New("the dev server closed the HMR socket")
			return
		case 0x0:
			message = append(message, payload...)
		default:
			opcode, message = op, append(message[:0], payload...)
		}
		if !fin {
			continue
		}
		if opcode == 0x1 || opcode == 0x2 {
			s.frames <- string(message)
		}
		message = message[:0]
	}
}

// pong answers a ping. A control frame carries at most 125 bytes by spec, which
// is why this is the whole of the client's write path.
func (s *hmrSocket) pong(payload []byte) error {
	if len(payload) > 125 {
		return fmt.Errorf("ping payload is %d bytes, which is not a control frame", len(payload))
	}
	var mask [4]byte
	if _, err := rand.Read(mask[:]); err != nil {
		return err
	}
	frame := append([]byte{0x8A, 0x80 | byte(len(payload))}, mask[:]...)
	for i, b := range payload {
		frame = append(frame, b^mask[i%4])
	}
	_, err := s.conn.Write(frame)
	return err
}

func readFrame(reader *bufio.Reader) (opcode byte, fin bool, payload []byte, err error) {
	var head [2]byte
	if _, err = io.ReadFull(reader, head[:]); err != nil {
		return
	}
	fin = head[0]&0x80 != 0
	opcode = head[0] & 0x0F
	masked := head[1]&0x80 != 0
	size := uint64(head[1] & 0x7F)
	switch size {
	case 126:
		var ext [2]byte
		if _, err = io.ReadFull(reader, ext[:]); err != nil {
			return
		}
		size = uint64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err = io.ReadFull(reader, ext[:]); err != nil {
			return
		}
		size = binary.BigEndian.Uint64(ext[:])
	}
	if size > maxFramePayload {
		err = fmt.Errorf("a %d-byte frame is not an HMR message", size)
		return
	}
	var mask [4]byte
	if masked {
		if _, err = io.ReadFull(reader, mask[:]); err != nil {
			return
		}
	}
	payload = make([]byte, size)
	if _, err = io.ReadFull(reader, payload); err != nil {
		return
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	return
}
