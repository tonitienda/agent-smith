package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/schema"
)

// callID builds a tool_call block for tool name with an explicit tool_use id, so
// a batch test can give each of a turn's calls a distinct, recognizable id.
func callID(toolUseID, name, args string) schema.Block {
	return schema.Block{
		ID:   schema.NewID(),
		Kind: schema.KindToolCall,
		Role: schema.RoleAssistant,
		ToolCall: &schema.ToolCallBody{
			ToolUseID: toolUseID,
			Name:      name,
			Arguments: json.RawMessage(args),
		},
	}
}

// sleepTool sleeps for its "ms" argument (honoring cancellation) and returns its
// "out" argument as text, so a test can control completion order independently of
// call order.
func sleepTool(name string) Tool {
	return Func{
		Spec: Def{Name: name, InputSchema: json.RawMessage(`{"type":"object"}`)},
		Fn: func(ctx context.Context, args json.RawMessage) (Output, error) {
			var in struct {
				MS  int    `json:"ms"`
				Out string `json:"out"`
			}
			_ = json.Unmarshal(args, &in)
			select {
			case <-time.After(time.Duration(in.MS) * time.Millisecond):
				return Output{Text: in.Out}, nil
			case <-ctx.Done():
				return Output{}, ctx.Err()
			}
		},
	}
}

// blockText returns the concatenated text of a tool_result block.
func blockText(b schema.Block) string {
	var s string
	if b.ToolResult == nil {
		return ""
	}
	for _, p := range b.ToolResult.Content {
		if p.Type == "text" {
			s += p.Text
		}
	}
	return s
}

// resultOrder returns the tool_use ids of every tool_result on the log, in append
// order.
func resultOrder(log *eventlog.Log) []string {
	var order []string
	for _, b := range log.Events() {
		if b.Kind == schema.KindToolResult && b.ToolResult != nil {
			order = append(order, b.ToolResult.ToolUseID)
		}
	}
	return order
}

// TestExecuteBatchRunsConcurrently checks two independent slow tools complete in
// ~max(t1, t2) rather than t1+t2 — the core speed win of AS-019.
func TestExecuteBatchRunsConcurrently(t *testing.T) {
	rt, _ := newTestRuntime(t, sleepTool("sleep"))
	calls := []schema.Block{
		callID("a", "sleep", `{"ms":150,"out":"A"}`),
		callID("b", "sleep", `{"ms":150,"out":"B"}`),
	}

	start := time.Now()
	res, err := rt.ExecuteBatch(context.Background(), calls, BatchHooks{})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("ExecuteBatch: %v", err)
	}
	// Serial execution would be ~300ms; concurrent is ~150ms. A generous ceiling
	// keeps the test robust on a loaded CI box while still failing if the calls
	// ran one after the other.
	if elapsed > 270*time.Millisecond {
		t.Errorf("two 150ms tools took %s; want ~max not sum", elapsed)
	}
	if blockText(res[0]) != "A" || blockText(res[1]) != "B" {
		t.Errorf("results = %q,%q want A,B", blockText(res[0]), blockText(res[1]))
	}
}

// TestExecuteBatchRecordsInCallOrder forces completion order to be the reverse of
// call order and asserts both the returned results and the log stay in call
// order, and that the start/finish hooks fire in call order.
func TestExecuteBatchRecordsInCallOrder(t *testing.T) {
	rt, log := newTestRuntime(t, sleepTool("sleep"))

	const n = 6
	var calls []schema.Block
	for i := 0; i < n; i++ {
		// Earlier calls sleep longer, so call 0 finishes last.
		ms := (n - i) * 15
		calls = append(calls, callID("c"+strconv.Itoa(i), "sleep",
			fmt.Sprintf(`{"ms":%d,"out":"%d"}`, ms, i)))
	}

	var (
		mu             sync.Mutex
		started, ended []int
	)
	hooks := BatchHooks{
		Started: func(i int, _ schema.Block) { mu.Lock(); started = append(started, i); mu.Unlock() },
		Finished: func(i int, _ schema.Block, _ *schema.ToolResultBody) {
			mu.Lock()
			ended = append(ended, i)
			mu.Unlock()
		},
	}

	res, err := rt.ExecuteBatch(context.Background(), calls, hooks)
	if err != nil {
		t.Fatalf("ExecuteBatch: %v", err)
	}

	for i := range calls {
		if got := blockText(res[i]); got != strconv.Itoa(i) {
			t.Errorf("result[%d] text = %q, want %d", i, got, i)
		}
	}

	want := []string{"c0", "c1", "c2", "c3", "c4", "c5"}
	if got := resultOrder(log); !equalStrings(got, want) {
		t.Errorf("log result order = %v, want %v", got, want)
	}
	for i := 0; i < n; i++ {
		if started[i] != i || ended[i] != i {
			t.Errorf("hook order: started=%v ended=%v, want call order", started, ended)
			break
		}
	}
}

// TestExecuteBatchFailingToolDoesNotCancelSiblings verifies one tool's failure is
// recorded as its own error result and leaves its siblings' results intact.
func TestExecuteBatchFailingToolDoesNotCancelSiblings(t *testing.T) {
	boom := Func{
		Spec: Def{Name: "boom", InputSchema: json.RawMessage(`{"type":"object"}`)},
		Fn: func(context.Context, json.RawMessage) (Output, error) {
			return Output{}, errors.New("kaboom")
		},
	}
	rt, _ := newTestRuntime(t, boom, echoTool("echo", nil))
	calls := []schema.Block{
		callID("a", "echo", `{"msg":"A"}`),
		callID("b", "boom", `{}`),
		callID("c", "echo", `{"msg":"C"}`),
	}

	res, err := rt.ExecuteBatch(context.Background(), calls, BatchHooks{})
	if err != nil {
		t.Fatalf("ExecuteBatch returned a Go error for a tool failure: %v", err)
	}
	if blockText(res[0]) != "A" {
		t.Errorf("sibling A result = %q, want A", blockText(res[0]))
	}
	if !res[1].ToolResult.IsError {
		t.Errorf("failing tool result not marked error: %+v", res[1].ToolResult)
	}
	if blockText(res[2]) != "C" {
		t.Errorf("sibling C result = %q, want C", blockText(res[2]))
	}
}

// TestExecuteBatchDenialDoesNotCancelSiblings checks a denied call records its
// own error while the approved siblings still run (AS-019: a denial doesn't
// cancel approvals already granted).
func TestExecuteBatchDenialDoesNotCancelSiblings(t *testing.T) {
	perm := func(_ context.Context, c Call) Decision {
		if c.Name == "secret" {
			return Denied("not allowed")
		}
		return Allowed()
	}
	reg := NewRegistry()
	mustRegister(t, reg, echoTool("echo", nil))
	mustRegister(t, reg, echoTool("secret", nil))
	rt := NewRuntime(reg, eventlog.New(), WithPermission(perm))

	calls := []schema.Block{
		callID("a", "echo", `{"msg":"A"}`),
		callID("b", "secret", `{"msg":"B"}`),
		callID("c", "echo", `{"msg":"C"}`),
	}
	res, err := rt.ExecuteBatch(context.Background(), calls, BatchHooks{})
	if err != nil {
		t.Fatalf("ExecuteBatch: %v", err)
	}
	if blockText(res[0]) != "A" || blockText(res[2]) != "C" {
		t.Errorf("approved siblings results = %q,%q want A,C", blockText(res[0]), blockText(res[2]))
	}
	if !res[1].ToolResult.IsError {
		t.Errorf("denied call result not marked error: %+v", res[1].ToolResult)
	}
}

// TestExecuteBatchSerializesPermission verifies the permission hook is never
// invoked for two calls at once and is asked in call order, so an interactive
// ask-mode prompt cannot interleave (AS-019).
func TestExecuteBatchSerializesPermission(t *testing.T) {
	var (
		mu       sync.Mutex
		inFlight int
		maxSeen  int
		order    []string
	)
	perm := func(_ context.Context, c Call) Decision {
		mu.Lock()
		inFlight++
		if inFlight > maxSeen {
			maxSeen = inFlight
		}
		order = append(order, c.ToolUseID)
		mu.Unlock()

		time.Sleep(10 * time.Millisecond) // hold the "prompt" open

		mu.Lock()
		inFlight--
		mu.Unlock()
		return Allowed()
	}
	reg := NewRegistry()
	mustRegister(t, reg, echoTool("echo", nil))
	rt := NewRuntime(reg, eventlog.New(), WithPermission(perm))

	calls := []schema.Block{
		callID("p0", "echo", `{"msg":"0"}`),
		callID("p1", "echo", `{"msg":"1"}`),
		callID("p2", "echo", `{"msg":"2"}`),
		callID("p3", "echo", `{"msg":"3"}`),
	}
	if _, err := rt.ExecuteBatch(context.Background(), calls, BatchHooks{}); err != nil {
		t.Fatalf("ExecuteBatch: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if maxSeen != 1 {
		t.Errorf("max concurrent permission checks = %d, want 1 (serialized)", maxSeen)
	}
	if want := []string{"p0", "p1", "p2", "p3"}; !equalStrings(order, want) {
		t.Errorf("permission order = %v, want call order %v", order, want)
	}
}

// TestExecuteBatchBoundedParallelism checks WithMaxParallel caps how many tools
// run at once while still reaching the bound.
func TestExecuteBatchBoundedParallelism(t *testing.T) {
	var (
		mu       sync.Mutex
		inFlight int
		maxSeen  int
	)
	gate := Func{
		Spec: Def{Name: "gate", InputSchema: json.RawMessage(`{"type":"object"}`)},
		Fn: func(context.Context, json.RawMessage) (Output, error) {
			mu.Lock()
			inFlight++
			if inFlight > maxSeen {
				maxSeen = inFlight
			}
			mu.Unlock()
			time.Sleep(20 * time.Millisecond)
			mu.Lock()
			inFlight--
			mu.Unlock()
			return Output{Text: "ok"}, nil
		},
	}
	reg := NewRegistry()
	mustRegister(t, reg, gate)
	rt := NewRuntime(reg, eventlog.New(), WithMaxParallel(2))

	var calls []schema.Block
	for i := 0; i < 6; i++ {
		calls = append(calls, callID("g"+strconv.Itoa(i), "gate", `{}`))
	}
	if _, err := rt.ExecuteBatch(context.Background(), calls, BatchHooks{}); err != nil {
		t.Fatalf("ExecuteBatch: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if maxSeen > 2 {
		t.Errorf("max concurrent = %d, want <= 2 (bounded)", maxSeen)
	}
	if maxSeen < 2 {
		t.Errorf("max concurrent = %d, want the bound 2 to actually be reached", maxSeen)
	}
}

// TestExecuteBatchCancelAbortsSiblings cancels the turn while every tool is
// in-flight; the runtime must abort them all and surface the cancellation, with
// nothing recorded for the abandoned calls.
func TestExecuteBatchCancelAbortsSiblings(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := make(chan struct{}, 3)
	block := Func{
		Spec: Def{Name: "block", InputSchema: json.RawMessage(`{"type":"object"}`)},
		Fn: func(ctx context.Context, _ json.RawMessage) (Output, error) {
			started <- struct{}{}
			<-ctx.Done()
			return Output{}, ctx.Err()
		},
	}
	rt, log := newTestRuntime(t, block)
	calls := []schema.Block{
		callID("x", "block", `{}`),
		callID("y", "block", `{}`),
		callID("z", "block", `{}`),
	}

	go func() {
		for i := 0; i < len(calls); i++ {
			<-started
		}
		cancel()
	}()

	_, err := rt.ExecuteBatch(ctx, calls, BatchHooks{})
	if err == nil {
		t.Fatal("ExecuteBatch returned nil error, want cancellation")
	}
	// The turn was abandoned: no tool_result was recorded for any call. (The loop
	// reconciles the orphaned calls; the runtime simply records nothing.)
	if got := resultOrder(log); len(got) != 0 {
		t.Errorf("recorded results on cancellation = %v, want none", got)
	}
}

func mustRegister(t *testing.T, reg *Registry, tl Tool) {
	t.Helper()
	if err := reg.Register(tl); err != nil {
		t.Fatalf("Register %s: %v", tl.Def().Name, err)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
