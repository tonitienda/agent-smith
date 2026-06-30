// Package orchestrator is the always-on Smith orchestrator (AS-161): the
// deterministic daemon that loads `.agent-smith/jobs/*.yaml` specs, turns cron /
// manual / GitHub triggers into queued runs, and supervises those runs under
// concurrency, timeout, retry, and budget policy. It owns the *deterministic
// shell*; the cognitive work of a run is delegated to an [Executor] (AS-147/149/
// 150/151 wire the real one), and every run's narrative stays in a normal Smith
// session log (ADR D-ORCH-4). Run-control state lives in the SQLite store
// (internal/orchestrator/store).
//
// This package sits at the orchestration tier alongside internal/loop and the
// faces; it depends inward on the job-spec model (internal/orchestrator/spec) and
// the run store, and is never imported by inward-core packages (archtest-guarded,
// ADR D-ORCH-3).
package orchestrator

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tonitienda/agent-smith/internal/orchestrator/spec"
	"gopkg.in/yaml.v3"
)

// JobsDir is the conventional, repo-committed location of job specs.
const JobsDir = ".agent-smith/jobs"

// LoadJobs reads every `*.yaml` / `*.yml` file under fsys (rooted at the jobs
// directory), validates each against the spec format, and enforces cross-file id
// uniqueness. It returns the specs that loaded cleanly and a combined error naming
// every rejection (fail-closed: a malformed spec never schedules). A clean load
// returns a nil error even if some files were rejected only when len(specs)>0 and
// no errors — callers should treat a non-nil error as "do not trust this set".
func LoadJobs(fsys fs.FS) ([]*spec.Spec, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("orchestrator: read jobs dir: %w", err)
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if ext := strings.ToLower(filepath.Ext(e.Name())); ext == ".yaml" || ext == ".yml" {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	var specs []*spec.Spec
	var errs []string
	for _, name := range files {
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: read: %v", name, err))
			continue
		}
		var raw map[string]any
		if err := yaml.Unmarshal(data, &raw); err != nil {
			errs = append(errs, fmt.Sprintf("%s: parse yaml: %v", name, err))
			continue
		}
		s, verrs := spec.Load(name, raw)
		if len(verrs) > 0 {
			errs = append(errs, verrs.Error())
			continue
		}
		specs = append(specs, s)
	}
	if u := spec.CheckUnique(specs); len(u) > 0 {
		errs = append(errs, u.Error())
		// Drop duplicates so the returned set is internally consistent even when the
		// caller chooses to proceed on a non-fatal subset.
		specs = dedupeByID(specs)
	}
	if len(errs) > 0 {
		return specs, fmt.Errorf("orchestrator: %d job spec(s) rejected:\n%s", len(errs), strings.Join(errs, "\n"))
	}
	return specs, nil
}

func dedupeByID(specs []*spec.Spec) []*spec.Spec {
	seen := map[string]bool{}
	var out []*spec.Spec
	for _, s := range specs {
		if seen[s.ID] {
			continue
		}
		seen[s.ID] = true
		out = append(out, s)
	}
	return out
}
