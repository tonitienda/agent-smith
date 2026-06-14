# Provider conformance suite (AS-012)

Provider API drift is the top risk in the PRD (§9). This package is the
mitigation: one suite of behavioral expectations that **every** provider adapter
must satisfy *identically*, run on recorded fixtures so CI needs no API keys and
no network.

## How it works

- `conformance.Cases()` defines the canonical scenarios — streaming text,
  tool-call normalization, multi-tool turns, reasoning, unicode content, usage
  accounting, and error mapping (rate limit / context-too-long / auth). Each case
  declares the **normalized** turn (`Want`) the core must observe, or the typed
  error (`WantErr`) it must produce.
- Each provider package replays the suite through its adapter from fixtures it
  stores under `testdata/conformance/<case>.http`:

  ```go
  func TestConformance(t *testing.T) {
      conformance.Run(t, model, func(t *testing.T, c conformance.Case) provider.Provider {
          path := conformance.FixturePath(conformance.FixtureDir, c.Name)
          return New("test-key", WithHTTPClient(&http.Client{
              Transport: conformance.FileTransport(t, path),
          }))
      })
  }
  ```

The same `Want` holds for every provider. What is compared is the normalized
**semantics** (block kinds/roles, text, tool name/kind/arguments, stop reason,
token usage); inherently vendor-specific values (model id, response/tool-use ids)
are only required to be present. That is what makes the suite a cross-provider
equivalence check: a normalization divergence (e.g. tool-call arguments
reformatted instead of preserved verbatim) fails the suite rather than leaking
into the loop. `divergence_test.go` proves the suite catches exactly that.

## Fixture format

A fixture is a raw HTTP/1.1 response: a status line, a small allowlist of headers
(`Content-Type`, `Retry-After`, `Date`), a blank line, then the body (the SSE
stream for success cases, the JSON error body for error cases). Plain `\n` line
endings are fine. Success fixtures may omit `Content-Length` (replay reads the
body to EOF); recorded fixtures include it.

## Refreshing fixtures (`make record-fixtures`)

When a provider changes its wire format, regenerate the fixtures from live calls:

```sh
ANTHROPIC_API_KEY=sk-... OPENAI_API_KEY=sk-... make record-fixtures
```

This runs `TestRecordConformance` (skipped without `SMITH_RECORD=1` and a key, so
it never runs in CI). It issues each **recordable** case live through a
`RecordingTransport` that writes the response to its fixture, and sanity-checks
that the live stream still assembles. Error cases cannot be elicited on demand,
so their fixtures are curated by hand and left untouched.

A live turn's text differs from the curated fixtures, so recording does **not**
assert exact content. After recording, review the fixture diffs and reconcile any
wire-format change with the `Want` expectations in `conformance.go`.
