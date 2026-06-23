package tool

import "context"

// callCtxKey is the unexported context key under which the runtime stashes the
// executing call's tool_use id, so a tool can correlate a side effect (a file
// snapshot for /rewind --restore-files, AS-084) to the exact call on the event
// log without threading the id through every tool's Run signature.
type callCtxKey struct{}

// ContextWithToolUseID returns ctx carrying the executing call's tool_use id.
// The runtime sets it just before invoking a tool.
func ContextWithToolUseID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, callCtxKey{}, id)
}

// ToolUseID returns the executing call's tool_use id, or "" when none is set
// (a tool invoked outside the runtime, e.g. a unit test).
func ToolUseID(ctx context.Context) string {
	id, _ := ctx.Value(callCtxKey{}).(string)
	return id
}
