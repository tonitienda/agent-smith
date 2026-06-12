package main

import (
	"bytes"
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
