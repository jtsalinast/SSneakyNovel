<script>
  // InfoTip — small clickable "ⓘ" icon that toggles a popover explaining the
  // adjacent field/term (~200 chars). Used next to every segmented combobox
  // label in the Novel-parameters form (Genre, Subgenre, Story length, Story
  // structure, Conflict scale, Protagonist type) and inside ComboSelect's
  // rich item rows. Click-outside / Escape closes it.
  //
  // Round 25: the popover is rendered with `position: fixed` at viewport
  // coordinates computed from the trigger button, so it can NEVER be clipped
  // or painted *below* the top bar / side rail / chat panel (previously an
  // absolute popover ended up behind those elements depending on which
  // ancestor created the stacking context).
  import { tick } from 'svelte';

  export let text = '';       // tooltip content (the ~200-char explanation)
  export let label = 'Info';  // aria-label for the trigger button
  // Dark variant matching the SmartSelect / SmartCombobox summary popovers.
  export let dark = false;
  export let width = 240;
  // Optional: when a `trigger` slot is provided, it replaces the default "i"
  // circle as the clickable popover trigger (e.g. the colored trope-issue dots
  // in Writing.svelte). The popover behavior (fixed positioning, click-outside,
  // Escape) is identical either way.

  let open = false;
  let root;
  let popX = 0;
  let popY = 0;

  async function positionPop() {
    await tick();
    if (!root) return;
    const r = root.getBoundingClientRect();
    const W = width;
    const pad = 8;
    const vw = window.innerWidth;
    const vh = window.innerHeight;
    // Horizontal: centered on the icon, clamped inside the viewport.
    popX = Math.min(Math.max(r.left + r.width / 2 - W / 2, pad), vw - W - pad);
    // Vertical: prefer above the icon; flip below when there is no room.
    const hGuess = Math.min(320, 14 + String(text || '').length * 0.62);
    popY = r.top - 8 - hGuess;
    if (popY < pad) popY = r.bottom + 8;
    popY = Math.min(Math.max(popY, pad), Math.max(pad, vh - hGuess - pad));
  }

  function toggle(e) {
    e.preventDefault();
    e.stopPropagation();
    open = !open;
    if (open) positionPop();
  }
  function onDocClick(e) {
    if (open && root && !root.contains(e.target)) open = false;
  }
  function onKeydown(e) {
    if (open && e.key === 'Escape') open = false;
  }
</script>

<svelte:window on:mousedown={onDocClick} on:keydown={onKeydown} />

{#if text}
  <span bind:this={root} class="relative inline-block leading-none align-middle">
    {#if $$slots.trigger}
      <button
        type="button"
        class="info-tip-trigger inline-flex items-center justify-center p-0 border-0 bg-transparent cursor-help select-none leading-none"
        on:click={toggle}
        aria-expanded={open}
        aria-label={label}
        title={label}
      ><slot name="trigger" /></button>
    {:else}
      <button
        type="button"
        class="info-tip-btn inline-flex items-center justify-center w-4 h-4 rounded-full border border-base-content/30 text-[10px] font-bold opacity-60 hover:opacity-100 hover:border-base-content/60 transition-colors cursor-help p-0 select-none"
        class:info-tip-active={open}
        on:click={toggle}
        aria-expanded={open}
        aria-label={label}
        title={label}
      >i</button>
    {/if}
  </span>
{/if}

{#if open && text}
  <span
    role="tooltip"
    style="position: fixed; left: {popX}px; top: {popY}px; width: {width}px; max-width: calc(100vw - 16px);"
    class="z-[9999] rounded-xl border px-3.5 py-3 text-[11px] font-normal leading-snug shadow-2xl text-left whitespace-normal normal-case tracking-normal pointer-events-auto {dark ? 'border-white/10 bg-neutral-900 text-white' : 'border-base-content/20 bg-base-100'}"
  >{#each String(text).split(/\n{2,}/) as para, i}{#if i > 0}<br /><br />{/if}{para}{/each}</span>
{/if}

<style>
  .info-tip-active {
    opacity: 1;
    border-color: var(--fallback-bc, oklch(var(--bc) / 0.6));
    background: color-mix(in oklab, currentColor 12%, transparent);
  }
</style>
