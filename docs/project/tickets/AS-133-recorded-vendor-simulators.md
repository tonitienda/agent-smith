---
id: AS-133
title: Recorded vendor simulators for Anthropic, OpenAI, and compatible providers
status: done
github_issue: 413
depends_on: [AS-008, AS-009, AS-010, AS-012, AS-060, AS-135]
area: provider
priority: P0
source: AS-060 regression-testing follow-on; docs/project/PRD.md D2, D4, D5
---

# AS-133 · Recorded vendor simulators for Anthropic, OpenAI, and compatible providers

## Problem

AS-060 needs real vendor captures to validate the pre-V1 block schema, and AS-009/AS-010
need confidence that provider adapters keep handling vendor-specific streaming shapes over
time. Live API calls are expensive, flaky in CI, and cannot safely exercise pathological
cases such as huge tool arguments, retryable stream failures, or vendor error envelopes on
every pull request.

Build small in-process fake vendor servers that replay redacted request/response fixtures
for the provider surfaces Smith supports. They should be faithful enough to drive the real
provider adapters over HTTP, while deterministic and token-free enough to run in default
CI.

## What to build

- A shared recorded-server test harness, owned by provider/conformance code, that can:
  - bind to loopback on an ephemeral port;
  - match incoming requests by method, path, selected headers, and normalized JSON body;
  - stream Server-Sent Events or chunked responses with fixture-controlled timing;
  - return fixture-defined vendor error bodies, status codes, and retry headers;
  - fail loudly when an unexpected request arrives or an expected request is not consumed.
- Initial simulators/fixture packs for:
  - Anthropic Messages API streaming, including text, thinking/reasoning-like blocks,
    tool use, tool results, usage, cache usage, and representative error envelopes;
  - OpenAI Responses API streaming, including typed `output[]`, reasoning items,
    function/tool calls, usage, and errors;
  - OpenAI-compatible chat/completions projection for xAI/Grok-style deltas, including
    `reasoning_content` and citation/search metadata when present in captures.
- Fixture metadata that records where the fixture came from (real redacted AS-060 capture,
  synthetic edge case, or hand-authored regression), what adapter behavior it guards, and
  whether it is safe for public CI.
- Documentation for adding a new fixture without committing secrets, PII, or live API keys.

## Acceptance criteria

- [x] Default provider tests can run against the fake servers with no network access and no
      vendor API keys. *(`conformance.NewRecordedServer` on loopback; AS-012 `FileTransport`
      replay already covered the no-network/no-key path.)*
- [x] The Anthropic and OpenAI providers are exercised through their normal HTTP client path;
      tests do not bypass adapter serialization, streaming, or error handling. *(Adapters point
      at the loopback server via `WithBaseURL`/`WithHTTPClient` — real TCP/headers/serialization.)*
- [x] At least one fixture per supported vendor shape covers a tool-call round trip and a
      large tool argument/result payload. *(`large_tool_args` for Anthropic Messages + OpenAI
      Responses; `large_tool_args_chat` for the OpenAI-compatible chat projection — 1 KiB args
      preserved verbatim; existing `tool_call`/`multi_tool` cover round trips.)*
- [x] Unexpected provider requests produce a clear diff of the method/path/body mismatch.
      *(`RecordedServer` reports method/path/body diffs, unexpected extra requests, and
      unconsumed exchanges; self-tested in `conformance/server_test.go`.)*
- [x] Fixture metadata distinguishes redacted real captures from synthetic edge cases.
      *(`fixtures.json` manifest per dir + `AssertFixtureMetadata` guard; `synthetic` vs
      `redacted-real` sources.)*
- [x] Docs explain the capture-redaction-review workflow and point AS-060 implementers at the
      simulator as the preferred regression harness. *(`docs/design/capture-to-fixture.md`
      "The recorded vendor simulator (AS-133)".)*

## Implementation notes

Built on the AS-012 conformance harness rather than duplicating it: `FileTransport`
already replays raw `.http` fixtures through the real adapters with no network. The
new surface is the **request-validating** loopback server (`RecordedServer`) that
fails loudly on a method/path/body mismatch — the exfil/regression channel
`FileTransport` could not guard — plus large-payload fixtures and real-vs-synthetic
fixture metadata. AS-060's manual capture pass is still the source of `redacted-real`
fixtures; this harness is ready to consume them.

## Dependencies

- AS-008 through AS-010 provide the provider interface and concrete adapters to drive.
- AS-012 provides the conformance-test home this should extend rather than replace.
- AS-060 supplies real captures and schema gaps that should become recorded fixtures.
- AS-135 defines the safe capture-to-fixture workflow and metadata shape this harness consumes.
