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
  //  - The header shows the field label (same `.field-label` style the rest of
  //    the form uses) followed by the current selection name (+ AUTO badge).
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
  import { get } from 'svelte/store';
  import { onMount, onDestroy, tick } from 'svelte';
  import { uiLocale, translate } from '../i18n/index.js';

  export let options = [];
  export let value = '';
  export let autoValue = 'random';
  export let emptyLabel = 'Not specified';
  export let title = '';
  export let disabled = false;
  export let ariaLabel = '';

  let root;          // outermost wrapper (outside-click detection)
  let ro = null;     // ResizeObserver on the track (re-measure on layout shifts)

  // Longest visible label among the segments — rendered once per segment in
  // the invisible sizer row inside the track, which drives the measured
  // equal-segment width (--seg-w).
  $: longestLabel = segments.reduce(
    (a, o) => ((o.label || '').length >= (a || '').length ? o.label : a),
    '');

  // Track DOM node + measured metrics. The sizer row (invisible, one cell per
  // segment holding the LONGEST label) drives the track's shrink-to-fit
  // width; we then read it back and pin the real buttons to exact equal
  // widths so no label can ever wrap or overflow (see updateMetrics).
  let trackEl;
  let segW = 0;      // width of one segment (widest label incl. padding)
  let trackH = 0;    // natural height of the track content (button height)

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
  // sizer row is absolutely positioned with `width: auto`, so it shrink-to-
  // fits around n copies of the longest label (whitespace-nowrap → one line)
  // while contributing NO height. We read its width back, divide by the
  // segment count and expose it as --seg-w; the real buttons then use a hard
  // `width: var(--seg-w)` (not flex-1), which guarantees every segment is
  // exactly as wide as the widest option and no label ever wraps.
  function updateMetrics() {
    if (!trackEl) return;
    const sizer = trackEl.querySelector('.seg-sizer');
    if (!sizer || !segments.length) return;
    const w = Math.ceil(sizer.getBoundingClientRect().width / segments.length);
    if (w > 0 && Math.abs(w - segW) > 0.5) segW = w;
    const btn = trackEl.querySelector('.seg-btn');
    if (btn) {
      const h = Math.ceil(btn.getBoundingClientRect().height);
      if (h > 0 && Math.abs(h - trackH) > 0.5) trackH = h;
    }
  }

  // Re-measure whenever options/labels/locale change (after DOM update).
  $: if (trackEl && options) {
    options; uiLocale; value;
    tick().then(updateMetrics);
  }

  onMount(() => {
    updateMetrics();
    if (typeof ResizeObserver !== 'undefined' && trackEl) {
      ro = new ResizeObserver(updateMetrics);
      ro.observe(trackEl);
    }
  });

  onDestroy(() => {
    if (ro) { ro.disconnect(); ro = null; }
  });
</script>

<svelte:window on:mousedown={onDocMouseDown} on:click={onDocClick} />

<div
  bind:this={root}
  class="flex flex-col gap-2"
  class:opacity-60={disabled}
>
<!-- HEADER: field label only (the selection name is conveyed by the pill
     itself). The tooltip trigger moved to the right of the segment bar. -->
<div class="field-label">{ariaLabel}</div>

<!-- AUTO BUTTON (left) | separator | SEGMENTED CONTROL | TOOLTIP TRIGGER (right).
     The row wrapper carries BOTH Tailwind's `group` (used by the smart
     selects' own triggers) and a component-scoped `seg-group` class: Svelte
     scopes every selector in <style>, so an ancestor like `.group` gets its
     own scope hash — which only matches if it also appears in this template.
     `seg-group` guarantees that match, making the hover-reveal reliable. -->
<div class="seg-group group flex items-stretch gap-2">
  <!-- External button: Auto, on the LEFT of the segment track. Its chrome
       mirrors the white track surface exactly (same rounded-2xl corners,
       neutral border and soft card shadow) so both read as one unit. -->
  <button
    type="button"
    on:click={toggleAuto}
    aria-pressed={isAuto}
    disabled={disabled}
    title={'Auto'}
    class={'seg-auto shrink-0 flex items-center gap-1.5 rounded-2xl border border-neutral-200/80 bg-white px-3 text-xs font-semibold transition-all duration-150 focus:outline-none ' +
      (isAuto ? '!border-neutral-900 !bg-neutral-900 !text-white' : 'text-neutral-600 hover:bg-neutral-50 hover:text-neutral-900')}
  >
    <svg class="h-3.5 w-3.5" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24">
      <path stroke-linecap="round" stroke-linejoin="round" d="M13 10V3L4 14h7v7l9-11h-7z"></path>
    </svg>
    <span>Auto</span>
  </button>

  <!-- Vertical separator between the Auto button and the segment bar —
       restored from the prototype (`border-l border-neutral-200/80` with a
       little breathing room on each side). -->
  <div class="self-stretch border-l border-neutral-200/80 px-1.5 py-1.5"></div>

  <!-- Track sized to its content: the invisible sizer row below shrink-to-
       fits around n copies of the LONGEST label; we measure it in JS and pin
       every real button to exactly that width (--seg-w), so all segments are
       equal, none ever wraps, and the bar is only as wide as it needs to be.
       Surface matches the Auto button (white fill, neutral border, soft
       shadow). -->
  <div bind:this={trackEl} class="seg-track relative inline-flex w-fit items-center overflow-hidden rounded-2xl border border-neutral-200/80 bg-white" style="--seg-count: {segments.length}; --seg-w: {segW}px; --seg-h: {trackH}px;">
    <!-- Active background pill (fades out during Auto mode); width/step come
         from the measured segment width (--seg-w). NO visible border/gap
         between the selected black pill and the bar: `.seg-pill` fills the
         whole track box exactly (inset-0 over the border-box, width 1/N)
         and carries the SAME `rounded-2xl` radius as the bar and the Auto
         button — the pill's outer corners coincide with the bar's corners,
         so the selection keeps the identical rounding while reading as one
         piece with the bar (same height as the Auto button). The track's
         `overflow-hidden` additionally clips the pill's trailing corners to
         the bar radius when it sits on the last segment. -->
    <div
      class="seg-pill pointer-events-none absolute rounded-2xl transition-all duration-200 ease-out"
      style={'background-color: ' + (isAuto ? 'rgba(212,212,212,0.5)' : 'rgb(23 23 23)') + '; transform: translateX(calc(' + level + ' * 100%)); opacity: ' + (isAuto ? 0 : 1) + ';'}
    ></div>

    <!-- Visible buttons: fixed height (measured from the natural single-line
         layout) + exact equal widths (--seg-w) + nowrap → a long label like
         "very explicit" can never wrap or grow the track. No inner rounding:
         the black pill underneath carries the bar's `rounded-2xl` radius, so
         the selection reads as one piece with the bar. Before the first
         measurement they fall back to flex-1 so the sizer can still drive
         the initial shrink-to-fit width. -->
    <div class="relative z-10 flex items-stretch" style={segW ? 'height: var(--seg-h)' : ''}>
      {#each segments as item, idx}
        <button
          type="button"
          on:click={() => selectSegment(idx)}
          disabled={disabled}
          aria-pressed={!isAuto && level === idx}
          class="seg-btn whitespace-nowrap px-3 py-2 text-center text-xs font-semibold transition-colors duration-150 focus:outline-none"
          style={segW ? 'width: var(--seg-w); flex: none;' : 'flex: 1 1 0%;'}
          class:text-white={!isAuto && level === idx}
          class:text-neutral-500={isAuto || level !== idx}
        >{item.label}</button>
      {/each}
    </div>

    <!-- Invisible sizing row: absolutely positioned (contributes NO height),
         `left/right: auto` makes it shrink-to-fit around one cell per
         segment holding the longest label — same padding/font as the
         buttons — so the track reserves exactly n × widest-option width. -->
    <div aria-hidden="true" class="seg-sizer invisible pointer-events-none absolute top-1 bottom-1 left-0 flex items-stretch">
      {#each segments as _}
        <span class="flex items-center justify-center whitespace-nowrap px-3 py-2 text-xs font-semibold">{longestLabel}</span>
      {/each}
    </div>
  </div>

  <!-- Tooltip trigger + Revolut-style dark summary popover, identical to
       SmartSelect's (question-circle icon and "Seleccionados" panel), placed
       at the RIGHT end of the segment bar. Shares the global `.tip-trigger`
       style (app.css): hidden by default, it fades IN/OUT with a smooth
       opacity transition while the pointer is over this control (`group`
       wrapper above) or while it has keyboard focus — the element stays
       mounted, so the fade-out actually plays. -->
  <div class="relative shrink-0">
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
  /* The sliding pill occupies exactly ONE segment and slides across the
     bar: it fills the whole track box edge-to-edge (inset-0 over the
     border-box → width 1/N via --seg-count, no padding gap and no inner
     border between the black selection and the bar), and it carries the
     SAME `rounded-2xl` radius as the bar and the Auto button. Because the
     pill's own corners coincide with the bar's corners, the selected black
     area keeps the identical rounding instead of being a square-cut block.
     The inline `transform: translateX(level * 100%)` shifts it by whole
     segment steps. */
  .seg-pill {
    top: 0;
    right: auto;
    bottom: 0;
    left: 0;
    width: calc(100% / var(--seg-count, 1));
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
