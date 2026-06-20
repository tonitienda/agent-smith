package streamio_test

import (
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/tonitienda/agent-smith/internal/streamio"
)

// readAll drains every event from r until EOF, failing on any other error.
func readAll(t *testing.T, r *streamio.SSEReader) []string {
	t.Helper()
	var out []string
	for {
		data, err := r.ReadEvent()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatalf("ReadEvent: %v", err)
		}
		out = append(out, string(data))
	}
}

func TestReadEventFramesAndIgnoresNonData(t *testing.T) {
	raw := strings.Join([]string{
		`: a comment`,
		`event: message`,
		`id: 7`,
		`data: {"type":`,
		`data: "ping"}`,
		``,
		`data: {"type":"stop"}`,
		``,
	}, "\n")

	got := readAll(t, streamio.NewSSEReader(strings.NewReader(raw)))
	// Multi-line data is joined with "\n" per the SSE spec; comment/event/id
	// lines are dropped.
	want := []string{"{\"type\":\n\"ping\"}", `{"type":"stop"}`}
	if len(got) != len(want) {
		t.Fatalf("got %d events %q, want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("event %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestReadEventChunkedInputIsFramedIdentically(t *testing.T) {
	raw := "data: one\n\ndata: two\n\n"
	// A one-byte-at-a-time reader exercises ReadString across buffer boundaries.
	got := readAll(t, streamio.NewSSEReader(iotest.OneByteReader(strings.NewReader(raw))))
	if len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("chunked framing = %q, want [one two]", got)
	}
}

func TestReadEventTrailingFrameWithoutBlankLine(t *testing.T) {
	// A stream that ends mid-frame still yields the buffered data, then EOF.
	r := streamio.NewSSEReader(strings.NewReader("data: last"))
	data, err := r.ReadEvent()
	if err != nil {
		t.Fatalf("ReadEvent: %v", err)
	}
	if string(data) != "last" {
		t.Errorf("data = %q, want last", data)
	}
	if _, err := r.ReadEvent(); !errors.Is(err, io.EOF) {
		t.Errorf("err = %v, want io.EOF", err)
	}
}

func TestReadEventEmptyStreamIsEOF(t *testing.T) {
	r := streamio.NewSSEReader(strings.NewReader(""))
	if _, err := r.ReadEvent(); !errors.Is(err, io.EOF) {
		t.Errorf("err = %v, want io.EOF", err)
	}
}

func TestReadEventPropagatesReadError(t *testing.T) {
	// A mid-stream read failure (e.g. a cancelled request closing the body)
	// surfaces verbatim rather than being mistaken for a clean end.
	boom := errors.New("connection reset")
	r := streamio.NewSSEReader(errReaderAfter("data: partial", boom))
	if _, err := r.ReadEvent(); !errors.Is(err, boom) {
		t.Fatalf("ReadEvent err = %v, want %v", err, boom)
	}
}

// errReaderAfter returns prefix once, then err on the next read.
func errReaderAfter(prefix string, err error) io.Reader {
	return &errReader{rest: prefix, err: err}
}

type errReader struct {
	rest string
	err  error
}

func (e *errReader) Read(p []byte) (int, error) {
	if e.rest != "" {
		n := copy(p, e.rest)
		e.rest = e.rest[n:]
		return n, nil
	}
	return 0, e.err
}
