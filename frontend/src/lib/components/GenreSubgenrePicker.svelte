<script>
  // GenreSubgenrePicker — round 19, written from scratch as the ONLY place in
  // the UI that decides which subgenres appear for the selected parent genre(s).
  //
  // Design contract (see lib/subgenreTaxonomy.js):
  //  - The backend (/api/novel-params) ships ALL parameters unfiltered.
  //  - This component owns the filtering: it builds a single merged index
  //    (static frontend taxonomy + backend catalogs/parents/presets) where
  //    every subgenre has EXACTLY ONE canonical parent, and renders only the
  //    entries whose parent is among the genres the user selected.
  //  - Entries whose parent cannot be resolved are NEVER dumped into a
  //    genre-filtered dropdown (that fallback was the root cause of "todos los
  //    subgéneros aparecen en todas las categorías").
  //
  // It wraps ComboSelect so look & feel stay identical to the rest of the form.
  import ComboSelect from './ComboSelect.svelte';
  import { createEventDispatcher } from 'svelte';
  import { GENRES } from '../novelwriterGenres.js';
  import { buildSubgenreIndex, subgenresForKeys, resolveParent, norm } from '../subgenreTaxonomy.js';

  const dispatch = createEventDispatcher();

  // Payload of /api/novel-params (the FULL unfiltered parameter blob). Reactive:
  // whenever the parent reassigns it, the index below rebuilds automatically.
  export let paramsPayload = null;
  // Currently selected parent-genre keys (frontend GENRES keys; '__other__' or
  // unknown keys simply contribute nothing to the filter).
  export let genreKeys = [];
  // Bound subgenre value (display label or free-typed custom text).
  export let value = '';
  export let disabled = false;
  export let placeholder = '';
  export let emptyLabel = 'Auto';
  export let clearLabel = 'Clear';
  export let otherLabel = 'Other';
  export let title = '';
  export let fieldTip = '';
  // token -> localized hint, for the rich secondary line of each option.
  export let hints = {};

  // Merged single-parent index. Recomputed when the payload object changes.
  $: index = buildSubgenreIndex(paramsPayload);

  // Filtered options for the current selection.
  $: filteredLabels = subgenresForKeys(index, genreKeys);

  // ComboSelect options: catalog entries + the stored free-text value (so an
  // "Other" subgenre keeps displaying its own label instead of vanishing).
  $: comboOpts = (() => {
    const opts = filteredLabels.map((s) => ({
      value: s,
      label: s,
      desc: hints[norm(s)] || hints[s.toLowerCase()] || '',
    }));
    const cur = String(value || '').trim();
    if (cur && !opts.some((o) => o.value.toLowerCase() === cur.toLowerCase())) {
      opts.push({ value: cur, label: cur, desc: hints[norm(cur)] || '' });
    }
    return opts;
  })();

  // Round 28 fix: the "Other" search editor must always show the FULL subgenre
  // catalog, unfiltered by the selected genre(s). subgenresForKeys with an
  // empty key list returns that full union (browse-everything mode).
  $: allOtherOpts = (() => {
    const opts = subgenresForKeys(index, []).map((s) => ({
      value: s,
      label: s,
      desc: hints[norm(s)] || hints[s.toLowerCase()] || '',
    }));
    const cur = String(value || '').trim();
    if (cur && !opts.some((o) => o.value.toLowerCase() === cur.toLowerCase())) {
      opts.push({ value: cur, label: cur, desc: hints[norm(cur)] || '' });
    }
    return opts;
  })();

  function onChange(e) {
    const v = String(e.detail.value || '').trim();
    value = v;
    dispatch('change', { value: v, parent: resolveParent(index, v) });
  }
</script>

<ComboSelect
  options={comboOpts}
  otherSearchOptions={allOtherOpts}
  allowOther={true}
  otherLabel={otherLabel}
  placeholder={placeholder}
  emptyLabel={emptyLabel}
  clearLabel={clearLabel}
  disabled={disabled}
  title={title}
  fieldTip={fieldTip}
  bind:value
  on:change={onChange}
/>
