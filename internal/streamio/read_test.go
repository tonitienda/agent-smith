package streamio

import (
	"errors"
	"io"
	"strings"
	"testing"
)

type closeErrReader struct{ *strings.Reader }

func (c closeErrReader) Close() error { return errors.New("close failed") }

func TestReadAllLimitNilReader(t *testing.T) {
	got, err := ReadAllLimit(nil, 4)
	if err != nil {
		t.Fatalf("ReadAllLimit(nil) error = %v", err)
	}
	if got != nil {
		t.Fatalf("ReadAllLimit(nil) = %q, want nil", got)
	}
}

func TestDrainCloseNilReadCloser(t *testing.T) {
	if err := DrainClose(nil, 4); err != nil {
		t.Fatalf("DrainClose(nil) error = %v", err)
	}
}

func TestDrainCloseReturnsCloseError(t *testing.T) {
	err := DrainClose(closeErrReader{strings.NewReader("abcdef")}, 3)
	if err == nil || err.Error() != "close failed" {
		t.Fatalf("DrainClose() error = %v, want close failed", err)
	}
}

func TestReadAllLimitBoundsBytes(t *testing.T) {
	got, err := ReadAllLimit(strings.NewReader("abcdef"), 3)
	if err != nil {
		t.Fatalf("ReadAllLimit() error = %v", err)
	}
	if string(got) != "abc" {
		t.Fatalf("ReadAllLimit() = %q, want abc", got)
	}
}

var _ io.ReadCloser = closeErrReader{}
