package benchmark

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/internal/provider"
	"github.com/tonitienda/agent-smith/schema"
)

// Suite is the frozen V1 fixture set: a small, deterministic set of coding tasks
// judged by file diffs, plus one context-bloat fixture that exercises the Smith-
// vs-naive context gap. It is repo-local and offline by construction (no external
// benchmark dependency, per AS-030's clarified decisions); the harness shape can
// later import public benchmark tasks. Keep tasks small and judgeable.
func Suite() []Task {
	return []Task{
		createFileTask(),
		overwriteTask(),
		readThenWriteTask(),
		multiFileTask(),
		textOnlyTask(),
		contextBloatTask(),
	}
}

// createFileTask: the agent creates a new file with required content.
func createFileTask() Task {
	const path, want = "greeting.txt", "hello world\n"
	return Task{
		ID:     "create-file",
		Prompt: "Create greeting.txt containing a greeting.",
		Turns:  []ScriptedTurn{writeTurn("c1", path, want)},
		Check:  fileEquals(path, want),
	}
}

// overwriteTask: the agent edits an existing file in place.
func overwriteTask() Task {
	const path, want = "config.ini", "mode=fast\n"
	return Task{
		ID:     "edit-file",
		Prompt: "Set the mode to fast in config.ini.",
		Setup:  seedFile(path, "mode=slow\n"),
		Turns: []ScriptedTurn{provider.ToolCallTurn("e1", "edit",
			mustJSON(map[string]string{"path": path, "old_string": "mode=slow", "new_string": "mode=fast"}))},
		Check: fileEquals(path, want),
	}
}

// readThenWriteTask: a two-tool round-trip (read, then write) across turns.
func readThenWriteTask() Task {
	const src, dst, content = "in.txt", "out.txt", "payload\n"
	return Task{
		ID:     "read-then-write",
		Prompt: "Copy in.txt to out.txt.",
		Setup:  seedFile(src, content),
		Turns: []ScriptedTurn{
			provider.ToolCallTurn("r1", "read", mustJSON(map[string]string{"path": src})),
			writeTurn("w1", dst, content),
		},
		Check: fileEquals(dst, content),
	}
}

// multiFileTask: two writes dispatched in one turn (parallel execution, AS-019).
func multiFileTask() Task {
	return Task{
		ID:     "multi-file",
		Prompt: "Create a.txt and b.txt.",
		Turns: []ScriptedTurn{provider.ToolCallsTurn(
			provider.ToolCall{ToolUseID: "m1", Name: "write", Args: mustJSON(map[string]string{"path": "a.txt", "content": "A\n"})},
			provider.ToolCall{ToolUseID: "m2", Name: "write", Args: mustJSON(map[string]string{"path": "b.txt", "content": "B\n"})},
		)},
		Check: func(dir string) (bool, string) {
			if ok, d := fileEquals("a.txt", "A\n")(dir); !ok {
				return false, d
			}
			return fileEquals("b.txt", "B\n")(dir)
		},
	}
}

// textOnlyTask: a no-tool turn — measures a plain answer's cost and latency.
func textOnlyTask() Task {
	return Task{
		ID:     "text-only",
		Prompt: "Explain what a unit test is in one sentence.",
		Turns:  []ScriptedTurn{provider.TextTurn("A unit test verifies one unit of code in isolation.", provider.StopEndTurn)},
		Check:  func(string) (bool, string) { return true, "answered" },
	}
}

// contextBloatTask seeds a large prior block and an exclusion event removing it.
// The Smith harness drops it from the window; the naive baseline resends it every
// turn — so naive's token/cost figures are higher. This is the fixture that makes
// a deliberate context-bloat regression measurable (AS-030 acceptance).
func contextBloatTask() Task {
	bloat := schema.Block{
		ID:   "bench-bloat-1",
		Kind: schema.KindText,
		Role: schema.RoleUser,
		Text: &schema.TextBody{Text: strings.Repeat("stale debugging output that was already resolved. ", 400), Subtype: schema.TextSubtypeNormal},
	}
	const path, want = "fix.txt", "fixed\n"
	return Task{
		ID:     "context-bloat",
		Prompt: "Record the fix in fix.txt.",
		Seed:   []schema.Block{bloat, eventlog.NewExclusion("benchmark", bloat.ID)},
		Turns:  []ScriptedTurn{writeTurn("b1", path, want)},
		Check:  fileEquals(path, want),
	}
}

// writeTurn scripts a single "write" tool call.
func writeTurn(id, path, content string) ScriptedTurn {
	return provider.ToolCallTurn(id, "write", mustJSON(map[string]string{"path": path, "content": content}))
}

// seedFile returns a Setup that writes rel=content into the workspace.
func seedFile(rel, content string) func(dir string) error {
	return func(dir string) error {
		return os.WriteFile(fixtureRoot(dir, rel), []byte(content), 0o644)
	}
}

// fileEquals returns a Check that passes when rel holds exactly want.
func fileEquals(rel, want string) func(dir string) (bool, string) {
	return func(dir string) (bool, string) {
		got, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			return false, "missing " + rel
		}
		if string(got) != want {
			return false, "unexpected " + rel + " contents"
		}
		return true, rel + " correct"
	}
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
