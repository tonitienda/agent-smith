---
id: AS-146
title: Cloud control-plane run queue and worker protocol
status: ready-to-implement
area: cloud
priority: P2
depends_on: [AS-144, AS-145]
source: docs/projects/smith-cloud-prd.md
---

# AS-146 · Cloud control-plane run queue and worker protocol

## Description

Design and implement the cloud queue semantics that turn schedules/events into runnable sandbox tasks and let workers claim, heartbeat, stream, and finish them.

## Acceptance criteria

- [ ] Queue records are append/audit friendly and map every cloud run/subtask to a Smith session/run identifier.
- [ ] Worker protocol supports claim, heartbeat, log/event streaming, artifact upload, cancellation, timeout, and terminal status.
- [ ] Concurrency and idempotency rules prevent duplicate execution across workers and define retry behavior after worker loss.

## Dependencies

[AS-144, AS-145]
