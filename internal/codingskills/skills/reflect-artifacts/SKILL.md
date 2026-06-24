---
name: reflect-artifacts
description: Reflect phase — produce the three success artifacts (a measurable success metric, an instrumentation diff, a check-back ticket draft) without ever reading shipped-app runtime data.
---

# reflect-artifacts

You are in the **reflect** phase of Coding Mode. Judging whether the *shipped*
feature actually succeeded needs the user's deployed app's runtime telemetry,
which a coding harness cannot see — that is a different product and is **out of
scope** (D-CODE-7). What you *can* do is hand the user **artifacts** so success
is measurable later. Produce all three, only where there is something real:

- **Success metric.** State one *measurable* metric for the feature — a number,
  an event count, a latency/error bound — not "users like it". Name the concrete
  signal it reads (e.g. a `checkout_completed` event, the `p95` of a handler).
- **Instrumentation proposal (a diff).** Scaffold the code that *emits* that
  signal — an event/log/metric the user wires into their app — and output it as
  an ordinary edit **diff/proposal**, the same as any other code work. It is a
  proposal: the user applies it. Anchor it to the file that would carry it.
- **Check-back ticket.** File a ticket so success is revisited instead of
  forgotten. Inside this repo, draft it in the house ticket format — frontmatter
  (`id: AS-NNN` continuing the sequence, `title`, `status: ready-to-implement`)
  plus a short body stating the metric to check and when. In any other project,
  write the equivalent as a markdown check-back note. Draft only: never call
  `cmd/ticket-sync`, and never create or mutate a remote issue.

## Never read runtime data

You produce artifacts; you do **not** ingest, fetch, or claim shipped-app
runtime telemetry. Do not invent metric *values*, do not pretend to have read an
analytics dashboard, and do not propose that Smith poll a telemetry endpoint.
The metric and instrumentation are *plans for the user to run*, nothing more.

## Grounding (required)

Every artifact **must anchor to a concrete file, symbol, or ticket**. A success
metric with no signal to read, or an instrumentation note with no file to land
in, teaches nothing. Never write "measure engagement" or "add some metrics".

Good (grounded):

- Success metric: count `OrderConfirmed` events emitted from
  `internal/checkout/handler.go` ConfirmOrder(); target ≥ 95% of started carts
  within 30 days, file check-back ticket AS-201.
- Instrumentation: add a `metrics.Inc("order.confirmed")` call in
  `internal/checkout/handler.go` ConfirmOrder() — proposed as a diff for review.

Bad (rejected — no anchor / reads runtime data):

- Track engagement and see if it improved.
- Pull last week's conversion numbers from the analytics dashboard.
