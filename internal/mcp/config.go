package mcp

import (
	"fmt"
	"sort"
	"time"

	"github.com/tonitienda/agent-smith/internal/config"
)

// rawServer is the JSON shape of one `mcp.servers.<name>` entry: command/args/env
// for a stdio server, or url/headers for an HTTP/SSE server, plus an optional
// per-call timeout as a Go duration string.
type rawServer struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Timeout string            `json:"timeout"`
}

// Parse reads the `mcp.servers` config section into ServerConfig specs, sorted by
// name for a deterministic connect order. A transport-ambiguous entry (neither
// command nor url, or both set) is skipped with a warning; an entry with a
// malformed timeout is kept (the default timeout applies) with a warning, since
// the server is otherwise usable. Nothing fails the session — one broken server
// must not block the others (the §7.4 isolation ethos, and config's
// tolerate-but-warn rule, D2). Returns no specs when the section is absent.
func Parse(cfg *config.Config) (specs []ServerConfig, warnings []string, err error) {
	var servers map[string]rawServer
	ok, err := cfg.Decode("mcp.servers", &servers)
	if err != nil {
		return nil, nil, fmt.Errorf("mcp: parse config: %w", err)
	}
	if !ok || len(servers) == 0 {
		return nil, nil, nil
	}

	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		raw := servers[name]
		switch {
		case raw.URL != "" && raw.Command != "":
			warnings = append(warnings, fmt.Sprintf("mcp server %q sets both command and url; skipping", name))
			continue
		case raw.URL == "" && raw.Command == "":
			warnings = append(warnings, fmt.Sprintf("mcp server %q sets neither command nor url; skipping", name))
			continue
		}
		spec := ServerConfig{
			Name:    name,
			Command: raw.Command,
			Args:    raw.Args,
			Env:     raw.Env,
			URL:     raw.URL,
			Headers: raw.Headers,
		}
		if raw.Timeout != "" {
			d, derr := time.ParseDuration(raw.Timeout)
			if derr != nil {
				warnings = append(warnings, fmt.Sprintf("mcp server %q has invalid timeout %q; using default", name, raw.Timeout))
			} else {
				spec.Timeout = d
			}
		}
		specs = append(specs, spec)
	}
	return specs, warnings, nil
}
