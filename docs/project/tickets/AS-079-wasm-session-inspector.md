---
id: AS-079
title: WASM observability core + static session inspector
status: ready-to-implement
github_issue: null
depends_on: [AS-005, AS-006, AS-020, AS-061, AS-038]
area: observability
priority: P2
source: PRD.md §4 (observability thesis), §7.26; GUI grilling session 2026-06
---

# AS-079 · WASM session inspector

**Status: ready to implement (carries a build-pipeline proof — see acceptance)**

## Description

The one place WASM genuinely earns its rent. Smith's thesis is an **open, stable
event log (D3)** with **first-class context observability (§4)**. The views that
express that — projection (`/context`, AS-006), per-block token/cost accounting
(AS-020/AS-063), composition (AS-026), and `/clean`/`/compact` *preview* — are
**pure computation over a session log**: no shell, no filesystem, no network, no
API keys. That subset compiles cleanly to `GOOS=js GOARCH=wasm` and runs fully
client-side.

Deliverable: a **static web page that opens any agent-smith session log and
renders the observability views with zero backend.** It is shareable (drop a log
in, get a replayable, inspectable view), it matches the product's actual moat,
and — because it executes nothing — it doubles as the **safe public demo** for
strangers (canned sample sessions), sidestepping the AS-080 sandboxing problem
entirely.

Reusing the real Go core (compiled to WASM) rather than reimplementing
projection/cost math in JS keeps the inspector honest: it shows exactly what the
agent computes, guarded by the same schema (AS-061).

## Scope

- A small `cmd/` or `internal/` WASM entry point that exposes the existing
  projection (AS-006), cost (AS-020/063), composition (AS-026), and clean/compact
  *preview* (AS-028/038, read-only — appends nothing) over a session log loaded
  in the browser, via `syscall/js` bindings.
- A static page: load a log file (file picker / drag-drop / `?url=`), then render
  `/context`, the composition view, the cost/context meters, and a clean/compact
  preview. Read-only — it never writes events.
- **Exclude the live agent by construction:** no provider (`net/http`), no tool
  runtime (`os/exec`, `os`), no keychain in this build. Enforce with build tags
  so the WASM target physically cannot pull them in.

## Acceptance criteria

- [ ] **Build-pipeline proof (spike-grade, do this first):** decide and document
      stdlib `GOOS=js` vs TinyGo for this target, measure the gzipped artifact
      size against a stated budget, and confirm the projection/cost packages
      compile and run under the chosen toolchain (reflection/`encoding/json`
      caveats noted). Record the outcome in an ADR or the ticket.
- [ ] Loading a real saved session log in the browser renders `/context` matching
      what the native CLI produces for the same log (golden parity).
- [ ] Per-block token estimates and total cost match the native AS-020/063 output.
- [ ] A `/clean` or `/compact` preview renders without mutating the loaded log.
- [ ] The WASM build excludes provider/tool/keychain code (verified by build tags
      + a failing-import check), so no live-agent capability ships to the browser.
- [ ] Static assets only: hostable on any static host; the public-demo variant
      ships with canned sample sessions and no upload-to-server path.

## Non-goals

- Driving a live agent / running tools in the browser — architecturally
  impossible without a host; that path is AS-077 + AS-078.
- A `net/http`→`fetch` shim for in-browser provider calls — not needed for a
  read-only inspector; noted as a future option only if a client-side live path
  is ever pursued.

## Dependencies

- AS-005 (event log), AS-006 (projection), AS-020 (cost; AS-063 token estimates),
  AS-061 (published JSON Schema, so the inspector validates what it loads).
  Renders AS-026 composition and AS-028/AS-038 previews.
