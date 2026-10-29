<script>
  // Novel-parameters field with the wand "Enhance" button: the button only
  // shows while this exact field has focus (or its rewrite is in flight) and
  // acts on this field's value only.
  import EnhanceButton from './EnhanceButton.svelte';

  export let value = '';
  export let field = '';
  export let busy = false;
  export let disabled = false;
  export let placeholder = '';
  export let rows = undefined;
  export let type = 'textarea';
  export let controlClass = 'textarea textarea-sm w-full text-sm';
  export let title = '';
  export let label = 'Enhance';
  export let onEnhance = () => {};

  let focused = false;
  let control;

  function onFocusOut(e) {
    if (!e.currentTarget.contains(e.relatedTarget)) focused = false;
  }
  function enhance() {
    // Async handlers may return the rewritten text (Lore modals keep it in the
    // unsaved form); Novel parameters return nothing because the chat fast
    // path already persists and refreshes it.
    Promise.resolve(onEnhance(field, value)).then((res) => {
      if (typeof res === 'string' && res.trim() !== '') value = res;
    });
    control?.focus();
  }
</script>

<div class="relative" on:focusin={() => (focused = true)} on:focusout={onFocusOut}>
  {#if type === 'input'}
    <input type="text" class="{controlClass} pr-10" bind:this={control} bind:value {placeholder} {disabled} {title} />
  {:else}
    <textarea class="{controlClass} pr-10" bind:this={control} bind:value {placeholder} {disabled} {title} {rows}></textarea>
  {/if}
  {#if focused || busy}
    <EnhanceButton {busy} {label} inline={type === 'input'} on:click={enhance} />
  {/if}
</div>
