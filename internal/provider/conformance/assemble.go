package conformance

import (
	"fmt"

	"github.com/tonitienda/agent-smith/internal/provider"
	"github.com/tonitienda/agent-smith/schema"
)

// Result is a normalized turn assembled from a provider's event stream — the
// same reduction the loop (AS-018) performs. It is the comparable shape the
// suite asserts against, so a turn produced from any vendor's wire format can be
// checked against one Want.
type Result struct {
	Model       string
	ResponseID  string
	StopReason  string
	Usage       schema.Tokens
	ServiceTier string
	Blocks      []ResultBlock
}

// ResultBlock is one assembled content block, flattened to the fields the suite
// compares across providers.
type ResultBlock struct {
	Kind         schema.Kind
	Role         schema.Role
	Text         string // text, or the visible text of a reasoning block
	Signature    string
	Encrypted    string
	ToolUseID    string
	ToolName     string
	ToolKind     string
	ToolSubtype  string
	ArgumentsRaw string
}

// Assemble drains s into a Result, reducing the normalized event stream back
// into the turn it describes. It always Closes s (via provider.Collect) and
// returns the stream's terminating error, if any.
func Assemble(s provider.Stream) (Result, error) {
	events, err := provider.Collect(s)
	if err != nil {
		return Result{}, err
	}

	var r Result
	open := map[int]*ResultBlock{}
	rawArgs := map[int]string{}

	for _, ev := range events {
		switch ev.Type {
		case provider.EventTurnStart:
			if ev.Turn != nil {
				r.Model = ev.Turn.Model
				r.ResponseID = ev.Turn.ResponseID
			}
		case provider.EventBlockStart:
			if ev.Header == nil {
				return Result{}, fmt.Errorf("block_start at index %d has no header", ev.BlockIndex)
			}
			open[ev.BlockIndex] = &ResultBlock{
				Kind: ev.Header.Kind, Role: ev.Header.Role,
				ToolUseID: ev.Header.ToolUseID, ToolName: ev.Header.ToolName,
				ToolKind: ev.Header.ToolKind, ToolSubtype: ev.Header.ToolSubtype,
			}
		case provider.EventTextDelta:
			b, ok := open[ev.BlockIndex]
			if !ok {
				return Result{}, fmt.Errorf("text_delta for unopened block %d", ev.BlockIndex)
			}
			b.Text += ev.TextDelta
		case provider.EventReasoningDelta:
			b, ok := open[ev.BlockIndex]
			if !ok {
				return Result{}, fmt.Errorf("reasoning_delta for unopened block %d", ev.BlockIndex)
			}
			b.Text += ev.TextDelta
			b.Signature += ev.SignatureDelta
			b.Encrypted += ev.EncryptedDelta
		case provider.EventToolCallDelta:
			rawArgs[ev.BlockIndex] += ev.ArgumentsDelta
		case provider.EventBlockStop:
			b, ok := open[ev.BlockIndex]
			if !ok {
				return Result{}, fmt.Errorf("block_stop for unopened block %d", ev.BlockIndex)
			}
			if b.Kind == schema.KindToolCall {
				b.ArgumentsRaw = rawArgs[ev.BlockIndex]
			}
			r.Blocks = append(r.Blocks, *b)
			delete(open, ev.BlockIndex)
		case provider.EventUsage:
			mergeUsage(&r.Usage, ev.Usage)
			if ev.UsageMeta != nil && ev.UsageMeta.ServiceTier != "" {
				r.ServiceTier = ev.UsageMeta.ServiceTier
			}
		case provider.EventTurnStop:
			r.StopReason = ev.StopReason
		}
	}

	if len(open) != 0 {
		return Result{}, fmt.Errorf("stream ended with %d block(s) still open", len(open))
	}
	return r, nil
}

// mergeUsage folds an incremental usage event into the running total, taking the
// latest non-nil value for each counter (usage may arrive more than once per
// turn; consumers accumulate — union §8).
func mergeUsage(dst *schema.Tokens, u *schema.Tokens) {
	if u == nil {
		return
	}
	if u.Input != nil {
		dst.Input = u.Input
	}
	if u.Output != nil {
		dst.Output = u.Output
	}
	if u.CacheRead != nil {
		dst.CacheRead = u.CacheRead
	}
	if u.CacheWrite != nil {
		dst.CacheWrite = u.CacheWrite
	}
	if u.Reasoning != nil {
		dst.Reasoning = u.Reasoning
	}
}
