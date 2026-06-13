package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestRunVersion(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"--version"}, &out); err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	if got := out.String(); !strings.HasPrefix(got, "smith ") {
		t.Fatalf("version output %q does not start with smith", got)
	}
}

func TestRunRejectsUnexpectedArguments(t *testing.T) {
	var out bytes.Buffer
	err := run([]string{"chat"}, &out)
	if err == nil {
		t.Fatal("run returned nil error, want unexpected argument error")
	}
	if !strings.Contains(err.Error(), "unexpected arguments") {
		t.Fatalf("error %q does not mention unexpected arguments", err)
	}
}

func TestRunHelpPropagatesWriterError(t *testing.T) {
	want := errors.New("write failed")
	err := run([]string{"--help"}, errWriter{err: want})
	if !errors.Is(err, want) {
		t.Fatalf("run returned %v, want %v", err, want)
	}
}

type errWriter struct {
	err error
}

func (w errWriter) Write([]byte) (int, error) {
	return 0, w.err
}
