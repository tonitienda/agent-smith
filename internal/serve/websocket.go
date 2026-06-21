package serve

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
)

// wsGUID is the RFC 6455 §1.3 magic string mixed into the accept-key hash.
const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// maxMessageBytes caps a single inbound message so a misbehaving client cannot
// drive the server to allocate without bound. JSON-RPC control messages are tiny;
// this ceiling is generous for a prompt or tool result while staying defensive.
const maxMessageBytes = 8 << 20 // 8 MiB

// WebSocket frame opcodes (RFC 6455 §5.2).
const (
	opContinuation = 0x0
	opText         = 0x1
	opBinary       = 0x2
	opClose        = 0x8
	opPing         = 0x9
	opPong         = 0xA
)

// errNotWebSocket marks an HTTP request that is not a WebSocket upgrade, so the
// handler can answer with a plain 400 instead of hijacking the connection.
var errNotWebSocket = errors.New("serve: not a websocket upgrade request")

// wsConn is a minimal RFC 6455 server-side WebSocket connection over a hijacked
// net.Conn. It speaks only the subset Agent Smith's JSON-RPC face needs: text
// data messages (with continuation), ping/pong, and close. There is no
// extension (RSV) or per-message-deflate support, which keeps the codec small and
// dependency-free (the core stays stdlib-first, PRD D6). Writes are serialized so
// the event stream and a server-initiated permission request can share one
// connection from several goroutines.
type wsConn struct {
	conn net.Conn
	br   *bufio.Reader
	wmu  sync.Mutex
}

// upgrade performs the RFC 6455 opening handshake and hijacks the connection,
// returning a wsConn ready for ReadMessage/WriteMessage. It validates the
// Upgrade/Connection headers and the Sec-WebSocket-Key, then writes the 101
// response with the derived accept key.
func upgrade(w http.ResponseWriter, r *http.Request) (*wsConn, error) {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") || !headerContains(r.Header.Get("Connection"), "upgrade") {
		return nil, errNotWebSocket
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		return nil, errors.New("serve: missing Sec-WebSocket-Key")
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		return nil, errors.New("serve: response writer does not support hijacking")
	}
	conn, brw, err := hj.Hijack()
	if err != nil {
		return nil, fmt.Errorf("serve: hijack: %w", err)
	}
	resp := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + acceptKey(key) + "\r\n\r\n"
	if _, err := io.WriteString(conn, resp); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("serve: write handshake: %w", err)
	}
	// The hijacked reader may already hold bytes the server read past the request,
	// so keep using it rather than reading the raw conn directly.
	return &wsConn{conn: conn, br: brw.Reader}, nil
}

// acceptKey derives the Sec-WebSocket-Accept response value from the client key
// (RFC 6455 §4.2.2): base64(SHA1(key + GUID)).
func acceptKey(key string) string {
	h := sha1.New()
	_, _ = io.WriteString(h, key+wsGUID)
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// headerContains reports whether a comma-separated header value lists token
// (case-insensitively), e.g. Connection: keep-alive, Upgrade.
func headerContains(header, token string) bool {
	for _, part := range strings.Split(header, ",") {
		if strings.EqualFold(strings.TrimSpace(part), token) {
			return true
		}
	}
	return false
}

// ReadMessage returns the next complete text or binary data message, handling
// control frames transparently: a ping is answered with a pong, a close ends the
// stream with io.EOF. Continuation frames are reassembled into one message.
func (c *wsConn) ReadMessage() ([]byte, error) {
	var msg []byte
	inMessage := false
	for {
		fin, opcode, payload, err := c.readFrame()
		if err != nil {
			return nil, err
		}
		switch opcode {
		case opPing:
			if err := c.writeFrame(opPong, payload); err != nil {
				return nil, err
			}
			continue
		case opPong:
			continue
		case opClose:
			_ = c.writeFrame(opClose, nil)
			return nil, io.EOF
		case opText, opBinary:
			// A new data frame before the previous fragmented message finished is a
			// protocol violation (RFC 6455 §5.4).
			if inMessage {
				return nil, errors.New("serve: new data frame before previous message finished")
			}
			inMessage = true
			msg = append(msg, payload...)
		case opContinuation:
			if !inMessage {
				return nil, errors.New("serve: continuation frame without an open message")
			}
			msg = append(msg, payload...)
		default:
			return nil, fmt.Errorf("serve: unsupported opcode %d", opcode)
		}
		if len(msg) > maxMessageBytes {
			return nil, errors.New("serve: message exceeds size limit")
		}
		if fin && inMessage {
			return msg, nil
		}
	}
}

// readFrame reads one WebSocket frame, unmasking the payload when the client set
// the mask bit (clients always must, RFC 6455 §5.3).
func (c *wsConn) readFrame() (fin bool, opcode byte, payload []byte, err error) {
	var head [2]byte
	if _, err = io.ReadFull(c.br, head[:]); err != nil {
		return
	}
	fin = head[0]&0x80 != 0
	if head[0]&0x70 != 0 {
		err = errors.New("serve: reserved bits set (extensions unsupported)")
		return
	}
	opcode = head[0] & 0x0f
	masked := head[1]&0x80 != 0
	// A client MUST mask every frame (RFC 6455 §5.1); an unmasked frame is a
	// protocol/security violation (cache poisoning, smuggling), so reject it.
	if !masked {
		err = errors.New("serve: client frame is not masked (RFC 6455 §5.1)")
		return
	}
	n := int64(head[1] & 0x7f)
	switch n {
	case 126:
		var ext [2]byte
		if _, err = io.ReadFull(c.br, ext[:]); err != nil {
			return
		}
		n = int64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err = io.ReadFull(c.br, ext[:]); err != nil {
			return
		}
		n = int64(binary.BigEndian.Uint64(ext[:]))
	}
	if n < 0 || n > maxMessageBytes {
		err = errors.New("serve: frame exceeds size limit")
		return
	}
	// Control frames (opcode >= 0x8) must carry <=125 bytes and must not be
	// fragmented (RFC 6455 §5.5).
	if opcode >= 0x8 {
		if n > 125 {
			err = errors.New("serve: control frame payload exceeds 125 bytes")
			return
		}
		if !fin {
			err = errors.New("serve: fragmented control frame")
			return
		}
	}
	var mask [4]byte
	if masked {
		if _, err = io.ReadFull(c.br, mask[:]); err != nil {
			return
		}
	}
	payload = make([]byte, n)
	if _, err = io.ReadFull(c.br, payload); err != nil {
		return
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i&3]
		}
	}
	return
}

// WriteMessage sends b as a single unmasked text frame (server frames are never
// masked, RFC 6455 §5.1).
func (c *wsConn) WriteMessage(b []byte) error {
	return c.writeFrame(opText, b)
}

// writeFrame writes one final, unmasked frame with the given opcode and payload.
// Writes are serialized so concurrent emitters cannot interleave a frame.
func (c *wsConn) writeFrame(opcode byte, payload []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	n := len(payload)
	b0 := byte(0x80) | opcode // FIN set
	var header []byte
	switch {
	case n < 126:
		header = []byte{b0, byte(n)}
	case n < 1<<16:
		header = []byte{b0, 126, byte(n >> 8), byte(n)}
	default:
		header = make([]byte, 10)
		header[0], header[1] = b0, 127
		binary.BigEndian.PutUint64(header[2:], uint64(n))
	}
	if _, err := c.conn.Write(header); err != nil {
		return err
	}
	if n > 0 {
		if _, err := c.conn.Write(payload); err != nil {
			return err
		}
	}
	return nil
}

// Close sends a close frame (best-effort) and closes the underlying connection.
func (c *wsConn) Close() error {
	_ = c.writeFrame(opClose, nil)
	return c.conn.Close()
}
