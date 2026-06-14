package loop

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/tonitienda/agent-smith/internal/provider"
	"github.com/tonitienda/agent-smith/schema"
)

// StopCanceled is the stop reason surfaced on a Result when the run was
// cancelled via ctx. It is loop-defined (not a provider stop reason) so a face
// can distinguish a user cancel from a model end-turn.
const StopCanceled = "canceled"

// turnResult is the outcome of one provider turn the loop assembled from the
// stream: the normalized stop reason, the visible assistant text, and the
// client tool_call blocks (already appended to the log) the loop must dispatch.
type turnResult struct {
	stopReason  string
	text        string
	clientCalls []schema.Block
}

// streamTurn issues one turn's request with the engine's retry/backoff policy.
// A turn is retried only when its attempt appended nothing and failed with a
// retryable provider error: once blocks are on the log, or the failure is not
// retryable, or the context is cancelled, the error is surfaced with whatever
// turnResult the attempt produced (so Run can reconcile any appended calls).
func (e *Engine) streamTurn(ctx context.Context, iter int, req provider.Request) (turnResult, error) {
	var lastErr error
	for attempt := 0; attempt < e.maxAttempts; attempt++ {
		if attempt > 0 {
			if err := e.backoff(ctx, attempt, lastErr); err != nil {
				return turnResult{}, err
			}
		}

		turn, appended, err := e.streamOnce(ctx, iter, req)
		if err == nil {
			return turn, nil
		}
		// A cancelled context, a partially-recorded turn, or a non-retryable
		// failure all surface immediately rather than retry.
		if ctx.Err() != nil || appended > 0 || !provider.IsRetryable(err) {
			return turn, err
		}
		lastErr = err
	}
	return turnResult{}, lastErr
}

// streamOnce drives one provider stream to completion, assembling each block and
// appending it to the log as it closes, while emitting face-agnostic UIEvents.
// It returns the turn outcome, how many blocks it appended (so the caller knows
// whether a clean retry is possible), and the stream's terminating error.
func (e *Engine) streamOnce(ctx context.Context, iter int, req provider.Request) (turnResult, int, error) {
	s, err := e.provider.Stream(ctx, req)
	if err != nil {
		return turnResult{}, 0, err
	}
	defer s.Close() //nolint:errcheck // Close is best-effort once the stream is drained

	e.emit(UIEvent{Kind: UITurnStart, Iteration: iter})

	var (
		turn     turnResult
		appended int
		text     strings.Builder
		turnInfo provider.TurnInfo
		// open holds the block currently streaming, keyed by BlockIndex; rawArgs
		// accumulates a tool call's verbatim argument fragments.
		open    = map[int]*schema.Block{}
		rawArgs = map[int]*strings.Builder{}
	)
	turnModel := req.Model

	for s.Next() {
		ev := s.Event()
		switch ev.Type {
		case provider.EventTurnStart:
			if ev.Turn != nil {
				turnInfo = *ev.Turn
				turnModel = firstNonZero(ev.Turn.Model, turnModel)
			}

		case provider.EventBlockStart:
			open[ev.BlockIndex] = newBlock(ev.Header)
			rawArgs[ev.BlockIndex] = &strings.Builder{}

		case provider.EventTextDelta:
			if b := open[ev.BlockIndex]; b != nil && b.Text != nil {
				b.Text.Text += ev.TextDelta
			}
			text.WriteString(ev.TextDelta)
			e.emit(UIEvent{Kind: UITextDelta, Iteration: iter, Text: ev.TextDelta})

		case provider.EventReasoningDelta:
			if b := open[ev.BlockIndex]; b != nil && b.Reasoning != nil {
				b.Reasoning.Text += ev.TextDelta
				b.Reasoning.Signature += ev.SignatureDelta
				b.Reasoning.Encrypted += ev.EncryptedDelta
			}
			if ev.TextDelta != "" {
				e.emit(UIEvent{Kind: UIReasoningDelta, Iteration: iter, Text: ev.TextDelta})
			}

		case provider.EventToolCallDelta:
			if rb := rawArgs[ev.BlockIndex]; rb != nil {
				rb.WriteString(ev.ArgumentsDelta)
			}

		case provider.EventBlockStop:
			b := open[ev.BlockIndex]
			if b == nil {
				continue
			}
			delete(open, ev.BlockIndex)
			if b.Kind == schema.KindToolCall && b.ToolCall != nil {
				finalizeToolArgs(b, rawArgs[ev.BlockIndex])
			}
			delete(rawArgs, ev.BlockIndex)

			stored, aerr := e.appendAssistant(b, turnModel, turnInfo)
			if aerr != nil {
				return turn, appended, aerr
			}
			appended++
			if stored.Kind == schema.KindToolCall && stored.ToolCall != nil &&
				stored.ToolCall.ToolKind != schema.ToolKindServer {
				turn.clientCalls = append(turn.clientCalls, stored)
			}

		case provider.EventTurnStop:
			turn.stopReason = ev.StopReason
		}
	}

	if err := s.Err(); err != nil {
		turn.text = text.String()
		return turn, appended, err
	}

	turn.text = text.String()
	e.emit(UIEvent{Kind: UITurnComplete, Iteration: iter, StopReason: turn.stopReason})
	return turn, appended, nil
}

// newBlock builds the in-progress block scaffold for an opening block, with the
// typed body matching its kind ready to accumulate deltas.
func newBlock(h *provider.BlockHeader) *schema.Block {
	if h == nil {
		return &schema.Block{Kind: schema.KindText, Role: schema.RoleAssistant, Text: &schema.TextBody{}}
	}
	b := &schema.Block{Kind: h.Kind, Role: h.Role}
	if b.Role == "" {
		b.Role = schema.RoleAssistant
	}
	switch h.Kind {
	case schema.KindText:
		b.Text = &schema.TextBody{Subtype: schema.TextSubtypeNormal}
	case schema.KindReasoning:
		b.Reasoning = &schema.ReasoningBody{}
	case schema.KindToolCall:
		b.ToolCall = &schema.ToolCallBody{
			ToolUseID:   h.ToolUseID,
			Name:        h.ToolName,
			ToolKind:    firstNonZero(h.ToolKind, schema.ToolKindClient),
			ToolSubtype: h.ToolSubtype,
		}
	default:
		// An unrecognized kind still carries a text body so deltas have somewhere
		// to land; additive kinds (PRD D2) degrade rather than panic.
		b.Text = &schema.TextBody{}
	}
	return b
}

// finalizeToolArgs records a tool call's accumulated argument string as both the
// verbatim ArgumentsRaw (signatures/caching depend on exact bytes) and the
// structured Arguments the runtime validates and the tool reads.
//
// The structured Arguments is set only when the streamed string is valid JSON:
// an invalid json.RawMessage makes json.Marshal of the whole block fail, which
// would abort the disk-backed append and corrupt any later marshal of an
// in-memory block. When the model streams malformed arguments we leave
// Arguments unset (omitted) while preserving the raw bytes in ArgumentsRaw, so
// the block is always serializable and the runtime's schema validation rejects
// the empty-argument call gracefully (a model-readable error) rather than the
// log failing to persist.
func finalizeToolArgs(b *schema.Block, raw *strings.Builder) {
	if raw == nil {
		return
	}
	s := raw.String()
	if s == "" {
		return
	}
	b.ToolCall.ArgumentsRaw = s
	if json.Valid([]byte(s)) {
		b.ToolCall.Arguments = json.RawMessage(s)
	}
}

// appendAssistant stamps provider provenance/identity on an assembled block and
// appends it to the log, returning the stored block (with its assigned ID/Seq).
func (e *Engine) appendAssistant(b *schema.Block, model string, info provider.TurnInfo) (schema.Block, error) {
	b.ID = schema.NewID()
	b.Provider = &schema.Provider{Vendor: e.provider.Name(), Model: model}
	b.Provenance = &schema.Provenance{
		Producer:   e.provider.Name(),
		ResponseID: info.ResponseID,
		TurnID:     info.TurnID,
	}
	return e.log.Append(*b)
}

// backoff waits before a retry attempt, honoring a provider-suggested RetryAfter
// and otherwise using capped exponential backoff. It returns the context error
// if the wait is cancelled, so a cancel during backoff ends the run promptly.
func (e *Engine) backoff(ctx context.Context, attempt int, lastErr error) error {
	wait := e.backoffBase << (attempt - 1)
	if wait <= 0 || wait > e.backoffMax {
		wait = e.backoffMax
	}
	if pe, ok := provider.AsError(lastErr); ok && pe.RetryAfter > wait {
		wait = pe.RetryAfter
	}

	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
