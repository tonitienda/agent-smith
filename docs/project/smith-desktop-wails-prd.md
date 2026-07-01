# PRD: Smith Desktop app on Wails

> Status: Draft · Owner: Product · Date: 2026-06-30

## Problem

Smith already has the right architectural seam for a desktop product: the core
agent is local, the `smith serve` face exists, and the web GUI wave is defined.
What is missing is a packaged desktop surface that feels closer to Claude
Desktop, Codex, and other app-native agent products: easier launch, stronger
session management, clearer multi-pane interaction, and less dependence on the
terminal for day-to-day use.

The decision to make now is not "should Smith have a desktop app?" but "which
desktop shell best fits Smith's local-first architecture and product thesis?"

## Recommendation

Build the first Smith desktop app on **Wails** and keep the product
architecture thin:

- Wails provides the native desktop shell, packaging, windowing, menus, dialogs,
  and Go↔web UI bridge.
- Smith remains one packaged desktop application from the user's point of view.
- The desktop app preserves a **strict internal adapter boundary** over the
  Smith core rather than binding random internals directly into the frontend.
- The desktop shell does not become a second agent runtime, second permission
  model, or second persistence engine.

This is the most consistent choice with Smith's product and architecture
decisions:

- Smith already treats faces as adapters over one core.
- D9 says Smith runs locally with the user's privileges and is not a sandbox.
- The product thesis values low overhead, local-first control, and explicit
  boundaries more than a maximal plugin/browser ecosystem.
- Wails lets Smith present as a **single packaged desktop process** without
  throwing away the architectural discipline Smith already needs across faces.

## Why Wails over Tauri/Electron

### Decision

Choose **Wails by default** for V1 desktop.

### Why Wails now

Wails is not a widget toolkit. It is a Go desktop app framework built around web
frontends, native windowing/menu/dialog integrations, and a Go↔JavaScript bridge.
That makes it much closer to Tauri and Electron than to Fyne or Gio, while
keeping the host side in Go.

For Smith, that matters because the desired product shape is now:

- **single desktop process for the user**;
- **one Go-centric packaged application**;
- **strict internal adapter boundary** around the Smith core;
- **no user-visible server/client split** even if the code keeps face/core
  discipline internally.

### Rationale

1. **Architecture fit**

Smith already has a native core written in Go and faces layered over that core.
Wails lets Smith keep that face/core shape while shipping the desktop app as one
packaged Go application instead of normalizing the product around a visible
server/client split.

2. **Better user mental model**

Users get one app, one lifecycle, one install, one quit path, and fewer visible
failure modes. That is better than asking the product to explain a UI process
and a local runtime process as separate moving pieces.

3. **Language alignment with the repository**

The repository is Go-first. Wails keeps the desktop host in Go, which lowers
contributor friction and makes a "single packaged process" approach easier to
adopt without adding Rust as another systems layer.

4. **Still compatible with strict boundaries**

Wails makes it easy to call Go from JavaScript, but Smith does not need to use
that convenience naively. The desktop app can still define a **narrow adapter**
that owns session/view actions and delegates to the Smith core, preserving the
same discipline the multi-face architecture needs.

5. **Resource and lifecycle simplicity**

One packaged app means fewer process-management, connection, and idle-runtime
concerns from the user's perspective, while still allowing Smith to organize the
code around adapters internally.

### When Tauri would still be the better choice

Tauri remains the stronger fallback if Smith later decides these outweigh the
single-process Go-host preference:

- stronger built-in capability/trust-boundary primitives for the desktop shell;
- a desire to keep the frontend/backend bridge narrower and more explicitly
  permissioned at the framework level;
- update/signing/plugin ecosystem advantages that materially outweigh the extra
  Rust host layer.

### When Electron would still be the better choice

Electron remains the fallback only if Smith later needs:

- Chromium-level UI uniformity across platforms;
- a broader desktop package ecosystem than Wails/Tauri provide;
- behavior that is significantly easier to ship in a Chromium + Node stack.

That is not the current Smith shape.

## Wails vs Tauri vs Electron summary

| Dimension | Wails | Tauri 2 | Electron | Smith implication |
|---|---|---|---|---|
| Process/user model | Can ship as one Go-hosted desktop app | Thin shell commonly paired with a more explicit runtime split | Bundled Chromium + Node app | Smith now prefers the single-app experience. |
| Host language | Go | Rust | JavaScript/TypeScript | Wails best matches the repo's language center. |
| Boundary discipline | Must be enforced intentionally in app design | Stronger capability/trust-boundary framework story | Powerful but easiest to overexpose | Wails needs discipline, but Smith can supply it architecturally. |
| Footprint | Small, system WebView based | Small, system WebView based | Larger, bundled Chromium/Node | Wails and Tauri both fit Smith's lightweight goal better than Electron. |
| UI portability | System WebView variance | System WebView variance | Chromium uniformity | Acceptable for Smith's first desktop scope. |
| Ecosystem | Smaller | Smaller/moderate | Larger | Electron still wins breadth, but not enough to drive the decision. |

## Tauri vs Wails vs other Go options

| Option | Strengths | Weaknesses | Smith fit |
|---|---|---|---|
| **Wails** | Go-first desktop host, web frontend friendly, native menus/dialogs/theming, small runtime using system WebView, naturally supports a single packaged app | Less opinionated security/capability model than Tauri; easier to blur host logic into the app bridge | **Best default** if Smith wants single-process UX with a strict internal adapter boundary. |
| **Tauri 2** | Thin shell, system WebView, explicit trust-boundary and capability model, small bundles, strong updater/plugin story | Rust host layer; WebView variance across OSes; nudges toward a more explicit shell/runtime split | Strong fallback if Smith decides framework-level boundary controls matter more than Go-host alignment. |
| **Fyne** | Pure Go widget toolkit with packaging, desktop APIs, tray/preferences/shortcuts, many built-in widgets | Pushes Smith toward a native-widget app instead of reusing the web UI path; less natural fit for shared UI with AS-078 | Good toolkit, but not the best fit for Smith's thin-client-over-`serve` direction. |
| **Gio** | Pure Go, efficient custom-rendered immediate-mode UI, low dependency footprint | Lower-level; higher UI-build cost; less conventional app-shell ergonomics out of the box | Strong for custom rendering, weak for a fast first desktop product. |

### Recommendation among Go-native options

If the team wants a **Go-native shell**, pick **Wails**, not Fyne or Gio.

Reason:

- Smith already wants a web-style UI surface that can share concepts and
  possibly code with AS-078.
- Wails, like Tauri, uses the system webview instead of forcing a native-widget
  rewrite.
- Fyne and Gio are better choices when the product wants a true Go GUI toolkit,
  not when it wants a thin host around a web UI and local runtime seam.

### Final shell ranking for Smith today

1. **Wails** for a single packaged desktop app with a strict internal adapter
   boundary over the Smith core.
2. **Tauri 2** if framework-level shell boundary controls become more important
   than the single-process Go-host preference.
3. **Electron** only if Chromium uniformity or ecosystem breadth becomes more
   important than footprint.
4. **Fyne/Gio** only if Smith decides to abandon the web-UI/thin-client
   direction and build a native-Go GUI instead.

## Product goals

1. Ship a **local-first desktop app** that makes Smith easier to launch and use
   than the TUI for everyday interactive work.
2. Reuse the **existing Smith core and event stream** instead of inventing a new
   desktop-only runtime path.
3. Deliver a desktop experience competitive with Claude Desktop/Codex-style
   agent apps in the areas users notice first: session launch, streaming chat,
   visibility into actions, approvals, and history.
4. Keep the first version intentionally simple and robust rather than broad.

## Non-goals

- Replacing the TUI as Smith's flagship power-user face.
- Hosting remote agents or remote workspaces.
- Building a browser-based sandbox or changing D9.
- Inventing a second session store, second cost engine, or second permission
  system inside the desktop shell.
- Building a full IDE/editor replacement in the first wave.

## Users

- **Terminal-tired power user:** likes Smith's core behavior but wants a calmer,
  always-available desktop surface for longer sessions.
- **Manager of many sessions:** wants session lists, resume, and quick switching
  without living in terminal tabs.
- **First-time evaluator:** is more likely to try a signed desktop app than a
  CLI-first workflow.

## Product principles

1. **Thin face over the real core**

The desktop app is a face over the Smith core through a narrow desktop adapter,
not a parallel implementation.

2. **Local machine, explicit authority**

All model calls, file writes, shell execution, permissions, and cost accounting
remain in Smith. The desktop shell should not bypass the existing guardrails.

3. **Interactive mode first**

The first version should nail the main session loop before layering workboards,
teams, or background orchestration.

4. **Desktop conventions, Smith truth**

Use desktop-native affordances where they help, but keep Smith concepts intact:
sessions, approvals, tools, context, cost, and append-only logs.

## Competitive feature target

For the first desktop release, Smith should borrow the most valuable traits from
Claude Desktop, Codex, and similar agent apps without copying their product
scope:

- fast launch into a new or recent workspace;
- a persistent conversation list with easy resume;
- streaming transcript with a stable composer;
- visible tool activity instead of hidden background actions;
- approval prompts that feel first-class, not like CLI errors;
- durable workspace/project context;
- clear "what is the agent doing now?" status;
- keyboard-first interaction and basic command/search affordances.

Smith should **not** try to match cloud-task boards, IDE-native editing, or
remote collaboration in the first desktop wave.

## Architecture

### High-level shape

```text
Wails desktop shell
  -> local windowing, menus, dialogs, shortcuts, notifications
  -> embedded web UI bundle
  -> narrow desktop adapter layer
  -> Smith core APIs

Smith core
  -> unchanged product truth
```

### Why this shape matters

- It preserves the face/core boundary already documented in the repository.
- It gives users a single packaged desktop app instead of a visible two-process
  product model.
- It lets the web GUI and desktop app share concepts and potentially UI code
  later, even if the desktop app does not speak `smith serve` directly.
- It limits Wails-specific code to shell concerns plus a narrow adapter layer,
  instead of letting desktop bindings sprawl into business logic.

### Adapter model

The desktop app should define a dedicated **desktop adapter** between the UI and
the Smith core:

1. The frontend calls a small set of desktop-facing adapter methods.
2. The adapter translates those calls into Smith session/turn/runtime actions.
3. The adapter emits view-safe events/state back to the frontend.
4. The adapter owns no alternative session semantics, permission model, or cost
   logic.

This preserves the internal boundary Smith needs without forcing the product to
be explained as server + client to the user.

## MVP scope

### 1. Desktop shell and workspace home

- Signed desktop app for macOS and Linux first; Windows can follow if packaging
  cost stays contained.
- Home screen with:
  - recent workspaces;
  - new session;
  - resume recent session;
  - runtime status.

### 2. Interactive session view

- Main transcript area with streaming assistant output.
- Prompt composer with send/cancel.
- Session title and workspace breadcrumb.
- Clear turn status: idle, thinking, waiting on permission, running tool,
  completed, failed.

### 3. Tool and approval visibility

- Side rail or lower pane showing tool calls and results.
- Approval modals/cards for ask-mode actions with allow / allow-always / deny.
- Diff preview for file edits when the underlying event stream provides it.

### 4. Context and cost visibility

- Compact always-visible context meter.
- Live per-session token/cost summary.
- Entry points into richer `/context` and `/cost` style views later.

### 5. Session list and resume

- Session list filtered by workspace.
- Resume an existing session into the main view.
- Show last activity time and basic status.

### 6. Settings and runtime health

- Runtime health/status for the embedded Smith core.
- Model/provider display and basic selection if supported by the desktop
  adapter surface.
- Links to auth/setup guidance rather than re-implementing all auth flows in V1.

## Post-MVP but near-term

- Multi-window or tabbed sessions.
- Global quick-open / command palette.
- Desktop notifications for approval-needed / task-complete states.
- Tray/menu bar presence.
- Better settings/auth management.
- Shared UI substrate with the AS-078 browser client.
- Workboard integration when the workboard product is ready.

## UX notes for the first version

Use a simple three-part layout:

- **Left:** workspaces and sessions.
- **Center:** transcript and composer.
- **Right:** activity rail for tools, approvals, cost, and context summary.

This is the simplest layout that still differentiates the desktop app from the
TUI and makes Smith's transparency wedge visible.

The first visual direction should be calm and utility-heavy, not "terminal skin
inside a browser frame." It should preserve Smith's identity, but the desktop
app should feel like a desktop application first and a terminal homage second.

## Risks and mitigations

### Risk: system WebView variance

Wails depends on the platform WebView. That can produce rendering differences,
especially on Linux.

Mitigation:

- keep the first UI straightforward;
- avoid browser-fragile visual techniques;
- test on the supported platform set early;
- retain Electron as an explicit fallback if WebView variance becomes a real UX
  blocker.

### Risk: two-process lifecycle complexity

Single-process mode removes most of the external runtime supervision complexity,
but it replaces it with a different risk: allowing the Wails bridge to become a
grab bag of direct core calls.

Mitigation:

- keep the desktop adapter narrow and explicit;
- define desktop-facing interfaces instead of binding arbitrary internal
  packages;
- add fixture-driven smoke coverage around session start/stream/approval flows.

### Risk: desktop shell duplicates core logic

A fast UI iteration cycle can tempt product logic into the shell/frontend.

Mitigation:

- keep permissions, session semantics, cost, and tool execution in Smith only;
- treat Wails bindings as desktop adapter hooks, not business logic endpoints;
- do not expose arbitrary core internals just because Wails makes that easy.

### Risk: updater and signing complexity

Desktop packaging is operationally real work.

Mitigation:

- make updater/signing a first-class ticket, not an afterthought;
- ship a manual-install developer preview before automatic updates if needed.

## Release slices

### Slice A: developer-preview desktop shell

- packaged app boots;
- initializes the Smith core through the desktop adapter;
- can open a workspace and run a session;
- streams transcript and shows approvals/tool activity.

### Slice B: usable daily driver for simple interactive work

- session list/resume;
- context/cost rail;
- error/reconnect handling;
- basic settings/runtime status;
- signing and update path defined.

## Acceptance criteria

- [ ] A user can install the desktop app, open a local workspace, and start a
      Smith session without touching a terminal.
- [ ] The desktop app launches or attaches to a local Smith runtime and never
      performs model/tool work itself.
- [ ] Streaming assistant output, tool activity, and approval prompts are all
      visible in one coherent UI.
- [ ] A user can leave and later resume a session from the desktop home screen.
- [ ] The app surfaces basic context and cost state during the session.
- [ ] The implementation preserves the documented face/core layering and does
      not fork Smith semantics into Wails/frontend-only code.

## External research notes

The recommendation above is based on current primary-source documentation
reviewed on 2026-06-30:

- Wails positions itself as a lightweight Electron alternative for Go, supports
  native menus/dialogs/theming, uses the native rendering engine rather than
  bundling a browser, and exposes Go methods to JavaScript.
- Tauri 2 positions itself as a framework for "small, fast, secure" apps,
  supports any web frontend, relies on the system WebView, and documents a
  trust-boundary/capability model plus signed updater metadata.
- Electron documents a bundled Chromium + Node architecture, a multi-process
  model, a larger hardening burden, and built-in updater support only for macOS
  and Windows.
- Fyne documents a broad cross-platform Go widget toolkit with desktop
  packaging, widgets, dialogs, shortcuts, tray support, and app-store guidance.
- Gio documents an immediate-mode cross-platform Go GUI library with few
  dependencies and its own renderer stack.

## Sources

- Tauri 2 home: <https://v2.tauri.app/>
- Tauri "What is Tauri?": <https://v2.tauri.app/start/>
- Tauri security overview: <https://v2.tauri.app/security/>
- Tauri updater plugin: <https://v2.tauri.app/plugin/updater/>
- Wails introduction: <https://wails.io/docs/introduction/>
- Wails "How does it work?": <https://wails.io/docs/howdoesitwork/>
- Wails runtime reference: <https://wails.io/docs/reference/runtime/intro/>
- Electron docs: <https://www.electronjs.org/docs/latest/>
- Electron process model: <https://www.electronjs.org/docs/latest/tutorial/process-model>
- Electron security: <https://www.electronjs.org/docs/latest/tutorial/security>
- Electron autoUpdater: <https://www.electronjs.org/docs/latest/api/auto-updater>
- Fyne docs: <https://docs.fyne.io/started/>
- Gio UI: <https://gioui.org/>
- Claude Code docs index/navigation: <https://code.claude.com/docs/en/mcp>
- AGENTS.md: <https://agents.md/>
