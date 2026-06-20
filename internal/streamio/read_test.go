package streamio_test

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/internal/streamio"
)

func TestReadAllLimitCapsLongBodies(t *testing.T) {
	got, err := streamio.ReadAllLimit(strings.NewReader("abcdefghij"), 4)
	if err != nil {
		t.Fatalf("ReadAllLimit: %v", err)
	}
	if string(got) != "abcd" {
		t.Errorf("got %q, want abcd", got)
	}
}

func TestReadAllLimitReturnsShortBodyWhole(t *testing.T) {
	got, err := streamio.ReadAllLimit(strings.NewReader("hi"), 4096)
	if err != nil {
		t.Fatalf("ReadAllLimit: %v", err)
	}
	if string(got) != "hi" {
		t.Errorf("got %q, want hi", got)
	}
}

// drainCloser records whether Close ran and how many bytes were read.
type drainCloser struct {
	r      io.Reader
	read   int
	closed bool
}

func (d *drainCloser) Read(p []byte) (int, error) {
	n, err := d.r.Read(p)
	d.read += n
	return n, err
}

func (d *drainCloser) Close() error {
	d.closed = true
	return nil
}

func TestDrainCloseDrainsBoundedAndCloses(t *testing.T) {
	d := &drainCloser{r: strings.NewReader(strings.Repeat("x", 100))}
	if err := streamio.DrainClose(d, 10); err != nil {
		t.Fatalf("DrainClose: %v", err)
	}
	if !d.closed {
		t.Error("DrainClose did not close the body")
	}
	if d.read > 10 {
		t.Errorf("drained %d bytes, want at most 10", d.read)
	}
}

func TestDrainCloseReturnsCloseError(t *testing.T) {
	boom := errors.New("close failed")
	if err := streamio.DrainClose(errCloser{boom}, 10); !errors.Is(err, boom) {
		t.Errorf("DrainClose err = %v, want %v", err, boom)
	}
}

type errCloser struct{ err error }

func (errCloser) Read(p []byte) (int, error) { return 0, io.EOF }
func (e errCloser) Close() error             { return e.err }
