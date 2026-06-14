package tool

import (
	"context"
	"encoding/json"
)

// Decision is the outcome of a permission check. Allow gates execution; Reason
// is surfaced to the model in the error result when a call is denied, so the
// model learns why and can adjust.
type Decision struct {
	Allow  bool
	Reason string
}

// Allowed is the permit decision.
func Allowed() Decision { return Decision{Allow: true} }

// Denied is the refuse decision carrying a model-facing reason.
func Denied(reason string) Decision { return Decision{Allow: false, Reason: reason} }

// PermissionFunc is the hook the Runtime invokes before every tool execution,
// after the call's arguments have validated. It decides whether the call may
// proceed; the security model (AS-016) supplies the real ask/allowlist/auto
// policy. A nil PermissionFunc defaults to AllowAll, so the runtime is usable on
// its own before AS-016 lands — but a Runtime always routes through some
// PermissionFunc, so the gate is never bypassed.
//
// call describes the tool being invoked (name, validated arguments, tool_use
// id). Implementations must be safe for concurrent use: parallel tool calls
// (AS-019) check permission concurrently.
type PermissionFunc func(ctx context.Context, call Call) Decision

// Call is the in-flight tool invocation handed to the permission hook and used
// internally by the Runtime. Arguments is the model's arguments object, already
// validated against the tool's schema.
type Call struct {
	ToolUseID string
	Name      string
	Arguments json.RawMessage
}

// AllowAll permits every call. It is the Runtime's default when no policy is
// configured and a convenient explicit choice in tests.
func AllowAll(context.Context, Call) Decision { return Allowed() }
