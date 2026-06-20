package topic_test

import (
	"reflect"
	"testing"

	"github.com/tonitienda/agent-smith/internal/topic"
	"github.com/tonitienda/agent-smith/schema"
)

func TestTags(t *testing.T) {
	tests := []struct {
		name  string
		block schema.Block
		want  []string
	}{
		{
			name:  "user text is a conversation",
			block: schema.Block{Kind: schema.KindText, Role: schema.RoleUser},
			want:  []string{"conversation"},
		},
		{
			name:  "system text is system",
			block: schema.Block{Kind: schema.KindText, Role: schema.RoleSystem},
			want:  []string{"system"},
		},
		{
			name: "file read carries its module directory",
			block: schema.Block{
				Kind:     schema.KindFileRead,
				FileRead: &schema.FileReadBody{Path: "internal/projection/projection.go"},
			},
			want: []string{"file", "file:internal/projection"},
		},
		{
			name: "top-level file is tagged by its name",
			block: schema.Block{
				Kind:     schema.KindFileRead,
				FileRead: &schema.FileReadBody{Path: "README.md"},
			},
			want: []string{"file", "file:README.md"},
		},
		{
			name: "tool call carries the specific tool",
			block: schema.Block{
				Kind:     schema.KindToolCall,
				ToolCall: &schema.ToolCallBody{Name: "shell"},
			},
			want: []string{"tool", "tool:shell"},
		},
		{
			name: "attribution and producer add skill, mcp, hook, and command tags",
			block: schema.Block{
				Kind:        schema.KindToolResult,
				Attribution: &schema.Attribution{Skill: "lint", MCPServer: "github", MCPTool: "list_issues", Tool: "grep", Hook: "post-run"},
				Provenance:  &schema.Provenance{Producer: "/clean"},
			},
			want: []string{"cmd:/clean", "hook:post-run", "mcp:github", "mcp:github/list_issues", "skill:lint", "tool", "tool:grep"},
		},
		{
			name:  "reasoning maps to its own coarse tag",
			block: schema.Block{Kind: schema.KindReasoning},
			want:  []string{"reasoning"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := topic.Tags(tc.block)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Tags = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestTagsNeverEmpty is the AC that every block carries at least one tag.
func TestTagsNeverEmpty(t *testing.T) {
	blocks := []schema.Block{
		{}, // zero value
		{Kind: schema.KindText, Role: schema.RoleAssistant},
		{Kind: schema.KindFallback},
		{Kind: schema.KindCompaction},
		{Kind: schema.KindToolCall, ToolCall: &schema.ToolCallBody{}}, // no name
	}
	for i, b := range blocks {
		if got := topic.Tags(b); len(got) == 0 {
			t.Errorf("block %d: Tags is empty, want at least one coarse tag", i)
		}
	}
}

// TestTagsDeterministic is the AC that the same block always yields the same,
// sorted, de-duplicated tag set (a stable handle for AS-029).
func TestTagsDeterministic(t *testing.T) {
	b := schema.Block{
		Kind:        schema.KindToolResult,
		Attribution: &schema.Attribution{Tool: "grep"},
		ToolCall:    &schema.ToolCallBody{Name: "grep"}, // same tag from two sources: de-duped
	}
	first := topic.Tags(b)
	for i := 0; i < 5; i++ {
		if got := topic.Tags(b); !reflect.DeepEqual(got, first) {
			t.Fatalf("Tags not deterministic: %v vs %v", got, first)
		}
	}
	want := []string{"tool", "tool:grep"}
	if !reflect.DeepEqual(first, want) {
		t.Fatalf("Tags = %v, want de-duplicated %v", first, want)
	}
}

func TestPrimary(t *testing.T) {
	b := schema.Block{
		Kind:     schema.KindFileRead,
		FileRead: &schema.FileReadBody{Path: "internal/topic/topic.go"},
	}
	if got, want := topic.Primary(b), "file:internal/topic"; got != want {
		t.Fatalf("Primary = %q, want %q", got, want)
	}
}
