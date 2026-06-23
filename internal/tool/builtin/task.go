package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tonitienda/agent-smith/internal/tool"
	"github.com/tonitienda/agent-smith/schema"
)

// TaskRequest is one delegation: the prompt the child agent runs, plus optional
// hints. AgentType names a preset role for the child (advisory in V1; the
// composition root may map it to a system prompt or skill set later); Model
// overrides the child's model, otherwise the spawner picks the cheap routing
// tier so fan-out stays inexpensive (AS-046, PRD §7.17).
type TaskRequest struct {
	Prompt    string
	AgentType string
	Model     string
}

// TaskResult is what a delegation returns to the parent: the child's final
// summary text and the ID of the persisted child session, so the parent can link
// to, inspect, or resume the delegated run.
type TaskResult struct {
	Summary   string
	SessionID string
}

// Spawner runs a delegated child agent and returns its summary. It is the
// consumer seam (AS-091) the task tool depends on: the tool stays a pure,
// schema-and-stdlib leaf, and the heavy wiring — building a child session, loop,
// and cost rollup — lives in the composition/orchestration layer that satisfies
// this interface (internal/delegate). Implementations must be safe for
// concurrent use: the runtime may run several task calls at once during a
// parallel-tool turn (AS-019), each spawning its own isolated child.
type Spawner interface {
	Spawn(ctx context.Context, req TaskRequest) (TaskResult, error)
}

// SpawnerFunc adapts a plain function into a Spawner, so the composition root can
// pass a closure over its controller state without a named type.
type SpawnerFunc func(ctx context.Context, req TaskRequest) (TaskResult, error)

// Spawn calls the wrapped function.
func (f SpawnerFunc) Spawn(ctx context.Context, req TaskRequest) (TaskResult, error) {
	return f(ctx, req)
}

// taskInputSchema is the model-facing argument contract for the task tool.
const taskInputSchema = `{
  "type": "object",
  "properties": {
    "prompt": {
      "type": "string",
      "description": "The task for the delegated agent to perform. It runs in its own isolated context window and returns a summary; pass everything it needs, since it cannot see this conversation."
    },
    "agent_type": {
      "type": "string",
      "description": "Optional preset role for the child agent (advisory)."
    },
    "model": {
      "type": "string",
      "description": "Optional model override for the child; defaults to the cheap routing tier."
    }
  },
  "required": ["prompt"]
}`

// taskTool delegates a scoped task to a child agent (AS-046, PRD §7.17). The
// child runs its own loop over its own event log — the parent's context is not
// consumed by the child's work — and its final answer is summarized back into the
// parent as the tool result, attributed to the child session.
type taskTool struct {
	spawner Spawner
}

// NewTask builds the user-delegated subagent tool over a Spawner. The
// composition root registers it on the parent's tool registry; the child's
// registry deliberately omits it, so delegation does not recurse.
func NewTask(spawner Spawner) tool.Tool {
	return taskTool{spawner: spawner}
}

// Def returns the task tool's definition.
func (t taskTool) Def() tool.Def {
	return tool.Def{
		Name: "task",
		Description: "Delegate a scoped task to a child agent that runs in its own isolated context window " +
			"and returns a summary. Use it to fan work out (the child cannot see this conversation, so " +
			"give it everything it needs) and to keep large, self-contained subtasks out of the main context.",
		InputSchema: json.RawMessage(taskInputSchema),
	}
}

// Run parses the delegation arguments, runs the child via the Spawner, and
// returns its summary as the tool result. A blank prompt or a spawner failure is
// reported as a model-readable error result (IsError) rather than an
// infrastructure error, so the loop feeds it back and continues.
func (t taskTool) Run(ctx context.Context, args json.RawMessage) (tool.Output, error) {
	var in struct {
		Prompt    string `json:"prompt"`
		AgentType string `json:"agent_type"`
		Model     string `json:"model"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return tool.Output{Text: fmt.Sprintf("invalid task arguments: %v", err), IsError: true}, nil
	}
	if strings.TrimSpace(in.Prompt) == "" {
		return tool.Output{Text: "task requires a non-empty prompt", IsError: true}, nil
	}
	res, err := t.spawner.Spawn(ctx, TaskRequest{Prompt: in.Prompt, AgentType: in.AgentType, Model: in.Model})
	if err != nil {
		// A cancelled parent turn is an infrastructure failure the loop must see;
		// any other delegation error is reported to the model so it can react.
		if ctx.Err() != nil {
			return tool.Output{}, err
		}
		return tool.Output{Text: fmt.Sprintf("task delegation failed: %v", err), IsError: true}, nil
	}
	summary := strings.TrimSpace(res.Summary)
	if summary == "" {
		summary = "(the delegated agent returned no summary)"
	}
	text := summary
	if res.SessionID != "" {
		text = fmt.Sprintf("%s\n\n[delegated session: %s]", summary, res.SessionID)
	}
	return tool.Output{
		Text:        text,
		Attribution: &schema.Attribution{Tool: "task"},
	}, nil
}
