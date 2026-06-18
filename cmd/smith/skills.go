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

// registerSkillTool registers a single "skill" tool the model invokes by name to
// load a skill's instructions into the context, built over the given snapshot of
// portable skills (AS-034). An empty snapshot offers no tool, so a project
// without skills sends the model nothing extra. The caller scans skills once at
// startup and reuses that snapshot for both the tool and seedSkills, so the
// offered catalog and the recorded skill_load events can never diverge.
func registerSkillTool(reg *tool.Registry, skills []skill.Skill) error {
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

// seedSkills reconciles the session's skill_load events with the process's skill
// snapshot (AS-034): it appends a skill-load event for every snapshot skill the
// log does not already record, giving the living-skills analyzers (AS-047) a
// stable hook for what was loaded. These are control events that never enter
// model-facing context — a skill's instructions arrive only when the model
// invokes it. Because the snapshot also builds the skill tool, every offered
// skill ends up with a load event and vice versa, on a fresh, cleared, or
// resumed session alike. Reconciling (rather than blindly appending) keeps a
// resumed session from duplicating the events it was created with.
func seedSkills(sess *session.Session, skills []skill.Skill) error {
	logged := map[string]bool{}
	for _, b := range sess.Log.Events() {
		if b.Kind == eventlog.KindSkillLoad && b.Attribution != nil {
			logged[b.Attribution.Skill] = true
		}
	}
	// Deterministic order on the log regardless of discovery order.
	ordered := append([]skill.Skill(nil), skills...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Name < ordered[j].Name })
	for _, s := range ordered {
		if logged[s.Name] {
			continue
		}
		if _, err := sess.Log.Append(eventlog.NewSkillLoad(skillProducer, s.Name)); err != nil {
			return fmt.Errorf("append skill-load event: %w", err)
		}
	}
	return nil
}
