---
id: AS-168
title: Wails desktop shell bootstrap over Smith core adapter
status: ready-to-implement
github_issue: null
depends_on: [AS-077]
area: faces
priority: P1
source: docs/project/smith-desktop-wails-prd.md
---

# AS-168 · Wails desktop shell bootstrap over Smith core adapter

**Status: ready to implement**

## Description

Create the initial Wails desktop application shell for Smith. The shell should
host a packaged web UI and expose only a narrow desktop adapter into the Smith
core.

The goal is to prove the product architecture, not to build a feature-rich app
yet.

## Scope

- Add a desktop app workspace rooted around Wails.
- Boot a simple window with Smith branding and a placeholder app shell.
- Define the first narrow desktop adapter surface into Smith.
- Keep Wails bindings limited to shell/adapter concerns only.
- Document the desktop app structure and local dev loop.

## Acceptance criteria

- [ ] The repository can build and launch a Wails desktop app locally.
- [ ] The desktop shell loads a packaged UI bundle inside Wails.
- [ ] The app exposes a narrow desktop adapter over the Smith core.
- [ ] No provider/tool/session logic is reimplemented in the Wails host layer.
- [ ] The desktop app structure is documented so future tickets have a stable
      composition root.

## Non-goals

- Managed runtime lifecycle.
- Real transcript rendering.
- Session list, approvals, tools, or cost panels.

## Dependencies

- The existing Smith core/session seams provide the runtime substrate this shell
  consumes.
