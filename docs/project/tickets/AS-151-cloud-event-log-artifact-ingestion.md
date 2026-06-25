---
id: AS-151
title: Cloud run event-log and artifact ingestion
status: ready-to-implement
area: observability
priority: P2
depends_on: [AS-146, AS-147]
source: docs/projects/smith-cloud-prd.md
---

# AS-151 · Cloud run event-log and artifact ingestion

## Description

Upload cloud sandbox sessions, tool traces, diffs, logs, cost records, and artifacts into the normal Smith append-only session substrate.

## Acceptance criteria

- [ ] Every cloud run creates a resumable/readable Smith session with cloud metadata blocks linking job ID, trigger, sandbox ID, worker ID, GitHub refs, and artifact IDs.
- [ ] Artifacts are content-addressed or otherwise integrity-checked and referenced from the event log without embedding large blobs.
- [ ] Cost accounting and insights can read cloud sessions without a separate code path.
- [ ] Retention/export policy is enforced for logs, artifacts, snapshots, and redacted records.

## Dependencies

[AS-146, AS-147]
