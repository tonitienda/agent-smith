# Capture-to-fixture workflow (AS-135)

How a real vendor or CLI session becomes a deterministic, CI-safe regression
fixture — without leaking secrets, PII, or account identifiers, and without a
contributor hand-editing every field.

This is the bridge between **AS-060** (capture real vendor sessions to validate
the block schema) and **AS-133/AS-134** (recorded vendor simulators and the
offline E2E suite that *replay* those captures with no network and no API keys).
Read AS-060's capture procedure first
([`docs/projects/manual-test-campaign.md`](../projects/manual-test-campaign.md),
"AS-060 vendor session captures"); this doc covers everything after the raw
artifact exists.

## The states an artifact moves through

```
raw local capture  ──►  redacted reviewed capture  ──►  schema validation report
   (private, never        (secrets/PII scrubbed,            (round-trips through the
    committed)             reviewed by a human)              AS-003 types?)
                                   │
                                   ▼
                          recorded-server fixture  ──►  E2E scenario
                          (normalized JSONL + metadata,    (AS-133 replays it;
                           committed to the repo)           AS-134 drives the loop)
```

Keep these four data classes distinct — the metadata `redaction_status` field
records which one a file is:

| Class | `redaction_status` | Committed? |
|---|---|---|
| Raw private capture | `raw-private` | **Never.** Keep it out of git. |
| Redacted reviewed capture | `redacted` | Only after the review checklist passes. |
| Synthetic derivative | `synthetic-derivative` | Yes — no real account ever touched it. |
| Public CI fixture | `public-ci` | Yes — the normalized output of this tool. |

When in doubt about whether a redacted real capture is safe to publish, **author
a synthetic derivative instead**: reproduce the *shape* the simulator needs to
exercise (a tool call, a large argument, a streaming error envelope) with
invented content. AS-133 cares about field shapes, not real prompts.

## The tool

`cmd/capture-fixture` does the two mechanical transforms so review can focus on
judgment, not field-by-field editing. It reads a JSONL of `schema.Block` (the
format AS-060 produces by parsing a capture through the AS-003 types) and:

1. **Normalizes** identifying, non-deterministic envelope values — block IDs and
   sequence, timestamps, request/response/turn IDs, provider native IDs, and
   sub-agent thread/agent IDs — to stable placeholders (`blk-0001`, `req-0001`,
   …). References between blocks (`thread.parent_block_id`,
   `provenance.derived_from`, `excluded_by`) are mapped through the **same** table
   as the block IDs, so sub-agent and session links survive the rewrite.
2. **Redacts** the block body through the AS-115 rules
   ([`internal/redaction`](../../internal/redaction)) so a high-confidence secret
   that slipped into the capture never reaches the fixture.

Neither transform changes a block's kind, body shape, streaming structure, tool
arguments/results, usage, cache semantics, or sub-agent/session links — only the
values that identify a real account or leak a secret. Every block is validated
against the schema; the tool exits non-zero (and writes nothing) if any block
fails, so a malformed capture fails loudly instead of landing in CI.

```sh
go run ./cmd/capture-fixture \
  -in   captures/raw/anthropic-toolcall.jsonl \
  -out  internal/provider/anthropic/testdata/fixtures/toolcall.jsonl \
  -source real-capture -status redacted \
  -intent "Anthropic Messages tool-call round trip + large argument" \
  -providers anthropic/messages,anthropic/messages-stream
```

The sidecar metadata is written to `<out>.meta.json`. Flags:

- `-in` (default `-` = stdin), `-out` (empty = stdout, then `-meta` is required),
  `-meta` (default `<out>.meta.json`).
- `-source` `real-capture | synthetic | hand-authored`,
  `-status` `raw-private | redacted | synthetic-derivative | public-ci`.
- `-intent` (required) — the adapter behavior this fixture guards.
- `-providers` — comma-separated vendor/surface shapes.
- `-live` — whether a live API call can reproduce the capture.
- `-no-redact` — normalize only (use when the input is already a synthetic
  derivative with no secrets to scrub).

## Fixture metadata

Committed next to every fixture as `<name>.jsonl.meta.json`; this is the contract
AS-133 reads to decide what to load and how to drive it:

```json
{
  "source": "real-capture",
  "redaction_status": "redacted",
  "intent": "Anthropic Messages tool-call round trip + large argument",
  "providers": ["anthropic/messages"],
  "live_reproducible": true,
  "stats": { "blocks": 6, "redaction_spans": 1, "normalized": { "blk": 6, "req": 1 } }
}
```

`stats` is filled by the tool (counts only — never the secret values); the rest
is supplied by the contributor and validated (unknown source/status or an empty
intent is rejected).

## Review checklist (before committing a redacted real capture)

The tool scrubs high-confidence secrets, but redaction is best-effort
(`docs/design/compliance-archiving.md` §2.2) — a human still reviews:

- [ ] **Secrets** — no API keys, tokens, passwords, private keys, or session
      cookies remain. (The tool's built-in rules catch the common formats; eyeball
      the rest.)
- [ ] **PII** — names, emails, phone numbers, addresses, customer data in prompts
      or tool results are removed or invented.
- [ ] **Prompt licensing / sensitivity** — the captured prompt and any pasted
      source/document is yours to publish (no proprietary or third-party content).
- [ ] **Vendor account metadata** — org/project/account IDs, billing identifiers,
      and internal request IDs are normalized away (the tool handles the modeled
      ones; check `ext` and any free-text body).
- [ ] **Public-commit decision** — if anything above is uncertain, set
      `redaction_status` to `raw-private` and keep it out of git, or rebuild it as
      a `synthetic-derivative`.

## How AS-133 / AS-134 consume the result

- **AS-133** (recorded vendor simulators) loads the fixture JSONL + metadata,
  binds a fake vendor server on loopback, and replays the request/response shapes
  through the real provider adapters — driven entirely by the normalized fixture,
  so default CI needs no network and no keys.
- **AS-134** (offline E2E) promotes those simulators into full-loop scenarios
  (loop → TUI → append-only log), using the same fixtures so a schema or adapter
  regression surfaces as a failing E2E.

Because the metadata records `providers` and `intent`, those suites can select
fixtures by the vendor shape and the behavior under test rather than re-deriving
it from the file.

## The recorded vendor simulator (AS-133)

The simulator lives in `internal/provider/conformance` and reuses the existing
AS-012 conformance fixtures (`<vendor>/testdata/conformance/<case>.http`):

- `conformance.NewRecordedServer(exchanges...)` binds a fake vendor on loopback
  (ephemeral port). Each `Exchange` asserts the incoming request (method, path,
  body substrings) and returns a recorded response. It **fails loudly** with a
  method/path/body diff on a mismatch, an unexpected extra request, or an
  exchange that was never consumed — validation `FileTransport` cannot do.
- `conformance.FixtureExchange(t, path, reqPath, bodyContains...)` builds an
  Exchange from an existing `.http` fixture, so a vendor reuses its conformance
  corpus to drive the request-validating server.
- Point an adapter at it with the vendor's `WithBaseURL(srv.URL)` +
  `WithHTTPClient(srv.Client())`, run the turn, then `srv.AssertConsumed(t)`.
  This exercises the real HTTP client path (TCP, headers, serialization), unlike
  the transport-level `FileTransport` replay.

Each fixture directory carries a `fixtures.json` manifest classifying every
`<case>.http` as `synthetic` (hand-authored edge case) or `redacted-real` (a real
capture run through this workflow). `conformance.AssertFixtureMetadata` guards
that every fixture is classified, so a real AS-060 capture can never masquerade
as synthetic (or vice versa) once it lands.

**AS-060 implementers:** prefer turning a redacted capture into a recorded-server
fixture (a `redacted-real` `.http` + manifest entry) over a one-off report — it
becomes a permanent regression guard for the adapter shape the capture exposed.
