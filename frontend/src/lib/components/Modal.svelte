<script>
  // Native <dialog> modal used by the Lore "New ..." forms: focus trap,
  // Escape and backdrop click come from the browser; the page only manages
  // the `open` flag.
  import { createEventDispatcher } from 'svelte';

  export let open = false;
  export let title = '';
  export let wide = false;

  const dispatch = createEventDispatcher();
  let dialog;

  $: if (dialog) {
    if (open && !dialog.open) dialog.showModal();
    else if (!open && dialog.open) dialog.close();
  }

  function onBackdrop(e) {
    if (e.target === dialog) dispatch('close');
  }
</script>

<!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
<!-- svelte-ignore a11y-click-events-have-key-events -->
<dialog bind:this={dialog} class="modal" on:cancel|preventDefault={() => dispatch('close')} on:close={() => dispatch('close')} on:click={onBackdrop}>
  <div class="modal-box max-w-2xl" class:max-w-3xl={wide}>
    <div class="flex items-center justify-between gap-2 mb-3">
      <h3 class="text-base font-semibold">{title}</h3>
      <div class="flex items-center gap-1.5">
        <slot name="actions" />
        <button type="button" class="btn btn-ghost btn-sm px-2" aria-label="Close" on:click={() => dispatch('close')}>✕</button>
      </div>
    </div>
    <slot />
    <div class="modal-action">
      <slot name="footer" />
    </div>
  </div>
</dialog>
