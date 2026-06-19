package mcp

import (
	"context"
	"encoding/json"
	"fmt"
)

// maxPages caps cursor following so a server that returns a non-empty cursor
// forever (or repeats one) can't loop the client indefinitely (AS-083). Real
// catalogs page in single digits; this is a safety valve, not a budget.
const maxPages = 10000

// pageThrough drives an MCP cursor-paginated list method. page requests one page
// (with the cursor already injected) and returns the server's nextCursor; the loop
// ends when the cursor is empty or repeats. It is transport-agnostic: callers bind
// it to either the raw transport (handshake-time tools/list) or rpc (resources and
// prompts), so health-checking and circuit-breaking live in one place.
func pageThrough(page func(params map[string]any) (next string, err error)) error {
	cursor := ""
	for i := 0; i < maxPages; i++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		next, err := page(params)
		if err != nil {
			return err
		}
		if next == "" || next == cursor {
			return nil
		}
		cursor = next
	}
	return nil
}

// ResourceInfo is a resource advertised by a server (resources/list). URI is the
// key passed to ReadResource; the rest is descriptive.
type ResourceInfo struct {
	URI         string
	Name        string
	Title       string
	Description string
	MIMEType    string
}

// ListResources returns the server's resource catalog, following pagination. It
// honours the isolation contract (an unhealthy server returns ErrUnavailable).
func (c *Client) ListResources(ctx context.Context) ([]ResourceInfo, error) {
	var out []ResourceInfo
	err := pageThrough(func(params map[string]any) (string, error) {
		raw, err := c.rpc(ctx, "resources/list", params)
		if err != nil {
			return "", err
		}
		var page struct {
			Resources []struct {
				URI         string `json:"uri"`
				Name        string `json:"name"`
				Title       string `json:"title"`
				Description string `json:"description"`
				MIMEType    string `json:"mimeType"`
			} `json:"resources"`
			NextCursor string `json:"nextCursor"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			return "", fmt.Errorf("mcp: decode resources: %w", err)
		}
		for _, r := range page.Resources {
			if r.URI == "" {
				continue
			}
			out = append(out, ResourceInfo{URI: r.URI, Name: r.Name, Title: r.Title, Description: r.Description, MIMEType: r.MIMEType})
		}
		return page.NextCursor, nil
	})
	return out, err
}

// ReadResource fetches a resource by URI and flattens its contents to text. Text
// parts are joined; a binary (blob) part is noted in place so the model knows
// content was elided rather than missing.
func (c *Client) ReadResource(ctx context.Context, uri string) (string, error) {
	raw, err := c.rpc(ctx, "resources/read", map[string]any{"uri": uri})
	if err != nil {
		return "", err
	}
	var out struct {
		Contents []struct {
			URI  string `json:"uri"`
			Text string `json:"text"`
			Blob string `json:"blob"`
		} `json:"contents"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("mcp: decode resource: %w", err)
	}
	var parts []string
	for _, ct := range out.Contents {
		switch {
		case ct.Text != "":
			parts = append(parts, ct.Text)
		case ct.Blob != "":
			parts = append(parts, fmt.Sprintf("[binary resource %s omitted]", ct.URI))
		}
	}
	return joinLines(parts), nil
}

// PromptArgument is one templated argument a prompt declares (prompts/list).
type PromptArgument struct {
	Name        string
	Description string
	Required    bool
}

// PromptInfo is a prompt advertised by a server (prompts/list). Name is the key
// passed to GetPrompt; Arguments declares the template's parameters.
type PromptInfo struct {
	Name        string
	Title       string
	Description string
	Arguments   []PromptArgument
}

// ListPrompts returns the server's prompt catalog, following pagination.
func (c *Client) ListPrompts(ctx context.Context) ([]PromptInfo, error) {
	var out []PromptInfo
	err := pageThrough(func(params map[string]any) (string, error) {
		raw, err := c.rpc(ctx, "prompts/list", params)
		if err != nil {
			return "", err
		}
		var page struct {
			Prompts []struct {
				Name        string `json:"name"`
				Title       string `json:"title"`
				Description string `json:"description"`
				Arguments   []struct {
					Name        string `json:"name"`
					Description string `json:"description"`
					Required    bool   `json:"required"`
				} `json:"arguments"`
			} `json:"prompts"`
			NextCursor string `json:"nextCursor"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			return "", fmt.Errorf("mcp: decode prompts: %w", err)
		}
		for _, p := range page.Prompts {
			if p.Name == "" {
				continue
			}
			info := PromptInfo{Name: p.Name, Title: p.Title, Description: p.Description}
			for _, a := range p.Arguments {
				info.Arguments = append(info.Arguments, PromptArgument{Name: a.Name, Description: a.Description, Required: a.Required})
			}
			out = append(out, info)
		}
		return page.NextCursor, nil
	})
	return out, err
}

// GetPrompt expands a prompt with the given arguments and returns its messages
// flattened to text, ready to submit as a fresh user turn.
func (c *Client) GetPrompt(ctx context.Context, name string, args map[string]string) (string, error) {
	params := map[string]any{"name": name}
	if len(args) > 0 {
		params["arguments"] = args
	}
	raw, err := c.rpc(ctx, "prompts/get", params)
	if err != nil {
		return "", err
	}
	var out struct {
		Messages []struct {
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("mcp: decode prompt: %w", err)
	}
	var parts []string
	for _, m := range out.Messages {
		if t := promptContentText(m.Content); t != "" {
			parts = append(parts, t)
		}
	}
	return joinLines(parts), nil
}

// promptContentText flattens a prompt message's content, which MCP encodes either
// as a single typed content object or an array of them. Only text is extracted;
// any non-text part is noted in place.
func promptContentText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	type part struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	flatten := func(ps []part) string {
		var out []string
		for _, p := range ps {
			switch {
			case p.Type == "text":
				out = append(out, p.Text)
			case p.Type != "":
				out = append(out, fmt.Sprintf("[%s content omitted]", p.Type))
			}
		}
		return joinLines(out)
	}
	var arr []part
	if err := json.Unmarshal(raw, &arr); err == nil {
		return flatten(arr)
	}
	var one part
	if err := json.Unmarshal(raw, &one); err == nil {
		return flatten([]part{one})
	}
	return ""
}
