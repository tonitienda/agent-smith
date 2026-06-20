package streamio

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestSSEReaderPreservesPayloadCarriageReturn(t *testing.T) {
	r := NewSSEReader(strings.NewReader("data: foo\r\r\n\n"))
	got, err := r.ReadEvent()
	if err != nil {
		t.Fatalf("ReadEvent() error = %v", err)
	}
	if string(got) != "foo\r" {
		t.Fatalf("ReadEvent() = %q, want %q", got, "foo\r")
	}
}

func TestSSEReaderJoinsDataLinesAndSkipsMetadata(t *testing.T) {
	r := NewSSEReader(strings.NewReader(": comment\nevent: message\nid: 1\ndata: one\ndata: two\n\n"))
	got, err := r.ReadEvent()
	if err != nil {
		t.Fatalf("ReadEvent() error = %v", err)
	}
	if string(got) != "one\ntwo" {
		t.Fatalf("ReadEvent() = %q, want %q", got, "one\\ntwo")
	}
}

func TestSSEReaderReturnsTrailingFrameAtEOF(t *testing.T) {
	r := NewSSEReader(strings.NewReader("data: tail"))
	got, err := r.ReadEvent()
	if err != nil {
		t.Fatalf("ReadEvent() error = %v", err)
	}
	if string(got) != "tail" {
		t.Fatalf("ReadEvent() = %q, want tail", got)
	}
	_, err = r.ReadEvent()
	if !errors.Is(err, io.EOF) {
		t.Fatalf("second ReadEvent() error = %v, want io.EOF", err)
	}
}
