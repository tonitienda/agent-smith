package main

import (
	"io"
	"strings"
	"testing"
)

func TestCheckBindLoopbackAllowed(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:8765", "localhost:8765", "[::1]:8765"} {
		if err := checkBind(addr, false, io.Discard); err != nil {
			t.Errorf("checkBind(%q) = %v, want nil", addr, err)
		}
	}
}

func TestCheckBindNonLoopbackRequiresUnsafe(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:8765", ":8765", "192.168.1.5:8765"} {
		if err := checkBind(addr, false, io.Discard); err == nil {
			t.Errorf("checkBind(%q) without --unsafe-bind = nil, want refusal", addr)
		}
	}
}

func TestCheckBindUnsafeWarns(t *testing.T) {
	var sb strings.Builder
	if err := checkBind("0.0.0.0:8765", true, &sb); err != nil {
		t.Fatalf("checkBind with --unsafe-bind = %v, want nil", err)
	}
	if !strings.Contains(strings.ToLower(sb.String()), "not a sandbox") {
		t.Errorf("expected AS-080 caveat in warning, got %q", sb.String())
	}
}

func TestCheckBindInvalidAddr(t *testing.T) {
	if err := checkBind("not-an-addr", false, io.Discard); err == nil {
		t.Error("checkBind(invalid) = nil, want usage error")
	}
}
