package main

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/tonitienda/agent-smith/internal/config"
	"github.com/tonitienda/agent-smith/internal/factdetector"
	"github.com/tonitienda/agent-smith/internal/memory"
	"github.com/tonitienda/agent-smith/internal/session"
	"github.com/tonitienda/agent-smith/internal/skill"
	"github.com/tonitienda/agent-smith/internal/subagent"
)

// buildSubAgents constructs the sub-agent registry every face installs (AS-107):
// it registers the built-in system sub-agents (AS-044) and applies the
// `subagents.<name>` config overlay (Appendix C.3), then returns the registry and
// the in-memory insights store findings record into — the seam /insights (AS-045)
// will read. A config entry naming an unknown sub-agent, or an unparsable
// schedule, is surfaced to stderr as a warning rather than failing startup, the
// same tolerate-but-warn ethos as the hook and budget loaders.
//
// factdetector wiring (AS-108): the consumer injects the real save-target
// resolver (memory/skill-aware, keeping internal/factdetector free of those
// imports) and the durable ledger so a dismissed fact stays dismissed across
// sessions and the precision tally survives a restart.
func buildSubAgents(cfg *config.Config, resolve factdetector.Resolve, ledger factdetector.Ledger, stderr io.Writer) (*subagent.Registry, subagent.Store, error) {
	reg := subagent.NewRegistry()
	if err := reg.Register(factdetector.Factory(resolve, ledger)); err != nil {
		return nil, nil, fmt.Errorf("register sub-agent: %w", err)
	}
	// Guard the typed-nil here rather than relying on Load's nil check: a nil
	// *config.Config passed through the configReader interface is a non-nil
	// interface value, so Load would dereference it. With no config the built-ins
	// run on their manifest defaults.
	if cfg != nil {
		warns, err := reg.Load(cfg)
		if err != nil {
			return nil, nil, fmt.Errorf("load sub-agent config: %w", err)
		}
		for _, w := range warns {
			_, _ = fmt.Fprintf(stderr, "warning: %s\n", w)
		}
	}
	return reg, subagent.NewMemStore(), nil
}

// factLedgerName is the per-project durable ledger file, kept alongside the
// project's sessions so the dismissal set and precision tally are shared across
// every session of that project (interactive and headless alike).
const factLedgerName = "fact-ledger.json"

// openFactLedger loads the project's durable fact ledger (AS-108) from the
// session store. A load failure (a corrupted file) degrades to the in-memory
// ledger with a warning rather than aborting the session: a session that can't
// read past dismissals is worse than one that re-offers them once.
func openFactLedger(store *session.Store, stderr io.Writer) factdetector.Ledger {
	path := filepath.Join(store.ProjectSessionsDir(), factLedgerName)
	l, err := factdetector.OpenFileLedger(path)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "warning: fact ledger unavailable, using in-memory: %v\n", err)
		return factdetector.NewMemLedger()
	}
	return l
}

// saveTargetResolver builds the C.1 save-target resolver (AS-108): a fact found
// inside a skill scope proposes saving to that skill's SKILL.md (its
// memory/contract); otherwise the deepest applicable memory file for the files
// involved (or, with no files, those visible from the working directory); with
// the project-root fallback (DefaultTarget) left to the detector when nothing
// resolves. Keeping this in the composition root is what keeps
// internal/factdetector free of memory/skill imports.
func saveTargetResolver(wd string, skills []skill.Skill) factdetector.Resolve {
	bySkill := make(map[string]string, len(skills))
	for _, s := range skills {
		bySkill[s.Name] = s.Source
	}
	return func(skillName string, files []string) string {
		if skillName != "" {
			if src, ok := bySkill[skillName]; ok {
				return src
			}
		}
		return deepestMemoryFile(anchorDir(wd, files))
	}
}

// anchorDir picks the directory the memory search is rooted at: the deepest
// directory among the files involved, or the working directory when none are
// given (the V1 command-fact case — facts carry no files yet, AS-106).
func anchorDir(wd string, files []string) string {
	dir := wd
	for _, f := range files {
		fd := filepath.Dir(f)
		if !filepath.IsAbs(fd) {
			fd = filepath.Join(wd, fd)
		}
		if pathDepth(fd) > pathDepth(dir) {
			dir = fd
		}
	}
	return dir
}

// pathDepth counts a path's directory segments by its separators, so the deepest
// directory is chosen by nesting rather than by string length (a long shallow
// name is not deeper than a short nested one).
func pathDepth(p string) int {
	return strings.Count(p, string(filepath.Separator))
}

// deepestMemoryFile returns the most specific memory file visible from dir, or ""
// when none exists (the detector then uses the project-root fallback). Discover
// orders paths lowest precedence first, so the deepest applicable file is last.
func deepestMemoryFile(dir string) string {
	paths := memory.Discover(memory.UserDir(), dir)
	if len(paths) == 0 {
		return ""
	}
	return paths[len(paths)-1]
}
