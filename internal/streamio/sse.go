// Package streamio holds protocol-agnostic stream I/O mechanics shared by the
// provider adapters (Anthropic, OpenAI) and the MCP client. It deliberately
// carries no domain knowledge: event normalization and JSON-RPC correlation stay
// in their own packages. Only the low-level framing and bounded-read plumbing
// live here, where all three consumers were otherwise duplicating it.
package streamio

import (
	"bufio"
	"errors"
	"io"
	"strings"
)

// SSEReader reads Server-Sent Events from a byte stream and returns each event's
// concatenated `data:` payload. SSE framing is identical wherever it appears, so
// Anthropic, OpenAI (Responses and Chat Completions), and MCP's Streamable HTTP
// transport all read through this one reader; only the JSON payloads they decode
// differ.
//
// Frames are separated by a blank line. A single event's data may span multiple
// `data:` lines, which are joined with "\n" per the SSE spec. `event:`, `id:`,
// and comment lines are ignored because each payload's own type field
// discriminates the event. The reader operates on an io.Reader and never closes
// it — lifetime (and thus context-driven cancellation, which surfaces as a read
// error once the caller closes the body) stays with the transport that owns the
// stream.
type SSEReader struct {
	br *bufio.Reader
}

// NewSSEReader wraps r in an SSEReader.
func NewSSEReader(r io.Reader) *SSEReader {
	return &SSEReader{br: bufio.NewReader(r)}
}

// ReadEvent reads one Server-Sent Event, returning its concatenated data
// payload. It returns io.EOF only at a clean end with no buffered data; a stream
// that ends mid-frame (no trailing blank line) still yields the buffered data.
func (r *SSEReader) ReadEvent() ([]byte, error) {
	var data []byte
	haveData := false
	for {
		line, err := r.br.ReadString('\n')
		if len(line) > 0 {
			trimmed := strings.TrimRight(line, "\r\n")
			switch {
			case trimmed == "":
				if haveData {
					return data, nil
				}
				// Blank line before any data (e.g. between comment frames): keep reading.
			case strings.HasPrefix(trimmed, "data:"):
				v := strings.TrimPrefix(strings.TrimPrefix(trimmed, "data:"), " ")
				if haveData {
					data = append(data, '\n')
				}
				data = append(data, v...)
				haveData = true
			default:
				// event:/id:/comment line — ignored.
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) && haveData {
				return data, nil
			}
			return nil, err
		}
	}
}
