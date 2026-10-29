---
name: SSSNovelStudio
description: A calm, high-density workspace for long-form fiction.
colors:
  canvas: "oklch(20% 0 0)"
  surface: "oklch(26% 0 0)"
  surface-raised: "oklch(37% 0 0)"
  ink: "oklch(96% 0.001 286.375)"
  primary: "oklch(81% 0.117 11.638)"
  info: "oklch(75% 0.10 250)"
  success: "oklch(75% 0.10 150)"
  warning: "oklch(82% 0.14 85)"
  error: "oklch(70% 0.18 25)"
typography:
  title:
    fontFamily: "Inter, Noto Sans SC, ui-sans-serif, system-ui, sans-serif"
    fontSize: "1.25rem"
    fontWeight: 600
    lineHeight: 1.3
  body:
    fontFamily: "Inter, Noto Sans SC, ui-sans-serif, system-ui, sans-serif"
    fontSize: "1rem"
    fontWeight: 400
    lineHeight: 1.65
  label:
    fontFamily: "Inter, Noto Sans SC, ui-sans-serif, system-ui, sans-serif"
    fontSize: "0.875rem"
    fontWeight: 500
    lineHeight: 1.5
rounded:
  control: "0.5rem"
  surface: "0.5rem"
spacing:
  xs: "0.25rem"
  sm: "0.5rem"
  md: "1rem"
  lg: "1.25rem"
components:
  button-primary:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.canvas}"
    rounded: "{rounded.control}"
    height: "2rem"
  panel:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink}"
    rounded: "{rounded.surface}"
    padding: "1rem"
---

# Design System: SSSNovelStudio

## Overview

**Creative North Star: "The Writer's Desk"**

A focused working surface for authors who stay in the application for hours. The interface is restrained and familiar: prose and decisions lead, while system state remains easy to scan. Density is purposeful, not cramped, and visual personality comes from precise rhythm rather than decoration.

**Key Characteristics:** flat tonal layers, one restrained accent, compact controls, readable prose, visible system status, and responsive drawers.

## Colors

The light Sneakystyle palette is normative: a near-white canvas, white cards, and one coral accent (`oklch(81% 0.117 11.638)`) whose content is black. Primary is reserved for the current location and the single main action. Status colour is monochrome by default — success reads as ink, info as grey — while amber (warning) and red (error) keep their hue because they flag a real problem. Inner tiles use a tonal base-100/base-300 wash instead of borders or shadows; ghost chips need an ink wash because base-200 is white. Writing scene-plan chips are feature markers, not statuses: they keep the dark theme's bright pastel fills (`.tint-*`) with black content. The in-prose scene/subscene marker pills have no border: the scene pill is `#4b5563` with white text and the subscene pill a faint ink wash, both at 35% opacity until hovered. Inline critic marks stay translucent highlighter tints (`mark.iv-mark`, one hue per suggestion) and fact marks stay amber, both inheriting the prose ink. Critic reports, toasts, the Writing meta row and the global alerts use the matching `.wash-*` chips: the same mark hues at 35% over white with no alert border, so the UI reads like the highlight on the page. The assistant chat keeps the studio mockup's bubble language, flattened: white right-aligned user bubbles with a hairline `#EAEAEA` border, `#f4f4f5` left-aligned assistant/tool bubbles, a 4px tail corner on the outer edge, no shadows, and a white rounded input with a black Send button (no coral focus outline on the textarea itself). Tool calls, tool results and pending tools are feature markers, not statuses, so they reuse the Writing mark palette: read = blue, generate/revise/suggest = violet, create/confirm = green, update/edit = amber, delete/reset = red, anything else = pink; ordinary replies stay neutral. The Brainstorm panel opens in the same right slot as the chat and log (only one at a time; opening it collapses the agent, and closing it leaves the slot empty) and speaks the same flat, hairline-bordered language. Its Describe modes are a two-column icon grid ordered from the primary senses to the subtle ones, and a finished request collapses the form into one card per option, each with its own Copy, a local token pill, More at the bottom and a back-to-new-request control. Lore, Foreshadows and Proofread cards use generous `p-12` bodies and reveal their icon-only edit/delete actions plus the bottom-right create/submit row only while the card is hovered; the row always reserves its height. Row edit/delete controls are black pills with a white pencil and light-coral trash. Explanations reuse the dark SmartSelect popover (`InfoTip dark`).

## Typography

One sans-serif family serves interface text. Body copy is 16px with relaxed leading; labels use 14px; 12px is reserved for metadata. Prose stays within 75 characters per line.

## Layout

At 1280px and above the application uses navigation, primary workspace, and assistant columns. From 1024px to 1279px the assistant becomes a drawer. Below 1024px both navigation and assistant become drawers; below 768px fixed two-column editors stack into one column. The main workspace always uses `min-width: 0` and independent vertical scrolling. Lore, Outline, Foreshadows and the novel-parameters form share one 52rem reading column; Writing cards use 2.5rem bodies so their sections align. The header title has no truncation cap and may wrap; responsive drawers are positioned within the workspace so header height may grow safely. Writing opens on a single collapsible “Overview & Chapters” card: chapters and act-critique steps in the left 7/12 column, writing progress and the foreshadow tracker in the right 5/12 column. It starts expanded and folds automatically when a chapter or act step is picked; the header row reopens it. The current chapter row uses the mockup's coral `#ffa1ad` (10% fill, bold coral label, solid coral bookmark); accepted chapters keep a solid grey bookmark, pending ones an outline bookmark.

The shell header stacks a slim project bar (centered project name) above a ribbon bar: grouped breadcrumb on the left, step tabs in the center, AI token status on the right, and a 2px progress bar underneath. Pages with steps register them through `frontend/src/lib/ribbon.js`; pages without steps get a single fallback step showing a short section instruction (`app.ribbon.hint.<page>`) with no step number. A 40px toolbar row stays visible under the progress bar on every project page: save on the left, Agent / Brainstorm / Log on the right (Agent toggles the assistant; Log opens the task-log dialog without the chat), step tools in the middle driven by `frontend/src/lib/toolbar.js`; buttons without a backend action explain that instead of faking it. Rail and toolbar icons reuse the mockup's Font Awesome 6.4 glyphs inlined in `frontend/src/lib/icons.js`; no icon font or extra dependency. The project name is centered in the project bar and “Switch / new project” only appears on hover (or focus) next to it without shifting the title. The workspace canvas is `#fbfbfb`; the shell, rail and ribbon stay white with `#eaeaea` separators. The left rail groups links as General / World building / Composing / Analysis and collapses to icons on desktop; below 1024px it becomes a drawer.

## Elevation & Depth

The system is flat. Tonal surfaces, one-pixel borders, spacing, and selected states communicate depth; shadows are not used.

## Shapes

Controls and panels use the existing 0.5rem radius. Pills are limited to compact status badges. Nested panels use tonal contrast without a second border.

## Components

Primary buttons are solid and rare. Ordinary actions are outlined, low-priority actions are ghost buttons, and destructive actions remain outlined error buttons. Inputs use the content surface with a clear accent focus ring. Cards group a real task only; spacing and dividers replace unnecessary nested cards. Navigation is a grouped icon rail (General / World building / Composing / Analysis) with a light selected fill, collapsing to icons on desktop. Critic suggestions keep neutral category/priority chips, put the title on its own line, and reveal their actions only on hover or focus.

## Do's and Don'ts

### Do:
- Do keep the active writing or review surface dominant.
- Do expose loading, error, empty, disabled, focus, and selected states.
- Do use motion only for feedback, drawer state changes, and the graph camera’s 320ms ease-out fit transition; reduced-motion uses the final camera transform immediately.

### Don't:
- Don't add decorative emoji, gradients, shadows, or status colors.
- Don't introduce another component or icon library for this visual system.
- Don't change routes, product terminology, or business behavior for visual novelty.
