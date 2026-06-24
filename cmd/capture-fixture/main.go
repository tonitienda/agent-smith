// Command capture-fixture turns a redacted, reviewed session capture (a JSONL of
// schema.Block, as AS-060 produces) into a deterministic, CI-safe fixture plus a
// sidecar metadata file for the recorded vendor simulators (AS-133) and the
// offline E2E suite (AS-134).
//
// It normalizes identifying envelope values (IDs, timestamps, request/thread
// IDs) and scrubs secrets through the AS-115 redaction rules, without changing
// any block's kind or body shape, then validates every block so a malformed
// capture is reported instead of silently committed.
//
//	capture-fixture \
//	  -in   captures/raw/anthropic-toolcall.jsonl \
//	  -out  internal/provider/anthropic/testdata/fixtures/toolcall.jsonl \
//	  -source real-capture -status redacted \
//	  -intent "Anthropic Messages tool-call round trip + large argument" \
//	  -providers anthropic/messages
//
// The metadata is written next to -out as <out>.meta.json unless -meta is given.
// Reading from "-" reads stdin; an omitted -out writes the fixture to stdout (and
// requires -meta). The redaction-status and source values are validated, and the
// command exits non-zero if any block fails schema validation, so a bad fixture
// fails loudly rather than landing in CI.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/tonitienda/agent-smith/internal/capturefixture"
	"github.com/tonitienda/agent-smith/internal/redaction"
	"github.com/tonitienda/agent-smith/schema"
)

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "capture-fixture:", err)
		os.Exit(1)
	}
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("capture-fixture", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		in        = fs.String("in", "-", "input capture JSONL (one schema.Block per line; \"-\" for stdin)")
		out       = fs.String("out", "", "output fixture JSONL path (empty writes to stdout)")
		metaPath  = fs.String("meta", "", "metadata output path (default: <out>.meta.json)")
		source    = fs.String("source", "", "data provenance: real-capture | synthetic | hand-authored")
		status    = fs.String("status", "", "lifecycle state: raw-private | redacted | synthetic-derivative | public-ci")
		intent    = fs.String("intent", "", "what adapter behavior this fixture guards")
		providers = fs.String("providers", "", "comma-separated provider/surface shapes (e.g. anthropic/messages)")
		live      = fs.Bool("live", false, "whether a live API call can reproduce this capture")
		noRedact  = fs.Bool("no-redact", false, "skip the AS-115 redaction pass (normalize only)")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *out == "" && *metaPath == "" {
		return fmt.Errorf("writing the fixture to stdout requires -meta for the sidecar metadata")
	}

	meta := capturefixture.Metadata{
		Source:           *source,
		RedactionStatus:  *status,
		Intent:           *intent,
		Providers:        splitCSV(*providers),
		LiveReproducible: *live,
	}
	if err := meta.Validate(); err != nil {
		return err
	}

	blocks, err := readBlocks(*in, stdin)
	if err != nil {
		return err
	}

	var redactor *redaction.Redactor
	if !*noRedact {
		redactor = redaction.Default()
	}
	processed, stats, verr := capturefixture.Process(blocks, redactor)
	meta.Stats = stats

	fmt.Fprintf(stderr, "blocks=%d redaction-spans=%d normalized=%v\n", stats.Blocks, stats.RedactionSpans, stats.Normalized) //nolint:errcheck // progress on stderr
	if len(verr) > 0 {
		for _, e := range verr {
			fmt.Fprintln(stderr, "  validation:", e) //nolint:errcheck // progress on stderr
		}
		return fmt.Errorf("%d block(s) failed schema validation; fixture not written", len(verr))
	}

	if err := writeFixture(*out, stdout, processed); err != nil {
		return err
	}
	mp := *metaPath
	if mp == "" {
		mp = *out + ".meta.json"
	}
	mb, err := meta.Marshal()
	if err != nil {
		return err
	}
	if err := os.WriteFile(mp, append(mb, '\n'), 0o644); err != nil {
		return fmt.Errorf("write metadata %s: %w", mp, err)
	}
	fmt.Fprintf(stderr, "wrote fixture metadata %s\n", mp) //nolint:errcheck // progress on stderr
	return nil
}

// isBlank reports whether b is empty or only ASCII whitespace.
func isBlank(b []byte) bool {
	for _, c := range b {
		if c != ' ' && c != '\t' && c != '\r' && c != '\n' {
			return false
		}
	}
	return true
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// readBlocks parses a JSONL capture (one schema.Block per line). Blank lines are
// skipped; a malformed line aborts with its line number so the contributor can
// fix the source.
func readBlocks(path string, stdin io.Reader) ([]schema.Block, error) {
	src := stdin
	if path != "-" {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close() //nolint:errcheck // read-only handle
		src = f
	}
	var blocks []schema.Block
	sc := bufio.NewScanner(src)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	line := 0
	for sc.Scan() {
		line++
		// Read the raw bytes (no per-line string allocation); json.Unmarshal ignores
		// surrounding whitespace, so only blank lines need skipping.
		raw := sc.Bytes()
		if isBlank(raw) {
			continue
		}
		var b schema.Block
		if err := json.Unmarshal(raw, &b); err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		blocks = append(blocks, b)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(blocks) == 0 {
		return nil, fmt.Errorf("no blocks read from %s", path)
	}
	return blocks, nil
}

// writeFixture emits the processed blocks as JSONL, to a file or to stdout. When
// writing a file, the handle is closed explicitly so a flush/close error (e.g. a
// full disk) is reported rather than dropped.
func writeFixture(path string, stdout io.Writer, blocks []schema.Block) (err error) {
	w := stdout
	if path != "" {
		f, ferr := os.Create(path)
		if ferr != nil {
			return ferr
		}
		defer func() {
			if cerr := f.Close(); cerr != nil && err == nil {
				err = cerr
			}
		}()
		w = f
	}
	bw := bufio.NewWriter(w)
	enc := json.NewEncoder(bw)
	for _, b := range blocks {
		if err := enc.Encode(b); err != nil {
			return err
		}
	}
	return bw.Flush()
}
