// Command schema-guard enforces and maintains the additive-only schema promise
// (PRD D2) for the content-block schema (package schema, AS-003).
//
// Usage:
//
//	go run ./cmd/schema-guard            # check: fail if the schema changed non-additively
//	go run ./cmd/schema-guard -update    # regenerate the baseline + generated golden corpus
//
// The check mode is what CI relies on (the same comparison also runs as a unit
// test via `go test ./...`). The -update mode is for contributors who have made
// an intentional *additive* change and need to record the new fields/types/enum
// values in the committed baseline. It never lets a breaking change through:
// -update refuses to write if the new descriptor drops anything in the baseline.
//
// See docs/schema/EVOLUTION.md for the full process.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tonitienda/agent-smith/internal/schemaguard"
	"github.com/tonitienda/agent-smith/schema"
)

func main() {
	update := flag.Bool("update", false, "regenerate the committed baseline and generated golden corpus")
	flag.Parse()

	if err := run(*update); err != nil {
		fmt.Fprintln(os.Stderr, "schema-guard: "+err.Error())
		os.Exit(1)
	}
}

func run(update bool) error {
	current := schemaguard.Generate()

	if update {
		return doUpdate(current)
	}
	return doCheck(current)
}

// doCheck compares the current schema against the committed baseline and parses
// every golden session, returning an error (non-zero exit) on any breaking
// change or unparseable golden.
func doCheck(current schemaguard.Descriptor) error {
	baseline, err := readBaseline()
	if err != nil {
		return err
	}
	if breaks := schemaguard.Compare(baseline, current); len(breaks) > 0 {
		fmt.Fprintf(os.Stderr, "BREAKING schema change(s) detected (the schema is additive-only from V1, PRD D2):\n  - %s\n\nRemovals, renames, type changes, and repurposing are forbidden. If your change is purely additive, run `go run ./cmd/schema-guard -update` to record it; see docs/schema/EVOLUTION.md.\n",
			strings.Join(breaks, "\n  - "))
		return errors.New("schema is not additive-only against the committed baseline")
	}
	if err := checkGoldens(); err != nil {
		return err
	}
	fmt.Println("schema-guard: OK — schema is additive-only against the baseline; golden corpus parses.")
	return nil
}

// doUpdate writes the baseline and generated goldens, after refusing any change
// that would break the existing baseline (additions only).
func doUpdate(current schemaguard.Descriptor) error {
	if baseline, err := readBaseline(); err == nil {
		if breaks := schemaguard.Compare(baseline, current); len(breaks) > 0 {
			fmt.Fprintf(os.Stderr, "refusing to -update: the change is not additive:\n  - %s\n\nThe schema is additive-only from V1 (PRD D2). Revert the removal/rename/type change instead of regenerating the baseline.\n",
				strings.Join(breaks, "\n  - "))
			return errors.New("refusing to -update: change is not additive")
		}
	} // a missing baseline is fine: first run creates it.

	if err := writeJSON(schemaguard.BaselineFile, current); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", schemaguard.BaselineFile)

	for name, doc := range schemaguard.GeneratedGoldens() {
		path := filepath.Join(schemaguard.GoldenDir, name)
		if err := writeJSON(path, doc); err != nil {
			return err
		}
		fmt.Printf("wrote %s\n", path)
	}
	return nil
}

// checkGoldens parses every committed golden session, validating each block and
// confirming a parse → re-emit → parse cycle preserves semantics.
func checkGoldens() error {
	files, err := filepath.Glob(filepath.Join(schemaguard.GoldenDir, "*.json"))
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no golden session files found in %s (run from the repo root)", schemaguard.GoldenDir)
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		var doc schema.Document
		if err := json.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("golden %s no longer parses: %w", f, err)
		}
		for _, b := range doc.Blocks {
			if err := b.Validate(); err != nil {
				return fmt.Errorf("golden %s block %s no longer validates: %w", f, b.ID, err)
			}
		}
	}
	return nil
}

func readBaseline() (schemaguard.Descriptor, error) {
	var d schemaguard.Descriptor
	data, err := os.ReadFile(schemaguard.BaselineFile)
	if err != nil {
		return d, fmt.Errorf("reading baseline %s: %w", schemaguard.BaselineFile, err)
	}
	if err := json.Unmarshal(data, &d); err != nil {
		return d, fmt.Errorf("parsing baseline %s: %w", schemaguard.BaselineFile, err)
	}
	return d, nil
}

// writeJSON marshals v as indented JSON with a trailing newline, creating parent
// directories as needed.
func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}
