package streamio

import (
	"bufio"
	"errors"
	"io"
	"strings"
)

// SSEReader reads Server-Sent Events from a stream. It returns only event data:
// multiple data: lines are joined with "\n", while comments, event names, IDs,
// and other fields are ignored by this protocol-agnostic helper.
type SSEReader struct {
	br *bufio.Reader
}

// NewSSEReader returns an SSE reader over r.
func NewSSEReader(r io.Reader) *SSEReader {
	return &SSEReader{br: bufio.NewReader(r)}
}

// ReadEvent reads one Server-Sent Event data payload. It returns io.EOF only at
// a clean end with no buffered data.
func (r *SSEReader) ReadEvent() ([]byte, error) {
	var data []byte
	haveData := false
	for {
		line, err := r.br.ReadString('\n')
		if len(line) > 0 {
			trimmed := trimSSELineEnding(line)
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

func trimSSELineEnding(line string) string {
	line = strings.TrimSuffix(line, "\n")
	return strings.TrimSuffix(line, "\r")
}
