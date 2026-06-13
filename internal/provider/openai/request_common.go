package openai

import (
	"encoding/json"
	"fmt"

	"github.com/tonitienda/agent-smith/internal/provider"
	"github.com/tonitienda/agent-smith/schema"
)

// argumentsString returns a tool call's arguments as the verbatim JSON string
// OpenAI expects, preferring ArgumentsRaw (exact bytes), then the structured
// Arguments, and falling back to an empty object.
func argumentsString(body *schema.ToolCallBody) string {
	switch {
	case body.ArgumentsRaw != "":
		return body.ArgumentsRaw
	case len(body.Arguments) > 0:
		return string(body.Arguments)
	default:
		return "{}"
	}
}

// toolResultText flattens a tool_result body into the single output string the
// OpenAI surfaces accept. It prefers structured text parts, then stdout/stderr,
// matching the anthropic adapter's content handling.
func toolResultText(body *schema.ToolResultBody) string {
	var out string
	for i := range body.Content {
		p := &body.Content[i]
		if p.Type == "text" || p.Text != "" {
			out += p.Text
		}
	}
	if out == "" {
		out = body.Stdout + body.Stderr
	}
	return out
}

// toolParameters returns a tool definition's JSON Schema, defaulting an empty
// schema to an open object schema (which the API requires) and rejecting invalid
// JSON.
func toolParameters(d *provider.ToolDef) (json.RawMessage, error) {
	params := d.InputSchema
	if len(params) == 0 {
		return json.RawMessage(`{"type":"object"}`), nil
	}
	if !json.Valid(params) {
		return nil, fmt.Errorf("tool %q has invalid input schema", d.Name)
	}
	return params, nil
}
