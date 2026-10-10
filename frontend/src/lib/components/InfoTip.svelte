<script>
  // InfoTip — small clickable "ⓘ" icon that toggles a popover explaining the
  // adjacent field/term (~200 chars). Used next to every segmented combobox
  // label in the Novel-parameters form (Genre, Subgenre, Story length, Story
  // structure, Conflict scale, Protagonist type) and inside ComboSelect's
  // rich item rows. Click-outside / Escape closes it.
  import { onDestroy } from 'svelte';

  export let text = '';       // tooltip content (the ~200-char explanation)
  export let label = 'Info';  // aria-label for the trigger button

  let open = false;
  let root;

  function toggle(e) {
    e.preventDefault();
    e.stopPropagation();
    open = !open;
  }
  function onDocClick(e) {
    if (open && root && !root.contains(e.target)) open = false;
  }
  function onKeydown(e) {
    if (open && e.key === 'Escape') open = false;
  }
  onDestroy(() => {}); // svelte:window handlers are torn down automatically
</script>

<svelte:window on:mousedown={onDocClick} on:keydown={onKeydown} />

{#if text}
  <span bind:this={root} class="relative inline-block leading-none align-middle">
    <button
      type="button"
      class="info-tip-btn inline-flex items-center justify-center w-4 h-4 rounded-full border border-base-content/30 text-[10px] font-bold opacity-60 hover:opacity-100 hover:border-base-content/60 transition-colors cursor-help p-0 select-none"
      class:info-tip-active={open}
      on:click={toggle}
      aria-expanded={open}
      aria-label={label}
      title={label}
    >i</button>
    {#if open}
      <span
        role="tooltip"
        class="info-tip-pop absolute z-40 left-1/2 -translate-x-1/2 bottom-full mb-1.5 w-56 max-w-[70vw] rounded-md border border-base-content/20 bg-base-100 px-2.5 py-2 text-[11px] font-normal leading-snug shadow-lg text-left whitespace-normal normal-case tracking-normal"
      >{text}</span>
    {/if}
  </span>
{/if}

<style>
  .info-tip-active {
    opacity: 1;
    border-color: var(--fallback-bc, oklch(var(--bc) / 0.6));
    background: color-mix(in oklab, currentColor 12%, transparent);
  }
</style>
