<script>
  // Per-field wand "Enhance" button (user request): the SAME control that used
  // to live only inside the Differentiating-idea box now appears on EVERY
  // "Novel parameters" textarea AND on the Tone input. The parent renders it
  // while its field has focus (or while its own enhance generation is in
  // flight), and each instance works INDEPENDENTLY: clicking it sends that
  // field's draft through the chat pipeline as
  //   [generate-section:<field>;enhance=<urlencoded draft>]
  // which the backend routes to enhanceStoryField (rewrites ONLY that field).
  //
  // NOTE ON STYLES: this component deliberately ships NO scoped <style> block.
  // All of its visual classes (.appeal-enhance-btn, .magic-border-glow,
  // .aurora-stop*, .star-tip-linear, ...) are defined globally in
  // Config.svelte's non-scoped style block — the same rules the original
  // appeal-box wand uses — so every mount renders identically and the aurora
  // animations stay in sync. The SVG gradient id embeds `field` so multiple
  // mounted wands never collide on duplicate DOM ids.
  // IMPORTANT (Svelte 4): the click contract is a REAL event, not an
  // `onclick` prop. Parents MUST listen with `on:click` on this component —
  // passing `onclick={handler}` as a plain attribute/prop was one of the
  // reasons the wands never fired (see Config.svelte fix notes). We forward
  // the native click through a <svelte:component this="button">-free
  // dispatch() so both patterns keep working during migration, but the
  // canonical wiring is `on:click`.
  import { createEventDispatcher } from 'svelte';
  export let field = '';
  export let busy = false;
  export let flash = false;
  export let label = 'Enhance';
  // Legacy prop kept ONLY as a fallback so any stale `onclick={fn}` call site
  // still works; new code uses the forwarded `click` event instead.
  export let onclick = null;
  const dispatch = createEventDispatcher();
  function handleClick(e) {
    if (typeof onclick === 'function') onclick(e);
    dispatch('click', { field });
  }
</script>

<button type="button" class="appeal-enhance-btn" class:clicked={flash} class:busy={busy}
  disabled={busy} aria-label={label} title={label}
  on:click={handleClick}>
  <span class="magic-border-glow"></span>
  <span class="btn-content">
    <span class="icon-container">
      {#if busy}
        <span class="loading loading-spinner loading-xs"></span>
      {:else}
        <svg class="wand-svg" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
          <defs>
            <linearGradient id={'wandAuroraCfg-' + field} x1="0%" y1="0%" x2="100%" y2="100%">
              <stop offset="0%" class="aurora-stop1" />
              <stop offset="50%" class="aurora-stop2" />
              <stop offset="100%" class="aurora-stop3" />
            </linearGradient>
          </defs>
          <g class="wand-body">
            <path d="M4.5 19.5L12.5 11.5" stroke="url(#wandAuroraCfg-{field})" stroke-width="2.2" stroke-linecap="round"/>
            <path d="M12.5 11.5L15 9" stroke="url(#wandAuroraCfg-{field})" stroke-width="2.2" stroke-linecap="round"/>
            <path d="M12 12L13 11" stroke="url(#wandAuroraCfg-{field})" stroke-width="1.6" stroke-linecap="round"/>
            <path class="star-tip-linear" d="M17 1.5L18.4 4.8L21.8 5.2L19.2 7.5L20 10.8L17 9.1L14 10.8L14.8 7.5L12.2 5.2L15.6 4.8L17 1.5Z" stroke="url(#wandAuroraCfg-{field})" stroke-width="1.5" stroke-linejoin="round" fill="#FAFAFA"/>
            <path class="particle-filled-1" d="M5 4.5L5.5 5.8L6.8 6.2L5.8 7.1L6.1 8.3L5 7.6L3.9 8.3L4.2 7.1L3.2 6.2L4.5 5.8L5 4.5Z" fill="url(#wandAuroraCfg-{field})"/>
            <path class="particle-filled-2" d="M20 13.5L20.4 14.5L21.5 14.8L20.7 15.5L20.9 16.5L20 16L19.1 16.5L19.3 15.5L18.5 14.8L19.6 14.5L20 13.5Z" fill="url(#wandAuroraCfg-{field})"/>
            <circle cx="10" cy="4" r="1" fill="url(#wandAuroraCfg-{field})" />
          </g>
        </svg>
      {/if}
    </span>
    <span class="enhance-label">{label}</span>
  </span>
</button>
