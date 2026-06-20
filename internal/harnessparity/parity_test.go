// Package harnessparity holds the CI/local parity guard. It has no production
// code; it exists only to fail the build when the GitHub Actions quality jobs
// drift away from the local harness command mapping documented in
// docs/agent-quality-gates.md (AS-103).
//
// The guard is deterministic and offline: it reads two tracked files in the
// repository and compares the set of `make` commands CI runs against the set the
// parity table promises a local developer can run. Whenever CI gains or removes
// a `make` quality step, this test fails until the parity table is updated in
// the same change. See the "CI/local parity" section of
// docs/agent-quality-gates.md for how to update the mapping.
package harnessparity

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// makeCmd matches a `make <target>` invocation anywhere in a line.
var makeCmd = regexp.MustCompile(`make\s+([a-z][a-z0-9-]*)`)

// TestCIMatchesParityTable fails when the set of `make` commands run by the CI
// quality workflow differs from the set documented in the parity table. This is
// the guard that keeps CI and the local harness honest: adding or removing a CI
// quality job without updating docs/agent-quality-gates.md breaks the build.
func TestCIMatchesParityTable(t *testing.T) {
	root := moduleRoot(t)

	ci := makeTargetsInCIRunSteps(t, filepath.Join(root, ".github", "workflows", "ci.yml"))
	doc := makeTargetsInParityTable(t, filepath.Join(root, "docs", "agent-quality-gates.md"))

	if !equalSets(ci, doc) {
		t.Fatalf("CI/local parity drift between .github/workflows/ci.yml and docs/agent-quality-gates.md\n"+
			"  make targets in CI run steps: %v\n"+
			"  make targets in parity table: %v\n"+
			"Update the CI/local parity table in docs/agent-quality-gates.md (and the harness scripts) in the same change.",
			sortedKeys(ci), sortedKeys(doc))
	}
}

// makeTargetsInCIRunSteps returns the set of make targets invoked by `run:`
// steps in the CI workflow. It stays stdlib-only (no YAML dependency) by
// scanning `run:` lines, which is sufficient for the flat command steps this
// workflow uses.
func makeTargetsInCIRunSteps(t *testing.T, path string) map[string]struct{} {
	t.Helper()
	targets := map[string]struct{}{}
	const runPrefix = "run:"
	inBlock := false // inside a multi-line `run: |` / `run: >` block
	blockIndent := 0 // indentation of the `run:` key that opened the block
	for _, line := range readLines(t, path) {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		if inBlock {
			// The block ends when indentation returns to the run key or less.
			if indent <= blockIndent {
				inBlock = false
			} else {
				addMakeTargets(targets, trimmed)
				continue
			}
		}
		if !strings.HasPrefix(trimmed, runPrefix) {
			continue
		}
		cmd := strings.TrimSpace(strings.TrimPrefix(trimmed, runPrefix))
		switch cmd {
		case "|", "|-", "|+", ">", ">-", ">+":
			inBlock = true
			blockIndent = indent
		default:
			addMakeTargets(targets, cmd)
		}
	}
	if len(targets) == 0 {
		t.Fatalf("no `run: make ...` steps found in %s; the parser or workflow shape changed", path)
	}
	return targets
}

// makeTargetsInParityTable returns the set of make targets named in the "Local
// command" column of the CI/local parity table.
func makeTargetsInParityTable(t *testing.T, path string) map[string]struct{} {
	t.Helper()
	lines := readLines(t, path)
	targets := map[string]struct{}{}
	inSection := false
	localCol := -1 // column index of "Local command", discovered from the header
	for _, line := range lines {
		if strings.Contains(line, "CI/local parity") {
			inSection = true
			continue
		}
		if !inSection {
			continue
		}
		// A new heading ends the parity section.
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			break
		}
		if !strings.HasPrefix(strings.TrimSpace(line), "|") {
			continue
		}
		cols := strings.Split(line, "|")
		if localCol == -1 {
			// First table row is the header; locate the column by name so
			// reordering or adding columns can't silently shift the parse.
			for i, col := range cols {
				if strings.Contains(col, "Local command") {
					localCol = i
					break
				}
			}
			continue
		}
		if localCol >= len(cols) || strings.Contains(cols[localCol], "---") {
			continue // ragged row or the header/body separator
		}
		addMakeTargets(targets, cols[localCol])
	}
	if !inSection {
		t.Fatalf("CI/local parity section not found in %s", path)
	}
	if len(targets) == 0 {
		t.Fatalf("no `make` targets found in the CI/local parity table of %s", path)
	}
	return targets
}

// addMakeTargets records every `make <target>` found in s into the set.
func addMakeTargets(targets map[string]struct{}, s string) {
	for _, m := range makeCmd.FindAllStringSubmatch(s, -1) {
		targets[m[1]] = struct{}{}
	}
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return strings.Split(string(data), "\n")
}

func equalSets(a, b map[string]struct{}) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			return false
		}
	}
	return true
}

func sortedKeys(m map[string]struct{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// moduleRoot walks up from the test's working directory to the module root
// (the directory containing go.mod).
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("module root (go.mod) not found above the test directory")
		}
		dir = parent
	}
}
