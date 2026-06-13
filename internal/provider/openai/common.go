package openai

import "strings"

// copyInt returns a pointer to a copy of *p, or nil when p is nil, so a
// normalized event never aliases the decoded frame.
func copyInt(p *int) *int {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// firstNonEmpty returns the first non-empty string of its arguments, or "".
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// isServerToolCall reports whether a Responses output-item type is a server-side
// tool call (the provider runs it), e.g. web_search_call, file_search_call,
// code_interpreter_call, computer_call, image_generation_call, mcp_call. The
// "_call" suffix is the stable marker the union (§6.2) relies on.
func isServerToolCall(itemType string) bool {
	return strings.HasSuffix(itemType, "_call")
}

// serverToolName derives a server tool's name from its item type by trimming the
// "_call" suffix (web_search_call -> web_search), so the normalized tool name is
// the tool, not the call envelope.
func serverToolName(itemType string) string {
	return strings.TrimSuffix(itemType, "_call")
}
