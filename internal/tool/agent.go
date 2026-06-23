package tool

import "context"

// agentKey carries the identity of a delegated agent through a tool call's
// context, so a permission gate can attribute a prompt to the child that raised
// it (AS-120). It lives in the tool package because both the delegation layer
// (which sets it) and the permission policy (which reads it) already depend on
// tool, so threading identity this way adds no new package edge.
type agentKey struct{}

// WithAgent tags ctx as belonging to the delegated agent id. A delegation wraps
// its child's permission gate with this so the parent's approval prompt can name
// the child (AS-046/AS-120). An empty id is a no-op.
func WithAgent(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, agentKey{}, id)
}

// AgentFromContext returns the delegated-agent id tagged on ctx, or "" when the
// call is the main (non-delegated) agent's own.
func AgentFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(agentKey{}).(string)
	return id
}
