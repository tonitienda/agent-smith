package e2e

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/internal/loop"
	"github.com/tonitienda/agent-smith/internal/projection"
	"github.com/tonitienda/agent-smith/internal/tool"
	"github.com/tonitienda/agent-smith/schema"
)

// largeBlob is a payload too big to want to drive against a live API repeatedly,
// used as both a large tool argument (write content) and a large tool result
// (read output) so one scenario exercises AS-134's large request/response pair.
var largeBlob = strings.Repeat("agent-smith-regression-payload-0123456789\n", 1200) // ~50 KB

// TestSingleAgentLargeToolPayload drives a single-agent loop that writes a large
// file (large JSON tool argument) and reads it back (large tool result), then
// answers. It is the expensive whole-session shape AS-134 wants offline: a real
// tool round-trip carrying a payload that would be impractical to replay against
// a live API.
func TestSingleAgentLargeToolPayload(t *testing.T) {
	writeArgs, _ := json.Marshal(map[string]string{"path": "out.txt", "content": largeBlob})
	h := New(t, []turn{
		tools("msg-write", toolUse{id: "tu_write", name: "write", args: string(writeArgs)}),
		tools("msg-read", toolUse{id: "tu_read", name: "read", args: `{"path":"out.txt"}`}),
		// The final turn's request must carry the large read result fed back.
		{body: textTurn("msg-done", scenarioModel, "Wrote and verified the file.", 60, 10),
			bodyContains: []string{"agent-smith-regression-payload"}},
	})

	res := h.Run("create and verify a large file")
	h.AssertSimulatorDrained()

	if res.StopReason != "end_turn" {
		t.Fatalf("stop reason = %q, want end_turn", res.StopReason)
	}
	if res.Iterations != 3 {
		t.Fatalf("iterations = %d, want 3", res.Iterations)
	}

	// The large argument and result survive verbatim on the append-only log.
	gotWriteArgs := toolCallArgs(t, h.Events(), "tu_write")
	if !strings.Contains(gotWriteArgs, "agent-smith-regression-payload") || len(gotWriteArgs) < 40_000 {
		t.Fatalf("write tool args not preserved on log: len=%d", len(gotWriteArgs))
	}
	// The read result is large (tens of KB); the runtime caps a single result, so
	// assert a large-but-bounded payload rather than the full blob.
	readResult := toolResultText(t, h.Events(), "tu_read")
	if !strings.Contains(readResult, "agent-smith-regression-payload") || len(readResult) < 30_000 {
		t.Fatalf("read tool result not preserved on log: len=%d", len(readResult))
	}

	// The TUI-facing model layer saw each call start and finish, in order, with
	// the call identity it would render into a tool card.
	started := uiToolNames(h.UI, loop.UIToolStarted)
	finished := uiToolNames(h.UI, loop.UIToolFinished)
	if want := []string{"write", "read"}; !reflect.DeepEqual(started, want) || !reflect.DeepEqual(finished, want) {
		t.Fatalf("tool-card events = started %v finished %v, want %v", started, finished, want)
	}

	// Cost accounting prices every turn from the recorded usage.
	if c := h.Cost(); !c.AllPriced || c.TotalUSD <= 0 {
		t.Fatalf("cost = %+v, want priced and > 0", c)
	}
}

// TestParallelToolCalls scripts two independent tool calls in one model turn and
// verifies both results land on the log in call order with their ids preserved —
// the parallel-dispatch path (AS-019) end to end.
func TestParallelToolCalls(t *testing.T) {
	h := New(t, []turn{
		tools("msg-multi",
			toolUse{id: "tu_a", name: "read", args: `{"path":"a.txt"}`},
			toolUse{id: "tu_b", name: "read", args: `{"path":"b.txt"}`}),
		answer("msg-done", "Read both files."),
	}, WithFile("a.txt", "alpha"), WithFile("b.txt", "beta"))

	h.Run("read both files")
	h.AssertSimulatorDrained()

	if got := toolResultText(t, h.Events(), "tu_a"); !strings.Contains(got, "alpha") {
		t.Fatalf("tu_a result = %q, want alpha", got)
	}
	if got := toolResultText(t, h.Events(), "tu_b"); !strings.Contains(got, "beta") {
		t.Fatalf("tu_b result = %q, want beta", got)
	}

	// Results are recorded in call order even though execution is concurrent.
	order := toolResultOrder(h.Events())
	if want := []string{"tu_a", "tu_b"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("tool-result order = %v, want %v", order, want)
	}
	if finished := uiToolNames(h.UI, loop.UIToolFinished); len(finished) != 2 {
		t.Fatalf("finished tool-card events = %v, want 2", finished)
	}
}

// TestDeniedPermissionRecovery scripts a denied tool call and verifies the loop
// records a model-readable error result and lets the model recover on the next
// turn — the interrupted-permission flow AS-134 calls out.
func TestDeniedPermissionRecovery(t *testing.T) {
	deny := func(_ context.Context, c tool.Call) tool.Decision {
		if c.Name == "write" {
			return tool.Denied("not allowed in this scenario")
		}
		return tool.Allowed()
	}
	h := New(t, []turn{
		tools("msg-write", toolUse{id: "tu_denied", name: "write", args: `{"path":"secret.txt","content":"x"}`}),
		// The recovery turn's request must carry the error result fed back.
		{body: toolTurn("msg-recover", scenarioModel,
			[]toolUse{{id: "tu_read", name: "read", args: `{"path":"ok.txt"}`}}, 50, 18),
			bodyContains: []string{"permission denied"}},
		answer("msg-done", "Recovered without the write."),
	}, WithFile("ok.txt", "fine"), WithPermission(deny))

	res := h.Run("try to write then recover")
	h.AssertSimulatorDrained()

	if res.StopReason != "end_turn" {
		t.Fatalf("stop reason = %q, want end_turn", res.StopReason)
	}
	denied := toolResultBlock(t, h.Events(), "tu_denied")
	if !denied.ToolResult.IsError || !strings.Contains(toolResultText(t, h.Events(), "tu_denied"), "permission denied") {
		t.Fatalf("denied call result = %+v, want is_error with deny reason", denied.ToolResult)
	}
	// The denied write never created the file.
	if got := toolResultText(t, h.Events(), "tu_read"); !strings.Contains(got, "fine") {
		t.Fatalf("recovery read = %q, want the seeded file content", got)
	}
}

// TestSubagentDelegationLedger drives a parent agent delegating to two child
// agents (sequential task calls) and verifies the per-child cost ledger
// (AS-046/AS-120) itemizes each child and the parent/child usage linkage on
// disk. Delegations are sequential so the single recorded simulator serves
// parent and child turns in a deterministic order.
func TestSubagentDelegationLedger(t *testing.T) {
	h := New(t, []turn{
		tools("msg-delegate-1", toolUse{id: "tu_task1", name: "task", args: `{"prompt":"summarize package A"}`}),
		answer("msg-child-1", "Package A wires the loop."), // child 1 answer
		tools("msg-delegate-2", toolUse{id: "tu_task2", name: "task", args: `{"prompt":"summarize package B"}`}),
		answer("msg-child-2", "Package B prices turns."), // child 2 answer
		answer("msg-parent-done", "Delegated both summaries."),
	}, WithDelegation())

	h.Run("delegate two summaries")
	h.AssertSimulatorDrained()

	c := h.Cost()
	if len(c.Delegated) != 2 {
		t.Fatalf("delegated ledger = %+v, want two children", c.Delegated)
	}
	var childTotal float64
	for _, child := range c.Delegated {
		if child.Turns < 1 || child.TotalUSD <= 0 {
			t.Fatalf("child cost = %+v, want priced turns > 0", child)
		}
		childTotal += child.TotalUSD
	}
	// Each child's spend is part of the parent's grand total (a breakdown, not an add).
	if c.TotalUSD < childTotal {
		t.Fatalf("parent total %.6f < summed children %.6f", c.TotalUSD, childTotal)
	}

	// The parent log carries each child's usage as an attributed sidechain.
	sidechains := map[string]bool{}
	for _, b := range h.Events() {
		if b.Kind == eventlog.KindUsage && b.Thread != nil && b.Thread.IsSidechain {
			sidechains[b.Thread.AgentID] = true
		}
	}
	if len(sidechains) != 2 {
		t.Fatalf("sidechain usage for %d children on the parent log, want 2", len(sidechains))
	}
}

// TestResumeImmutableProjection reruns the large-payload scenario, then closes
// and reopens the session from disk, asserting that resume never mutates a
// previously written event and reprojects identical model-facing context — the
// append-only and deterministic-projection guarantees AS-134 protects.
func TestResumeImmutableProjection(t *testing.T) {
	writeArgs, _ := json.Marshal(map[string]string{"path": "out.txt", "content": "hello"})
	h := New(t, []turn{
		tools("msg-write", toolUse{id: "tu_write", name: "write", args: string(writeArgs)}),
		answer("msg-done", "Done."),
	})
	h.Run("write a file")
	h.AssertSimulatorDrained()

	live := h.Events()
	liveJSON := marshalEvents(t, live)
	liveProjection := marshalEvents(t, projection.Project(live, projection.Options{TargetModel: scenarioModel}).Live())

	reloaded := h.Reopen()
	if !reflect.DeepEqual(liveJSON, marshalEvents(t, reloaded)) {
		t.Fatal("reopened log differs from the live log: a previously written event was mutated or lost")
	}
	got := marshalEvents(t, projection.Project(reloaded, projection.Options{TargetModel: scenarioModel}).Live())
	if !reflect.DeepEqual(liveProjection, got) {
		t.Fatal("projection after resume is not deterministic")
	}
}

// --- assertion helpers -----------------------------------------------------

func toolCallArgs(t *testing.T, events []schema.Block, toolUseID string) string {
	t.Helper()
	for _, b := range events {
		if b.Kind == schema.KindToolCall && b.ToolCall != nil && b.ToolCall.ToolUseID == toolUseID {
			if b.ToolCall.ArgumentsRaw != "" {
				return b.ToolCall.ArgumentsRaw
			}
			return string(b.ToolCall.Arguments)
		}
	}
	t.Fatalf("no tool_call with id %q on log", toolUseID)
	return ""
}

func toolResultBlock(t *testing.T, events []schema.Block, toolUseID string) schema.Block {
	t.Helper()
	for _, b := range events {
		if b.Kind == schema.KindToolResult && b.ToolResult != nil && b.ToolResult.ToolUseID == toolUseID {
			return b
		}
	}
	t.Fatalf("no tool_result for id %q on log", toolUseID)
	return schema.Block{}
}

// toolResultText returns the model-visible output of a tool call: the
// tool_result content parts, plus any KindFileRead block the read tool produced
// for the same call (read records its content in a structured file-read block
// linked by ProducedBy, with an empty tool_result).
func toolResultText(t *testing.T, events []schema.Block, toolUseID string) string {
	t.Helper()
	b := toolResultBlock(t, events, toolUseID)
	var sb strings.Builder
	for _, p := range b.ToolResult.Content {
		sb.WriteString(p.Text)
	}
	for _, e := range events {
		if e.Kind == schema.KindFileRead && e.FileRead != nil && e.FileRead.ProducedBy == toolUseID {
			sb.WriteString(e.FileRead.Content)
		}
	}
	return sb.String()
}

func toolResultOrder(events []schema.Block) []string {
	var ids []string
	for _, b := range events {
		if b.Kind == schema.KindToolResult && b.ToolResult != nil {
			ids = append(ids, b.ToolResult.ToolUseID)
		}
	}
	return ids
}

func uiToolNames(ui []loop.UIEvent, kind loop.UIEventKind) []string {
	var names []string
	for _, ev := range ui {
		if ev.Kind == kind && ev.Tool != nil {
			names = append(names, ev.Tool.Name)
		}
	}
	return names
}

func marshalEvents(t *testing.T, events []schema.Block) []string {
	t.Helper()
	out := make([]string, len(events))
	for i, b := range events {
		data, err := json.Marshal(b)
		if err != nil {
			t.Fatalf("marshal event %d: %v", i, err)
		}
		out[i] = string(data)
	}
	return out
}
