# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Stack

Tauri 2 (Rust) desktop shell with Svelte + Vite screens, inside the Crush repository under `desktop/`. Chosen by the owner on 2026-10-18.

## Users

One person: the owner, a software-engineering student who uses Crush heavily across many projects with several AI tools (Claude, Codex, Grok, OpenCode, AGY and API models such as Abacus), often with several chats at once, and sometimes from the phone.

## Product Purpose

Crush Desktop is a desktop window for Crush that does everything the terminal app does and shares the same chats with it, live. It adds what a terminal can't: branching a chat from highlighted words, drawn visuals (diagrams, math, tables, images), and mouse-first navigation. Success means the owner can use either app on any chat at any time without losing work or features.

## Positioning

The owner branches a chat from a highlighted phrase with each AI tool's own memory copied exactly up to that point, while the terminal app stays live on the same chat.

## Operating Context

Daily use on a Linux desktop (KDE Plasma, high-refresh display) next to the terminal app, on the same projects under `~/dev`. Phone sharing over Tailscale. A Mac build for occasional use.

## Capabilities and Constraints

- Every terminal-app feature listed in `desktop/CHECKLIST.md`, except the exceptions the owner approved (`desktop/PLAN.md` section 12).
- Each chat always shows its model, the context used and the usage limits.
- One engine per project does the AI work for both apps.
- Branches keep each AI tool's memory exact where the tool allows it; otherwise a written copy.
- On a Mac, the background-jobs list and crash clean-up are Linux-only, as in the terminal app.

## Brand Commitments

- Name: Crush. Wordmark: "CRUSH" in heavy extended capitals with a gray "AI", as in `desktop/design/style-2.png`; on the Scholar's edition paper it is set in iron black.
- The Scholar's edition look of `desktop/.impeccable/mocks/decision/edition.png` is binding (chosen 2026-10-18): bone paper, iron-black text in a book serif, small-caps labels, one-pixel hairlines with small + corners, and a thin vermilion rail of letter keys where a chat branches or tools ran. It replaced the charcoal and amber of `desktop/design/style-2.png`, which stays as an extra theme.
- Banned: purple-to-blue gradients, glowing or glassy effects, emoji as icons, green as the main color, everything wrapped in rounded cards, generic AI sparkle icons, text or icons a few pixels off.
- The terminal app's key shortcuts are kept wherever they make sense.

## Evidence on Hand

- `desktop/.impeccable/mocks/decision/edition.png`: the approved mockup (2026-10-18).
- `desktop/design/style-2.png`: the earlier mockup (charcoal and amber).
- The terminal app's five built-in themes and the user theme files.
- The planning demos in `~/dev/crush-gui-demos` (behavior reference only).

## Product Principles

1. Nothing the terminal app does is missing.
2. Both apps show one truth: the same chat, live.
3. Tool work folds away so the conversation leads.
4. A branch remembers exactly what came before the cut.
5. Everything is aligned, compact and consistent.

## Accessibility & Inclusion

Full keyboard use, visible focus, and readable contrast in every theme.
