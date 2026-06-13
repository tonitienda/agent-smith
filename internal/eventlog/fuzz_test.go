package eventlog

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/schema"
)

// FuzzAppendReload drives append + reload round-trips with adversarial inputs:
// arbitrary bytes are sliced into a sequence of blocks of varying kinds (text,
// tool calls, and the two non-content event kinds, exclusion and derived), each
// with content drawn from the fuzz corpus. The log written to disk and re-read
// must yield identical events in identical order — the core durability promise.
func FuzzAppendReload(f *testing.F) {
	f.Add([]byte(""))
	f.Add([]byte("hello world"))
	f.Add([]byte{0x00, 0xff, '\n', '{', '}', 0x7f})
	f.Add([]byte("a\nb\nc\n\"quoted\"\t\\backslash"))

	f.Fuzz(func(t *testing.T, data []byte) {
		path := filepath.Join(t.TempDir(), "fuzz.jsonl")
		l, err := Open(path)
		if err != nil {
			t.Fatalf("open: %v", err)
		}

		// Build a deterministic block sequence from the fuzz bytes. One block per
		// non-overlapping 4-byte window keeps small inputs producing at least one
		// block while letting larger inputs produce many.
		want := []schema.Block{}
		appendBlock := func(b schema.Block) {
			stored, aerr := l.Append(b)
			if aerr != nil {
				t.Fatalf("append: %v", aerr)
			}
			want = append(want, stored)
		}

		// Content fields hold text, so use valid UTF-8 derived from the fuzz
		// bytes. (JSON encodes invalid UTF-8 lossily as U+FFFD — a property of
		// JSON, not the log — so feeding raw invalid bytes would test the wrong
		// thing.) Quotes, newlines, control bytes, and backslashes still survive.
		clean := func(b []byte) string { return strings.ToValidUTF8(string(b), "�") }

		appendBlock(textBlock(schema.NewID(), clean(data)))
		for i := 0; i < len(data); i += 4 {
			end := min(i+4, len(data))
			chunk := clean(data[i:end])
			switch data[i] % 4 {
			case 0:
				appendBlock(textBlock(schema.NewID(), chunk))
			case 1:
				appendBlock(toolCallBlock(schema.NewID(), chunk))
			case 2:
				// Exclusion referencing an existing event keeps it realistic.
				appendBlock(NewExclusion(chunk, want[len(want)-1].ID))
			case 3:
				der := Derive(textBlock(schema.NewID(), chunk), chunk, want[len(want)-1].ID)
				der.Kind = schema.KindCompaction
				appendBlock(der)
			}
		}
		if err := l.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}

		reopened, err := Open(path)
		if err != nil {
			t.Fatalf("reopen: %v", err)
		}
		defer mustClose(t, reopened)

		if got, w := marshalEvents(t, reopened.Events()), marshalEvents(t, want); got != w {
			t.Fatalf("round-trip mismatch.\n got: %s\nwant: %s", got, w)
		}
		for _, b := range want {
			if _, ok := reopened.ByID(b.ID); !ok {
				t.Fatalf("event %s missing after reload", b.ID)
			}
		}
	})
}
