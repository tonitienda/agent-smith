package main

import (
	"io"

	"github.com/tonitienda/agent-smith/internal/delegate"
	"github.com/tonitienda/agent-smith/internal/mcp"
	"github.com/tonitienda/agent-smith/internal/session"
	"github.com/tonitienda/agent-smith/internal/skill"
	"github.com/tonitienda/agent-smith/internal/tool"
)

// taskSpawner builds the user-delegated subagent spawner (AS-046/AS-119) shared by
// every face — interactive, headless, and serve. parent supplies the live
// parent-session context (log, ids, permission gate, router) at spawn time, so a
// child inherits the parent's gate per that face's policy: forwarded to the TUI or
// serve client, fail-fast denied under the headless allowlist-then-deny posture.
// The child's tool set is built fresh on each spawn by childTools.
func taskSpawner(store *session.Store, wd string, skills []skill.Skill, mcpClients []*mcp.Client, parent func() delegate.Parent) *delegate.Spawner {
	return delegate.New(store, appRuntime.Providers(),
		func() (*tool.Registry, error) { return childTools(wd, skills, mcpClients) },
		parent)
}

// childTools builds a delegated child's tool registry (AS-119): the builtin
// file/search/shell set plus the parent's skills (AS-034) and live MCP tools
// (AS-036) when the face has them — borrowed over the parent's clients, never
// re-dialled or closed by the child — and never the `task` tool, so delegation
// does not recurse. Faces without skills/MCP pass nil and get the builtin set.
func childTools(wd string, skills []skill.Skill, mcpClients []*mcp.Client) (*tool.Registry, error) {
	reg, err := appRuntime.BuiltinTools(wd)
	if err != nil {
		return nil, err
	}
	if err := registerSkillTool(reg, skills); err != nil {
		return nil, err
	}
	// The parent already warned about any tool-name collisions at startup; discard
	// duplicate warnings on every spawn.
	registerMCPTools(reg, mcpClients, io.Discard)
	return reg, nil
}
