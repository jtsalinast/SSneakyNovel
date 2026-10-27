<script>
  // SegmentedControl — Revolut-style pill selector adapted from the standalone
  // HTML/Alpine prototype to this Svelte + daisyUI/Tailwind app. A track of
  // equal segments with a sliding dark "pill" behind the active one, plus an
  // optional external "Auto" button (lightning icon) that decouples the
  // selection: turning Auto ON keeps the last segment visible but faded;
  // clicking any segment turns Auto OFF.
  //
  // Chrome notes (unification pass):
  //  - NO white card/border wrapper around the control — it sits directly on
  //    the form surface like every other field.
  //  - NO internal header/label: the surrounding grid cell owns the field
  //    label (`<span class="field-label">` in Config.svelte). Rendering one
  //    here too produced the double-label look; `ariaLabel` is now used only
  //    as the accessible name of the segment group.
  //  - Tooltip trigger and popover are IDENTICAL to SmartSelect's: the ⓘ
  //    (question-mark circle) button at the right end opens the same
  //    Revolut-style dark summary popover ("Seleccionados" panel with header,
  //    ✕ close button and one entry showing label + description).
  //
  // API mirrors SmartSelect loosely so it can drop into the Story grid:
  //   options   : [{ value, label, description }] — description is optional,
  //               shown inside the summary popover.
  //   bind:value: the currently selected option value ('' = unset).
  //   autoValue : the option value that represents "auto/random" (e.g. 'random').
  //               While bound value equals autoValue, the control renders in
  //               auto mode and `level` remembers the last explicit pick.
  //   emptyLabel: fallback popover text when nothing is selected (kept for
  //               API compatibility with SmartSelect; the header no longer
  //               shows the selection name).
  //   title     : fallback text for the popover when the option has no
  //               description (typically the field's ⓘ tip).
  //   width     : how much horizontal space the WHOLE CONTROL (the outer
  //               wrapper div, class "flex flex-col gap-2") takes inside its
  //               container (e.g. the Card body grid cell). Accepts any CSS
  //               length/percentage string — '80%', '100%', '320px'… — or a
  //               number of pixels. Default 'fit-content': the control is
  //               only as wide as its content (original behaviour). The
  //               percentage is applied ONCE, on the wrapper; the track then
  //               simply gets `width: stretch` (no `flex: none`) so it fills
  //               whatever leftover space the row has after the Auto button,
  //               divider and ⓘ trigger, with segments sharing it equally.
  import { get } from 'svelte/store';
  import { onMount, tick } from 'svelte';
  import { uiLocale, translate } from '../i18n/index.js';

  export let options = [];
  export let value = '';
  export let autoValue = 'random';
  export let emptyLabel = 'Not specified';
  export let title = '';
  export let disabled = false;
  export let ariaLabel = '';
  export let width = 'fit-content';

  let root;          // outermost wrapper (outside-click detection)
  let rowEl;         // Auto + divider + track + ⓘ row (kept for a11y refs)

  // Normalized CSS length for the `width` prop ('100%', '80%', number → px).
  $: widthCss = typeof width === 'number' ? width + 'px' : (width || 'fit-content');
  // Fixed-width mode (`width` ≠ fit-content): the percentage is applied ONCE,
  // on the OUTER WRAPPER div ("flex flex-col gap-2") via an inline style —
  // this makes the whole control occupy exactly that share of the Card-body
  // grid cell. The row inside it then has a definite width, and the track
  // simply gets `width: stretch` (NO `flex: none`, NO duplicated percentage)
  // so it fills ALL the leftover row space after Auto / divider / ⓘ. Every
  // segment is `flex: 1 1 0` with `min-width: var(--seg-w)` as floor, so the
  // stretched bar splits its width EQUALLY among the segments (proportional
  // segmentation) — pure CSS, zero JS measurement.
  // `fixedMode` records whether we're in that mode (reactive to the `width`
  // prop); nothing else about the layout depends on measurements.
  $: fixedMode = widthCss !== 'fit-content';

  // Longest visible label among the segments — rendered once per segment in
  // the invisible sizer row inside the track, which drives the measured
  // equal-segment width (--seg-w).
  $: longestLabel = segments.reduce(
    (a, o) => ((o.label || '').length >= (a || '').length ? o.label : a),
    '');

  // Track DOM node + measured metrics. The sizer row (invisible, absolutely
  // positioned with `width: max-content`, one cell per segment holding the
  // LONGEST label) shrink-to-fits around n copies of the widest option; we
  // read its width back and divide by the segment count to get the exact
  // equal-segment width (--seg-w), then pin every real button AND the track
  // itself to that measurement (see updateMetrics / trackStyle).
  let trackEl;
  let segW = 0;      // width of one segment (widest label incl. padding)
  let trackH = 0;    // natural height of the track content (button height)

  // Pill geometry. The track is a border-box; the button row inside it is
  // `width: 100%` of the track's CONTENT box, and every button is flex-1 so
  // each one occupies exactly (content width)/N — whether the bar sits at its
  // natural content size or is stretched to fill a fixed-width row. The pill
  // mirrors that with `calc(100%/N)` + `translateX(level*100%)`: percentages
  // resolve against the same content box, so the pill is always exactly one
  // segment wide and lands perfectly under the active label at every step,
  // in both modes, with zero JS measurement involved. (The old measured-px /
  // calc(--seg-track-w) approach misaligned and, when the measurement pass
  // failed, left the bar un-stretched entirely.)
  $: pillStyle =
    'width: calc(100% / ' + (segments.length || 1) + ');' +
    'transform: translateX(calc(' + level + ' * 100%));';

  // Track sizing. In fit-content mode (default) the track is pinned to its
  // natural content width once measured (n × --seg-w), preserving the
  // original behaviour. In fixed-width mode (`width` prop ≠ fit-content) the
  // percentage lives ONLY on the outer wrapper — here the track just gets
  // `width: stretch` so it fills all the leftover row space after Auto /
  // divider / ⓘ, with NO `flex: none` (the default flex-grow lets it expand)
  // and no duplicated percentage anywhere down the tree. The segments' own
  // `flex: 1 1 0` then splits the stretched bar EQUALLY (proportional
  // segmentation); `--seg-w` remains only as a per-segment MINIMUM floor so
  // labels never clip.
  $: trackStyle =
    '--seg-h: ' + trackH + 'px;' +
    (fixedMode
      ? '--seg-w: ' + segW + 'px; width: stretch; min-width: 0; max-width: 100%;'
      : segW
        ? '--seg-w: ' + segW + 'px; width: fit-content; min-width: calc(' + segments.length + ' * var(--seg-w) + 2px); max-width: calc(' + segments.length + ' * var(--seg-w) + 2px);'
        : '--seg-w: 0px; width: max-content;');

  // Localized chrome texts — same keys/pattern SmartSelect uses for its
  // summary panel. `translate()` returns the raw key when it has no entry →
  // guard for that. Reactive on uiLocale.
  function tr(key, fallback) {
    const v = translate(key, undefined, get(uiLocale));
    return v && v !== key ? v : fallback;
  }
  $: selectedTitleText = tr('config.story.selected', 'Seleccionados');
  $: emptySummaryText = tr('config.story.none_selected', 'Sin elementos seleccionados.');

  // Segments shown in the track: every option except the "auto" one — the
  // external Auto button replaces it (as in the prototype).
  $: segments = options.filter((o) => o.value !== autoValue);

  // Index (into `segments`) the pill sits on. Kept separate from `value` so
  // auto mode can hide behind the last manual pick.
  let level = 0;
  let isAuto = value === autoValue || value === '';

  // Re-sync when the bound value changes from outside (load / randomize):
  // an explicit value moves the pill and leaves auto mode; the auto/empty
  // value only flips the mode (the pill stays where the user left it).
  $: if (value === autoValue || value === '') {
    isAuto = true;
  } else {
    const i = segments.findIndex((o) => o.value === value);
    if (i >= 0) {
      level = i;
      isAuto = false;
    }
  }

  $: current = segments[level] || null;

  function selectSegment(idx) {
    if (disabled) return;
    level = idx;
    isAuto = false;
    value = segments[idx].value;
  }

  function toggleAuto() {
    if (disabled) return;
    isAuto = !isAuto;
    if (isAuto) {
      value = autoValue;
    } else if (current) {
      value = current.value;
    }
  }

  // Summary popover state + outside-click handling — same pattern as
  // SmartSelect's toggleTooltip / window listeners.
  let showTooltip = false;
  let pressStartedInside = false;
  function toggleTooltip() {
    showTooltip = !showTooltip;
  }
  function inSelf(t) {
    return !!(t instanceof Element && root && (t === root || root.contains(t)));
  }
  function onDocMouseDown(e) {
    const t = e.target;
    if (!(t instanceof Element)) { pressStartedInside = false; return; }
    if (inSelf(t)) { pressStartedInside = true; return; }
    pressStartedInside = false;
    if (showTooltip) showTooltip = false;
  }
  function onDocClick(e) {
    const t = e.target;
    if (!(t instanceof Element)) return;
    if (inSelf(t)) return;
    if (pressStartedInside) { pressStartedInside = false; return; }
    if (showTooltip) showTooltip = false;
  }

  // Popover body text: the option's description, falling back to the field's
  // ⓘ tip (title) — mirrors what SmartSelect lists per entry.
  $: summaryText = current
    ? current.description || title || current.label
    : title || emptyLabel;

  // Measure the track after layout and pin exact equal segment widths. The
  // sizer row is absolutely positioned with `width: max-content`, so it
  // shrink-to-fits around n copies of the longest label (whitespace-nowrap →
  // one line) while contributing NO height to the track. We divide its width
  // by the segment count to get --seg-w, then:
  //   - pin every real button to `width: var(--seg-w)` (not flex-1), so all
  //     segments are exactly as wide as the widest option and none wraps;
  //   - pin the TRACK itself to n × --seg-w (see `trackStyle`), fixing the
  //     pre-measurement collapse where a `w-fit` track with flex-1 buttons
  //     shrank to min-content and the bar rendered far too narrow (the
  //     "Target audience" width bug).
  function updateMetrics() {
    if (!trackEl) return;
    const sizer = trackEl.querySelector('.seg-sizer');
    if (!sizer || !segments.length) return;
    // Measure the WHOLE sizer row (n identical nowrap cells laid out at their
    // full intrinsic size — they can never be squeezed because the row is
    // `width: max-content`) and divide by N. Measuring a single cell instead
    // was fragile: if the browser ever resolved the row against a constrained
    // containing block, the first cell would report a shrunk width and every
    // segment inherited the error.
    const w = Math.ceil(sizer.getBoundingClientRect().width / segments.length);
    if (w > 0 && Math.abs(w - segW) > 0.5) segW = w;
    const btn = trackEl.querySelector('.seg-btn');
    if (btn) {
      const h = Math.ceil(btn.getBoundingClientRect().height);
      if (h > 0 && Math.abs(h - trackH) > 0.5) trackH = h;
    }
  }

  // (Removed: updateRowWidth / ResizeObserver-on-row machinery. The fixed-
  // width layout is now a single `width: widthCss` on the track — nothing to
  // re-measure when the container resizes; percentages re-resolve natively.)

  // Re-measure whenever options/labels/locale change (after DOM update) so
  // --seg-w (the per-segment MINIMUM floor + fit-content pin) stays exact.
  $: if (trackEl && options) {
    options; uiLocale; value; widthCss;
    tick().then(updateMetrics);
  }

  onMount(() => {
    updateMetrics();
  });
</script>

<svelte:window on:mousedown={onDocMouseDown} on:click={onDocClick} />

<div
  bind:this={root}
  class="flex flex-col gap-2"
  class:opacity-60={disabled}
  style={fixedMode ? 'width: ' + widthCss + ';' : undefined}
>
<!-- AUTO BUTTON (left) | separator | SEGMENTED CONTROL | TOOLTIP TRIGGER (right).
     No internal field label here — the grid cell above already renders it
     (double-label fix); `ariaLabel` only names the group for screen readers.
     The row wrapper carries BOTH Tailwind's `group` (used by the smart
     selects' own triggers) and a component-scoped `seg-group` class: Svelte
     scopes every selector in <style>, so an ancestor like `.group` gets its
     own scope hash — which only matches if it also appears in this template.
     `seg-group` guarantees that match, making the hover-reveal reliable.
     NOTE: the `width` prop is applied exactly ONCE, on the OUTER WRAPPER div
     above (`style="width: {widthCss}"`). The row stays a plain stretch flex
     box (it fills the wrapper's definite width), and the track simply gets
     `width: stretch` — so percentages never duplicate down nested boxes. -->
<div bind:this={rowEl} class="seg-group group flex items-stretch gap-1" role="group" aria-label={ariaLabel}>
  <!-- External button: Auto, on the LEFT of the segment bar. Its chrome
       mirrors the white track surface exactly (same rounded-2xl corners,
       neutral border and soft card shadow) so both read as one unit.
       `shrink-0 flex-none` keeps it content-sized; in fixed-width mode the
       leftover row space goes entirely to the track (`width: stretch`). -->
  <button
    type="button"
    on:click={toggleAuto}
    aria-pressed={isAuto}
    disabled={disabled}
    title={'Auto'}
    class={'seg-auto shrink-0 flex-none flex items-center gap-1.5 rounded-2xl border border-neutral-200/80 bg-white px-3 text-xs font-semibold transition-all duration-150 focus:outline-none ' +
      (isAuto ? '!border-neutral-900 !bg-neutral-900 !text-white' : 'text-neutral-600 hover:bg-neutral-50 hover:text-neutral-900')}
  >
    <svg class="h-3.5 w-3.5" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24">
      <path stroke-linecap="round" stroke-linejoin="round" d="M13 10V3L4 14h7v7l9-11h-7z"></path>
    </svg>
    <span>Auto</span>
  </button>

  <!-- Vertical separator between the Auto button and the segment bar.
       Drawn as an explicit scoped rule (`.seg-divider::before`) instead of
       Tailwind's `border-l` utility: utilities are generated on demand and
       this one was being purged, so the line silently disappeared. A plain
       div also has zero intrinsic width in flex layout, which is why the
       visible 1px line lives on a pseudo-element. The line color is a
       base-content mix (as used across app.css surfaces), NOT a hardcoded
       light hairline — on the default dark theme a fixed
       near-white 90%-opacity line was invisible against the dark card
       surface, which is exactly why the separator "never showed". -->
  <div class="seg-divider self-stretch" aria-hidden="true"></div>

  <!-- Track sizing (see `trackStyle`): in fit-content mode it is pinned to
       exactly n × --seg-w once measured; in fixed-width mode the WRAPPER has
       a definite width and the track simply gets `width: stretch` — no
       `flex: none` — so it absorbs ALL the leftover row space after the Auto
       button / divider / ⓘ trigger, with the surplus shared equally by the
       segments' own flex-1 layout. Surface matches the Auto button (white
       fill, neutral border, soft shadow). -->
  <div bind:this={trackEl} class="seg-track relative inline-flex min-w-0 items-center overflow-hidden rounded-2xl border border-neutral-200/80 bg-white" style={trackStyle}>
    <!-- Active background pill (fades out during Auto mode). It sits UNDER
         the button row: the track's white bg + border paint first, then the
         black pill (z-index 0), then the transparent buttons with their text
         (z-10) — so the selection is a clean black area with white label on
         top, never covering or bleeding over the bar's border. Width/step are
         the measured --seg-w (see `pillStyle`), guaranteeing one exact
         segment per step; it carries the SAME `rounded-2xl` radius as the bar
         and the Auto button, and the track's `overflow-hidden` clips its
         corners to the bar radius at the ends (same height as the Auto
         button). -->
    <div
      class="seg-pill pointer-events-none absolute z-0 rounded-2xl transition-all duration-200 ease-out"
      style={'background-color: ' + (isAuto ? 'rgba(212,212,212,0.5)' : 'rgb(23 23 23)') + '; opacity: ' + (isAuto ? 0 : 1) + ';' + pillStyle}
    ></div>

    <!-- Visible buttons: nowrap → a long label like "very explicit" can
         never wrap. In FIXED-WIDTH mode the button row spans `width: 100%`
         of the track's definite width and every segment is `flex: 1 1 0`
         (min-width floor = --seg-w), so the bar's full width is split
         EQUALLY among the segments — proportional segmentation. In
         fit-content mode each button is pinned to exactly --seg-w (the
         widest label — the sizer guarantees n × --seg-w) and the row height
         is pinned to --seg-h so nothing reflows after measuring. No inner
         rounding: the black pill underneath carries the bar's `rounded-2xl`
         radius, so the selection reads as one piece with the bar. -->
    <div class="relative z-10 flex items-stretch" style={fixedMode ? 'width: 100%;' : (segW ? 'height: var(--seg-h);' : '')}>
      {#each segments as item, idx}
        <button
          type="button"
          on:click={() => selectSegment(idx)}
          disabled={disabled}
          aria-pressed={!isAuto && level === idx}
          class="seg-btn min-w-0 whitespace-nowrap px-5 py-2 text-center text-xs font-semibold transition-colors duration-150 focus:outline-none"
          style={fixedMode ? 'flex: 1 1 0; min-width: var(--seg-w);' : segW ? 'width: var(--seg-w); flex: none;' : 'flex: 1 1 0%;'}
          class:text-white={!isAuto && level === idx}
          class:text-neutral-500={isAuto || level !== idx}
        >{item.label}</button>
      {/each}
    </div>

    <!-- Invisible sizing row: absolutely positioned (contributes NO height),
         `width: max-content` makes it shrink-to-fit around one cell per
         segment holding the longest label — same padding/font as the
         buttons. JS divides its width by the segment count to get --seg-w
         and pins both the buttons AND the track to that value. -->
    <div aria-hidden="true" class="seg-sizer invisible pointer-events-none absolute top-1 bottom-1 left-0 flex items-stretch">
      {#each segments as _}
        <span class="flex items-center justify-center whitespace-nowrap px-5 py-2 text-xs font-semibold">{longestLabel}</span>
      {/each}
    </div>
  </div>

  <!-- Tooltip trigger + Revolut-style dark summary popover, identical to
       SmartSelect's (question-circle icon and "Seleccionados" panel), placed
       at the RIGHT end of the segment bar — LAST row child, content-sized,
       right after the track. Shares the global `.tip-trigger` style (app.css):
       hidden by default, it fades IN/OUT with a smooth opacity transition
       while the pointer is over this control (`group` wrapper above) or while
       it has keyboard focus — the element stays mounted, so the fade-out
       actually plays (the ⓘ itself is opacity-only, so its box always
       measures). -->
  <div class="seg-tip-wrap relative shrink-0 flex-none">
    <button
      type="button"
      on:click|stopPropagation={toggleTooltip}
      class="tip-trigger flex items-center justify-center rounded-lg p-1.5
             text-neutral-400 hover:bg-neutral-200/50 hover:text-neutral-700"
      title="Resumen de selección"
      aria-label="Resumen de selección"
    >
      <svg class="h-4 w-4" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"></path></svg>
      {#if String(value || '').trim() && !isAuto}
        <span class="absolute -right-1 -top-1 flex h-3.5 w-3.5 items-center justify-center rounded-full bg-primary text-[8px] font-bold text-primary-content">1</span>
      {/if}
    </button>

    {#if showTooltip}
      <div
        class="animate-pop absolute right-0 z-50 mt-2 w-72 rounded-xl border border-white/10 bg-neutral-900 p-3.5 text-xs text-white shadow-2xl"
        on:mousedown|stopPropagation={() => {}}
      >
        <div class="mb-2 flex items-center justify-between border-b border-white/10 pb-2 text-[11px] font-semibold uppercase tracking-wider text-neutral-200">
          <span>{selectedTitleText} ({String(value || '').trim() && !isAuto ? 1 : 0})</span>
          <button type="button" on:click={() => { showTooltip = false; }} class="text-neutral-400 hover:text-white">&times;</button>
        </div>
        {#if isAuto || !current}
          <p class="py-2 text-center text-[11px] italic text-neutral-400">{emptySummaryText}</p>
        {:else}
          <div class="flex flex-col gap-0.5 px-0.5 py-1">
            <span class="text-xs font-medium text-neutral-100">{current.label}</span>
            <span class="text-[10px] leading-tight text-neutral-400">{summaryText}</span>
          </div>
        {/if}
      </div>
    {/if}
  </div>
</div>
</div>

<style>
  /* Content-width equal segments. An absolutely-positioned invisible sizer
     row (flex, shrink-to-fit around n copies of the longest nowrap label)
     drives the track's width; JS measures it and pins every button to the
     exact same width (--seg-w), so all segments are equal — the width of
     the longest option — labels never wrap ("very explicit" stays on one
     line) and the whole bar is only as wide as its content. */
  .seg-sizer {
    z-index: 0;
    width: max-content;   /* shrink-to-fit despite absolute positioning */
  }
  /* Fixed-width mode lives entirely in inline styles: `width: {widthCss}` on
     the outer wrapper div (the ONE place the percentage appears) +
     `width: stretch` on .seg-track (no `flex: none`) + `flex: 1 1 0` per
     segment. No `.seg-fill` spacer and no `!important` stretch overrides. */
  /* The sliding pill sits UNDER the button row (see z-index below) and is
     sized/positioned in JS from the measured segment width (--seg-w):
     `width: var(--seg-w)` + `transform: translateX(level * --seg-w)` keeps it
     exactly one segment wide and perfectly aligned with the active label at
     every step — a percentage width against the track's border-box used to be
     wider than a segment by the border allowance, which misaligned the black
     selection on multi-segment bars. It still carries the SAME `rounded-2xl`
     radius as the bar and the Auto button; on the first/last segments its
     outer corners coincide with the bar's corners, and the track's
     `overflow: hidden` clips them to the bar radius so the selection reads as
     one piece with the bar. */
  .seg-pill {
    top: 0;
    right: auto;
    bottom: 0;
    left: 0;
    transform-origin: left center;
    /* Same radius as `.seg-track` / `.seg-auto` (Tailwind rounded-2xl =
       var(--radius-2xl), 1rem) — reference the token directly so the pill,
       the bar and the Auto button round identically even if the theme ever
       changes that value. */
    border-radius: var(--radius-2xl, 1rem);
  }

  /* Track surface: white fill + neutral border (set inline via classes) with
     the SAME soft shadow as the Auto button — both are declared here so they
     match pixel-for-pixel regardless of Tailwind utility availability.
     `overflow: hidden` clips anything at the bar's radius; the pill already
     shares that exact radius, so nothing gets cut awkwardly. */
  .seg-track {
    overflow: hidden;
    box-shadow: 0 1px 2px 0 rgb(0 0 0 / 0.05);   /* shadow-sm, same as .seg-auto */
  }
  .seg-auto {
    box-shadow: 0 1px 2px 0 rgb(0 0 0 / 0.05);   /* shadow-sm, same as .seg-track */
  }

  /* Vertical separator between the Auto button and the segment bar. The
     wrapper div itself stays widthless (a plain div collapses to 0 width in
     flex layout), so the visible 1px hairline is drawn by a pseudo-element
     centered inside it — guaranteeing it renders regardless of whether the
     Tailwind `border-l` utility happens to be generated. `py-1.5`-style
     breathing room comes from the pseudo-element's own inset. */
  .seg-divider {
    position: relative;
    flex: none;
    width: 0.5rem;               /* slim gap — keeps Auto and the bar visually close */
  }
  .seg-divider::before {
    content: '';
    position: absolute;
    left: 50%;
    top: 6px;
    bottom: 6px;
    width: 1px;
    margin-left: -0.5px;
    /* Theme-aware hairline: subtle base-content mix on BOTH themes —
       ~12% white on the default dark theme, ~12% black on xianii-light.
       A hardcoded light color was invisible against the dark surface
       (the "separator never shows" bug). */
    background-color: color-mix(in oklch, var(--color-base-content) 12%, transparent);
  }
  /* Pop-in for the summary popover — identical keyframes/class as SmartSelect's
     scoped style (Tailwind v4 has no global `animate-pop` utility). */
  @keyframes popIn {
    0% { opacity: 0; transform: scale(0.97) translateY(-2px); }
    100% { opacity: 1; transform: scale(1) translateY(0); }
  }
  .animate-pop {
    animation: popIn 0.15s cubic-bezier(0.16, 1, 0.3, 1) forwards;
  }

  /* Tooltip trigger visibility lives in app.css as a GLOBAL `.tip-trigger`
     rule shared by SmartSelect, SmartCombobox and this control: hidden
     (opacity 0) until the pointer hovers the enclosing `group`, then fades
     IN/OUT with the same 200ms opacity transition everywhere. */
</style>
