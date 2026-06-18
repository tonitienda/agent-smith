package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/internal/session"
	"github.com/tonitienda/agent-smith/internal/skill"
	"github.com/tonitienda/agent-smith/internal/tool"
	"github.com/tonitienda/agent-smith/schema"
)

// skillProducer attributes skill-load events on the log.
const skillProducer = "skill-loader"

// skillToolInput is the argument schema for the skill tool: the name of the
// skill to load.
const skillToolInput = `{
  "type": "object",
  "properties": {
    "name": {"type": "string", "description": "Name of the skill to load."}
  },
  "required": ["name"],
  "additionalProperties": false
}`

// registerSkillTool discovers portable skills (AS-034) under wd and the user
// config dir and, when any exist, registers a single "skill" tool the model
// invokes by name to load a skill's instructions into the context. No skills
// means no tool is offered, so a project without skills sends the model nothing
// extra. A discovery error is surfaced so a broken setup fails loudly.
func registerSkillTool(reg *tool.Registry, wd string) error {
	skills, err := skill.Load(skill.UserDir(), skill.ProjectDir(wd))
	if err != nil {
		return fmt.Errorf("load skills: %w", err)
	}
	if len(skills) == 0 {
		return nil
	}
	if err := reg.Register(skillTool(skills)); err != nil {
		return fmt.Errorf("register skill tool: %w", err)
	}
	return nil
}

// skillTool builds the model-facing "skill" tool over the discovered skills: its
// description lists the available skills so the model knows when to invoke one,
// and Run returns the named skill's instruction body attributed to that skill so
// /context credits it (AS-034). An unknown name is a model-readable error, not an
// infrastructure failure, so the loop can let the model correct itself.
func skillTool(skills []skill.Skill) tool.Tool {
	byName := make(map[string]skill.Skill, len(skills))
	for _, s := range skills {
		byName[s.Name] = s
	}
	def := tool.Def{
		Name:        "skill",
		Description: skillToolDescription(skills),
		InputSchema: json.RawMessage(skillToolInput),
	}
	return tool.Func{
		Spec: def,
		Fn: func(_ context.Context, args json.RawMessage) (tool.Output, error) {
			var in struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return tool.Output{Text: fmt.Sprintf("invalid skill arguments: %v", err), IsError: true}, nil
			}
			s, ok := byName[in.Name]
			if !ok {
				return tool.Output{Text: fmt.Sprintf("unknown skill %q", in.Name), IsError: true}, nil
			}
			return tool.Output{
				Text:        s.Body,
				Attribution: &schema.Attribution{Skill: s.Name},
			}, nil
		},
	}
}

// skillToolDescription renders the tool's model-facing description: a one-line
// purpose plus the catalog of available skills so the model can match a request
// to a skill and invoke it by name.
func skillToolDescription(skills []skill.Skill) string {
	var b strings.Builder
	b.WriteString("Load a portable skill's instructions into the conversation when a request matches it. Available skills:")
	for _, s := range skills {
		b.WriteString("\n- ")
		b.WriteString(s.Name)
		if s.Description != "" {
			b.WriteString(": ")
			b.WriteString(s.Description)
		}
	}
	return b.String()
}

// seedSkills records a skill-load event for every portable skill available to
// the session (AS-034), giving the living-skills analyzers (AS-047) a stable hook
// for what was loaded. These are control events that never enter model-facing
// context — a skill's instructions arrive only when the model invokes it. Called
// for fresh sessions only, like seedMemory; a resumed session already carries
// the load events it was created with.
func seedSkills(wd string, sess *session.Session) error {
	skills, err := skill.Load(skill.UserDir(), skill.ProjectDir(wd))
	if err != nil {
		return fmt.Errorf("load skills: %w", err)
	}
	// Deterministic order on the log regardless of discovery order.
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	for _, s := range skills {
		if _, err := sess.Log.Append(eventlog.NewSkillLoad(skillProducer, s.Name)); err != nil {
			return fmt.Errorf("append skill-load event: %w", err)
		}
	}
	return nil
}
