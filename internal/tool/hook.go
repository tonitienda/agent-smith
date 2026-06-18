package tool

import (
	"context"
	"encoding/json"

	"github.com/tonitienda/agent-smith/schema"
)

// PreToolHook is the seam the Runtime calls after the permission gate passes and
// before a tool executes (AS-035). It is automation layered on top of the
// security boundary: permissions (AS-016) decide whether a call may run at all,
// then a pre-tool hook may still block the call or rewrite its arguments. A nil
// hook is skipped, so the Runtime is usable without one.
//
// call carries the tool name, tool_use id, and the validated arguments. The
// returned PreToolResult either lets the call proceed (the zero value), blocks
// it with a model-facing reason, or supplies replacement arguments that the
// Runtime re-validates and records on the log with hook provenance before
// running the tool.
type PreToolHook func(ctx context.Context, call Call) PreToolResult

// PreToolResult is a pre-tool hook's verdict. The zero value allows the call
// unchanged.
type PreToolResult struct {
	// Block stops the call; Reason is fed back to the model as the error result.
	Block  bool
	Reason string
	// Modified, when non-nil, replaces the call's arguments. The Runtime validates
	// it against the tool's schema and records the rewrite on the log as a derived
	// tool_call (provenance: the hook) before executing with the new arguments.
	Modified json.RawMessage
}

// PostToolHook is the seam the Runtime calls after a tool's result is recorded on
// the log (AS-035). It is observational: the result already exists, so the hook
// cannot change it — it exists to let automation react (notify, lint, annotate
// out of band). A nil hook is skipped.
type PostToolHook func(ctx context.Context, call Call, result *schema.ToolResultBody)
