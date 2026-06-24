# Vendor session captures

Real (AS-060) and synthetic capture corpus that feeds the recorded-provider
regression harness (AS-133/AS-134). Every committed file here is a **CI-safe**
fixture produced through the capture-to-fixture workflow — see
[`../capture-to-fixture.md`](../capture-to-fixture.md) for how a raw capture
becomes one of these, the redaction review checklist, and what must never be
committed.

- `examples/` — synthetic, hand-built fixtures that demonstrate the workflow and
  the metadata shape. No real account ever touched them
  (`redaction_status: synthetic-derivative`).

Each `*.jsonl` fixture has a sibling `*.jsonl.meta.json` recording its source,
redaction status, intent, covered provider shapes, and whether a live API call
can reproduce it. Generate both with `go run ./cmd/capture-fixture`.
