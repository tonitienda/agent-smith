package openai

import (
	"bufio"
	"errors"
	"io"
	"strings"
)

// sseReader reads Server-Sent Events from a response body. Both surface streams
// (Responses and Chat Completions) share it: SSE framing is identical across the
// two, only the JSON payloads differ. A frame's data may span multiple `data:`
// lines (joined with "\n"); `event:`, `id:`, and comment lines are ignored
// because each payload's own type field discriminates the event.
type sseReader struct {
	body io.ReadCloser
	br   *bufio.Reader
}

func newSSEReader(body io.ReadCloser) sseReader {
	return sseReader{body: body, br: bufio.NewReader(body)}
}

// readEvent reads one Server-Sent Event, returning its concatenated data
// payload. It returns io.EOF only at a clean end with no buffered data.
func (r sseReader) readEvent() ([]byte, error) {
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
